package bapedge

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"bap-system/internal/auditwire"
	"bap-system/internal/authzen"
	"bap-system/internal/policybundle"
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

func TestSessionStoreConcurrentClaudeInstancesShareOneWorkload(t *testing.T) {
	directory := t.TempDir()
	const workers = 40
	values := make(chan string, workers)
	errors := make(chan error, workers)
	var group sync.WaitGroup
	for i := 0; i < workers; i++ {
		group.Add(1)
		go func() {
			defer group.Done()
			store, err := NewSessionStore(directory)
			if err != nil {
				errors <- err
				return
			}
			value, err := store.LoadOrCreate("shared-session")
			if err != nil {
				errors <- err
				return
			}
			values <- value
		}()
	}
	group.Wait()
	close(values)
	close(errors)
	for err := range errors {
		t.Fatal(err)
	}
	first := ""
	for value := range values {
		if first == "" {
			first = value
		}
		if value != first {
			t.Fatalf("multiple workload identities: %q and %q", first, value)
		}
	}
}

func TestSessionStoreAcrossOperatingSystemProcesses(t *testing.T) {
	directory := t.TempDir()
	const workers = 12
	values := make(chan string, workers)
	errors := make(chan error, workers)
	var group sync.WaitGroup
	for i := 0; i < workers; i++ {
		group.Add(1)
		go func() {
			defer group.Done()
			command := exec.Command(os.Args[0], "-test.run=^TestSessionStoreProcessWorker$")
			command.Env = append(os.Environ(), "BAP_SESSION_CHILD_DIR="+directory)
			output, err := command.CombinedOutput()
			if err != nil {
				errors <- fmt.Errorf("child: %w: %s", err, output)
				return
			}
			for _, line := range strings.Split(string(output), "\n") {
				if strings.HasPrefix(line, "WORKLOAD:") {
					values <- strings.TrimSpace(strings.TrimPrefix(line, "WORKLOAD:"))
					return
				}
			}
			errors <- fmt.Errorf("child returned no workload: %s", output)
		}()
	}
	group.Wait()
	close(values)
	close(errors)
	for err := range errors {
		t.Fatal(err)
	}
	first := ""
	for value := range values {
		if first == "" {
			first = value
		}
		if value != first {
			t.Fatalf("processes observed different workloads: %q and %q", first, value)
		}
	}
}

func TestSessionStoreProcessWorker(t *testing.T) {
	directory := os.Getenv("BAP_SESSION_CHILD_DIR")
	if directory == "" {
		t.Skip("helper process")
	}
	store, err := NewSessionStore(directory)
	if err != nil {
		t.Fatal(err)
	}
	workload, err := store.LoadOrCreate("process-shared-session")
	if err != nil {
		t.Fatal(err)
	}
	fmt.Println("WORKLOAD:" + workload)
}

func TestSessionBudgetIsAtomicAndSessionsAreIsolated(t *testing.T) {
	directory := t.TempDir()
	bundle := sessionTestBundle()
	now := time.Now().UTC()
	operation := sessionTestOperation("resource-1")
	store, _ := NewSessionStore(directory)
	workload, _ := store.LoadOrCreate("session-a")
	var allowed atomic.Int32
	var group sync.WaitGroup
	for i := 0; i < 20; i++ {
		group.Add(1)
		go func(i int) {
			defer group.Done()
			other, _ := NewSessionStore(directory)
			reservation, err := other.ReserveOperation("session-a", workload, fmt.Sprintf("tool-%d", i), operation, bundle, now)
			if err == nil && reservation.Decision.Allowed {
				allowed.Add(1)
			}
		}(i)
	}
	group.Wait()
	if allowed.Load() != 3 {
		t.Fatalf("atomic budget allowed %d operations, want 3", allowed.Load())
	}
	otherWorkload, _ := store.LoadOrCreate("session-b")
	reservation, err := store.ReserveOperation("session-b", otherWorkload, "tool-isolated", operation, bundle, now)
	if err != nil || !reservation.Decision.Allowed {
		t.Fatalf("separate session inherited budget: %+v %v", reservation, err)
	}
}

func TestSessionCompositionPolicyAndFailureOutcome(t *testing.T) {
	store, _ := NewSessionStore(t.TempDir())
	workload, _ := store.LoadOrCreate("session")
	bundle := sessionTestBundle()
	bundle.SessionPolicy.CompositionRules = []policybundle.SessionCompositionRule{{ID: "forbid-repeat", PriorCapabilities: []string{"deploy"}, CurrentCapabilities: []string{"deploy"}, WithinSeconds: 300, Reason: "review between deploys", Owner: "security", Approval: "ticket"}}
	now := time.Now().UTC()
	first, err := store.ReserveOperation("session", workload, "first", sessionTestOperation("r"), bundle, now)
	if err != nil || !first.Reserved {
		t.Fatalf("first reservation: %+v %v", first, err)
	}
	blocked, _ := store.ReserveOperation("session", workload, "second", sessionTestOperation("r"), bundle, now.Add(time.Second))
	if blocked.Decision.Allowed || blocked.Decision.ReasonCode != "SESSION_COMPOSITION_FORBID" {
		t.Fatalf("pending sequence was not blocked: %+v", blocked)
	}
	if err := store.CompleteOperation("session", workload, "first", "failure"); err != nil {
		t.Fatal(err)
	}
	afterFailure, err := store.ReserveOperation("session", workload, "third", sessionTestOperation("r"), bundle, now.Add(2*time.Second))
	if err != nil || !afterFailure.Decision.Allowed {
		t.Fatalf("failed action incorrectly accrued capability: %+v %v", afterFailure, err)
	}
}

