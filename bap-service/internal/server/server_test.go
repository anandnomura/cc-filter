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
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"bap-system/bap-service/internal/agentsts"
	"bap-system/bap-service/internal/audit"
	"bap-system/bap-service/internal/metrics"
	"bap-system/internal/agentgrant"
	"bap-system/internal/auditwire"
	"bap-system/internal/authzen"
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
	bundle := policybundle.Bundle{SchemaVersion: policybundle.SchemaVersion, Version: 7, RulesDigest: "sha256:rules", IssuedAt: now, ExpiresAt: now.Add(time.Hour), RefreshAfterSeconds: 60, MaxOfflineSeconds: 300, MinimumEdgeVersion: "1", RevocationEpoch: 4, ForceUpdate: true, EnforcementMode: "enforce", PolicyProfile: "standard-developer", CedarPolicy: "forbid(principal, action, resource);"}
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
		Intent: agentgrant.IntentEvidence{IntentID: "intent-1", SessionID: "session", WorkloadID: "workload", IntentHash: "sha256:intent", RuleIDs: []string{"intent"}, CapturedAt: time.Now().Unix()},
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

func TestMutualTLSPrincipalRegistryRejectsUnenrolledCertificate(t *testing.T) {
	service := &Server{metrics: metrics.New()}
	if err := service.SetMTLSPrincipals([]string{"edge-allowed"}); err != nil {
		t.Fatal(err)
	}
	called := false
	handler := service.authenticate(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { called = true }))
	request := httptest.NewRequest(http.MethodPost, "/bap/v1/edge/sync", nil)
	request.TLS = &tls.ConnectionState{PeerCertificates: []*x509.Certificate{{Raw: []byte("device-certificate"), Subject: pkix.Name{CommonName: "edge-revoked"}}}}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if called || response.Code != http.StatusUnauthorized {
		t.Fatalf("unenrolled certificate called handler=%t status=%d", called, response.Code)
	}
}
func (s *recordingAuditStore) HasEvent(string) (bool, error) { return false, nil }
func (s *recordingAuditStore) HasAllowedOperation(string, string, string, string) (bool, error) {
	return true, nil
}
func (s *recordingAuditStore) Ready(context.Context) error { return nil }

func TestLegacyAuthZENEndpointsArePermanentlyAbsent(t *testing.T) {
	service := &Server{apiKey: "test-api-key", principal: "device", metrics: metrics.New()}
	handler := service.Handler()
	for _, test := range []struct {
		method string
		path   string
	}{
		{http.MethodGet, "/.well-known/authzen-configuration"},
		{http.MethodPost, "/access/v1/evaluation"},
		{http.MethodPost, "/bap/v1/audit/grant-consumption"},
	} {
		request := httptest.NewRequest(test.method, test.path, bytes.NewReader([]byte(`{}`)))
		request.Header.Set("Authorization", "Bearer test-api-key")
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != http.StatusNotFound {
			t.Fatalf("removed legacy endpoint %s returned %d", test.path, response.Code)
		}
	}
}

func TestShadowDecisionPersistsEvaluatedAndEffectiveOutcomes(t *testing.T) {
	store := &recordingAuditStore{}
	service := New(nil, "issuer", store, "test-api-key", "pilot-edge")
	decision := auditwire.EdgeDecision{
		EventID: "shadow-decision-1",
		Request: authzen.EvaluationRequest{
			Subject:  authzen.Entity{Type: "agent", ID: "claude"},
			Action:   authzen.Action{Name: "command.execute"},
			Resource: authzen.Entity{Type: "tool-invocation", ID: "operation", Properties: map[string]any{"tool": "Bash", "command": "redacted"}},
			Context:  map[string]any{"session_id": "session", "workload_id": "workload", "tool_use_id": "tool-use"},
		},
		Allowed: true, ReasonCode: "SHADOW_ALLOW", EvaluatedAllowed: false,
		EvaluatedReasonCode: "MANUAL_EXECUTION_REQUIRED", EnforcementMode: "shadow",
		PolicyVersion: "bundle:9:digest", BundleVersion: 9, BundleDigest: "sha256:digest",
	}
	body, _ := json.Marshal(decision)
	request := httptest.NewRequest(http.MethodPost, "/bap/v1/audit/edge-decision", bytes.NewReader(body))
	request.Header.Set("Authorization", "Bearer test-api-key")
	response := httptest.NewRecorder()
	service.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusOK || len(store.events) != 1 {
		t.Fatalf("shadow audit status=%d events=%d", response.Code, len(store.events))
	}
	event := store.events[0]
	if event.Allowed == nil || !*event.Allowed || event.EvaluatedAllowed == nil || *event.EvaluatedAllowed || event.EnforcementMode != "shadow" || event.EvaluatedReasonCode != "MANUAL_EXECUTION_REQUIRED" {
		t.Fatalf("shadow evidence was not preserved: %#v", event)
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
