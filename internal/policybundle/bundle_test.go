package policybundle

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"cc-filter/internal/authzen"
)

func testBundle(t *testing.T, now time.Time) Bundle {
	t.Helper()
	source, policy := testSourceAndPolicy(t)
	bundle, err := Build(source, policy, now)
	if err != nil {
		t.Fatal(err)
	}
	return bundle
}

func testSourceAndPolicy(t *testing.T) (Source, []byte) {
	t.Helper()
	sourceData, err := os.ReadFile(filepath.Join("..", "..", "bap-service", "policies", "edge-policy-source.json"))
	if err != nil {
		t.Fatal(err)
	}
	source, err := LoadSource(sourceData)
	if err != nil {
		t.Fatal(err)
	}
	policy, err := os.ReadFile(filepath.Join("..", "..", "bap-service", "policies", "agent-tools.cedar"))
	if err != nil {
		t.Fatal(err)
	}
	return source, policy
}

func commandRequest(command string) authzen.EvaluationRequest {
	return authzen.EvaluationRequest{
		Subject: authzen.Entity{Type: "agent", ID: "claude-code-local"}, Action: authzen.Action{Name: "command.execute"},
		Resource: authzen.Entity{Type: "tool-invocation", ID: "command", Properties: map[string]any{
			"tool": "Bash", "command": command, "target": command, "path": "", "protected": false,
			"outsideWorkspace": false, "securityControl": false, "destructive": false, "privileged": false,
			"exfiltration": false, "obfuscated": false, "shellApproved": true, "policyProfile": "standard-developer",
		}},
	}
}

