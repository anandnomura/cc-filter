package bapedge

import (
	"fmt"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

type Config struct {
	ServiceURL            string   `yaml:"service_url"`
	PublicKeyPath         string   `yaml:"public_key_path"`
	CABundlePath          string   `yaml:"ca_bundle_path,omitempty"`
	CacheDirectory        string   `yaml:"cache_directory,omitempty"`
	StateDirectory        string   `yaml:"state_directory,omitempty"`
	APIKeyEnv             string   `yaml:"api_key_env"`
	SubjectID             string   `yaml:"subject_id"`
	TimeoutMS             int      `yaml:"timeout_ms"`
	PolicyProfile         string   `yaml:"policy_profile,omitempty"`
	AllowedNetworkDomains []string `yaml:"allowed_network_domains,omitempty"`
	ApprovedMCPTools      []string `yaml:"approved_mcp_tools,omitempty"`
	ApprovedSubagentTypes []string `yaml:"approved_subagent_types,omitempty"`
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
	if config.PolicyProfile == "" {
		config.PolicyProfile = "standard-developer"
	}
	if config.PolicyProfile != "standard-developer" && config.PolicyProfile != "read-only" {
		return Config{}, fmt.Errorf("policy_profile must be standard-developer or read-only")
	}
	return config, nil
}

func (c Config) NormalizationPolicy() NormalizationPolicy {
	return NormalizationPolicy{Profile: c.PolicyProfile, AllowedNetworkDomains: c.AllowedNetworkDomains, ApprovedMCPTools: c.ApprovedMCPTools, ApprovedSubagentTypes: c.ApprovedSubagentTypes}
}

func (c Config) Timeout() time.Duration { return time.Duration(c.TimeoutMS) * time.Millisecond }

func (c Config) APIKey() string { return os.Getenv(c.APIKeyEnv) }
