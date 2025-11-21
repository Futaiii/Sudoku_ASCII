// internal/config/config.go
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
	GeoIPURL         string `json:"geoip_url"`  // 留空则使用默认，支持 "global", "direct" 关键字
	ProxyMode        string `json:"proxy_mode"` // 运行时状态，非JSON字段，由Load解析逻辑填充
	ASCII            string `json:"ascii"`      // "prefer_entropy" (默认): 旧模式, 低熵, 二进制混淆"，prefer_ascii": 新模式, 纯ASCII字符，高熵
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

	if cfg.GeoIPURL == "global" || cfg.GeoIPURL == "direct" {
		cfg.ProxyMode = cfg.GeoIPURL
	} else {
		cfg.ProxyMode = "pac"
		if cfg.GeoIPURL == "" {
			// 默认使用 gh-proxy 代理 raw github 链接
			cfg.GeoIPURL = "https://gh-proxy.org/https://raw.githubusercontent.com/fernvenue/chn-cidr-list/master/ipv4.txt"
		}
	}

	return &cfg, nil
}
