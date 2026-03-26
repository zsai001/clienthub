package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

type ServerConfig struct {
	ListenAddr string `yaml:"listen_addr"`
	UDPAddr    string `yaml:"udp_addr"`
	AdminAddr  string `yaml:"admin_addr"`
	Secret     string `yaml:"secret"`
	LogLevel   string `yaml:"log_level"`
	StorePath  string `yaml:"store_path"`
}

type ExposeService struct {
	Name      string `yaml:"name"`
	LocalAddr string `yaml:"local_addr"`
	Protocol  string `yaml:"protocol"`
}

type ForwardRule struct {
	RemoteClient  string `yaml:"remote_client"`
	RemoteService string `yaml:"remote_service"`
	ListenAddr    string `yaml:"listen_addr"`
	Protocol      string `yaml:"protocol"`
}

type ClientConfig struct {
	ServerAddr string          `yaml:"server_addr"`
	ClientName string          `yaml:"client_name"`
	Secret     string          `yaml:"secret"`
	LogLevel   string          `yaml:"log_level"`
	Expose     []ExposeService `yaml:"expose"`
	Forward    []ForwardRule   `yaml:"forward"`
}

type ManagerConfig struct {
	AdminAddr string `yaml:"admin_addr"`
	Secret    string `yaml:"secret"`
}

func LoadServerConfig(path string) (*ServerConfig, error) {
	cfg := &ServerConfig{
		ListenAddr: ":7900",
		UDPAddr:    ":7901",
		AdminAddr:  ":7902",
		LogLevel:   "info",
		StorePath:  "clienthub-store.json",
	}
	if err := loadYAML(path, cfg); err != nil {
		return nil, fmt.Errorf("load server config: %w", err)
	}
	if cfg.Secret == "" {
		return nil, fmt.Errorf("server config: secret is required")
	}
	return cfg, nil
}

func LoadClientConfig(path string) (*ClientConfig, error) {
	cfg := &ClientConfig{
		LogLevel: "info",
	}
	if err := loadYAML(path, cfg); err != nil {
		return nil, fmt.Errorf("load client config: %w", err)
	}
	if cfg.ServerAddr == "" {
		return nil, fmt.Errorf("client config: server_addr is required")
	}
	if cfg.ClientName == "" {
		return nil, fmt.Errorf("client config: client_name is required")
	}
	if cfg.Secret == "" {
		return nil, fmt.Errorf("client config: secret is required")
	}
	for i := range cfg.Expose {
		if cfg.Expose[i].Protocol == "" {
			cfg.Expose[i].Protocol = "tcp"
		}
	}
	for i := range cfg.Forward {
		if cfg.Forward[i].Protocol == "" {
			cfg.Forward[i].Protocol = "tcp"
		}
	}
	return cfg, nil
}

func LoadManagerConfig(path string) (*ManagerConfig, error) {
	cfg := &ManagerConfig{
		AdminAddr: "127.0.0.1:7902",
	}
	if err := loadYAML(path, cfg); err != nil {
		return nil, fmt.Errorf("load manager config: %w", err)
	}
	if cfg.Secret == "" {
		return nil, fmt.Errorf("manager config: secret is required")
	}
	return cfg, nil
}

func loadYAML(path string, out any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return yaml.Unmarshal(data, out)
}
