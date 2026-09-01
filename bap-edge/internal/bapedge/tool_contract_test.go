package bapedge

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"bap-system/internal/policybundle"
)

type toolContractCase struct {
	Name             string         `json:"name"`
	Tool             string         `json:"tool"`
	Input            map[string]any `json:"input"`
	Action           string         `json:"action"`
	Decision         string         `json:"decision"`
	Error            bool           `json:"error"`
	ShellApproved    bool           `json:"shell_approved"`
	Destructive      bool           `json:"destructive"`
	Obfuscated       bool           `json:"obfuscated"`
	ApprovedNetwork  bool           `json:"approved_network"`
	ApprovedMCP      bool           `json:"approved_mcp"`
	ApprovedDelegate bool           `json:"approved_delegate"`
	MCPServer        string         `json:"mcp_server"`
	MCPTool          string         `json:"mcp_tool"`
}

func TestMVP0ToolContractCorpus(t *testing.T) {
	raw, err := os.ReadFile("testdata/mvp0-tool-contract.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []toolContractCase
	if err := json.Unmarshal(raw, &cases); err != nil {
		t.Fatal(err)
	}
	workspace := t.TempDir()
	now := time.Date(2026, 8, 30, 12, 0, 0, 0, time.UTC)
	sourceData, err := os.ReadFile(filepath.Join("..", "..", "..", "bap-service", "policies", "edge-policy-source.json"))
	if err != nil {
		t.Fatal(err)
	}
	source, err := policybundle.LoadSource(sourceData)
	if err != nil {
		t.Fatal(err)
	}
	cedarPolicy, err := os.ReadFile(filepath.Join("..", "..", "..", "bap-service", "policies", "agent-tools.cedar"))
	if err != nil {
		t.Fatal(err)
	}
	bundle, err := policybundle.Build(source, cedarPolicy, now)
	if err != nil {
		t.Fatal(err)
	}
	policy := NormalizationPolicy{
		Profile: "standard-developer", AllowedNetworkDomains: []string{"*.example.test"},
		ApprovedMCPTools: []string{"mcp__github__search_code"}, ApprovedSubagentTypes: []string{"Explore"},
	}
	for _, test := range cases {
		t.Run(test.Name, func(t *testing.T) {
			request, normalizeErr := NormalizeWithPolicy(HookInput{CWD: workspace, ToolName: test.Tool, ToolInput: test.Input}, "claude-code-local", "fixture", policy)
			if test.Error {
				if normalizeErr == nil {
					t.Fatal("expected fail-closed normalization error")
				}
				return
			}
			if normalizeErr != nil {
				t.Fatal(normalizeErr)
			}
			if request.Action.Name != test.Action {
				t.Fatalf("action=%q want=%q", request.Action.Name, test.Action)
			}
			if test.Decision == "" {
				t.Fatal("fixture is missing its expected local policy decision")
			}
			decision, err := policybundle.Authorize(bundle, request, now.Add(time.Minute))
			if err != nil {
				t.Fatal(err)
			}
			actualDecision := "deny"
			if decision.Allowed {
				actualDecision = "allow"
			}
			if actualDecision != test.Decision {
				t.Fatalf("decision=%s reason=%s want=%s", actualDecision, decision.ReasonCode, test.Decision)
			}
			properties := request.Resource.Properties
			assertBool := func(name string, want bool) {
				t.Helper()
				if got, _ := properties[name].(bool); got != want {
					t.Fatalf("%s=%t want=%t", name, got, want)
				}
			}
			assertBool("shellApproved", test.ShellApproved || test.Action != "command.execute")
			assertBool("destructive", test.Destructive)
			assertBool("obfuscated", test.Obfuscated)
			assertBool("approvedNetwork", test.ApprovedNetwork)
			assertBool("approvedMCP", test.ApprovedMCP)
			assertBool("approvedDelegate", test.ApprovedDelegate)
			if properties["mcpServer"] != test.MCPServer || properties["mcpTool"] != test.MCPTool {
				t.Fatalf("unexpected MCP classification: %#v", properties)
			}
		})
	}
}
