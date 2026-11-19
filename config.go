package main

import (
	"fmt"
	"os"
	"time"

	"github.com/BurntSushi/toml"
)

type Config struct {
	SIP     SIPConfig     `toml:"sip"`
	FreePBX FreePBXConfig `toml:"freepbx"`
	Push    PushConfig    `toml:"push"`
}

type SIPConfig struct {
	ListenIP           string `toml:"listen_ip"`
	ListenPort         int    `toml:"listen_port"`
	PublicIP           string `toml:"public_ip"`
	AdvertiseIP        string `toml:"advertise_ip"`
	WakeTimeoutSeconds int    `toml:"wake_timeout_seconds"`
}

func (s SIPConfig) WakeTimeout() time.Duration {
	if s.WakeTimeoutSeconds <= 0 {
		return 12 * time.Second
	}
	return time.Duration(s.WakeTimeoutSeconds) * time.Second
}

func (s SIPConfig) EffectivePublicIP() string {
	if s.AdvertiseIP != "" {
		return s.AdvertiseIP
	}
	return s.PublicIP
}

type FreePBXConfig struct {
	IP        string `toml:"ip"`
	Port      int    `toml:"port"`
	Transport string `toml:"transport"`
}

type PushConfig struct {
	APNs APNsConfig `toml:"apns"`
	FCM  FCMConfig  `toml:"fcm"`
}

type APNsConfig struct {
	Enabled    bool   `toml:"enabled"`
	KeyPath    string `toml:"key_path"`
	KeyID      string `toml:"key_id"`
	TeamID     string `toml:"team_id"`
	VoIPTopic  string `toml:"voip_topic"`
	Production bool   `toml:"production"`
}

type FCMConfig struct {
	Enabled         bool   `toml:"enabled"`
	CredentialsPath string `toml:"credentials_path"`
}

func LoadConfig(path string) (*Config, error) {
	cfg := &Config{
		SIP: SIPConfig{
			ListenIP:           "0.0.0.0",
			ListenPort:         5060,
			WakeTimeoutSeconds: 12,
		},
		FreePBX: FreePBXConfig{
			Port:      5060,
			Transport: "udp",
		},
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config file %q: %w", path, err)
	}

	if err := toml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parse config file %q: %w", path, err)
	}

	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	return cfg, nil
}

func (c *Config) Validate() error {
	if c.SIP.EffectivePublicIP() == "" {
		return fmt.Errorf("config error: sip.public_ip is required")
	}
	if c.FreePBX.IP == "" {
		return fmt.Errorf("config error: freepbx.ip is required")
	}
	if c.Push.APNs.Enabled {
		if c.Push.APNs.KeyPath == "" || c.Push.APNs.KeyID == "" || c.Push.APNs.TeamID == "" || c.Push.APNs.VoIPTopic == "" {
			return fmt.Errorf("config error: push.apns requires key_path, key_id, team_id, and voip_topic when enabled")
		}
	}
	if c.Push.FCM.Enabled && c.Push.FCM.CredentialsPath == "" {
		return fmt.Errorf("config error: push.fcm requires credentials_path when enabled")
	}
	return nil
}
