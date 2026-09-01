package server

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"bap-system/bap-service/internal/agentsts"
	"bap-system/bap-service/internal/audit"
	"bap-system/bap-service/internal/cedaradapter"
	"bap-system/bap-service/internal/metrics"
	"bap-system/internal/agentgrant"
	"bap-system/internal/auditwire"
	"bap-system/internal/authzen"
	"bap-system/internal/grants"
	"bap-system/internal/policybundle"
)

type recordingAuditStore struct{ events []audit.Event }

func (s *recordingAuditStore) Append(event audit.Event) error {
	s.events = append(s.events, event)
	return nil
}

func TestPolicySyncAuthenticationAndDirectives(t *testing.T) {
	_, privateKey, _ := ed25519.GenerateKey(rand.Reader)
	now := time.Now().UTC()
	bundle := policybundle.Bundle{SchemaVersion: policybundle.SchemaVersion, Version: 7, RulesDigest: "sha256:rules", IssuedAt: now, ExpiresAt: now.Add(time.Hour), RefreshAfterSeconds: 60, MaxOfflineSeconds: 300, MinimumEdgeVersion: "1", RevocationEpoch: 4, ForceUpdate: true, PolicyProfile: "standard-developer", CedarPolicy: "forbid(principal, action, resource);"}
	envelope, err := policybundle.Sign(privateKey, "test", bundle)
	if err != nil {
		t.Fatal(err)
	}
	service := &Server{apiKey: "test-api-key", principal: "device", metrics: metrics.New(), audit: &recordingAuditStore{}}
	service.SetPolicyBundle(bundle, envelope)
	testServer := httptest.NewServer(service.Handler())
	defer testServer.Close()

	requestSync := func(apiKey string, installed uint64, digest string, epoch uint64) (int, policybundle.SyncResponse) {
		body, _ := json.Marshal(policybundle.SyncRequest{EdgeInstanceID: "edge-1", EdgeVersion: "1", InstalledVersion: installed, InstalledDigest: digest, RevocationEpoch: epoch, Nonce: "nonce"})
		request, _ := http.NewRequest(http.MethodPost, testServer.URL+"/bap/v1/edge/sync", bytes.NewReader(body))
		request.Header.Set("Authorization", "Bearer "+apiKey)
		request.Header.Set("Content-Type", "application/json")
		response, err := http.DefaultClient.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		var value policybundle.SyncResponse
		_ = json.NewDecoder(response.Body).Decode(&value)
		return response.StatusCode, value
	}
	if status, _ := requestSync("wrong", 0, "", 0); status != http.StatusUnauthorized {
		t.Fatalf("unauthenticated sync status=%d", status)
	}
	service.policyBundle.ForceUpdate = false
	if status, response := requestSync("test-api-key", 1, "sha256:old", 1); status != http.StatusOK || response.Directive != "UPDATE" {
		t.Fatalf("stale normal sync status=%d response=%#v", status, response)
	}
	service.policyBundle.ForceUpdate = true
	if status, response := requestSync("test-api-key", 1, "sha256:old", 1); status != http.StatusOK || response.Directive != "UPDATE_REQUIRED" {
		t.Fatalf("stale forced sync status=%d response=%#v", status, response)
	}
	if status, response := requestSync("test-api-key", 7, "sha256:rules", 4); status != http.StatusOK || response.Directive != "CURRENT" {
		t.Fatalf("current sync status=%d response=%#v", status, response)
	}
	service.policyBundle.KillSwitch = true
	if _, response := requestSync("test-api-key", 7, "sha256:rules", 4); response.Directive != "KILL_SWITCH" {
		t.Fatalf("kill switch directive=%q", response.Directive)
	}
}

func TestAgentSTSRoleExposesOnlySTSAndOperationalEndpoints(t *testing.T) {
	service := &Server{apiKey: "test-api-key", principal: "gateway", metrics: metrics.New(), role: "agent-sts"}
	handler := service.Handler()
	for _, path := range []string{"/bap/v1/edge/sync", "/access/v1/evaluation", "/bap/v1/audit/outcome"} {
		request := httptest.NewRequest(http.MethodPost, path, bytes.NewReader([]byte(`{}`)))
		request.Header.Set("Authorization", "Bearer test-api-key")
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != http.StatusNotFound {
			t.Fatalf("STS-only role exposed %s with status %d", path, response.Code)
		}
	}
	request := httptest.NewRequest(http.MethodPost, "/bap/v1/agent-sts/issue", bytes.NewReader([]byte(`{}`)))
	request.Header.Set("Authorization", "Bearer test-api-key")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code == http.StatusNotFound {
		t.Fatal("STS-only role did not expose Agent STS issuance")
	}
}

