package bapedge

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net"
	"net/url"
	"os"
	"os/user"
	"path/filepath"
	"regexp"
	"strings"

	"cc-filter/internal/authzen"
)

type HookInput struct {
	HookEventName string         `json:"hook_event_name"`
	SessionID     string         `json:"session_id"`
	CWD           string         `json:"cwd"`
	ToolName      string         `json:"tool_name"`
	ToolUseID     string         `json:"tool_use_id"`
	ToolInput     map[string]any `json:"tool_input"`
}

// NormalizationPolicy is administrator-controlled endpoint policy for the pilot.
// SPIFFE will replace the asserted workload portion in a later phase.
type NormalizationPolicy struct {
	Profile               string
	AllowedNetworkDomains []string
	ApprovedMCPTools      []string
	ApprovedSubagentTypes []string
}

type toolSpec struct {
	action   string
	required []string
	target   string
	path     string
	command  string
}

var toolRegistry = map[string]toolSpec{
	"Read":            {action: "file.read", required: []string{"file_path"}, target: "file_path", path: "file_path"},
	"Edit":            {action: "file.write", required: []string{"file_path", "old_string", "new_string"}, target: "file_path", path: "file_path"},
	"Write":           {action: "file.write", required: []string{"file_path", "content"}, target: "file_path", path: "file_path"},
	"MultiEdit":       {action: "file.write", required: []string{"file_path"}, target: "file_path", path: "file_path"},
	"NotebookEdit":    {action: "notebook.write", required: []string{"notebook_path"}, target: "notebook_path", path: "notebook_path"},
	"Glob":            {action: "file.search", required: []string{"pattern"}, target: "pattern", path: "path"},
	"Grep":            {action: "file.search", required: []string{"pattern"}, target: "pattern", path: "path"},
	"Search":          {action: "file.search", required: []string{"pattern"}, target: "pattern", path: "path"},
	"Bash":            {action: "command.execute", required: []string{"command"}, target: "command", command: "command"},
	"PowerShell":      {action: "command.execute", required: []string{"command"}, target: "command", command: "command"},
	"WebFetch":        {action: "network.fetch", required: []string{"url", "prompt"}, target: "url"},
	"WebSearch":       {action: "network.search", required: []string{"query"}, target: "query"},
	"Agent":           {action: "agent.delegate", required: []string{"prompt", "description", "subagent_type"}, target: "subagent_type"},
	"Task":            {action: "agent.delegate", required: []string{"prompt"}, target: "subagent_type"},
	"AskUserQuestion": {action: "user.interact", required: []string{"questions"}},
	"EnterPlanMode":   {action: "session.plan"}, "ExitPlanMode": {action: "session.plan"},
	"EnterWorktree": {action: "worktree.manage"}, "ExitWorktree": {action: "worktree.manage"},
	"LSP":                  {action: "code.intelligence", required: []string{"operation"}},
	"Skill":                {action: "skill.invoke", required: []string{"skill"}, target: "skill"},
	"ToolSearch":           {action: "tool.discover", required: []string{"query"}, target: "query"},
	"ListMcpResourcesTool": {action: "mcp.resource.list"},
	"ReadMcpResourceTool":  {action: "mcp.resource.read", required: []string{"server", "uri"}, target: "server"},
	"WaitForMcpServers":    {action: "mcp.server.wait"},
	"TaskCreate":           {action: "task.manage", required: []string{"subject", "description"}},
	"TaskGet":              {action: "task.read", required: []string{"taskId"}}, "TaskList": {action: "task.read"},
	"TaskOutput": {action: "task.read", required: []string{"task_id"}},
	"TaskUpdate": {action: "task.manage", required: []string{"taskId"}}, "TaskStop": {action: "task.control", required: []string{"task_id"}},
	"TodoWrite":  {action: "task.manage", required: []string{"todos"}},
	"CronCreate": {action: "schedule.manage"}, "CronDelete": {action: "schedule.manage"}, "CronList": {action: "schedule.read"},
	"Artifact": {action: "artifact.publish"}, "Workflow": {action: "workflow.execute"},
	"ListAgents": {action: "agent.observe"}, "SendMessage": {action: "agent.message"},
	"Monitor": {action: "agent.observe"}, "ScheduleWakeup": {action: "agent.control"},
	"RemoteTrigger": {action: "remote.trigger"}, "PushNotification": {action: "notification.send"},
	"SendUserFile": {action: "file.send"}, "ShareOnboardingGuide": {action: "user.interact"},
	"ReportFindings": {action: "findings.report"}, "SendFeedback": {action: "feedback.prepare"},
}