func TestSignedBundleVerificationAndTamperResistance(t *testing.T) {
	now := time.Date(2026, 8, 30, 12, 0, 0, 0, time.UTC)
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	envelope, err := Sign(privateKey, "test-key", testBundle(t, now))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Verify(publicKey, envelope, now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	tampered := envelope
	tampered.Payload = append([]byte(nil), envelope.Payload...)
	var payload map[string]any
	_ = json.Unmarshal(tampered.Payload, &payload)
	payload["kill_switch"] = false
	payload["version"] = float64(99)
	tampered.Payload, _ = json.Marshal(payload)
	if _, err := Verify(publicKey, tampered, now.Add(time.Minute)); err == nil {
		t.Fatal("tampered signed bundle was accepted")
	}
	if _, err := Verify(publicKey, envelope, now.Add(31*24*time.Hour)); err == nil {
		t.Fatal("expired signed bundle was accepted")
	}
}

func TestLocalCommandAuthorizationUsesBundleRulesNotClientFlags(t *testing.T) {
	now := time.Date(2026, 8, 30, 12, 0, 0, 0, time.UTC)
	bundle := testBundle(t, now)
	for _, command := range []string{"git status --short", "git diff --stat", "ls", "ls -al", "rg --files"} {
		decision, err := Authorize(bundle, commandRequest(command), now.Add(time.Minute))
		if err != nil || !decision.Allowed || len(decision.RuleIDs) == 0 {
			t.Fatalf("%q should allow from signed registry: decision=%#v err=%v", command, decision, err)
		}
	}
	for _, command := range []string{"python -c print(1)", "git status; git reset --hard", "git reset --hard", "ls -Z"} {
		request := commandRequest(command)
		request.Resource.Properties["shellApproved"] = true
		request.Resource.Properties["destructive"] = false
		decision, err := Authorize(bundle, request, now.Add(time.Minute))
		if err != nil || decision.Allowed {
			t.Fatalf("%q should deny regardless of forged client flags: decision=%#v err=%v", command, decision, err)
		}
	}
}

func TestManualOnlyCommandsReturnDistinctFailClosedOutcome(t *testing.T) {
	now := time.Date(2026, 8, 30, 12, 0, 0, 0, time.UTC)
	bundle := testBundle(t, now)
	for _, command := range []string{"mysql -h orders-prod", "psql app", "sqlcmd.exe -S orders-prod", "sqlplus dba@orders-prod", "ssh admin@app-prod", "kubectl get pods"} {
		decision, err := Authorize(bundle, commandRequest(command), now.Add(time.Minute))
		if err != nil || decision.Allowed || !decision.ManualOnly || decision.ReasonCode != "MANUAL_EXECUTION_REQUIRED" || len(decision.RuleIDs) != 1 {
			t.Fatalf("%q should require manual execution: decision=%#v err=%v", command, decision, err)
		}
	}
}

func TestExplicitForbidOverridesManualOnly(t *testing.T) {
	now := time.Date(2026, 8, 30, 12, 0, 0, 0, time.UTC)
	bundle := testBundle(t, now)
	bundle.CommandRules = append(bundle.CommandRules, CommandRule{
		ID: "command.mysql.emergency-forbid", Executable: "mysql", Effect: "forbid",
		AllowAdditionalArguments: true, AdditionalArgumentPattern: ".*", Profiles: []string{"standard-developer"},
		Owner: "security", Approval: "incident", NotBefore: now, ExpiresAt: now.Add(time.Hour),
	})
	decision, err := Authorize(bundle, commandRequest("mysql -h orders-prod"), now.Add(time.Minute))
	if err != nil || decision.Allowed || decision.ManualOnly || decision.ReasonCode != "LOCAL_EXPLICIT_FORBID" {
		t.Fatalf("explicit forbid did not override manual-only: decision=%#v err=%v", decision, err)
	}
}

func TestCedarForbidOverridesManualOnly(t *testing.T) {
	now := time.Date(2026, 8, 30, 12, 0, 0, 0, time.UTC)
	bundle := testBundle(t, now)
	bundle.PolicyProfile = "read-only"
	decision, err := Authorize(bundle, commandRequest("mysql -h orders-prod"), now.Add(time.Minute))
	if err != nil || decision.Allowed || decision.ManualOnly || decision.ReasonCode != "LOCAL_EXPLICIT_FORBID" {
		t.Fatalf("Cedar forbid did not override manual-only: decision=%#v err=%v", decision, err)
	}
}

func TestExpiredRuleAndKillSwitchDeny(t *testing.T) {
	now := time.Date(2026, 8, 30, 12, 0, 0, 0, time.UTC)
	bundle := testBundle(t, now)
	for index := range bundle.CommandRules {
		bundle.CommandRules[index].ExpiresAt = now.Add(-time.Second)
	}
	decision, err := Authorize(bundle, commandRequest("ls -al"), now)
	if err != nil || decision.Allowed {
		t.Fatalf("expired command rule allowed: %#v %v", decision, err)
	}
	bundle.KillSwitch = true
	decision, err = Authorize(bundle, commandRequest("git status --short"), now)
	if err != nil || decision.Allowed || decision.ReasonCode != "KILL_SWITCH" {
		t.Fatalf("kill switch did not deny: %#v %v", decision, err)
	}
}

func TestRemovingRuleInNewBundleVersionRevokesCommand(t *testing.T) {
	now := time.Date(2026, 8, 30, 12, 0, 0, 0, time.UTC)
	bundle := testBundle(t, now)
	kept := bundle.CommandRules[:0]
	for _, rule := range bundle.CommandRules {
		if rule.Executable != "ls" {
			kept = append(kept, rule)
		}
	}
	bundle.CommandRules = kept
	bundle.Version++
	decision, err := Authorize(bundle, commandRequest("ls -al"), now.Add(time.Minute))
	if err != nil || decision.Allowed {
		t.Fatalf("removed central rule still authorized command: %#v %v", decision, err)
	}
}

func TestInvalidSourceFailsClosed(t *testing.T) {
	source := Source{SchemaVersion: SchemaVersion, Version: 1, ValidForSeconds: 10, RefreshAfterSeconds: 5, MaxOfflineSeconds: 10, PolicyProfile: "standard-developer", CommandRules: []CommandRule{{ID: "bad", Executable: "git", Effect: "eligible-for-permit", Owner: "owner", Approval: "ticket", ArgumentPatterns: []string{"["}}}}
	if _, err := LoadSource(mustJSON(source)); err == nil {
		t.Fatal("invalid command argument pattern was accepted")
	}
	if _, err := LoadSource([]byte(`{"schema_version":1,"version":1,"unknown_policy_knob":true}`)); err == nil {
		t.Fatal("unknown policy source field was accepted")
	}
	invalidEffect := Source{SchemaVersion: SchemaVersion, Version: 1, ValidForSeconds: 10, RefreshAfterSeconds: 5, MaxOfflineSeconds: 10, PolicyProfile: "standard-developer", CommandRules: []CommandRule{{ID: "bad-effect", Executable: "mysql", Effect: "manual", Owner: "owner", Approval: "ticket"}}}
	if _, err := LoadSource(mustJSON(invalidEffect)); err == nil {
		t.Fatal("unknown command rule effect was accepted")
	}
}

func TestPromptIntentRulesAreAdvisorySignals(t *testing.T) {
	now := time.Date(2026, 8, 30, 12, 0, 0, 0, time.UTC)
	bundle := testBundle(t, now)
	for _, prompt := range []string{
		"mysql -h orders-prod -u dba",
		"Please connect to the MySQL orders database and reindex it",
		"Execute this command exactly: mysql -h orders-prod -u dba",
		"Log in to the remote server with SSH",
		"Use kubectl to deploy this to the cluster",
	} {
		if matched := MatchPrompt(bundle, prompt); len(matched) == 0 {
			t.Fatalf("expected privileged intent signal for %q", prompt)
		}
	}
	for _, prompt := range []string{
		"Explain how database indexes work",
		"Review this Go code without executing it",
		"Summarize the Kubernetes architecture document",
	} {
		if matched := MatchPrompt(bundle, prompt); len(matched) != 0 {
			t.Fatalf("unexpected privileged intent signal for %q: %v", prompt, matched)
		}
	}
}

func TestActivationRequiresVersionIncrementAndDoesNotRenewOnRestart(t *testing.T) {
	now := time.Date(2026, 8, 30, 12, 0, 0, 0, time.UTC)
	_, privateKey, _ := ed25519.GenerateKey(rand.Reader)
	source, policy := testSourceAndPolicy(t)
	statePath := filepath.Join(t.TempDir(), "active.json")
	first, _, err := Activate(source, policy, privateKey, "test", statePath, now)
	if err != nil {
		t.Fatal(err)
	}
	second, _, err := Activate(source, policy, privateKey, "test", statePath, now.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if !second.IssuedAt.Equal(first.IssuedAt) || !second.ExpiresAt.Equal(first.ExpiresAt) {
		t.Fatal("service restart silently renewed rule lifetime")
	}
	changed := source
	changed.KillSwitch = true
	if _, _, err := Activate(changed, policy, privateKey, "test", statePath, now.Add(time.Hour)); err == nil {
		t.Fatal("changed rules under the same version were accepted")
	}
	changed.Version++
	if next, _, err := Activate(changed, policy, privateKey, "test", statePath, now.Add(time.Hour)); err != nil || next.Version != changed.Version || !next.KillSwitch {
		t.Fatalf("incremented policy version did not activate: %#v %v", next, err)
	}
}
