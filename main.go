package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/emiago/sipgo"
	"github.com/emiago/sipgo/sip"
)

type Config struct {
	ListenIP   string
	ListenPort int
	PublicIP   string
	FreePBXIP  string
	FreePBXPort int
}

type PushSIPProxy struct {
	cfg    Config
	ua     *sipgo.UserAgent
	srv    *sipgo.Server
	client *sipgo.Client
	store  *Store
	push   *PushGateway
}

func main() {
	cfg := Config{}
	flag.StringVar(&cfg.ListenIP, "listen-ip", "0.0.0.0", "Local IP to bind SIP proxy")
	flag.IntVar(&cfg.ListenPort, "listen-port", 5060, "Local port to bind SIP proxy")
	flag.StringVar(&cfg.PublicIP, "public-ip", "203.0.113.10", "Public IP of this proxy (for Record-Route/Contact)")
	flag.StringVar(&cfg.FreePBXIP, "pbx-ip", "10.0.0.50", "FreePBX IP address")
	flag.IntVar(&cfg.FreePBXPort, "pbx-port", 5060, "FreePBX PJSIP port")
	flag.Parse()

	store := NewStore()

	// Configure APNs / FCM credentials via environment variables
	pushGw, err := NewPushGateway(
		os.Getenv("APNS_KEY_PATH"),
		os.Getenv("APNS_KEY_ID"),
		os.Getenv("APNS_TEAM_ID"),
		os.Getenv("APNS_VOIP_TOPIC"),
		os.Getenv("FCM_CREDS_PATH"),
	)
	if err != nil {
		slog.Error("Failed to initialize push gateway", "err", err)
		os.Exit(1)
	}

	ua, err := sipgo.NewUA(sipgo.WithUserAgent("FreePBX-Go-PushProxy/1.0"))
	if err != nil {
		panic(err)
	}
	defer ua.Close()

	srv, err := sipgo.NewServer(ua)
	if err != nil {
		panic(err)
	}

	client, err := sipgo.NewClient(ua,
		sipgo.WithClientHostname(cfg.PublicIP),
		sipgo.WithClientPort(cfg.ListenPort),
	)
	if err != nil {
		panic(err)
	}

	proxy := &PushSIPProxy{
		cfg:    cfg,
		ua:     ua,
		srv:    srv,
		client: client,
		store:  store,
		push:   pushGw,
	}

	proxy.registerHandlers()

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	listenAddr := fmt.Sprintf("%s:%d", cfg.ListenIP, cfg.ListenPort)
	slog.Info("Starting FreePBX Push SIP Proxy", "addr", listenAddr, "pbx", fmt.Sprintf("%s:%d", cfg.FreePBXIP, cfg.FreePBXPort))

	go func() {
		if err := srv.ListenAndServe(ctx, "udp", listenAddr); err != nil {
			slog.Error("UDP listener stopped", "err", err)
		}
	}()
	go func() {
		if err := srv.ListenAndServe(ctx, "tcp", listenAddr); err != nil {
			slog.Error("TCP listener stopped", "err", err)
		}
	}()

	<-ctx.Done()
	slog.Info("Shutting down SIP proxy...")
}

func (p *PushSIPProxy) registerHandlers() {
	p.srv.OnRegister(p.handleRegister)
	p.srv.OnInvite(p.handleInvite)
	p.srv.OnAck(p.forwardInDialog)
	p.srv.OnBye(p.forwardInDialog)
	p.srv.OnCancel(p.forwardInDialog)
	p.srv.OnOptions(p.forwardInDialog)
}

// handleRegister extracts RFC 8599 push parameters, stores mobile contact, and proxies REGISTER to FreePBX.
func (p *PushSIPProxy) handleRegister(req *sip.Request, tx sip.ServerTransaction) {
	from := req.From()
	contact := req.Contact()
	if from == nil || contact == nil {
		_ = tx.Respond(sip.NewResponseFromRequest(req, 400, "Bad Request", nil))
		return
	}

	ext := from.Address.User
	params := contact.Address.UriParams

	// Extract RFC 8599 parameters (pn-provider, pn-prid, pn-param)
	pnProvider, hasPN := params.Get("pn-provider")
	pnPrid, hasPrid := params.Get("pn-prid")
	pnParam, _ := params.Get("pn-param")

	// Preserve actual NAT source IP:Port of the mobile phone
	mobileURI := contact.Address.Clone()
	if rport := req.Via().Params; rport != nil {
		mobileURI.Host = req.Source()
	}

	if hasPN && hasPrid {
		slog.Info("Registered push-enabled endpoint", "ext", ext, "provider", pnProvider, "contact", mobileURI.String())
		p.store.UpsertDevice(&DeviceRegistration{
			Extension:  ext,
			ContactURI: mobileURI.String(),
			PNProvider: pnProvider,
			PNToken:    pnPrid,
			PNParam:    pnParam,
		})
	} else {
		// Regular registration or re-registration without PN params; still update Contact if device exists
		if existing, ok := p.store.GetDevice(ext); ok {
			existing.ContactURI = mobileURI.String()
			p.store.UpsertDevice(existing)
		}
	}

	// Rewrite Contact so FreePBX sends future INVITEs to this Go Proxy instead of the sleeping mobile IP
	proxyContact := fmt.Sprintf("<sip:%s@%s:%d;transport=udp>", ext, p.cfg.PublicIP, p.cfg.ListenPort)
	req.RemoveHeader("Contact")
	req.AppendHeader(sip.NewHeader("Contact", proxyContact))

	// Forward REGISTER (including Authorization / Digest challenge headers) to FreePBX
	pbxDest := fmt.Sprintf("%s:%d", p.cfg.FreePBXIP, p.cfg.FreePBXPort)
	req.SetDestination(pbxDest)

	clientTx, err := p.client.TransactionRequest(context.Background(), req, sipgo.ClientRequestAddVia)
	if err != nil {
		_ = tx.Respond(sip.NewResponseFromRequest(req, 502, "Bad Gateway", nil))
		return
	}
	defer clientTx.Terminate()

	for {
		select {
		case res := <-clientTx.Responses():
			// Strip top Via added by our client before returning 401/200 to mobile app
			res.RemoveHeader("Via")
			_ = tx.Respond(res)
			if res.StatusCode >= 200 {
				return
			}
		case <-clientTx.Done():
			return
		}
	}
}