var (
	destructiveCommand  = regexp.MustCompile(`(?i)(^|[;&|]\s*)(rm\s+-[^\r\n]*r[^\r\n]*f|del\s+/[sqf]|remove-item[^\r\n]*(-recurse|-force)|format\s+[a-z]:|diskpart|git\s+(reset\s+--hard|clean\s+-[^\r\n]*f|push[^\r\n]*--force)|drop\s+(database|table)|truncate\s+table|shutdown|reboot)(\s|$)`)
	privilegedCommand   = regexp.MustCompile(`(?i)(^|[;&|]\s*)(sudo|runas|net\s+(user|localgroup)|sc(\.exe)?\s+(create|config)|schtasks(\.exe)?\s+/create|reg(\.exe)?\s+add\s+hklm|set-executionpolicy|start-process[^\r\n]*-verb\s+runas)(\s|$)`)
	exfiltrationCommand = regexp.MustCompile(`(?i)(curl|wget|invoke-webrequest|invoke-restmethod)[^\r\n]*(--data|--form|--upload-file|-body\b|-infile\b|\s-[dft]\s)|(^|[;&|]\s*)(nc|ncat|netcat|scp|sftp|rsync)(\s|$)`)
	obfuscatedCommand   = regexp.MustCompile(`(?i)(powershell|pwsh)[^\r\n]*(-e|-en|-enc|-enco|-encod|-encode|-encodedcommand)\s+[a-z0-9+/=]{8,}|base64\s+(-d|--decode)[^\r\n]*\|\s*(sh|bash|zsh|powershell|pwsh)`)
	unsafeShellSyntax   = regexp.MustCompile("[;&|<>`\\r\\n]|\\$\\(|(?i)\\b(eval|iex|invoke-expression|cmd\\s+/c|powershell|pwsh|bash|sh)\\b")
	safeCommand         = regexp.MustCompile(`(?i)^\s*(git\s+(status|diff|log|show|branch|rev-parse)(\s+[^;&|<>\r\n]*)?|go\s+(test|vet|list|build)(\s+[^;&|<>\r\n]*)?|dotnet\s+(test|build)(\s+[^;&|<>\r\n]*)?|npm\s+(test|run\s+(test|lint|build))(\s+[^;&|<>\r\n]*)?|pytest(\s+[^;&|<>\r\n]*)?|rg(\.exe)?(\s+[^;&|<>\r\n]*)?|get-(childitem|content|location)(\s+[^;&|<>\r\n]*)?)\s*$`)
)

func Normalize(input HookInput, subjectID, workloadID string) (authzen.EvaluationRequest, error) {
	return NormalizeWithPolicy(input, subjectID, workloadID, NormalizationPolicy{Profile: "standard-developer"})
}