func TestSessionRejectsToolReplayAndMidSessionPolicyChange(t *testing.T) {
	store, _ := NewSessionStore(t.TempDir())
	workload, _ := store.LoadOrCreate("session")
	bundle := sessionTestBundle()
	now := time.Now().UTC()
	if reservation, err := store.ReserveOperation("session", workload, "same-tool", sessionTestOperation("r"), bundle, now); err != nil || !reservation.Reserved {
		t.Fatalf("initial reserve: %+v %v", reservation, err)
	}
	if _, err := store.ReserveOperation("session", workload, "same-tool", sessionTestOperation("r"), bundle, now); err == nil {
		t.Fatal("duplicate tool_use_id was accepted")
	}
	changed := bundle
	changed.Version++
	changed.RulesDigest = "sha256:changed"
	if _, err := store.ReserveOperation("session", workload, "new-tool", sessionTestOperation("r"), changed, now); err == nil {
		t.Fatal("mid-session policy reclassified accumulated capabilities")
	}
}

func TestSessionIdleAndLifetimeLimitsFailClosed(t *testing.T) {
	store, _ := NewSessionStore(t.TempDir())
	workload, _ := store.LoadOrCreate("session")
	now := time.Now().UTC()
	if err := store.mutateWithTouch("session", false, false, func(state *sessionState) error {
		state.CreatedAt = now.Add(-2 * time.Hour)
		state.LastActivity = now.Add(-20 * time.Minute)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	bundle := sessionTestBundle()
	if _, err := store.ReserveOperation("session", workload, "tool", sessionTestOperation("r"), bundle, now); err == nil {
		t.Fatal("expired session was renewed by a new operation")
	}
}

func sessionTestBundle() policybundle.Bundle {
	return policybundle.Bundle{Version: 8, RulesDigest: "sha256:test", PolicyProfile: "standard-developer", SessionPolicy: policybundle.SessionPolicy{MaxEvents: 100, MaxLifetimeSeconds: 3600, IdleTimeoutSeconds: 600, Capabilities: []policybundle.SessionCapability{{ID: "deploy", Actions: []string{"gateway.execute"}, Tools: []string{"gateway"}, Owner: "test", Approval: "test"}}, BudgetRules: []policybundle.SessionBudgetRule{{ID: "budget", Capabilities: []string{"deploy"}, MaxCount: 3, WindowSeconds: 300, Scope: "resource", Reason: "budget", Owner: "test", Approval: "test"}}}}
}

func sessionTestOperation(resource string) authzen.EvaluationRequest {
	return authzen.EvaluationRequest{Action: authzen.Action{Name: "gateway.execute"}, Resource: authzen.Entity{ID: resource, Properties: map[string]any{"tool": "gateway"}}}
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

func TestAuditSpoolEnforcesBoundsWithoutDeletingEvidence(t *testing.T) {
	directory := t.TempDir()
	spool, err := NewAuditSpool(directory)
	if err != nil {
		t.Fatal(err)
	}
	spool.maxEntries = 1
	first := auditwire.Outcome{EventID: "first", Tool: "Read", Outcome: "success"}
	second := auditwire.Outcome{EventID: "second", Tool: "Read", Outcome: "success"}
	if err := spool.QueueOutcome(first); err != nil {
		t.Fatal(err)
	}
	if err := spool.QueueOutcome(second); err == nil {
		t.Fatal("spool accepted an event past its configured bound")
	}
	stats, err := spool.Stats(time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if stats.Depth != 1 || stats.Bytes == 0 {
		t.Fatalf("unexpected spool stats: %+v", stats)
	}
}

func TestAuditSpoolRecoversStaleClaim(t *testing.T) {
	directory := t.TempDir()
	spool, err := NewAuditSpool(directory)
	if err != nil {
		t.Fatal(err)
	}
	if err := spool.QueueOutcome(auditwire.Outcome{EventID: "event", Tool: "Read", Outcome: "success"}); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(filepath.Join(directory, "audit-spool"))
	if err != nil || len(entries) != 1 {
		t.Fatalf("queued entries=%d err=%v", len(entries), err)
	}
	queuePath := filepath.Join(directory, "audit-spool", entries[0].Name())
	claimPath := strings.TrimSuffix(queuePath, ".json") + ".sending"
	if err := os.Rename(queuePath, claimPath); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-auditClaimStaleAfter - time.Minute)
	if err := os.Chtimes(claimPath, old, old); err != nil {
		t.Fatal(err)
	}
	if _, err := NewAuditSpool(directory); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(queuePath); err != nil {
		t.Fatalf("stale claim was not recovered: %v", err)
	}
}
