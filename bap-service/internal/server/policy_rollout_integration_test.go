package server_test

import (
	"context"
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

	"bap-system/bap-edge/pkg/bapedge"
	serviceaudit "bap-system/bap-service/internal/audit"
	serviceserver "bap-system/bap-service/internal/server"
	"bap-system/internal/authzen"
	"bap-system/internal/policybundle"
	"net/http/httptest"
)

type rolloutAuditStore struct{}

func (rolloutAuditStore) Append(serviceaudit.Event) error { return nil }
func (rolloutAuditStore) HasEvent(string) (bool, error)   { return false, nil }
func (rolloutAuditStore) HasAllowedOperation(string, string, string, string) (bool, error) {
	return true, nil
}
func (rolloutAuditStore) Ready(context.Context) error { return nil }

func TestSignedPolicyRolloutEndToEnd(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	source, cedarPolicy := rolloutSource(t)
	initialVersion := source.Version
	initialEpoch := source.RevocationEpoch
	bundleV1, envelopeV1 := rolloutBundle(t, source, cedarPolicy, privateKey, now)

	controlPlane := serviceserver.New(nil, "", rolloutAuditStore{}, "rollout-api-key", "rollout-edge")
	controlPlane.SetPolicyBundle(bundleV1, envelopeV1)
	tlsServer := httptest.NewTLSServer(controlPlane.Handler())

	directory := t.TempDir()
	caPath := filepath.Join(directory, "service-ca.pem")
	caPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: tlsServer.Certificate().Raw})
	if err := os.WriteFile(caPath, caPEM, 0600); err != nil {
		t.Fatal(err)
	}
	bundlePublicPath := filepath.Join(directory, "bundle-public.pem")
	publicDER, err := x509.MarshalPKIXPublicKey(publicKey)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(bundlePublicPath, pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: publicDER}), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("BAP_ROLLOUT_TEST_KEY", "rollout-api-key")
	config := bapedge.Config{
		ServiceURL: tlsServer.URL, BundlePublicKeyPath: bundlePublicPath, CABundlePath: caPath,
		StateDirectory: filepath.Join(directory, "edge-state"), APIKeyEnv: "BAP_ROLLOUT_TEST_KEY",
		SubjectID: "claude-code-local", TimeoutMS: 1000,
	}
	client, err := bapedge.NewClient(config)
	if err != nil {
		t.Fatal(err)
	}
	store, err := bapedge.NewPolicyStore(config)
	if err != nil {
		t.Fatal(err)
	}

	installedV1, err := bapedge.EnsurePolicy(context.Background(), client, store, "rollout-edge", true, now.Add(time.Second))
	if err != nil || installedV1.Version != initialVersion {
		t.Fatalf("install policy v1: version=%d err=%v", installedV1.Version, err)
	}
	decision, err := policybundle.Authorize(installedV1, rolloutCommandRequest("ls -al"), now.Add(time.Second))
	if err != nil || !decision.Allowed {
		t.Fatalf("v1 did not permit centrally registered ls command: decision=%#v err=%v", decision, err)
	}
	installedVersion, installedDigest, installedEpoch := store.Posture()
	currentResponse, err := client.SyncPolicy(context.Background(), policybundle.SyncRequest{
		EdgeInstanceID: "rollout-edge", EdgeVersion: bapedge.EdgeProtocolVersion,
		InstalledVersion: installedVersion, InstalledDigest: installedDigest, RevocationEpoch: installedEpoch, Nonce: "current-check",
	})
	if err != nil || currentResponse.Directive != "CURRENT" {
		t.Fatalf("matching Edge posture did not receive CURRENT: response=%#v err=%v", currentResponse, err)
	}

	source.Version = initialVersion + 1
	source.RevocationEpoch = initialEpoch + 1
	source.ForceUpdate = true
	source.CommandRules = removeExecutable(source.CommandRules, "ls")
	bundleV2, envelopeV2 := rolloutBundle(t, source, cedarPolicy, privateKey, now.Add(2*time.Second))
	controlPlane.SetPolicyBundle(bundleV2, envelopeV2)
	forcedResponse, err := client.SyncPolicy(context.Background(), policybundle.SyncRequest{
		EdgeInstanceID: "rollout-edge", EdgeVersion: bapedge.EdgeProtocolVersion,
		InstalledVersion: installedVersion, InstalledDigest: installedDigest, RevocationEpoch: installedEpoch, Nonce: "forced-update-check",
	})
	if err != nil || forcedResponse.Directive != "UPDATE_REQUIRED" {
		t.Fatalf("stale Edge posture did not receive UPDATE_REQUIRED: response=%#v err=%v", forcedResponse, err)
	}
	installedV2, err := bapedge.EnsurePolicy(context.Background(), client, store, "rollout-edge", true, now.Add(3*time.Second))
	if err != nil || installedV2.Version != source.Version {
		t.Fatalf("install forced policy v2: version=%d err=%v", installedV2.Version, err)
	}
	decision, err = policybundle.Authorize(installedV2, rolloutCommandRequest("ls -al"), now.Add(3*time.Second))
	if err != nil || decision.Allowed {
		t.Fatalf("v2 still permitted removed ls rule: decision=%#v err=%v", decision, err)
	}

	controlPlane.SetPolicyBundle(bundleV1, envelopeV1)
	if _, err := bapedge.EnsurePolicy(context.Background(), client, store, "rollout-edge", true, now.Add(4*time.Second)); err == nil || !strings.Contains(err.Error(), "rollback") {
		t.Fatalf("signed rollback was not rejected: %v", err)
	}

	equivocatedSource := source
	equivocatedSource.AllowedNetwork = []string{"example.invalid"}
	equivocatedBundle, equivocatedEnvelope := rolloutBundle(t, equivocatedSource, cedarPolicy, privateKey, now.Add(4*time.Second))
	controlPlane.SetPolicyBundle(equivocatedBundle, equivocatedEnvelope)
	if _, err := bapedge.EnsurePolicy(context.Background(), client, store, "rollout-edge", true, now.Add(5*time.Second)); err == nil || !strings.Contains(err.Error(), "equivocation") {
		t.Fatalf("same-version changed-content bundle was not rejected: %v", err)
	}

	var tamperedPayload map[string]any
	if err := json.Unmarshal(envelopeV2.Payload, &tamperedPayload); err != nil {
		t.Fatal(err)
	}
	tamperedPayload["version"] = float64(99)
	tamperedEnvelope := envelopeV2
	tamperedEnvelope.Payload, _ = json.Marshal(tamperedPayload)
	controlPlane.SetPolicyBundle(bundleV2, tamperedEnvelope)
	if _, err := bapedge.EnsurePolicy(context.Background(), client, store, "rollout-edge", true, now.Add(6*time.Second)); err == nil || !strings.Contains(err.Error(), "signature") {
		t.Fatalf("tampered policy envelope was not rejected: %v", err)
	}

	killSource := source
	killSource.Version = source.Version + 1
	killSource.RevocationEpoch = source.RevocationEpoch + 1
	killSource.KillSwitch = true
	killBundle, killEnvelope := rolloutBundle(t, killSource, cedarPolicy, privateKey, now.Add(6*time.Second))
	controlPlane.SetPolicyBundle(killBundle, killEnvelope)
	if _, err := bapedge.EnsurePolicy(context.Background(), client, store, "rollout-edge", true, now.Add(7*time.Second)); err == nil || !strings.Contains(err.Error(), "kill switch") {
		t.Fatalf("kill switch did not stop local traffic: %v", err)
	}

	offlineSource := source
	offlineSource.Version = source.Version + 2
	offlineSource.RevocationEpoch = source.RevocationEpoch + 2
	offlineSource.ForceUpdate = false
	offlineSource.KillSwitch = false
	offlineSource.RefreshAfterSeconds = 1
	offlineSource.MaxOfflineSeconds = 2
	offlineBundle, offlineEnvelope := rolloutBundle(t, offlineSource, cedarPolicy, privateKey, now.Add(8*time.Second))
	controlPlane.SetPolicyBundle(offlineBundle, offlineEnvelope)
	if _, err := bapedge.EnsurePolicy(context.Background(), client, store, "rollout-edge", true, now.Add(9*time.Second)); err != nil {
		t.Fatalf("install short-lease policy: %v", err)
	}
	tlsServer.Close()
	if _, err := bapedge.EnsurePolicy(context.Background(), client, store, "rollout-edge", false, now.Add(12*time.Second)); err == nil {
		t.Fatal("expired offline lease continued authorizing after control-plane loss")
	}
}

