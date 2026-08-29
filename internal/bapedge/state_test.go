package bapedge

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"cc-filter/internal/auditwire"
)

func TestSessionStoreKeepsOneWorkloadPerClaudeSession(t *testing.T) {
	store, err := NewSessionStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	first, err := store.LoadOrCreate("claude-session")
	if err != nil {
		t.Fatal(err)
	}
	second, err := store.LoadOrCreate("claude-session")
	if err != nil || first != second {
		t.Fatalf("workload ID changed within a session: %q != %q, err=%v", first, second, err)
	}
	if err := store.Remove("claude-session"); err != nil {
		t.Fatal(err)
	}
	third, _ := store.LoadOrCreate("claude-session")
	if third == first {
		t.Fatal("new session state reused the retired workload ID")
	}
}

func TestAuditSpoolRetriesAndDeletesOnlyAfterAcknowledgement(t *testing.T) {
	directory := t.TempDir()
	spool, err := NewAuditSpool(directory)
	if err != nil {
		t.Fatal(err)
	}
	value := auditwire.Outcome{EventID: "event", SessionID: "s", WorkloadID: "w", ToolUseID: "t", Tool: "Read", Outcome: "success"}
	if err := spool.QueueOutcome(value); err != nil {
		t.Fatal(err)
	}
	var received atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer key" {
			t.Errorf("missing API authentication")
		}
		received.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	client := &Client{baseURL: server.URL, apiKey: "key", http: server.Client()}
	if err := spool.Flush(context.Background(), client); err != nil {
		t.Fatal(err)
	}
	if received.Load() != 1 {
		t.Fatalf("expected one retried event, got %d", received.Load())
	}
	entries, err := os.ReadDir(filepath.Join(directory, "audit-spool"))
	if err != nil || len(entries) != 0 {
		t.Fatalf("acknowledged event remained in spool: entries=%d err=%v", len(entries), err)
	}
}
