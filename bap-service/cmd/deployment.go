package main

import (
	"fmt"
	"strings"

	"bap-system/internal/policybundle"
)

type deploymentSettings struct {
	Mode                  string
	Role                  string
	InstanceID            string
	PolicyMode            string
	DatabaseConfigured    bool
	DatabaseAllowInsecure bool
	DatabaseCAPath        string
	DatabaseTLSServerName string
	DevelopmentTLS        bool
	AllowKeyGeneration    bool
	TLSCertPath           string
	TLSKeyPath            string
	ClientCAPath          string
	EdgeMTLSPrincipals    []string
	STSEdgePrincipal      string
	STSGatewayPrincipal   string
	STSConsumersJSON      string
}

func validateRuntimePolicy(deploymentMode string, bundle policybundle.Bundle) error {
	if deploymentMode == "production" && bundle.EnforcementMode != "enforce" {
		return fmt.Errorf("production requires a signed policy bundle with enforcement_mode=enforce")
	}
	return nil
}

func validateDeployment(settings deploymentSettings) error {
	if settings.Mode != "development" && settings.Mode != "pilot" && settings.Mode != "production" {
		return fmt.Errorf("BAP_DEPLOYMENT_MODE must be development, pilot, or production")
	}
	if settings.PolicyMode != "activate" && settings.PolicyMode != "verified" {
		return fmt.Errorf("BAP_POLICY_MODE must be activate or verified")
	}
	if settings.Mode == "development" {
		return nil
	}
	missing := make([]string, 0)
	if strings.TrimSpace(settings.InstanceID) == "" {
		missing = append(missing, "BAP_INSTANCE_ID")
	}
	if !settings.DatabaseConfigured {
		missing = append(missing, "BAP_DATABASE_DSN_FILE")
	}
	if settings.DatabaseCAPath == "" {
		missing = append(missing, "BAP_DATABASE_TLS_CA_PATH")
	}
	if settings.DatabaseTLSServerName == "" {
		missing = append(missing, "BAP_DATABASE_TLS_SERVER_NAME")
	}
	if settings.TLSCertPath == "" {
		missing = append(missing, "BAP_TLS_CERT_PATH")
	}
	if settings.TLSKeyPath == "" {
		missing = append(missing, "BAP_TLS_KEY_PATH")
	}
	if settings.ClientCAPath == "" {
		missing = append(missing, "BAP_CLIENT_CA_PATH")
	}
	if settings.PolicyMode != "verified" {
		missing = append(missing, "BAP_POLICY_MODE=verified")
	}
	if settings.Role == "combined" && len(settings.EdgeMTLSPrincipals) == 0 {
		missing = append(missing, "BAP_EDGE_MTLS_PRINCIPALS")
	}
	if settings.STSEdgePrincipal == "" {
		missing = append(missing, "BAP_AGENT_STS_EDGE_PRINCIPAL")
	}
	if settings.STSGatewayPrincipal == "" {
		missing = append(missing, "BAP_AGENT_STS_GATEWAY_PRINCIPAL")
	}
	if settings.STSConsumersJSON == "" {
		missing = append(missing, "BAP_AGENT_STS_CONSUMERS_JSON")
	}
	if len(missing) > 0 {
		return fmt.Errorf("%s mode requires %s", settings.Mode, strings.Join(missing, ", "))
	}
	if settings.DevelopmentTLS || settings.AllowKeyGeneration || settings.DatabaseAllowInsecure {
		return fmt.Errorf("%s mode forbids development TLS, startup key generation, and insecure database TLS", settings.Mode)
	}
	if settings.STSEdgePrincipal == settings.STSGatewayPrincipal {
		return fmt.Errorf("Agent STS Edge and gateway principals must be distinct")
	}
	return nil
}

func splitPrincipals(value string) []string {
	result := make([]string, 0)
	for _, item := range strings.Split(value, ",") {
		if principal := strings.TrimSpace(item); principal != "" {
			result = append(result, principal)
		}
	}
	return result
}