func rolloutSource(t *testing.T) (policybundle.Source, []byte) {
	t.Helper()
	sourceData, err := os.ReadFile(filepath.Join("..", "..", "policies", "edge-policy-source.json"))
	if err != nil {
		t.Fatal(err)
	}
	source, err := policybundle.LoadSource(sourceData)
	if err != nil {
		t.Fatal(err)
	}
	cedarPolicy, err := os.ReadFile(filepath.Join("..", "..", "policies", "agent-tools.cedar"))
	if err != nil {
		t.Fatal(err)
	}
	return source, cedarPolicy
}

func rolloutBundle(t *testing.T, source policybundle.Source, cedarPolicy []byte, privateKey ed25519.PrivateKey, now time.Time) (policybundle.Bundle, policybundle.Envelope) {
	t.Helper()
	bundle, err := policybundle.Build(source, cedarPolicy, now)
	if err != nil {
		t.Fatal(err)
	}
	envelope, err := policybundle.Sign(privateKey, "rollout-test", bundle)
	if err != nil {
		t.Fatal(err)
	}
	return bundle, envelope
}

func removeExecutable(rules []policybundle.CommandRule, executable string) []policybundle.CommandRule {
	result := make([]policybundle.CommandRule, 0, len(rules))
	for _, rule := range rules {
		if !strings.EqualFold(rule.Executable, executable) {
			result = append(result, rule)
		}
	}
	return result
}

func rolloutCommandRequest(command string) authzen.EvaluationRequest {
	return authzen.EvaluationRequest{
		Subject: authzen.Entity{Type: "agent", ID: "claude-code-local"},
		Action:  authzen.Action{Name: "command.execute"},
		Resource: authzen.Entity{Type: "tool-invocation", ID: "rollout-command", Properties: map[string]any{
			"tool": "Bash", "command": command, "target": command, "path": "", "protected": false,
			"outsideWorkspace": false, "securityControl": false, "destructive": false, "privileged": false,
			"exfiltration": false, "obfuscated": false, "shellApproved": false, "policyProfile": "read-only",
		}},
	}
}
