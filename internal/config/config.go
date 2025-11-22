// internal/config/config.go
package config

import (
	"encoding/json"
	"os"
)

type Config struct {
	Mode             string   `json:"mode"`      // "client" or "server"
	Transport        string   `json:"transport"` // "tcp" or "udp"
	LocalPort        int      `json:"local_port"`
	ServerAddress    string   `json:"server_address"`
	FallbackAddr     string   `json:"fallback_address"`
	Key              string   `json:"key"`
	AEAD             string   `json:"aead"`              // "aes-128-gcm", "chacha20-poly1305", "none"
	SuspiciousAction string   `json:"suspicious_action"` // "fallback" or "silent"
	PaddingMin       int      `json:"padding_min"`
	PaddingMax       int      `json:"padding_max"`
	RuleURLs         []string `json:"rule_urls"`  // 留空则使用默认，支持 "global", "direct" 关键字
	ProxyMode        string   `json:"proxy_mode"` // 运行时状态，非JSON字段，由Load解析逻辑填充
	ASCII            string   `json:"ascii"`      // "prefer_entropy" (默认): 旧模式, 低熵, 二进制混淆"，prefer_ascii": 新模式, 纯ASCII字符，高熵
	LegacyGeoIPURL   string   `json:"geoip_url,omitempty"`
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

	if cfg.Transport == "" {
		cfg.Transport = "tcp"
	}

	if cfg.ASCII == "" {
		cfg.ASCII = "prefer_entropy"
	}

	// 处理 ProxyMode 和 默认规则
	// 如果用户显式设置了 rule_urls 为 ["global"] 或 ["direct"]，则覆盖模式
	if len(cfg.RuleURLs) > 0 && (cfg.RuleURLs[0] == "global" || cfg.RuleURLs[0] == "direct") {
		cfg.ProxyMode = cfg.RuleURLs[0]
		cfg.RuleURLs = nil
	} else {
		if cfg.ProxyMode == "" {
			cfg.ProxyMode = "pac" // 默认为规则模式
		}

		// 如果 RuleURLs 为空，尝试使用旧字段或默认值
		if len(cfg.RuleURLs) == 0 {
			if cfg.LegacyGeoIPURL != "" && cfg.LegacyGeoIPURL != "global" && cfg.LegacyGeoIPURL != "direct" {
				cfg.RuleURLs = []string{cfg.LegacyGeoIPURL}
			} else {
				// 默认规则列表
				cfg.RuleURLs = []string{
					"https://gh-proxy.org/https://raw.githubusercontent.com/fernvenue/chn-cidr-list/master/ipv4.txt",
					"https://cdn.jsdelivr.net/gh/blackmatrix7/ios_rule_script@master/rule/Clash/China/China.list",
				}
			}
		}
	}

	return &cfg, nil
}
