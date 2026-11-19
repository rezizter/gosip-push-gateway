package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/emiago/sipgo"
	"github.com/emiago/sipgo/sip"
)

type PushSIPProxy struct {
	cfg    *Config
	ua     *sipgo.UserAgent
	srv    *sipgo.Server
	client *sipgo.Client
	store  *Store
	push   *PushGateway
}

func main() {
	var configPath string
	flag.StringVar(&configPath, "config", "config.toml", "Path to TOML configuration file")
	flag.Parse()

	cfg, err := LoadConfig(configPath)
	if err != nil {
		slog.Error("Failed to load configuration", "path", configPath, "err", err)
		os.Exit(1)
	}

	store := NewStore()

	var apnsKeyPath, keyID, teamID, voipTopic, fcmCredsPath string
	if cfg.Push.APNs.Enabled {
		apnsKeyPath = cfg.Push.APNs.KeyPath
		keyID = cfg.Push.APNs.KeyID
		teamID = cfg.Push.APNs.TeamID
		voipTopic = cfg.Push.APNs.VoIPTopic
	}
	if cfg.Push.FCM.Enabled {
		fcmCredsPath = cfg.Push.FCM.CredentialsPath
	}

	pushGw, err := NewPushGateway(
		apnsKeyPath,
		keyID,
		teamID,
		voipTopic,
		fcmCredsPath,
	)
	if err != nil {
		slog.Error("Failed to initialize push gateway", "err", err)
		os.Exit(1)
	}

	ua, err := sipgo.NewUA(sipgo.WithUserAgent("FreePBX-Go-PushProxy/1.0"))
	if err != nil {
		slog.Error("Failed to create SIP UserAgent", "err", err)
		os.Exit(1)
	}
	defer ua.Close()

	srv, err := sipgo.NewServer(ua)
	if err != nil {
		slog.Error("Failed to create SIP Server", "err", err)
		os.Exit(1)
	}

	publicIP := cfg.SIP.EffectivePublicIP()

	client, err := sipgo.NewClient(ua,
		sipgo.WithClientHostname(publicIP),
		sipgo.WithClientPort(cfg.SIP.ListenPort),
	)
	if err != nil {
		slog.Error("Failed to create SIP Client", "err", err)
		os.Exit(1)
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

	listenAddr := fmt.Sprintf("%s:%d", cfg.SIP.ListenIP, cfg.SIP.ListenPort)
	pbxAddr := fmt.Sprintf("%s:%d", cfg.FreePBX.IP, cfg.FreePBX.Port)

	slog.Info("Starting FreePBX Push SIP Proxy",
		"config", configPath,
		"listen", listenAddr,
		"public_ip", publicIP,
		"freepbx", pbxAddr,
	)

	go func() {
		if err := srv.ListenAndServe(ctx, "udp", listenAddr); err != nil {
			slog.Error("UDP
