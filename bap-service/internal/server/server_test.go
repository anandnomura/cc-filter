package server

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"cc-filter/bap-service/internal/audit"
	"cc-filter/bap-service/internal/cedaradapter"
	"cc-filter/bap-service/internal/metrics"
	"cc-filter/internal/auditwire"
	"cc-filter/internal/authzen"
	"cc-filter/internal/grants"
)

type recordingAuditStore struct{ events []audit.Event }

func (s *recordingAuditStore) Append(event audit.Event) error {
	s.events = append(s.events, event)
	return nil
}
func (s *recordingAuditStore) HasEvent(string) (bool, error) { return false, nil }
func (s *recordingAuditStore) HasAllowedOperation(string, string, string, string) (bool, error) {
	return true, nil
}
func (s *recordingAuditStore) Ready(context.Context) error { return nil }

func TestAuthZENEvaluationEndpoint(t *testing.T) {
	_, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	engine, err := cedaradapter.New(filepath.Join("..", "..", "policies", "agent-tools.cedar"))
	if err != nil {
		t.Fatal(err)
	}
	auditStore := &recordingAuditStore{}
	server := httptest.NewServer(New(engine, privateKey, "test", "bap-edge", time.Minute, nil, auditStore, "test-api-key", "test-user").Handler())
	defer server.Close()

	request := authzen.EvaluationRequest{
		Subject: authzen.Entity{Type: "agent", ID: "claude-code-local"}, Action: authzen.Action{Name: "file.read"},
		Resource: authzen.Entity{Type: "tool-invocation", ID: "safe", Properties: map[string]any{
			"tool": "Read", "target": "safe.go", "path": "safe.go", "command": "",
			"protected": false, "outsideWorkspace": false, "destructive": false,
		}}, Context: map[string]any{"session_id": "test-session"},
	}
	body, _ := json.Marshal(request)
	httpRequest, _ := http.NewRequest(http.MethodPost, server.URL+"/access/v1/evaluation", bytes.NewReader(body))
	httpRequest.Header.Set("Content-Type", "application/json")
	httpRequest.Header.Set("Authorization", "Bearer test-api-key")
	httpRequest.Header.Set("traceparent", "00-11111111111111111111111111111111-2222222222222222-01")
	response, err := http.DefaultClient.Do(httpRequest)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	var decision authzen.Decision
	if err := json.NewDecoder(response.Body).Decode(&decision); err != nil {
		t.Fatal(err)
	}
	if !decision.Decision || decision.Context["grant"] == "" {
		t.Fatalf("expected allow with grant, got %#v", decision)
	}
	if response.Header.Get("X-Trace-ID") != "11111111111111111111111111111111" {
		t.Fatalf("unexpected response trace ID %q", response.Header.Get("X-Trace-ID"))
	}
	if len(auditStore.events) != 1 || auditStore.events[0].TraceID != "11111111111111111111111111111111" || auditStore.events[0].ParentSpanID != "2222222222222222" {
		t.Fatalf("trace context was not persisted in audit: %#v", auditStore.events)
	}

	requestHash, _ := grants.HashRequest(request)
	issuedGrant, _ := decision.Context["grant"].(string)
	claims, err := grants.Verify(privateKey.Public().(ed25519.PublicKey), issuedGrant, "bap-edge", requestHash, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	claims.PolicyVersion = "stale-policy-version"
	staleGrant, err := grants.Sign(privateKey, claims)
	if err != nil {
		t.Fatal(err)
	}
	consumptionBody, _ := json.Marshal(auditwire.GrantConsumption{Request: request, Grant: staleGrant})
	consumptionRequest, _ := http.NewRequest(http.MethodPost, server.URL+"/bap/v1/audit/grant-consumption", bytes.NewReader(consumptionBody))
	consumptionRequest.Header.Set("Content-Type", "application/json")
	consumptionRequest.Header.Set("Authorization", "Bearer test-api-key")
	consumptionResponse, err := http.DefaultClient.Do(consumptionRequest)
	if err != nil {
		t.Fatal(err)
	}
	defer consumptionResponse.Body.Close()
	if consumptionResponse.StatusCode != http.StatusForbidden {
		t.Fatalf("stale-policy grant status=%d want=403", consumptionResponse.StatusCode)
	}

	metricsResponse, err := http.Get(server.URL + "/metrics")
	if err != nil {
		t.Fatal(err)
	}
	defer metricsResponse.Body.Close()
	metricsBody, _ := io.ReadAll(metricsResponse.Body)
	if !bytes.Contains(metricsBody, []byte(`bap_authorization_decisions_total{decision="allow"`)) {
		t.Fatalf("authorization metric missing:\n%s", metricsBody)
	}
}

func TestReadinessErrorsAreRateLimitedAndResetOnRecovery(t *testing.T) {
	service := &Server{metrics: metrics.New()}
	request := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	service.noteReadinessFailure(request, context.DeadlineExceeded)
	service.noteReadinessFailure(request, context.DeadlineExceeded)
	service.noteReadinessFailure(request, context.DeadlineExceeded)
	if service.readinessSuppressed != 2 || !service.readinessFailed {
		t.Fatalf("unexpected readiness limiter state: suppressed=%d failed=%t", service.readinessSuppressed, service.readinessFailed)
	}
	service.noteReadinessReady(request)
	if service.readinessSuppressed != 0 || service.readinessFailed {
		t.Fatal("readiness recovery did not reset limiter state")
	}
}
