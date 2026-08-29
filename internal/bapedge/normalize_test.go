package bapedge

import (
	"path/filepath"
	"testing"
)

func TestNormalizeFineGrainedOperations(t *testing.T) {
	workspace := t.TempDir()
	tests := []struct {
		name        string
		input       HookInput
		action      string
		protected   bool
		outside     bool
		destructive bool
	}{
		{"workspace read", HookInput{CWD: workspace, ToolName: "Read", ToolInput: map[string]any{"file_path": "src/app.go"}}, "file.read", false, false, false},
		{"secret write", HookInput{CWD: workspace, ToolName: "Write", ToolInput: map[string]any{"file_path": ".env"}}, "file.write", true, false, false},
		{"outside read", HookInput{CWD: workspace, ToolName: "Read", ToolInput: map[string]any{"file_path": filepath.Join(workspace, "..", "other.txt")}}, "file.read", false, true, false},
		{"destructive shell", HookInput{CWD: workspace, ToolName: "Bash", ToolInput: map[string]any{"command": "git reset --hard"}}, "command.execute", false, false, true},
		{"unknown tool", HookInput{CWD: workspace, ToolName: "Surprise", ToolInput: map[string]any{}}, "tool.invoke", false, false, false},
		{"mcp tool", HookInput{CWD: workspace, ToolName: "mcp__github__search_repositories", ToolInput: map[string]any{}}, "mcp.invoke", false, false, false},
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
				request.Resource.Properties["destructive"] != test.destructive {
				t.Fatalf("unexpected risk properties: %#v", request.Resource.Properties)
			}
		})
	}
}
