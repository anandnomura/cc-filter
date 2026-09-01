// bap-mock-resources is a local-only protected backend used by the resource
// PEP acceptance demo. It refuses direct calls that do not carry the identities
// owned by the API or MCP PEP.
package main

import (
	"encoding/json"
	"flag"
	"log"
	"net/http"
	"os"
	"sync/atomic"
	"time"
)

func main() {
	address := flag.String("listen", "127.0.0.1:19090", "mock protected-resource address")
	flag.Parse()
	apiKey, mcpKey := os.Getenv("BAP_ORDERS_BACKEND_API_KEY"), os.Getenv("BAP_MCP_UPSTREAM_API_KEY")
	if apiKey == "" || mcpKey == "" {
		log.Fatal("mock resource PEP identities are required")
	}
	var apiCalls, mcpCalls atomic.Int64
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		write(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	mux.HandleFunc("GET /state", func(w http.ResponseWriter, _ *http.Request) {
		write(w, http.StatusOK, map[string]int64{"api_calls": apiCalls.Load(), "mcp_calls": mcpCalls.Load()})
	})
	mux.HandleFunc("POST /orders/deploy", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+apiKey || r.Header.Get("Idempotency-Key") == "" {
			write(w, http.StatusUnauthorized, map[string]string{"error": "api_pep_identity_required"})
			return
		}
		var body map[string]any
		if json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&body) != nil || body["release"] == nil {
			write(w, http.StatusBadRequest, map[string]string{"error": "invalid_request"})
			return
		}
		apiCalls.Add(1)
		write(w, http.StatusOK, map[string]any{"deployed": true, "service": "orders", "environment": "staging", "release": body["release"]})
	})
	mux.HandleFunc("POST /mcp", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+mcpKey || r.Header.Get("X-BAP-Grant-ID") == "" {
			write(w, http.StatusUnauthorized, map[string]string{"error": "mcp_pep_identity_required"})
			return
		}
		var request struct {
			JSONRPC string `json:"jsonrpc"`
			ID      any    `json:"id"`
			Method  string `json:"method"`
			Params  struct {
				Name      string         `json:"name"`
				Arguments map[string]any `json:"arguments"`
			} `json:"params"`
		}
		if json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&request) != nil || request.Method != "tools/call" || request.Params.Name != "change_create" {
			write(w, http.StatusBadRequest, map[string]string{"error": "invalid_mcp_request"})
			return
		}
		if request.Params.Arguments["_bap_agent_grant"] != nil || request.Params.Arguments["_bap_operation"] != nil {
			write(w, http.StatusBadRequest, map[string]string{"error": "bap_transport_leak"})
			return
		}
		mcpCalls.Add(1)
		write(w, http.StatusOK, map[string]any{"jsonrpc": "2.0", "id": request.ID, "result": map[string]any{"content": []any{map[string]any{"type": "text", "text": "CHG001 created for orders staging"}}, "structuredContent": map[string]any{"change_id": "CHG001", "created": true}}})
	})
	server := &http.Server{Addr: *address, Handler: mux, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 10 * time.Second, IdleTimeout: 30 * time.Second}
	log.Printf("mock protected resources listening on %s", *address)
	log.Fatal(server.ListenAndServe())
}

func write(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