func TestAgentSTSIssueAndConsumeRequireDistinctClients(t *testing.T) {
	service := &Server{metrics: metrics.New(), role: "agent-sts"}
	if err := service.SetAgentSTSClients("edge-secret", "edge-device", "gateway-secret", "orders-gateway"); err != nil {
		t.Fatal(err)
	}
	handler := service.Handler()
	status := func(path, key string) int {
		request := httptest.NewRequest(http.MethodPost, path, bytes.NewReader([]byte(`{}`)))
		request.Header.Set("Authorization", "Bearer "+key)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		return response.Code
	}
	if got := status("/bap/v1/agent-sts/issue", "gateway-secret"); got != http.StatusUnauthorized {
		t.Fatalf("gateway credential called issue endpoint: status=%d", got)
	}
	if got := status("/bap/v1/agent-sts/consume", "edge-secret"); got != http.StatusUnauthorized {
		t.Fatalf("Edge credential called consume endpoint: status=%d", got)
	}
	if got := status("/bap/v1/agent-sts/issue", "edge-secret"); got == http.StatusUnauthorized || got == http.StatusNotFound {
		t.Fatalf("Edge credential did not authenticate to issue endpoint: status=%d", got)
	}
	if got := status("/bap/v1/agent-sts/consume", "gateway-secret"); got == http.StatusUnauthorized || got == http.StatusNotFound {
		t.Fatalf("gateway credential did not authenticate to consume endpoint: status=%d", got)
	}
	if err := service.SetAgentSTSClients("a", "same", "b", "same"); err == nil {
		t.Fatal("same principal was accepted for issue and consume")
	}
}

func TestAgentSTSIssueReturnsInvalidTargetForMissingResource(t *testing.T) {
	_, privateKey, _ := ed25519.GenerateKey(rand.Reader)
	service := &Server{
		metrics: metrics.New(), policyBundle: policybundle.Bundle{Version: 1},
		agentSTS: agentsts.New(privateKey, "issuer"),
	}
	requestBody := agentgrant.IssueRequest{
		EdgeInstanceID: "edge-1",
		Operation: authzen.EvaluationRequest{
			Subject: authzen.Entity{Type: "agent", ID: "claude"}, Action: authzen.Action{Name: "gateway.execute"},
			Resource: authzen.Entity{Type: "tool-invocation", ID: "operation", Properties: map[string]any{"tool": agentgrant.GatewayToolName}},
			Context:  map[string]any{"session_id": "session", "workload_id": "workload", "tool_use_id": "tool"},
		},
		Intent: agentgrant.IntentEvidence{SessionID: "session", WorkloadID: "workload", IntentHash: "sha256:intent", RuleIDs: []string{"intent"}, CapturedAt: time.Now().Unix()},
	}
	body, _ := json.Marshal(requestBody)
	request := httptest.NewRequest(http.MethodPost, "/bap/v1/agent-sts/issue", bytes.NewReader(body))
	request = request.WithContext(context.WithValue(request.Context(), callerContextKey{}, callerIdentity{Principal: "edge", Fingerprint: "sha256:edge"}))
	response := httptest.NewRecorder()
	service.issueAgentGrant(response, request)
	if response.Code != http.StatusBadRequest || !bytes.Contains(response.Body.Bytes(), []byte(`"error":"invalid_target"`)) {
		t.Fatalf("status=%d body=%s, want invalid_target", response.Code, response.Body.String())
	}
}

func TestMutualTLSIdentityTakesAuthorityFromVerifiedCertificate(t *testing.T) {
	service := &Server{apiKey: "bearer-is-not-required", principal: "bearer", metrics: metrics.New()}
	var caller callerIdentity
	handler := service.authenticate(http.HandlerFunc(func(_ http.ResponseWriter, request *http.Request) {
		caller = callerFrom(request.Context())
	}))
	request := httptest.NewRequest(http.MethodPost, "/bap/v1/edge/sync", nil)
	request.TLS = &tls.ConnectionState{PeerCertificates: []*x509.Certificate{{Raw: []byte("device-certificate"), Subject: pkix.Name{CommonName: "device-42"}}}}
	handler.ServeHTTP(httptest.NewRecorder(), request)
	if caller.Principal != "device-42" || caller.Fingerprint == "" {
		t.Fatalf("mTLS identity was not derived from certificate: %#v", caller)
	}
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
	service := New(engine, privateKey, "test", "bap-edge", time.Minute, nil, auditStore, "test-api-key", "test-user")
	service.legacyAuthZEN = true // internal coverage only; production handlers leave this false
	server := httptest.NewServer(service.Handler())
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
