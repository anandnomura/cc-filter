package main

import (
	"strings"
	"testing"
)

func validPilotSettings() deploymentSettings {
	return deploymentSettings{
		Mode: "pilot", Role: "combined", InstanceID: "bap-service-1", PolicyMode: "verified",
		DatabaseConfigured: true, DatabaseCAPath: "mysql-ca.pem", DatabaseTLSServerName: "mysql.company.example",
		TLSCertPath: "service.pem", TLSKeyPath: "service-key.pem", ClientCAPath: "client-ca.pem",
		EdgeMTLSPrincipals: []string{"edge-1", "edge-2"}, STSEdgePrincipal: "edge-sts",
		STSGatewayPrincipal: "api-pep", STSConsumersJSON: `[{"principal":"api-pep"}]`,
	}
}

func TestPilotDeploymentRequirements(t *testing.T) {
	settings := validPilotSettings()
	if err := validateDeployment(settings); err != nil {
		t.Fatal(err)
	}
	settings.DatabaseConfigured = false
	settings.ClientCAPath = ""
	settings.PolicyMode = "activate"
	err := validateDeployment(settings)
	if err == nil || !strings.Contains(err.Error(), "BAP_DATABASE_DSN_FILE") || !strings.Contains(err.Error(), "BAP_CLIENT_CA_PATH") || !strings.Contains(err.Error(), "BAP_POLICY_MODE=verified") {
		t.Fatalf("unsafe pilot was not rejected with actionable requirements: %v", err)
	}
}

func TestPilotRejectsDevelopmentShortcuts(t *testing.T) {
	settings := validPilotSettings()
	settings.DevelopmentTLS = true
	if err := validateDeployment(settings); err == nil {
		t.Fatal("pilot accepted development TLS")
	}
}

func TestDevelopmentModeRetainsNativeLaptopWorkflow(t *testing.T) {
	if err := validateDeployment(deploymentSettings{Mode: "development", Role: "combined", PolicyMode: "activate", DevelopmentTLS: true}); err != nil {
		t.Fatal(err)
	}
}
