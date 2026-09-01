package agentgateway

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"testing"
	"time"

	"bap-system/bap-service/internal/agentsts"
	"bap-system/internal/agentgrant"
	"bap-system/internal/policybundle"
)

type stsConsumer struct {
	service *agentsts.Service
	bundle  policybundle.Bundle
	now     time.Time
}

func (c stsConsumer) Consume(_ context.Context, request agentgrant.ConsumeRequest) (agentgrant.ConsumeResponse, error) {
	response, _, err := c.service.Consume(request, c.bundle, c.now)
	return response, err
}

func TestAgentGrantEndToEndIssueGatewayConsumeReplayDenied(t *testing.T) {
	_, privateKey, _ := ed25519.GenerateKey(rand.Reader)
	now := time.Now().UTC().Truncate(time.Second)
	bundle := gatewayBundle(now)
	service := agentsts.New(privateKey, "bap-agent-sts")
	input := testInput()
	input.Operation.Context = map[string]any{"session_id": "session-1", "workload_id": "workload-1", "tool_use_id": "tool-use-1"}
	issued, _, err := service.Issue(agentgrant.IssueRequest{
		EdgeInstanceID: "edge-1", Resource: testResource, Operation: input.Operation,
		Intent: agentgrant.IntentEvidence{SessionID: "session-1", WorkloadID: "workload-1", IntentHash: "sha256:intent", RuleIDs: []string{"intent.deploy"}, CapturedAt: now.Unix()},
	}, "edge-device", "fingerprint", bundle, now)
	if err != nil {
		t.Fatal(err)
	}
	input.Grant = issued.Token
	backend := &fakeBackend{}
	gateway := New(stsConsumer{service: service, bundle: bundle, now: now.Add(time.Second)}, backend, testResource)
	if _, err := gateway.Execute(context.Background(), input); err != nil {
		t.Fatal(err)
	}
	if _, err := gateway.Execute(context.Background(), input); err == nil {
		t.Fatal("replayed AgentGrant reached the backend")
	}
	if backend.calls != 1 {
		t.Fatalf("backend calls = %d, want 1", backend.calls)
	}
}

func gatewayBundle(now time.Time) policybundle.Bundle {
	return policybundle.Bundle{
		SchemaVersion: 1, Version: 6, RulesDigest: "sha256:gateway-policy", IssuedAt: now.Add(-time.Minute), ExpiresAt: now.Add(time.Hour), RevocationEpoch: 1, PolicyProfile: "standard-developer",
		CedarPolicy:     `forbid (principal is Agent, action == Action::"gateway.execute", resource is ToolInvocation) when { resource.policyProfile == "read-only" };`,
		AgentGrantRules: []policybundle.AgentGrantRule{{ID: "agentgrant.deploy", ResourceType: "api", Action: "gateway.execute", Tool: agentgrant.GatewayToolName, Methods: []string{"POST"}, Hosts: []string{"api.staging.company.example"}, Paths: []string{"/orders/deploy"}, IntentRuleIDs: []string{"intent.deploy"}, Resource: testResource, MaxTTLSeconds: 60, MaxIntentAgeSeconds: 300, Profiles: []string{"standard-developer"}, Owner: "test", Approval: "test"}},
	}
}
