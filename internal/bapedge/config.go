package bapedge

import (
	"fmt"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

type Config struct {
	ServiceURL     string `yaml:"service_url"`
	PublicKeyPath  string `yaml:"public_key_path"`
	CABundlePath   string `yaml:"ca_bundle_path,omitempty"`
	CacheDirectory string `yaml:"cache_directory,omitempty"`
	StateDirectory string `yaml:"state_directory,omitempty"`
	APIKeyEnv      string `yaml:"api_key_env"`
	SubjectID      string `yaml:"subject_id"`
	TimeoutMS      int    `yaml:"timeout_ms"`
}

func LoadConfig(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}, fmt.Errorf("read BAP Edge config %q: %w", path, err)
	}
	var config Config
	if err := yaml.Unmarshal(data, &config); err != nil {
		return Config{}, fmt.Errorf("parse BAP Edge config: %w", err)
	}
	if config.ServiceURL == "" || config.PublicKeyPath == "" || config.SubjectID == "" {
		return Config{}, fmt.Errorf("service_url, public_key_path, and subject_id are required")
	}
	if config.APIKeyEnv == "" {
		config.APIKeyEnv = "BAP_EDGE_API_KEY"
	}
	if err := validateServiceURL(config.ServiceURL); err != nil {
		return Config{}, err
	}
	if config.TimeoutMS <= 0 {
		config.TimeoutMS = 3000
	}
	return config, nil
}

func (c Config) Timeout() time.Duration { return time.Duration(c.TimeoutMS) * time.Millisecond }

func (c Config) APIKey() string { return os.Getenv(c.APIKeyEnv) }
