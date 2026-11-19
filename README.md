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

### 2. Configuration (`config.toml`)

Here is the default application configuration template (`config.toml`):

```toml
[sip]
listen_ip = "0.0.0.0"
listen_port = 5060
public_ip = "203.0.113.10"
# Optional: set if proxy public IP differs from local interface IP
# advertise_ip = "203.0.113.10"
wake_timeout_seconds = 12

[freepbx]
ip = "10.0.0.50"
port = 5060
transport = "udp"

[push.apns]
enabled = true
key_path = "/etc/push-proxy/AuthKey_XXXXXXXXXX.p8"
key_id = "XXXXXXXXXX"
team_id = "YYYYYYYYYY"
voip_topic = "com.example.softphone.voip"
# Set to false for Xcode debug builds (APNs Sandbox)
production = true

[push.fcm]
enabled = true
credentials_path = "/etc/push-proxy/firebase-service-account.json"
```

### 3. Running the Binary Directly with Configuration

Copy and customize `config.toml`:

```bash
cp config.toml config.local.toml
# Edit config.local.toml with your SIP, FreePBX, APNs, and FCM settings
```

Run the binary pointing to your configuration file:

```bash
./gosip-push-gateway -config config.local.toml
```

### 4. Running as a Systemd Service

To run `gosip-push-gateway` automatically on system startup and manage it as a background service:

1. Copy the binary to `/usr/local/bin/`:
   ```bash
   sudo cp gosip-push-gateway /usr/local/bin/gosip-push-gateway
   sudo chmod +x /usr/local/bin/gosip-push-gateway
   ```

2. Create the configuration directory and copy your config:
   ```bash
   sudo mkdir -p /etc/gosip-push-gateway
   sudo cp config.toml /etc/gosip-push-gateway/config.toml
   # Edit /etc/gosip-push-gateway/config.toml as needed
   ```

3. Install the systemd service file:
   ```bash
   sudo cp freepbx-push-proxy.service /etc/systemd/system/gosip-push-gateway.service
   ```
   *(Alternatively, use the provided Ansible role which automates this entire setup).*

4. Reload systemd, enable, and start the service:
   ```bash
   sudo systemctl daemon-reload
   sudo systemctl enable --now gosip-push-gateway
   ```

5. Check service status and logs:
   ```bash
   sudo systemctl status gosip-push-gateway
   sudo journalctl -u gosip-push-gateway -f
   ```

## Deployment via Ansible

An automated Ansible role is included under `ansible/roles/gosip_push_gateway` to provision `gosip-push-gateway` across **Amazon Linux 2023**, **Rocky Linux 9**, **Ubuntu 22.04 LTS**, **Ubuntu 24.04 LTS**, and **Debian 12**.

### Role Variables

| Variable | Default | Description |
| :--- | :--- | :--- |
| `gosip_push_gateway_user` | `gosip` | System user created to run the service |
| `gosip_push_gateway_group` | `gosip` | System group for the service |
| `gosip_push_gateway_install_dir` | `/opt/gosip-push-gateway` | Application installation directory |
| `gosip_push_gateway_bin_dir` | `/usr/local/bin` | Directory where binary is installed |
| `gosip_push_gateway_conf_dir` | `/etc/gosip-push-gateway` | Directory for configuration files |
| `gosip_push_gateway_log_dir` | `/var/log/gosip-push-gateway` | Directory for logs |
| `gosip_push_gateway_install_method` | `local` | Installation method (`local` sync or `source` clone) |
| `gosip_push_gateway_go_version` | `1.22.5` | Go version installed if building from source |
| `gosip_push_gateway_sip_listen_ip` | `0.0.0.0` | SIP proxy bind IP address |
| `gosip_push_gateway_sip_listen_port` | `5060` | SIP proxy bind port |
| `gosip_push_gateway_sip_public_ip` | `203.0.113.10` | Public IP advertised in SIP headers/Contact |
| `gosip_push_gateway_sip_advertise_ip` | `""` | Optional explicit advertise IP override |
| `gosip_push_gateway_sip_wake_timeout_seconds` | `12` | Seconds to hold incoming INVITE while waking device |
| `gosip_push_gateway_freepbx_ip` | `10.0.0.50` | FreePBX / upstream SIP server IP |
| `gosip_push_gateway_freepbx_port` | `5060` | FreePBX SIP port |
| `gosip_push_gateway_freepbx_transport` | `udp` | Upstream SIP transport (`udp`, `tcp`) |
| `gosip_push_gateway_apns_enabled` | `false` | Enable Apple Push Notification service (APNs VoIP) |
| `gosip_push_gateway_apns_key_path` | `/etc/gosip-push-gateway/AuthKey.p8` | Path to APNs .p8 private key file |
| `gosip_push_gateway_apns_key_id` | `""` | APNs Key ID |
| `gosip_push_gateway_apns_team_id` | `""` | Apple Developer Team ID |
| `gosip_push_gateway_apns_voip_topic` | `com.example.softphone.voip` | APNs VoIP push topic bundle ID |
| `gosip_push_gateway_apns_production` | `true` | Use APNs production gateway (false for sandbox) |
| `gosip_push_gateway_fcm_enabled` | `false` | Enable Firebase Cloud Messaging (FCM v1) |
| `gosip_push_gateway_fcm_credentials_path` | `/etc/gosip-push-gateway/firebase-service-account.json` | Path to FCM service account JSON file |

### Example Playbook

Create a playbook `site.yml`:

```yaml
- hosts: sip-gateways
  become: yes
  roles:
    - role: gosip-push-gateway/ansible/roles/gosip_push_gateway
      vars:
        gosip_push_gateway_sip_public_ip: "203.0.113.10"
        gosip_push_gateway_freepbx_ip: "10.0.0.50"
        gosip_push_gateway_apns_enabled: true
        gosip_push_gateway_apns_key_id: "YOUR_KEY_ID"
        gosip_push_gateway_apns_team_id: "YOUR_TEAM_ID"
        gosip_push_gateway_apns_voip_topic: "com.example.softphone.voip"
        gosip_push_gateway_fcm_enabled: true
```

Run the playbook:

```bash
ansible-playbook -i inventory.ini site.yml
```

### Molecule Testing

Molecule application tests validate the role across all supported Linux distributions (Amazon Linux 2023, Rocky 9, Ubuntu 22.04, Ubuntu 24.04, Debian 12) using Docker and systemd.

```bash
pip install molecule molecule-plugins[docker] docker
cd ansible/roles/gosip_push_gateway
molecule test
```
