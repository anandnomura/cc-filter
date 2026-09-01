package proposals

import (
	"path/filepath"
	"testing"

	"bap-system/internal/authzen"
)

func TestProposalStoreDeduplicatesWithoutSensitiveTargets(t *testing.T) {
	path := filepath.Join(t.TempDir(), "proposals.jsonl")
	store := New(path)
	request := authzen.EvaluationRequest{
		Subject: authzen.Entity{Type: "agent", ID: "sensitive-user-id"},
		Action:  authzen.Action{Name: "tool.invoke"},
		Resource: authzen.Entity{Type: "tool-invocation", ID: "hashed", Properties: map[string]any{
			"tool": "UnknownTool", "target": "do-not-store-this", "command": "secret command", "path": "secret path",
		}},
	}
	first, err := store.Record(request)
	if err != nil {
		t.Fatal(err)
	}
	second, err := store.Record(request)
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatal("equivalent proposals should have stable IDs")
	}
	summaries, err := Summarize(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(summaries) != 1 || summaries[0].Occurrences != 2 {
		t.Fatalf("unexpected summaries: %#v", summaries)
	}
}
