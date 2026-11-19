package main

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	firebase "firebase.google.com/go/v4"
	"firebase.google.com/go/v4/messaging"
	"github.com/sideshow/apns2"
	"github.com/sideshow/apns2/token"
	"google.golang.org/api/option"
)

type PushGateway struct {
	apnsClient *apns2.Client
	apnsTopic  string // e.g., "com.yourcompany.softphone.voip"
	fcmClient  *messaging.Client
}

func NewPushGateway(apnsKeyPath, keyID, teamID, apnsTopic, fcmCredsPath string) (*PushGateway, error) {
	pg := &PushGateway{apnsTopic: apnsTopic}

	// Initialize Apple APNs (Token-based .p8 auth)
	if apnsKeyPath != "" {
		authKey, err := token.AuthKeyFromFile(apnsKeyPath)
		if err != nil {
			return nil, fmt.Errorf("apns auth key error: %w", err)
		}
		t := &token.Token{
			AuthKey: authKey,
			KeyID:   keyID,
			TeamID:  teamID,
		}
		// Switch to .Development() when testing debug builds in Xcode
		pg.apnsClient = apns2.NewTokenClient(t).Production()
	}

	// Initialize Google Firebase Cloud Messaging (FCM v1)
	if fcmCredsPath != "" {
		app, err := firebase.NewApp(context.Background(), nil, option.WithCredentialsFile(fcmCredsPath))
		if err != nil {
			return nil, fmt.Errorf("firebase init error: %w", err)
		}
		fcm, err := app.Messaging(context.Background())
		if err != nil {
			return nil, fmt.Errorf("fcm client error: %w", err)
		}
		pg.fcmClient = fcm
	}

	return pg, nil
}

func (p *PushGateway) WakeDevice(ctx context.Context, dev *DeviceRegistration, callerID, callID string) error {
	provider := strings.ToLower(dev.PNProvider)

	switch {
	case strings.Contains(provider, "apns") && p.apnsClient != nil:
		notification := &apns2.Notification{
			DeviceToken: dev.PNToken,
			Topic:       p.apnsTopic,
			PushType:    apns2.PushTypeVOIP,
			Priority:    apns2.PriorityHigh,
			Payload: []byte(fmt.Sprintf(
				`{"aps":{"content-available":1},"caller_id":"%s","call_id":"%s","extension":"%s"}`,
				callerID, callID, dev.Extension,
			)),
		}
		res, err := p.apnsClient.PushWithContext(ctx, notification)
		if err != nil {
			return err
		}
		if !res.Sent() {
			return fmt.Errorf("apns rejected push: %d %s", res.StatusCode, res.Reason)
		}
		slog.Info("APNs VoIP push delivered", "ext", dev.Extension, "apns_id", res.ApnsID)
		return nil

	case strings.Contains(provider, "fcm") && p.fcmClient != nil:
		msg := &messaging.Message{
			Token: dev.PNToken,
			Data: map[string]string{
				"type":      "incoming_call",
				"caller_id": callerID,
				"call_id":   callID,
				"extension": dev.Extension,
			},
			Android: &messaging.AndroidConfig{
				Priority: "high", // Bypasses Android Doze mode
				TTL:      new(0), // Deliver immediately or drop
			},
		}
		msgID, err := p.fcmClient.Send(ctx, msg)
		if err != nil {
			return err
		}
		slog.Info("FCM high-priority push delivered", "ext", dev.Extension, "msg_id", msgID)
		return nil

	default:
		return fmt.Errorf("unsupported or unconfigured push provider: %s", dev.PNProvider)
	}
}