func NormalizeWithPolicy(input HookInput, subjectID, workloadID string, policy NormalizationPolicy) (authzen.EvaluationRequest, error) {
	workspace := input.CWD
	if workspace == "" {
		var err error
		workspace, err = os.Getwd()
		if err != nil {
			return authzen.EvaluationRequest{}, err
		}
	}
	workspace, _ = filepath.Abs(workspace)
	if strings.TrimSpace(input.ToolName) == "" {
		return authzen.EvaluationRequest{}, fmt.Errorf("tool_name is required")
	}
	if input.ToolInput == nil {
		return authzen.EvaluationRequest{}, fmt.Errorf("tool_input must be an object")
	}

	action, target, pathValue, command, mcpServer, mcpTool, err := classifyStrict(input, workspace)
	if err != nil {
		return authzen.EvaluationRequest{}, err
	}
	canonicalPath, outside := "", false
	if pathValue != "" {
		canonicalPath = canonicalize(pathValue, workspace)
		outside = outsideWorkspace(canonicalPath, workspace)
		target = canonicalPath
	}
	if policy.Profile == "" {
		policy.Profile = "standard-developer"
	}
	host := ""
	if action == "network.fetch" {
		parsed, parseErr := url.Parse(target)
		if parseErr != nil || !strings.EqualFold(parsed.Scheme, "https") || parsed.Hostname() == "" || net.ParseIP(parsed.Hostname()) != nil {
			return authzen.EvaluationRequest{}, fmt.Errorf("WebFetch.url must be an HTTPS URL with a DNS hostname")
		}
		host = strings.ToLower(parsed.Hostname())
		target = parsed.Scheme + "://" + host
	}
	subagentType, _ := optionalString(input.ToolInput, "subagent_type")
	approvedNetwork := action == "network.fetch" && matchesDomain(host, policy.AllowedNetworkDomains)
	approvedMCP := action == "mcp.invoke" && matchesExact(input.ToolName, policy.ApprovedMCPTools)
	approvedDelegate := action == "agent.delegate" && matchesExact(subagentType, policy.ApprovedSubagentTypes)
	protected := isProtected(canonicalPath)
	securityControl := (action == "file.write" || action == "notebook.write") && isSecurityControl(canonicalPath, workspace)
	resourceID := hashID(action + "\x00" + target)
	assertedUser := "unknown"
	if current, userErr := user.Current(); userErr == nil && current.Username != "" {
		assertedUser = current.Username
	}
	return authzen.EvaluationRequest{
		Subject: authzen.Entity{Type: "agent", ID: subjectID}, Action: authzen.Action{Name: action},
		Resource: authzen.Entity{Type: "tool-invocation", ID: resourceID, Properties: map[string]any{
			"tool": input.ToolName, "target": target, "path": canonicalPath, "command": command,
			"protected": protected, "outsideWorkspace": outside, "securityControl": securityControl,
			"destructive":   action == "command.execute" && destructiveCommand.MatchString(command),
			"privileged":    action == "command.execute" && privilegedCommand.MatchString(command),
			"exfiltration":  action == "command.execute" && exfiltrationCommand.MatchString(command),
			"obfuscated":    action == "command.execute" && obfuscatedCommand.MatchString(command),
			"shellApproved": action != "command.execute" || (safeCommand.MatchString(command) && !unsafeShellSyntax.MatchString(command)),
			"policyProfile": policy.Profile, "approvedNetwork": approvedNetwork, "approvedMCP": approvedMCP,
			"approvedDelegate": approvedDelegate, "networkHost": host, "mcpServer": mcpServer, "mcpTool": mcpTool,
		}},
		Context: map[string]any{"session_id": input.SessionID, "workload_id": workloadID, "tool_use_id": input.ToolUseID, "workspace": workspace, "asserted_user": assertedUser},
	}, nil
}

func classifyStrict(input HookInput, workspace string) (action, target, pathValue, command, mcpServer, mcpTool string, err error) {
	if strings.HasPrefix(input.ToolName, "mcp__") {
		parts := strings.SplitN(input.ToolName, "__", 3)
		if len(parts) != 3 || parts[1] == "" || parts[2] == "" {
			err = fmt.Errorf("invalid MCP tool name %q", input.ToolName)
			return
		}
		return "mcp.invoke", input.ToolName, "", "", parts[1], parts[2], nil
	}
	spec, known := toolRegistry[input.ToolName]
	if !known {
		return "tool.unknown", input.ToolName, "", "", "", "", nil
	}
	for _, field := range spec.required {
		if _, fieldErr := requiredField(input.ToolInput, field); fieldErr != nil {
			err = fmt.Errorf("%s.%s %w", input.ToolName, field, fieldErr)
			return
		}
	}
	action = spec.action
	if spec.target != "" {
		target, err = optionalString(input.ToolInput, spec.target)
		if err != nil {
			err = fmt.Errorf("%s.%s %w", input.ToolName, spec.target, err)
			return
		}
	}
	if spec.path != "" {
		pathValue, err = optionalString(input.ToolInput, spec.path)
		if err != nil {
			err = fmt.Errorf("%s.%s %w", input.ToolName, spec.path, err)
			return
		}
	}
	if (input.ToolName == "Glob" || input.ToolName == "Grep" || input.ToolName == "Search") && pathValue == "" {
		pathValue = workspace
	}
	if spec.command != "" {
		command, err = optionalString(input.ToolInput, spec.command)
		if err != nil {
			err = fmt.Errorf("%s.%s %w", input.ToolName, spec.command, err)
			return
		}
	}
	return
}

