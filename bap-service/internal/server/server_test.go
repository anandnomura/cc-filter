package server

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"cc-filter/bap-service/internal/cedaradapter"
	"cc-filter/internal/authzen"
)

func TestAuthZENEvaluationEndpoint(t *testing.T) {
	_, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	engine, err := cedaradapter.New(filepath.Join("..", "..", "policies", "agent-tools.cedar"))
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(New(engine, privateKey, "test", "bap-edge", time.Minute, nil, nil, "test-api-key", "test-user").Handler())
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
}
