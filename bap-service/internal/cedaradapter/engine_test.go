package cedaradapter

import (
	"path/filepath"
	"testing"

	"cc-filter/internal/authzen"
)

func TestCedarDecisions(t *testing.T) {
	engine, err := New(filepath.Join("..", "..", "policies", "agent-tools.cedar"))
	if err != nil {
		t.Fatal(err)
	}
	base := authzen.EvaluationRequest{
		Subject: authzen.Entity{Type: "agent", ID: "claude-code-local"},
		Action:  authzen.Action{Name: "file.read"},
		Resource: authzen.Entity{Type: "tool-invocation", ID: "one", Properties: map[string]any{
			"tool": "Read", "target": "src/app.go", "path": "src/app.go", "command": "",
			"protected": false, "outsideWorkspace": false, "destructive": false,
		}},
	}
	allowed, _, _, err := engine.Authorize(base)
	if err != nil || !allowed {
		t.Fatalf("safe workspace read should be allowed: allowed=%t err=%v", allowed, err)
	}
	base.Resource.Properties["protected"] = true
	allowed, _, code, err := engine.Authorize(base)
	if err != nil || allowed {
		t.Fatalf("protected resource should be denied: allowed=%t err=%v", allowed, err)
	}
	if code != "EXPLICIT_FORBID" {
		t.Fatalf("protected resource code = %q, want EXPLICIT_FORBID", code)
	}
	base.Resource.Properties["protected"] = false
	base.Action.Name = "tool.invoke"
	allowed, _, code, err = engine.Authorize(base)
	if err != nil || allowed {
		t.Fatalf("unknown action should default deny: allowed=%t err=%v", allowed, err)
	}
	if code != "NO_MATCHING_POLICY" {
		t.Fatalf("unknown action code = %q, want NO_MATCHING_POLICY", code)
	}
}
