package bapedge

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
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

var (
	destructiveCommand  = regexp.MustCompile(`(?i)(^|[;&|]\s*)(rm\s+-[^\r\n]*r[^\r\n]*f|del\s+/[sqf]|remove-item[^\r\n]*(-recurse|-force)|format\s+[a-z]:|diskpart|git\s+(reset\s+--hard|clean\s+-[^\r\n]*f|push[^\r\n]*--force)|drop\s+(database|table)|truncate\s+table|shutdown|reboot)(\s|$)`)
	privilegedCommand   = regexp.MustCompile(`(?i)(^|[;&|]\s*)(sudo|runas|net\s+(user|localgroup)|sc(\.exe)?\s+(create|config)|schtasks(\.exe)?\s+/create|reg(\.exe)?\s+add\s+hklm|set-executionpolicy|start-process[^\r\n]*-verb\s+runas)(\s|$)`)
	exfiltrationCommand = regexp.MustCompile(`(?i)(curl|wget|invoke-webrequest|invoke-restmethod)[^\r\n]*(--data|--form|--upload-file|-body\b|-infile\b|\s-[dft]\s)|(^|[;&|]\s*)(nc|ncat|netcat|scp|sftp|rsync)(\s|$)`)
	obfuscatedCommand   = regexp.MustCompile(`(?i)(powershell|pwsh)[^\r\n]*(-e|-en|-enc|-enco|-encod|-encode|-encodedcommand)\s+[a-z0-9+/=]{8,}|base64\s+(-d|--decode)[^\r\n]*\|\s*(sh|bash|zsh|powershell|pwsh)`)
)

func Normalize(input HookInput, subjectID, workloadID string) (authzen.EvaluationRequest, error) {
	workspace := input.CWD
	if workspace == "" {
		var err error
		workspace, err = os.Getwd()
		if err != nil {
			return authzen.EvaluationRequest{}, err
		}
	}
	workspace, _ = filepath.Abs(workspace)

	action, target, pathValue, command := classify(input, workspace)
	canonicalPath := ""
	outside := false
	if pathValue != "" {
		canonicalPath = canonicalize(pathValue, workspace)
		outside = outsideWorkspace(canonicalPath, workspace)
		target = canonicalPath
	}
	protected := isProtected(canonicalPath)
	securityControl := action == "file.write" && isSecurityControl(canonicalPath, workspace)
	destructive := action == "command.execute" && destructiveCommand.MatchString(command)
	privileged := action == "command.execute" && privilegedCommand.MatchString(command)
	exfiltration := action == "command.execute" && exfiltrationCommand.MatchString(command)
	obfuscated := action == "command.execute" && obfuscatedCommand.MatchString(command)
	resourceID := hashID(action + "\x00" + target)

	assertedUser := "unknown"
	if current, err := user.Current(); err == nil && current.Username != "" {
		assertedUser = current.Username
	}
	return authzen.EvaluationRequest{
		Subject: authzen.Entity{Type: "agent", ID: subjectID},
		Action:  authzen.Action{Name: action},
		Resource: authzen.Entity{
			Type: "tool-invocation",
			ID:   resourceID,
			Properties: map[string]any{
				"tool": input.ToolName, "target": target, "path": canonicalPath, "command": command,
				"protected": protected, "outsideWorkspace": outside, "securityControl": securityControl,
				"destructive": destructive, "privileged": privileged, "exfiltration": exfiltration, "obfuscated": obfuscated,
			},
		},
		Context: map[string]any{"session_id": input.SessionID, "workload_id": workloadID, "tool_use_id": input.ToolUseID, "workspace": workspace, "asserted_user": assertedUser},
	}, nil
}

func classify(input HookInput, workspace string) (action, target, pathValue, command string) {
	stringField := func(name string) string {
		value, _ := input.ToolInput[name].(string)
		return value
	}
	switch input.ToolName {
	case "Read":
		return "file.read", stringField("file_path"), stringField("file_path"), ""
	case "Edit", "Write", "MultiEdit", "NotebookEdit":
		pathValue = stringField("file_path")
		if pathValue == "" {
			pathValue = stringField("notebook_path")
		}
		if input.ToolName == "NotebookEdit" {
			return "notebook.write", pathValue, pathValue, ""
		}
		return "file.write", pathValue, pathValue, ""
	case "Glob", "Grep", "Search":
		pathValue = stringField("path")
		if pathValue == "" {
			pathValue = workspace
		}
		return "file.search", fmt.Sprintf("%s:%s", pathValue, stringField("pattern")), pathValue, ""
	case "Bash":
		command = stringField("command")
		return "command.execute", command, "", command
	case "WebFetch":
		target = stringField("url")
		if parsed, err := url.Parse(target); err == nil && parsed.Hostname() != "" {
			target = parsed.Scheme + "://" + parsed.Hostname()
		}
		return "network.fetch", target, "", ""
	case "WebSearch":
		return "network.search", "web-search", "", ""
	case "Task", "Agent":
		return "agent.delegate", input.ToolName, "", ""
	default:
		if strings.HasPrefix(input.ToolName, "mcp__") {
			return "mcp.invoke", input.ToolName, "", ""
		}
		return "tool.unknown", input.ToolName, "", ""
	}
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
	if err != nil {
		return true
	}
	return relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator))
}

func isProtected(pathValue string) bool {
	if pathValue == "" {
		return false
	}
	lower := strings.ToLower(filepath.ToSlash(pathValue))
	base := strings.ToLower(filepath.Base(pathValue))
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
	return relative == ".claude/settings.json" ||
		relative == ".claude/settings.local.json" ||
		strings.HasPrefix(relative, ".claude/hooks/") ||
		strings.HasPrefix(relative, ".git/hooks/")
}

func hashID(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}
