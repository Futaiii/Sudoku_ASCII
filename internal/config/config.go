// internal/config/config.go
package config

import (
	"encoding/json"
	"os"
)

type Config struct {
	Mode             string       `json:"mode"`      // "client" or "server"
	Transport        string       `json:"transport"` // "tcp" or "udp"
	LocalPort        int          `json:"local_port"`
	ServerAddress    string       `json:"server_address"`
	FallbackAddr     string       `json:"fallback_address"`
	Key              string       `json:"key"`
	AEAD             string       `json:"aead"`              // "aes-128-gcm", "chacha20-poly1305", "none"
	SuspiciousAction string       `json:"suspicious_action"` // "fallback" or "silent"
	PaddingMin       int          `json:"padding_min"`
	PaddingMax       int          `json:"padding_max"`
	RuleURLs         []string     `json:"rule_urls"`    // 留空则使用默认，支持 "global", "direct" 关键字
	ProxyMode        string       `json:"proxy_mode"`   // 运行时状态，非JSON字段，由Load解析逻辑填充
	ASCII            string       `json:"ascii"`        // "prefer_entropy" (默认): 旧模式, 低熵, 二进制混淆"，prefer_ascii": 新模式, 纯ASCII字符，高熵
	EnableMieru      bool         `json:"enable_mieru"` // 开启上下行分离
	MieruConfig      *MieruConfig `json:"mieru_config"` // Mieru 特定配置
}

type MieruConfig struct {
	Port          int    `json:"port"`      // 服务端 Mieru 监听端口 (区别于 Sudoku 端口)
	Transport     string `json:"transport"` // "TCP" or "UDP" (Mieru 底层)
	MTU           int    `json:"mtu"`
	Multiplexing  string `json:"multiplexing"` // "LOW", "MIDDLE", "HIGH"
	Username      string `json:"username"`     // 默认使用 "default"
	Password      string `json:"password"`     // 留空则复用 Sudoku Key
	ApplySettings bool   `json:"-"`            // 内部标记
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

	if cfg.EnableMieru {
		if cfg.MieruConfig == nil {
			cfg.MieruConfig = &MieruConfig{}
		}
		if cfg.MieruConfig.Port == 0 {
			cfg.MieruConfig.Port = cfg.LocalPort + 1000 // 默认偏移
		}
		if cfg.MieruConfig.Transport == "" {
			cfg.MieruConfig.Transport = "TCP"
		}
		if cfg.MieruConfig.Username == "" {
			cfg.MieruConfig.Username = "sudoku_user"
		}
		if cfg.MieruConfig.Password == "" {
			cfg.MieruConfig.Password = cfg.Key // 复用密码
		}
		if cfg.MieruConfig.MTU == 0 {
			cfg.MieruConfig.MTU = 1400
		}
		if cfg.MieruConfig.Multiplexing == "" {
			cfg.MieruConfig.Multiplexing = "MULTIPLEXING_HIGH"
		}
	}

	// 处理 ProxyMode 和 默认规则
	// 如果用户显式设置了 rule_urls 为 ["global"] 或 ["direct"]，则覆盖模式
	if len(cfg.RuleURLs) > 0 && (cfg.RuleURLs[0] == "global" || cfg.RuleURLs[0] == "direct") {
		cfg.ProxyMode = cfg.RuleURLs[0]
		cfg.RuleURLs = nil
	} else if len(cfg.RuleURLs) > 0 {
		cfg.ProxyMode = "pac"
	} else {
		if cfg.ProxyMode == "" {
			cfg.ProxyMode = "global" // 默认为全局代理模式
		}
	}

	return &cfg, nil
}
