# Ansible Role: `gosip_push_gateway`

An Ansible role that deploys and configures **Go SIP Push Gateway** (`gosip-push-gateway`) as a managed systemd service on Linux distributions.

## Requirements

- Ansible >= 2.12
- Target OS:
  - Amazon Linux 2023
  - Rocky Linux 9
  - Ubuntu 22.04 LTS (Jammy)
  - Ubuntu 24.04 LTS (Noble)
  - Debian 12 (Bookworm)

## Role Variables

Available variables are listed below, along with default values (see `defaults/main.yml`):

```yaml
gosip_push_gateway_user: "gosip"
gosip_push_gateway_group: "gosip"

gosip_push_gateway_install_dir: "/opt/gosip-push-gateway"
gosip_push_gateway_bin_dir: "/usr/local/bin"
gosip_push_gateway_conf_dir: "/etc/gosip-push-gateway"
gosip_push_gateway_log_dir: "/var/log/gosip-push-gateway"

# SIP Configuration
gosip_push_gateway_sip_listen_ip: "0.0.0.0"
gosip_push_gateway_sip_listen_port: 5060
gosip_push_gateway_sip_public_ip: "203.0.113.10"
gosip_push_gateway_sip_advertise_ip: ""
gosip_push_gateway_sip_wake_timeout_seconds: 12

# FreePBX Configuration
gosip_push_gateway_freepbx_ip: "10.0.0.50"
gosip_push_gateway_freepbx_port: 5060
gosip_push_gateway_freepbx_transport: "udp"

# Push Notifications Configuration (APNs / FCM)
gosip_push_gateway_apns_enabled: false
gosip_push_gateway_apns_key_path: "/etc/gosip-push-gateway/AuthKey.p8"
gosip_push_gateway_apns_key_id: ""
gosip_push_gateway_apns_team_id: ""
gosip_push_gateway_apns_voip_topic: "com.example.softphone.voip"
gosip_push_gateway_apns_production: true

gosip_push_gateway_fcm_enabled: false
gosip_push_gateway_fcm_credentials_path: "/etc/gosip-push-gateway/firebase-service-account.json"
```

## Example Playbook

```yaml
- hosts: sip-gateways
  become: yes
  roles:
    - role: gosip_push_gateway
      vars:
        gosip_push_gateway_sip_public_ip: "198.51.100.25"
        gosip_push_gateway_freepbx_ip: "192.168.1.100"
        gosip_push_gateway_apns_enabled: true
        gosip_push_gateway_apns_key_id: "ABC123XYZ"
        gosip_push_gateway_apns_team_id: "DEF456UVW"
        gosip_push_gateway_apns_voip_topic: "com.mycompany.softphone.voip"
        gosip_push_gateway_fcm_enabled: true
```

## Testing with Molecule

To run Molecule integration and application tests locally across supported platforms:

```bash
pip install molecule molecule-plugins[docker] docker
cd ansible/roles/gosip_push_gateway
molecule test
```

## License

MIT