func requiredField(values map[string]any, name string) (any, error) {
	value, ok := values[name]
	if !ok || value == nil {
		return nil, fmt.Errorf("is required")
	}
	if name == "questions" || name == "todos" {
		typed, valid := value.([]any)
		if !valid || len(typed) == 0 {
			return nil, fmt.Errorf("must be a non-empty array")
		}
		return value, nil
	}
	typed, valid := value.(string)
	if !valid || strings.TrimSpace(typed) == "" {
		return nil, fmt.Errorf("must be a non-empty string")
	}
	return value, nil
}

func optionalString(values map[string]any, name string) (string, error) {
	value, ok := values[name]
	if !ok || value == nil {
		return "", nil
	}
	result, ok := value.(string)
	if !ok {
		return "", fmt.Errorf("must be a string")
	}
	return result, nil
}
func matchesExact(value string, entries []string) bool {
	for _, entry := range entries {
		if strings.EqualFold(strings.TrimSpace(entry), value) {
			return true
		}
	}
	return false
}
func matchesDomain(host string, entries []string) bool {
	for _, entry := range entries {
		entry = strings.ToLower(strings.TrimSpace(entry))
		if strings.HasPrefix(entry, "*.") {
			suffix := strings.TrimPrefix(entry, "*")
			if strings.HasSuffix(host, suffix) && host != strings.TrimPrefix(suffix, ".") {
				return true
			}
		} else if host == entry {
			return true
		}
	}
	return false
}

func canonicalize(pathValue, workspace string) string {
	pathValue = os.ExpandEnv(pathValue)
	if !filepath.IsAbs(pathValue) {
		pathValue = filepath.Join(workspace, pathValue)
	}
	absolute, err := filepath.Abs(filepath.Clean(pathValue))
	if err != nil {
		return filepath.Clean(pathValue)
	}
	if resolved, err := filepath.EvalSymlinks(absolute); err == nil {
		return resolved
	}
	return absolute
}
func outsideWorkspace(target, workspace string) bool {
	relative, err := filepath.Rel(workspace, target)
	return err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator))
}
func isProtected(pathValue string) bool {
	if pathValue == "" {
		return false
	}
	lower, base := strings.ToLower(filepath.ToSlash(pathValue)), strings.ToLower(filepath.Base(pathValue))
	if base == ".env" || strings.HasPrefix(base, ".env.") {
		return true
	}
	for _, suffix := range []string{".pem", ".key", ".pfx", ".p12"} {
		if strings.HasSuffix(base, suffix) {
			return true
		}
	}
	return strings.Contains(lower, "/.ssh/") || strings.Contains(lower, "/secrets/") || strings.Contains(base, "credential")
}
func isSecurityControl(pathValue, workspace string) bool {
	if pathValue == "" {
		return false
	}
	relative, err := filepath.Rel(workspace, pathValue)
	if err != nil {
		return true
	}
	relative = strings.ToLower(filepath.ToSlash(relative))
	return relative == ".claude/settings.json" || relative == ".claude/settings.local.json" || strings.HasPrefix(relative, ".claude/hooks/") || strings.HasPrefix(relative, ".git/hooks/")
}
func hashID(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}
