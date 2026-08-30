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
	base.Action.Name = "tool.unknown"
	allowed, _, code, err = engine.Authorize(base)
	if err != nil || allowed {
		t.Fatalf("unknown action should be explicitly denied: allowed=%t err=%v", allowed, err)
	}
	if code != "EXPLICIT_FORBID" {
		t.Fatalf("unknown action code = %q, want EXPLICIT_FORBID", code)
	}

	base.Action.Name = "network.fetch"
	allowed, _, code, err = engine.Authorize(base)
	if err != nil || allowed || code != "EXPLICIT_FORBID" {
		t.Fatalf("unregistered network fetch should be explicitly denied: allowed=%t code=%q err=%v", allowed, code, err)
	}
	base.Resource.Properties["approvedNetwork"] = true
	allowed, _, code, err = engine.Authorize(base)
	if err != nil || !allowed || code != "POLICY_PERMIT" {
		t.Fatalf("registered network fetch should be allowed: allowed=%t code=%q err=%v", allowed, code, err)
	}
	base.Resource.Properties["approvedNetwork"] = false

	base.Action.Name = "file.write"
	base.Resource.Properties["securityControl"] = true
	allowed, _, code, err = engine.Authorize(base)
	if err != nil || allowed || code != "EXPLICIT_FORBID" {
		t.Fatalf("security-control write should be explicitly denied: allowed=%t code=%q err=%v", allowed, code, err)
	}

	base.Resource.Properties["securityControl"] = false
	for _, risk := range []string{"destructive", "privileged", "exfiltration", "obfuscated"} {
		base.Action.Name = "command.execute"
		base.Resource.Properties[risk] = true
		allowed, _, code, err = engine.Authorize(base)
		if err != nil || allowed || code != "EXPLICIT_FORBID" {
			t.Fatalf("%s command should be explicitly denied: allowed=%t code=%q err=%v", risk, allowed, code, err)
		}
		base.Resource.Properties[risk] = false
	}

	for _, action := range []string{"mcp.invoke", "agent.delegate"} {
		base.Action.Name = action
		allowed, _, code, err = engine.Authorize(base)
		if err != nil || allowed || code != "EXPLICIT_FORBID" {
			t.Fatalf("%s should be explicitly denied: allowed=%t code=%q err=%v", action, allowed, code, err)
		}
	}
	base.Action.Name = "mcp.invoke"
	base.Resource.Properties["approvedMCP"] = true
	allowed, _, code, err = engine.Authorize(base)
	if err != nil || !allowed || code != "POLICY_PERMIT" {
		t.Fatalf("approved MCP should permit: allowed=%t code=%q err=%v", allowed, code, err)
	}
	base.Resource.Properties["approvedMCP"] = false
	base.Action.Name = "agent.delegate"
	base.Resource.Properties["approvedDelegate"] = true
	allowed, _, code, err = engine.Authorize(base)
	if err != nil || !allowed || code != "POLICY_PERMIT" {
		t.Fatalf("approved delegation should permit: allowed=%t code=%q err=%v", allowed, code, err)
	}
	base.Resource.Properties["approvedDelegate"] = false

	base.Action.Name = "command.execute"
	base.Resource.Properties["shellApproved"] = true
	allowed, _, code, err = engine.Authorize(base)
	if err != nil || !allowed || code != "POLICY_PERMIT" {
		t.Fatalf("classified safe shell should permit: allowed=%t code=%q err=%v", allowed, code, err)
	}
	base.Resource.Properties["policyProfile"] = "read-only"
	allowed, _, code, err = engine.Authorize(base)
	if err != nil || allowed || code != "EXPLICIT_FORBID" {
		t.Fatalf("read-only shell should forbid: allowed=%t code=%q err=%v", allowed, code, err)
	}
	base.Resource.Properties["policyProfile"] = "standard-developer"

	base.Action.Name = "network.search"
	allowed, _, code, err = engine.Authorize(base)
	if err != nil || !allowed || code != "POLICY_PERMIT" {
		t.Fatalf("network search should be allowed: allowed=%t code=%q err=%v", allowed, code, err)
	}
}
