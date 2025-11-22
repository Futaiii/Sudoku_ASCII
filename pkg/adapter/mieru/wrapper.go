package mieru

import (
	"encoding/json"
	"github.com/enfein/mieru/v3/apis/client"
	"github.com/enfein/mieru/v3/apis/server"
	"log"
	"os"
)

// StartMieruClient 启动内置的 Mieru 客户端
func StartMieruClient(configPath string) (client.Client, error) {
	if configPath == "" {
		return nil, nil
	}

	content, err := os.ReadFile(configPath)
	if err != nil {
		return nil, err
	}

	var cfg client.ClientConfig
	if err := json.Unmarshal(content, &cfg); err != nil {
		return nil, err
	}

	c := client.NewClient()
	if err := c.Store(&cfg); err != nil {
		return nil, err
	}
	if err := c.Start(); err != nil {
		return nil, err
	}
	log.Printf("[Mieru] Client started with profile: %s", cfg.Profile.ProfileName)
	return c, nil
}

// StartMieruServer 启动内置的 Mieru 服务端
func StartMieruServer(configPath string) (server.Server, error) {
	if configPath == "" {
		return nil, nil
	}

	content, err := os.ReadFile(configPath)
	if err != nil {
		return nil, err
	}

	var cfg server.ServerConfig
	if err := json.Unmarshal(content, &cfg); err != nil {
		return nil, err
	}

	s := server.NewServer()
	if err := s.Store(&cfg); err != nil {
		return nil, err
	}
	if err := s.Start(); err != nil {
		return nil, err
	}
	log.Printf("[Mieru] Server started on configured ports")
	return s, nil
}
