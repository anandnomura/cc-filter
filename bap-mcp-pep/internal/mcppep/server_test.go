package mcppep

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"bap-system/internal/authzen"
)

func TestProtectedToolConsumesGrantAndUsesPEPIdentity(t *testing.T) {
	var consumes, upstreamCalls atomic.Int32
	sts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		consumes.Add(1)
		if r.Header.Get("Authorization") != "Bearer sts-secret" {
			t.Error("STS did not receive the MCP PEP identity")
		}
		writeJSON(w, http.StatusOK, map[string]any{"consumed": true, "grant_id": "ag_once"})
	}))
	defer sts.Close()
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamCalls.Add(1)
		if r.Header.Get("Authorization") != "Bearer upstream-secret" || r.Header.Get("X-BAP-Grant-ID") != "ag_once" {
			t.Error("upstream did not receive the protected PEP identity and idempotency key")
		}
		var request map[string]any
		_ = json.NewDecoder(r.Body).Decode(&request)
		params := request["params"].(map[string]any)
		arguments := params["arguments"].(map[string]any)
		if arguments["_bap_agent_grant"] != nil || arguments["_bap_operation"] != nil {
			t.Error("BAP transport fields reached the upstream MCP server")
		}
		writeJSON(w, http.StatusOK, map[string]any{"jsonrpc": "2.0", "id": request["id"], "result": map[string]any{"content": []any{map[string]any{"type": "text", "text": "CHG001 created"}}}})
	}))
	defer upstream.Close()

	server := newTestServer(t, sts.URL, upstream.URL)
	response := invoke(t, server, validCall(t, false))
	if response.Code != http.StatusOK || consumes.Load() != 1 || upstreamCalls.Load() != 1 {
		t.Fatalf("status=%d consumes=%d upstream=%d body=%s", response.Code, consumes.Load(), upstreamCalls.Load(), response.Body.String())
	}
}

func TestChangedArgumentsNeverReachSTSOrUpstream(t *testing.T) {
	var calls atomic.Int32
	downstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { calls.Add(1); w.WriteHeader(http.StatusForbidden) }))
	defer downstream.Close()
	server := newTestServer(t, downstream.URL, downstream.URL)
	response := invoke(t, server, validCall(t, true))
	if response.Code != http.StatusOK || calls.Load() != 0 || !contains(response.Body.String(), "MCP arguments do not match") {
		t.Fatalf("tamper was not denied before downstream: calls=%d body=%s", calls.Load(), response.Body.String())
	}
}

func TestWrongOriginIsDenied(t *testing.T) {
	server := newTestServer(t, "http://127.0.0.1:1", "http://127.0.0.1:1")
	request := httptest.NewRequest(http.MethodPost, "/mcp", validCall(t, false))
	request.Header.Set("Origin", "https://evil.example")
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusForbidden {
		t.Fatalf("wrong origin status=%d", response.Code)
	}
}

func newTestServer(t *testing.T, stsURL, upstreamURL string) *Server {
	t.Helper()
	t.Setenv("TEST_MCP_STS_KEY", "sts-secret")
	t.Setenv("TEST_MCP_UPSTREAM_KEY", "upstream-secret")
	config := Config{ListenAddress: "127.0.0.1:0", STSURL: stsURL, STSAPIKeyEnv: "TEST_MCP_STS_KEY", UpstreamURL: upstreamURL, UpstreamKeyEnv: "TEST_MCP_UPSTREAM_KEY", AllowedOrigins: []string{"https://claude.company.example"}, Tools: []ToolPolicy{{PublicName: "change_create", UpstreamName: "change_create", ClaudeToolName: "mcp__bap_mcp_pep__change_create", MCPServer: "bap_mcp_pep", Description: "test", InputSchema: map[string]any{"type": "object"}, RequiredArguments: []string{"release"}, ExactArguments: map[string]any{"service": "orders", "environment": "staging"}}}}
	server, err := New(config)
	if err != nil {
		t.Fatal(err)
	}
	return server
}

func validCall(t *testing.T, tamper bool) *bytes.Reader {
	t.Helper()
	arguments := map[string]any{"service": "orders", "environment": "staging", "release": "2026.08", "summary": "release"}
	data, _ := json.Marshal(arguments)
	sum := sha256.Sum256(data)
	operation := authzen.EvaluationRequest{Subject: authzen.Entity{Type: "agent", ID: "claude-code-local"}, Action: authzen.Action{Name: "mcp.invoke"}, Resource: authzen.Entity{Type: "tool-invocation", ID: "resource", Properties: map[string]any{"tool": "mcp__bap_mcp_pep__change_create", "target": "mcp__bap_mcp_pep__change_create", "mcpServer": "bap_mcp_pep", "mcpTool": "change_create", "argumentsDigest": "sha256:" + hex.EncodeToString(sum[:])}}, Context: map[string]any{"session_id": "session", "workload_id": "workload", "tool_use_id": "tool"}}
	if tamper {
		arguments["release"] = "changed"
	}
	arguments["_bap_agent_grant"] = "opaque"
	arguments["_bap_operation"] = operation
	payload, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": "tools/call", "params": map[string]any{"name": "change_create", "arguments": arguments}})
	return bytes.NewReader(payload)
}

func invoke(t *testing.T, server *Server, body io.Reader) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(http.MethodPost, "/mcp", body)
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	return response
}
func contains(value, part string) bool { return strings.Contains(value, part) }
