# FreePBX Go SIP Push Proxy

A standalone SIP edge proxy / mid-registrar written in Go that bridges iOS and Android mobile softphones with **FreePBX**, using **RFC 8599** push parameters, **Apple Push Notification service (APNs VoIP PushKit)**, and **Firebase Cloud Messaging (FCM)** to wake suspended devices on incoming calls.

## Why This Exists

Mobile operating systems suspend background apps and drop persistent SIP sockets to save battery. Without a push gateway, incoming `INVITE` requests from FreePBX hit stale sockets or time out before the softphone wakes up.

This proxy sits between mobile clients and FreePBX:

1. **Registration (`REGISTER`):** Captures RFC 8599 parameters (`pn-provider`, `pn-prid`, `pn-param`) and the mobile client's latest NAT `Contact`, rewrites `Contact` toward the proxy, and forwards registration to FreePBX.
2. **Incoming Call (`INVITE`):** Immediately sends `100 Trying` to FreePBX to prevent retransmissions or timeouts, triggers a high-priority APNs/FCM push, and holds the `INVITE` until the device wakes and re-registers (up to `wake_timeout_seconds`).
3. **Call Relay & Teardown:** Rewrites the `INVITE` Request-URI to the fresh mobile `Contact`, inserts `Record-Route` for in-dialog signaling (`ACK`, `BYE`, `CANCEL`, `OPTIONS`), and lets RTP media flow directly between the endpoint and FreePBX.

## Project Layout

```text
.
├── main.go                          # Stateful SIP proxy, REGISTER/INVITE/in-dialog handlers
├── config.go                        # TOML configuration loader and startup validation
├── store.go                         # In-memory device registry and INVITE wake-up synchronization
├── push.go                          # APNs PushKit and FCM v1 push notification gateway
├── config.toml                      # Application configuration template
├── freepbx-push-proxy.service       # systemd unit file
└── /etc/default/freepbx-push-proxy  # System environment defaults

### 1. Clone and Build the Binary

```bash
git clone https://github.com/rezizter/gosip-push-gateway.git
cd gosip-push-gateway
go mod tidy
go build -o gosip-push-gateway .
```
