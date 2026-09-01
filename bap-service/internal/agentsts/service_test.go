package agentsts

import (
	"crypto/ed25519"
	"crypto/rand"
	"testing"
	"time"

	"bap-system/internal/agentgrant"
	"bap-system/internal/authzen"
	"bap-system/internal/policybundle"
)

func TestIssueConsumeIsExactAndOneUse(t *testing.T) {
	_, privateKey, _ := ed25519.GenerateKey(rand.Reader)
	service := New(privateKey, "bap-agent-sts")
	now := time.Now().UTC().Truncate(time.Second)
	bundle := testBundle(now)
	operation := testOperation()
	request := agentgrant.IssueRequest{EdgeInstanceID: "edge-1", Operation: operation, Intent: agentgrant.IntentEvidence{
		SessionID: "session-1", WorkloadID: "workload-1", IntentHash: "sha256:intent",
		RuleIDs: []string{"intent.deploy"}, CapturedAt: now.Unix(),
	}}
	issued, claims, err := service.Issue(request, "edge-device", "fingerprint", bundle, now)
	if err != nil {
		t.Fatal(err)
	}
	if issued.GrantID != claims.GrantID || claims.MaxUses != 1 || claims.Audience != "bap-spring-gateway" {
		t.Fatalf("unexpected claims: %+v", claims)
	}
	consumption := agentgrant.ConsumeRequest{Token: issued.Token, Operation: operation}
	if response, _, err := service.Consume(consumption, bundle, now.Add(time.Second)); err != nil || !response.Consumed {
		t.Fatalf("first consumption failed: response=%+v err=%v", response, err)
	}
	if _, _, err := service.Consume(consumption, bundle, now.Add(2*time.Second)); err == nil {
		t.Fatal("replayed AgentGrant was accepted")
	}
}

func TestWrongPEPAudienceCannotConsumeOrBurnGrant(t *testing.T) {
	_, privateKey, _ := ed25519.GenerateKey(rand.Reader)
	service := New(privateKey, "bap-agent-sts")
	now := time.Now().UTC().Truncate(time.Second)
	bundle := testBundle(now)
	operation := testOperation()
	issued, _, err := service.Issue(agentgrant.IssueRequest{
		EdgeInstanceID: "edge-1", Operation: operation,
		Intent: agentgrant.IntentEvidence{SessionID: "session-1", WorkloadID: "workload-1", IntentHash: "sha256:intent", RuleIDs: []string{"intent.deploy"}, CapturedAt: now.Unix()},
	}, "edge-device", "fingerprint", bundle, now)
	if err != nil {
		t.Fatal(err)
	}
	consumption := agentgrant.ConsumeRequest{Token: issued.Token, Operation: operation}
	if _, _, err := service.ConsumeForAudiences(consumption, bundle, now.Add(time.Second), []string{"bap-mcp-pep"}); err == nil {
		t.Fatal("MCP PEP audience consumed an API PEP grant")
	}
	if response, _, err := service.ConsumeForAudiences(consumption, bundle, now.Add(2*time.Second), []string{"bap-spring-gateway"}); err != nil || !response.Consumed {
		t.Fatalf("wrong-audience attempt burned the grant: response=%+v err=%v", response, err)
	}
}

