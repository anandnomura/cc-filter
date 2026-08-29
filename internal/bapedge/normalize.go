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

var destructiveCommand = regexp.MustCompile(`(?i)(^|[;&|]\s*)(rm\s+-[^\r\n]*r[^\r\n]*f|del\s+/[sqf]|remove-item[^\r\n]*-recurse|format\s+[a-z]:|git\s+(reset\s+--hard|clean\s+-[^\r\n]*f|push[^\r\n]*--force)|drop\s+(database|table)|shutdown|reboot)(\s|$)`)

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
	destructive := action == "command.execute" && destructiveCommand.MatchString(command)
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
				"protected": protected, "outsideWorkspace": outside, "destructive": destructive,
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
		return "network.access", target, "", ""
	case "WebSearch":
		return "network.access", "web-search", "", ""
	default:
		if strings.HasPrefix(input.ToolName, "mcp__") {
			return "mcp.invoke", input.ToolName, "", ""
		}
		return "tool.invoke", input.ToolName, "", ""
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

func hashID(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}
