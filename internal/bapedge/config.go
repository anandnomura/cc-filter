package bapedge

import (
	"bytes"
	"fmt"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

type Config struct {
	ServiceURL            string `yaml:"service_url"`
	PublicKeyPath         string `yaml:"public_key_path"`
	BundlePublicKeyPath   string `yaml:"bundle_public_key_path,omitempty"`
	CABundlePath          string `yaml:"ca_bundle_path,omitempty"`
	ClientCertificatePath string `yaml:"client_certificate_path,omitempty"`
	ClientKeyPath         string `yaml:"client_key_path,omitempty"`
	CacheDirectory        string `yaml:"cache_directory,omitempty"`
	StateDirectory        string `yaml:"state_directory,omitempty"`
	APIKeyEnv             string `yaml:"api_key_env"`
	SubjectID             string `yaml:"subject_id"`
	TimeoutMS             int    `yaml:"timeout_ms"`
}

func LoadConfig(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}, fmt.Errorf("read BAP Edge config %q: %w", path, err)
	}
	var config Config
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	if err := decoder.Decode(&config); err != nil {
		return Config{}, fmt.Errorf("parse BAP Edge config: %w", err)
	}
	if config.ServiceURL == "" || config.SubjectID == "" || config.BundlePublicKeyPath == "" {
		return Config{}, fmt.Errorf("service_url, bundle_public_key_path, and subject_id are required")
	}
	if (config.ClientCertificatePath == "") != (config.ClientKeyPath == "") {
		return Config{}, fmt.Errorf("client_certificate_path and client_key_path must be configured together")
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