func TestIssueAndConsumeRejectChangedEvidence(t *testing.T) {
	_, privateKey, _ := ed25519.GenerateKey(rand.Reader)
	now := time.Now().UTC().Truncate(time.Second)
	bundle := testBundle(now)
	operation := testOperation()
	valid := agentgrant.IssueRequest{EdgeInstanceID: "edge-1", Operation: operation, Intent: agentgrant.IntentEvidence{SessionID: "session-1", WorkloadID: "workload-1", IntentHash: "sha256:intent", RuleIDs: []string{"intent.deploy"}, CapturedAt: now.Unix()}}

	t.Run("wrong intent", func(t *testing.T) {
		service := New(privateKey, "issuer")
		changed := valid
		changed.Intent.RuleIDs = []string{"intent.other"}
		if _, _, err := service.Issue(changed, "principal", "fingerprint", bundle, now); err == nil {
			t.Fatal("wrong intent was accepted")
		}
	})

	t.Run("wrong path", func(t *testing.T) {
		service := New(privateKey, "issuer")
		changed := valid
		changed.Operation = operation
		changed.Operation.Resource.Properties = clone(operation.Resource.Properties)
		changed.Operation.Resource.Properties["target"] = "https://api.staging.company.example/admin/delete"
		if _, _, err := service.Issue(changed, "principal", "fingerprint", bundle, now); err == nil {
			t.Fatal("unapproved path was accepted")
		}
	})

	t.Run("changed operation", func(t *testing.T) {
		service := New(privateKey, "issuer")
		issued, _, err := service.Issue(valid, "principal", "fingerprint", bundle, now)
		if err != nil {
			t.Fatal(err)
		}
		changed := operation
		changed.Resource.Properties = clone(operation.Resource.Properties)
		changed.Resource.Properties["bodyDigest"] = "sha256:changed"
		if _, _, err := service.Consume(agentgrant.ConsumeRequest{Token: issued.Token, Operation: changed}, bundle, now.Add(time.Second)); err == nil {
			t.Fatal("changed operation was accepted")
		}
	})

	t.Run("changed policy", func(t *testing.T) {
		service := New(privateKey, "issuer")
		issued, _, err := service.Issue(valid, "principal", "fingerprint", bundle, now)
		if err != nil {
			t.Fatal(err)
		}
		changed := bundle
		changed.Version++
		changed.RulesDigest = "sha256:changed"
		if _, _, err := service.Consume(agentgrant.ConsumeRequest{Token: issued.Token, Operation: operation}, changed, now.Add(time.Second)); err == nil {
			t.Fatal("stale policy-bound AgentGrant was accepted")
		}
	})
}

func testBundle(now time.Time) policybundle.Bundle {
	return policybundle.Bundle{
		SchemaVersion: 1, Version: 7, RulesDigest: "sha256:policy", IssuedAt: now.Add(-time.Minute), ExpiresAt: now.Add(time.Hour),
		PolicyProfile: "standard-developer", RevocationEpoch: 4,
		CedarPolicy:     `forbid (principal is Agent, action == Action::"gateway.execute", resource is ToolInvocation) when { resource.policyProfile == "read-only" };`,
		AgentGrantRules: []policybundle.AgentGrantRule{{ID: "agentgrant.deploy", ResourceType: "api", Action: "gateway.execute", Tool: agentgrant.GatewayToolName, Methods: []string{"POST"}, Hosts: []string{"api.staging.company.example"}, Paths: []string{"/orders/deploy"}, IntentRuleIDs: []string{"intent.deploy"}, Audience: "bap-spring-gateway", MaxTTLSeconds: 60, MaxIntentAgeSeconds: 300, Profiles: []string{"standard-developer"}, Owner: "test", Approval: "test"}},
	}
}

func testOperation() authzen.EvaluationRequest {
	return authzen.EvaluationRequest{
		Subject: authzen.Entity{Type: "agent", ID: "claude-code-local"}, Action: authzen.Action{Name: "gateway.execute"},
		Resource: authzen.Entity{Type: "tool-invocation", ID: "resource-1", Properties: map[string]any{
			"tool": "mcp__bap_gateway__execute", "target": "https://api.staging.company.example/orders/deploy", "networkHost": "api.staging.company.example", "httpMethod": "POST", "bodyDigest": "sha256:body",
		}},
		Context: map[string]any{"session_id": "session-1", "workload_id": "workload-1", "tool_use_id": "tool-use-1"},
	}
}

func clone(source map[string]any) map[string]any {
	result := make(map[string]any, len(source))
	for key, value := range source {
		result[key] = value
	}
	return result
}
