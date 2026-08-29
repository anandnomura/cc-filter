package main

import (
	"bufio"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"runtime"
	"strings"
	"time"

	"cc-filter/configs"
	"cc-filter/internal/auditwire"
	"cc-filter/internal/bapedge"
	"cc-filter/internal/filter"
	"cc-filter/internal/grants"
)

func main() {
	configPath := flag.String("config", defaultConfigPath(), "administrator-controlled BAP Edge configuration")
	flag.Parse()
	raw, err := io.ReadAll(bufio.NewReaderSize(os.Stdin, 1<<20))
	if err != nil {
		deny("BAP Edge could not read the Claude hook request")
	}
	var input bapedge.HookInput
	if err := json.Unmarshal(raw, &input); err != nil {
		deny("BAP Edge received malformed hook JSON")
	}

	config, err := bapedge.LoadConfig(*configPath)
	if err != nil {
		deny("BAP Edge configuration error: " + err.Error())
	}
	localFilter, err := filter.New(configs.DefaultRulesYAML)
	if err != nil {
		deny("BAP Edge local filter failed to initialize")
	}
	if input.HookEventName == "UserPromptSubmit" {
		local := localFilter.Process(string(raw))
		if local.Error != nil {
			fmt.Fprintln(os.Stderr, local.Error.Error())
			os.Exit(2)
		}
		fmt.Print(local.Output)
		return
	}
	client, err := bapedge.NewClient(config)
	if err != nil {
		deny("BAP Edge trust configuration error: " + err.Error())
	}
	sessions, err := bapedge.NewSessionStore(config.StateDirectory)
	if err != nil {
		deny("BAP Edge session state error: " + err.Error())
	}
	spool, err := bapedge.NewAuditSpool(config.StateDirectory)
	if err != nil {
		deny("BAP Edge audit spool error: " + err.Error())
	}
	workloadID, err := sessions.LoadOrCreate(input.SessionID)
	if err != nil {
		deny("BAP Edge workload identity error: " + err.Error())
	}
	flushContext, cancelFlush := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancelFlush()
	if err := spool.Flush(flushContext, client); err != nil {
		fmt.Fprintln(os.Stderr, "BAP Edge retained queued audit events:", err)
	}

	if input.HookEventName == "SessionStart" {
		if err := client.Health(context.Background()); err != nil {
			contextOutput("BAP authorization is unavailable. Tool calls will fail closed: " + err.Error())
		}
		contextOutput("BAP Edge is active and BAP Service is reachable. Workload " + workloadID + " is bound to this Claude session; every tool call requires authorization.")
	}
	if input.HookEventName == "PostToolUse" || input.HookEventName == "PostToolUseFailure" {
		outcome := "success"
		if input.HookEventName == "PostToolUseFailure" {
			outcome = "failure"
		}
		eventID := bapedge.AuditEventID("outcome", input.SessionID, workloadID, input.ToolUseID, outcome)
		event := auditwire.Outcome{EventID: eventID, SessionID: input.SessionID, WorkloadID: workloadID, ToolUseID: input.ToolUseID, Tool: input.ToolName, Outcome: outcome}
		if err := client.ReportOutcome(context.Background(), event); err != nil {
			if queueErr := spool.QueueOutcome(event); queueErr != nil {
				fmt.Fprintln(os.Stderr, "BAP Edge could not report or queue tool outcome:", queueErr)
			}
		}
		return
	}

	local := localFilter.Process(string(raw))
	if local.Error != nil {
		fmt.Fprintln(os.Stderr, local.Error.Error())
		os.Exit(2)
	}
	if input.HookEventName != "PreToolUse" {
		fmt.Print(local.Output)
		if input.HookEventName == "SessionEnd" {
			_ = sessions.Remove(input.SessionID)
		}
		return
	}
	request, err := bapedge.Normalize(input, config.SubjectID, workloadID)
	if err != nil {
		deny("BAP Edge could not normalize the requested operation")
	}
	if isDeny(local.Output) {
		denialID := bapedge.AuditEventID("edge-denial", input.SessionID, workloadID, input.ToolUseID)
		denial := auditwire.EdgeDenial{EventID: denialID, Request: request, Reason: "Denied by local cc-filter rules"}
		if err := client.ReportEdgeDenial(context.Background(), denial); err != nil {
			if queueErr := spool.QueueEdgeDenial(denial); queueErr != nil {
				fmt.Fprintln(os.Stderr, "BAP Edge could not report or queue local denial:", queueErr)
			}
		}
		fmt.Print(local.Output)
		return
	}
	requestHash, err := grants.HashRequest(request)
	if err != nil {
		deny("BAP Edge could not bind the authorization request")
	}
	cache, err := bapedge.NewGrantCache(config.CacheDirectory)
	if err != nil {
		deny("BAP Edge signed grant cache error: " + err.Error())
	}
	if cachedGrant, err := cache.Load(requestHash); err == nil && cachedGrant != "" {
		if err := client.VerifyGrant(cachedGrant, requestHash); err == nil {
			if err := client.AuditGrantConsumption(context.Background(), request, cachedGrant); err == nil {
				allow("Allowed by a cached, signed, request-bound BAP grant; consumption was centrally recorded")
				return
			}
		}
	}

	decision, err := client.Evaluate(context.Background(), request)
	if err != nil {
		deny("BAP authorization failed closed: " + err.Error())
	}
	reason, _ := decision.Context["reason"].(string)
	if !decision.Decision {
		if reason == "" {
			reason = "Denied by BAP policy"
		}
		deny(reason)
	}
	if grant, ok := decision.Context["grant"].(string); ok && grant != "" {
		if err := cache.Store(requestHash, grant); err != nil {
			deny("BAP Edge could not safely cache the signed grant")
		}
	}
	allow(reason)
}

func defaultConfigPath() string {
	if runtime.GOOS == "windows" {
		return `C:\Program Files\BAP Edge\bap-edge.yaml`
	}
	return "/etc/bap-edge/bap-edge.yaml"
}

func isDeny(output string) bool {
	return strings.Contains(output, `"permissionDecision":"deny"`) || strings.Contains(output, `"permissionDecision": "deny"`)
}

func allow(reason string) { hookDecision("allow", reason) }

func deny(reason string) {
	hookDecision("deny", reason)
	os.Exit(0)
}

func hookDecision(decision, reason string) {
	if decision == "deny" {
		reason = "BAP EDGE BLOCKED THIS TOOL CALL; IT DID NOT EXECUTE. " + reason
	}
	value := map[string]any{"hookSpecificOutput": map[string]any{
		"hookEventName": "PreToolUse", "permissionDecision": decision, "permissionDecisionReason": reason,
	}}
	if decision == "deny" {
		// Claude's compact TUI labels attempted tool calls as "Ran" even when a
		// PreToolUse hook denies them. systemMessage makes the enforcement result
		// visible independently of the model's interpretation of the tool error.
		value["systemMessage"] = reason
	}
	_ = json.NewEncoder(os.Stdout).Encode(value)
}

func contextOutput(message string) {
	value := map[string]any{"hookSpecificOutput": map[string]any{
		"hookEventName": "SessionStart", "additionalContext": message,
	}}
	_ = json.NewEncoder(os.Stdout).Encode(value)
	os.Exit(0)
}
