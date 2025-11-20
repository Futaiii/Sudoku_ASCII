package config

import (
	"encoding/json"
	"os"
)

type Config struct {
	Mode             string `json:"mode"` // "client" or "server"
	LocalPort        int    `json:"local_port"`
	ServerAddress    string `json:"server_address"`
	FallbackAddr     string `json:"fallback_address"`
	Key              string `json:"key"`
	AEAD             string `json:"aead"`              // "aes-128-gcm", "chacha20-poly1305", "none"
	SuspiciousAction string `json:"suspicious_action"` // "fallback" or "silent"
	PaddingMin       int    `json:"padding_min"`
	PaddingMax       int    `json:"padding_max"`
}

func Load(path string) (*Config, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var cfg Config
	if err := json.NewDecoder(f).Decode(&cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}
