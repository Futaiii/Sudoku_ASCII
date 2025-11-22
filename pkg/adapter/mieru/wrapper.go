package mieru

import (
	"encoding/json"
	"fmt"

	"github.com/enfein/mieru/v3/apis/client"
	"github.com/enfein/mieru/v3/apis/server"
	"github.com/enfein/mieru/v3/pkg/appctl/appctlpb"
	"google.golang.org/protobuf/encoding/protojson"

	"log"
	"os"
)

// rawClientConfigShell 用于解析 mieru_client.json 的外层结构
// 因为 standard encoding/json 不支持 protobuf enum string，
// 所以我们将 Profiles 字段延迟解析为 RawMessage
type rawClientConfigShell struct {
	Profiles      []json.RawMessage `json:"profiles"`
	ActiveProfile string            `json:"activeProfile"`
	RPCPort       int               `json:"rpcPort"`
	Socks5Port    int               `json:"socks5Port"`
	LoggingLevel  string            `json:"loggingLevel"`
}

// StartMieruClient 启动内置的 Mieru 客户端
func StartMieruClient(configPath string) (client.Client, error) {
	if configPath == "" {
		return nil, nil
	}

	content, err := os.ReadFile(configPath)
	if err != nil {
		return nil, err
	}

	// 1. 第一步：使用标准 JSON 解析外层结构
	var shell rawClientConfigShell
	if err := json.Unmarshal(content, &shell); err != nil {
		return nil, fmt.Errorf("failed to unmarshal client config shell: %w", err)
	}

	// 2. 第二步：使用 protojson 解析内层的 Profiles
	var profiles []*appctlpb.ClientProfile
	unmarshalOpts := protojson.UnmarshalOptions{
		DiscardUnknown: true, // 允许配置文件中有额外字段，提高兼容性
	}

	for i, rawProfile := range shell.Profiles {
		var p appctlpb.ClientProfile
		if err := unmarshalOpts.Unmarshal(rawProfile, &p); err != nil {
			return nil, fmt.Errorf("failed to protojson unmarshal profile #%d: %w", i, err)
		}
		profiles = append(profiles, &p)
	}

	// 3. 寻找 Active Profile
	var activeProfile *appctlpb.ClientProfile
	if shell.ActiveProfile != "" {
		for _, p := range profiles {
			if *p.ProfileName == shell.ActiveProfile {
				activeProfile = p
				break
			}
		}
	} else if len(profiles) > 0 {
		activeProfile = profiles[0]
	}

	if activeProfile == nil {
		return nil, fmt.Errorf("no active profile found in config: %s", configPath)
	}

	// 4. 构造并启动
	apiCfg := &client.ClientConfig{
		Profile: activeProfile,
		// Dialer 等字段留空使用默认值
	}

	c := client.NewClient()
	if err := c.Store(apiCfg); err != nil {
		return nil, fmt.Errorf("failed to store client config: %w", err)
	}
	if err := c.Start(); err != nil {
		return nil, fmt.Errorf("failed to start client: %w", err)
	}
	log.Printf("[Mieru] Client started with profile: %s", activeProfile.ProfileName)
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

	// 1. 直接使用 protojson 解析 ServerConfig
	// 因为 mieru_server.json 的根结构就是 protobuf Message
	var pbConfig appctlpb.ServerConfig
	unmarshalOpts := protojson.UnmarshalOptions{
		DiscardUnknown: true,
	}

	if err := unmarshalOpts.Unmarshal(content, &pbConfig); err != nil {
		return nil, fmt.Errorf("failed to protojson unmarshal server config: %w", err)
	}

	// 2. 构造包装对象
	apiCfg := &server.ServerConfig{
		Config: &pbConfig,
	}

	// 3. 启动
	s := server.NewServer()
	if err := s.Store(apiCfg); err != nil {
		return nil, fmt.Errorf("failed to store server config: %w", err)
	}
	if err := s.Start(); err != nil {
		return nil, fmt.Errorf("failed to start server: %w", err)
	}
	log.Printf("[Mieru] Server started successfully on ports: %v", pbConfig.PortBindings)
	return s, nil
}