// handleInvite intercepts calls from FreePBX, wakes the mobile app via APNs/FCM, and relays the INVITE.
func (p *PushSIPProxy) handleInvite(req *sip.Request, tx sip.ServerTransaction) {
	targetExt := req.Recipient.User

	// Check if this INVITE is coming from FreePBX toward a push-registered mobile extension
	dev, isPushClient := p.store.GetDevice(targetExt)
	if isPushClient && req.Source() == fmt.Sprintf("%s:%d", p.cfg.FreePBXIP, p.cfg.FreePBXPort) {
		slog.Info("Incoming call from FreePBX for push endpoint", "ext", targetExt)

		// 1. Immediately send 100 Trying so FreePBX doesn't retransmit or fail the trunk
		_ = tx.Respond(sip.NewResponseFromRequest(req, 100, "Trying", nil))

		// 2. Fire APNs / FCM Push Notification
		callerID := req.From().Address.User
		callID := req.CallID().Value()
		if err := p.push.WakeDevice(context.Background(), dev, callerID, callID); err != nil {
			slog.Error("Push delivery failed", "ext", targetExt, "err", err)
		}

		// 3. Hold INVITE for up to 12 seconds waiting for mobile app to wake up and REGISTER
		freshDev, wokeUp := p.store.WaitForRegistration(targetExt, 12*time.Second)
		if !wokeUp {
			slog.Warn("Mobile device failed to wake in time", "ext", targetExt)
			_ = tx.Respond(sip.NewResponseFromRequest(req, 480, "Temporarily Unavailable", nil))
			return
		}

		// 4. Rewrite Request-URI to the mobile device's fresh Contact IP:Port
		var newURI sip.Uri
		if err := sip.ParseUri(freshDev.ContactURI, &newURI); err == nil {
			req.Recipient = newURI
			req.SetDestination(fmt.Sprintf("%s:%d", newURI.Host, newURI.Port))
		}
	} else {
		// Outbound call from Mobile -> FreePBX
		pbxDest := fmt.Sprintf("%s:%d", p.cfg.FreePBXIP, p.cfg.FreePBXPort)
		req.SetDestination(pbxDest)
	}

	// Insert Record-Route so subsequent ACK, re-INVITE, and BYE traverse this proxy
	rr := fmt.Sprintf("<sip:%s:%d;lr>", p.cfg.PublicIP, p.cfg.ListenPort)
	req.PrependHeader(sip.NewHeader("Record-Route", rr))

	clientTx, err := p.client.TransactionRequest(context.Background(), req, sipgo.ClientRequestAddVia)
	if err != nil {
		_ = tx.Respond(sip.NewResponseFromRequest(req, 502, "Bad Gateway", nil))
		return
	}
	defer clientTx.Terminate()

	for {
		select {
		case res := <-clientTx.Responses():
			res.RemoveHeader("Via")
			_ = tx.Respond(res)
			if res.StatusCode >= 200 {
				return
			}
		case <-tx.Cancels():
			// Propagate CANCEL if caller hangs up while mobile is ringing
			cancelReq := sip.NewRequest(sip.CANCEL, req.Recipient)
			cancelReq.SetDestination(req.Destination())
			_, _ = p.client.Do(context.Background(), cancelReq)
			return
		case <-clientTx.Done():
			return
		}
	}
}

// forwardInDialog routes ACK, BYE, CANCEL, and OPTIONS along the Record-Route / Request-URI.
func (p *PushSIPProxy) forwardInDialog(req *sip.Request, tx sip.ServerTransaction) {
	// Strip our own Route header if present (loose routing)
	if route := req.Route(); route != nil && route.Address.Host == p.cfg.PublicIP {
		req.RemoveHeader("Route")
	}

	dest := fmt.Sprintf("%s:%d", req.Recipient.Host, req.Recipient.Port)
	if req.Recipient.Port == 0 {
		dest = fmt.Sprintf("%s:5060", req.Recipient.Host)
	}
	req.SetDestination(dest)

	if req.Method == sip.ACK {
		_ = p.client.WriteRequest(req, sipgo.ClientRequestAddVia)
		return
	}

	clientTx, err := p.client.TransactionRequest(context.Background(), req, sipgo.ClientRequestAddVia)
	if err != nil {
		_ = tx.Respond(sip.NewResponseFromRequest(req, 502, "Bad Gateway", nil))
		return
	}
	defer clientTx.Terminate()

	for {
		select {
		case res := <-clientTx.Responses():
			res.RemoveHeader("Via")
			_ = tx.Respond(res)
			if res.StatusCode >= 200 {
				return
			}
		case <-clientTx.Done():
			return
		}
	}
}
