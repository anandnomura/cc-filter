package bapedge

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadConfigRejectsEndpointPolicyFields(t *testing.T) {
	path := filepath.Join(t.TempDir(), "edge.yaml")
	data := strings.Join([]string{
		`service_url: "https://bap.example.test"`,
		`bundle_public_key_path: "bundle-public.pem"`,
		`subject_id: "edge-1"`,
		`policy_profile: "standard-developer"`,
	}, "\n")
	if err := os.WriteFile(path, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadConfig(path); err == nil {
		t.Fatal("endpoint-configurable policy field was accepted")
	}
}
