package bapedge

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"cc-filter/internal/authzen"
	"cc-filter/internal/policybundle"
)

func TestFixtureCaptureIsPrivacySafeAndManifestBound(t *testing.T) {
	root := t.TempDir()
	directory := filepath.Join(root, "captures")
	now := time.Now().UTC()
	sourceData, err := os.ReadFile(filepath.Join("..", "..", "bap-service", "policies", "edge-policy-source.json"))
	if err != nil {
		t.Fatal(err)
	}
	source, err := policybundle.LoadSource(sourceData)
	if err != nil {
		t.Fatal(err)
	}
	cedarPolicy, err := os.ReadFile(filepath.Join("..", "..", "bap-service", "policies", "agent-tools.cedar"))
	if err != nil {
		t.Fatal(err)
	}
	bundle, err := policybundle.Build(source, cedarPolicy, now)
	if err != nil {
		t.Fatal(err)
	}
	raw := []byte(`{"hook_event_name":"PreToolUse","session_id":"secret-session","tool_use_id":"secret-tool-use","cwd":"C:\\secret\\project","tool_name":"Bash","tool_input":{"command":"echo TOP_SECRET_VALUE"},"unknown_future_field":"private-value"}`)
	input := HookInput{HookEventName: "PreToolUse", SessionID: "secret-session", ToolUseID: "secret-tool-use", CWD: `C:\secret\project`, ToolName: "Bash", ToolInput: map[string]any{"command": "echo TOP_SECRET_VALUE"}}
	request := authzen.EvaluationRequest{Action: authzen.Action{Name: "command.execute"}, Resource: authzen.Entity{Properties: map[string]any{"destructive": false, "privileged": false, "exfiltration": false, "obfuscated": false}}}
	t.Setenv("BAP_FIXTURE_CAPTURE_DIRECTORY", directory)
	t.Setenv("BAP_FIXTURE_SCENARIO", "safe-command")
	t.Setenv("BAP_FIXTURE_CLAUDE_VERSION", "2.1.0")
	t.Setenv("BAP_FIXTURE_EXPECTED_DECISION", "allow")
	for _, model := range []string{"company-sonnet", "company-opus"} {
		t.Setenv("BAP_FIXTURE_MODEL", model)
		if err := RecordFixtureFromEnvironment(raw, input, request, "allow", "LOCAL_POLICY_PERMIT", []string{"command.git.status.short"}, bundle); err != nil {
			t.Fatal(err)
		}
	}
	entries, err := os.ReadDir(directory)
	if err != nil || len(entries) != 2 {
		t.Fatalf("captured files=%d err=%v", len(entries), err)
	}
	for _, entry := range entries {
		data, err := os.ReadFile(filepath.Join(directory, entry.Name()))
		if err != nil {
			t.Fatal(err)
		}
		text := string(data)
		for _, secret := range []string{"TOP_SECRET_VALUE", "secret-session", "secret-tool-use", `C:\\secret\\project`, "private-value"} {
			if strings.Contains(text, secret) {
				t.Fatalf("fixture leaked %q: %s", secret, text)
			}
		}
	}
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	envelope, err := policybundle.Sign(privateKey, "fixture-test", bundle)
	if err != nil {
		t.Fatal(err)
	}
	bundleData, _ := json.Marshal(envelope)
	bundlePath := filepath.Join(root, "active-bundle.json")
	if err := os.WriteFile(bundlePath, bundleData, 0600); err != nil {
		t.Fatal(err)
	}
	publicDER, err := x509.MarshalPKIXPublicKey(publicKey)
	if err != nil {
		t.Fatal(err)
	}
	publicKeyPath := filepath.Join(root, "bundle-public.pem")
	if err := os.WriteFile(publicKeyPath, pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: publicDER}), 0600); err != nil {
		t.Fatal(err)
	}
	manifest, err := BuildFixtureManifest(directory, bundlePath, publicKeyPath, []string{"sonnet", "opus"}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	manifestPath := filepath.Join(directory, "certification-manifest.json")
	if err := WriteFixtureManifest(manifestPath, manifest); err != nil {
		t.Fatal(err)
	}
	report, err := VerifyFixtureManifest(directory, manifestPath, bundlePath, publicKeyPath, []string{"sonnet", "opus"})
	if err != nil || report.FixtureCount != 2 || report.Scenarios != 1 {
		t.Fatalf("verification=%#v err=%v", report, err)
	}

	fixturePath := filepath.Join(directory, manifest.Fixtures[0].File)
	file, _ := os.OpenFile(fixturePath, os.O_APPEND|os.O_WRONLY, 0600)
	_, _ = file.WriteString(" ")
	_ = file.Close()
	if _, err := VerifyFixtureManifest(directory, manifestPath, bundlePath, publicKeyPath, []string{"sonnet", "opus"}); err == nil || !strings.Contains(err.Error(), "hash mismatch") {
		t.Fatalf("tampered fixture was not rejected: %v", err)
	}
}

func TestFixtureCertificationRejectsUnknownToolSchema(t *testing.T) {
	fixture := ClaudeFixture{
		SchemaVersion: FixtureSchemaVersion, Scenario: "unknown-tool", ClaudeCodeVersion: "2.1.0", Model: "sonnet",
		HookEvent: "PreToolUse", Tool: "FutureTool", ToolKnown: false, Action: "tool.unknown",
		HookSchema: map[string]any{}, HookSchemaDigest: shapeDigest(map[string]any{}), InputSchema: map[string]any{}, InputSchemaDigest: shapeDigest(map[string]any{}),
		ExpectedDecision: "deny", ActualDecision: "deny", ReasonCode: "LOCAL_EXPLICIT_FORBID", PolicyVersion: 1, RulesDigest: "sha256:rules",
	}
	if err := ValidateClaudeFixture(fixture); err == nil {
		t.Fatal("unknown tool schema was certified")
	}
}
