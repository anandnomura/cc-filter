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
	"cc-filter/internal/tracecontext"
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
	edgeLogger, _ := bapedge.NewEdgeLogger(config.StateDirectory)
	localFilter, err := filter.New(configs.DefaultRulesYAML)
	if err != nil {
		deny("BAP Edge local filter failed to initialize")
	}
	if input.HookEventName == "UserPromptSubmit" {
		promptTrace := tracecontext.NewRoot()
		local := localFilter.Process(string(raw))
		if local.Error != nil {
			_ = edgeLogger.Log(bapedge.EdgeEvent{Event: "prompt_filter_completed", TraceID: promptTrace.TraceID, SpanID: promptTrace.SpanID, HookEvent: input.HookEventName, Decision: "deny", Source: "local_filter"})
			fmt.Fprintln(os.Stderr, local.Error.Error())
			os.Exit(2)
		}
		_ = edgeLogger.Log(bapedge.EdgeEvent{Event: "prompt_filter_completed", TraceID: promptTrace.TraceID, SpanID: promptTrace.SpanID, HookEvent: input.HookEventName, Decision: "pass", Source: "local_filter"})
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
	operationTrace := tracecontext.ForOperation(input.SessionID, workloadID, input.ToolUseID)
	_ = edgeLogger.Log(bapedge.EdgeEvent{Event: "hook_received", TraceID: operationTrace.TraceID, SpanID: operationTrace.SpanID, HookEvent: input.HookEventName, Tool: input.ToolName})
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
		event := auditwire.Outcome{EventID: eventID, SessionID: input.SessionID, WorkloadID: workloadID, ToolUseID: input.ToolUseID, Tool: input.ToolName, Outcome: outcome, TraceParent: operationTrace.TraceParent()}
		if err := client.ReportOutcome(context.Background(), event); err != nil {
			if queueErr := spool.QueueOutcome(event); queueErr != nil {
				fmt.Fprintln(os.Stderr, "BAP Edge could not report or queue tool outcome:", queueErr)
				_ = edgeLogger.Log(bapedge.EdgeEvent{Event: "tool_outcome_queue_failed", Level: "error", TraceID: operationTrace.TraceID, SpanID: operationTrace.SpanID, HookEvent: input.HookEventName, Tool: input.ToolName, Decision: outcome, Source: "retry_spool"})
			} else {
				_ = edgeLogger.Log(bapedge.EdgeEvent{Event: "tool_outcome_queued", TraceID: operationTrace.TraceID, SpanID: operationTrace.SpanID, HookEvent: input.HookEventName, Tool: input.ToolName, Decision: outcome, Source: "retry_spool"})
			}
		} else {
			_ = edgeLogger.Log(bapedge.EdgeEvent{Event: "tool_outcome_reported", TraceID: operationTrace.TraceID, SpanID: operationTrace.SpanID, HookEvent: input.HookEventName, Tool: input.ToolName, Decision: outcome, Source: "bap_service"})
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
	request, err := bapedge.NormalizeWithPolicy(input, config.SubjectID, workloadID, config.NormalizationPolicy())
	if err != nil {
		deny("BAP Edge rejected malformed or unsupported tool input: " + err.Error())
	}
	_ = edgeLogger.Log(bapedge.EdgeEvent{Event: "operation_normalized", TraceID: operationTrace.TraceID, SpanID: operationTrace.SpanID, HookEvent: input.HookEventName, Tool: input.ToolName, Action: request.Action.Name})
	if isDeny(local.Output) {
		denialID := bapedge.AuditEventID("edge-denial", input.SessionID, workloadID, input.ToolUseID)
		denial := auditwire.EdgeDenial{EventID: denialID, Request: request, Reason: "Denied by local cc-filter rules", TraceParent: operationTrace.TraceParent()}
		if err := client.ReportEdgeDenial(context.Background(), denial); err != nil {
			if queueErr := spool.QueueEdgeDenial(denial); queueErr != nil {
				fmt.Fprintln(os.Stderr, "BAP Edge could not report or queue local denial:", queueErr)
			}
		}
		_ = edgeLogger.Log(bapedge.EdgeEvent{Event: "authorization_result", TraceID: operationTrace.TraceID, SpanID: operationTrace.SpanID, HookEvent: input.HookEventName, Tool: input.ToolName, Action: request.Action.Name, Decision: "deny", ReasonCode: "LOCAL_FILTER_DENY", Source: "local_filter"})
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
			if err := client.AuditGrantConsumption(context.Background(), request, cachedGrant, operationTrace); err == nil {
				_ = edgeLogger.Log(bapedge.EdgeEvent{Event: "authorization_result", TraceID: operationTrace.TraceID, SpanID: operationTrace.SpanID, HookEvent: input.HookEventName, Tool: input.ToolName, Action: request.Action.Name, Decision: "allow", ReasonCode: "CACHED_SIGNED_GRANT", Source: "signed_grant_cache"})
				allow("Allowed by a cached, signed, request-bound BAP grant; consumption was centrally recorded")
				return
			}
		}
	}

	decision, err := client.Evaluate(context.Background(), request, operationTrace)
	if err != nil {
		_ = edgeLogger.Log(bapedge.EdgeEvent{Event: "authorization_result", Level: "error", TraceID: operationTrace.TraceID, SpanID: operationTrace.SpanID, HookEvent: input.HookEventName, Tool: input.ToolName, Action: request.Action.Name, Decision: "deny", ReasonCode: "SERVICE_ERROR", Source: "fail_closed"})
		deny("BAP authorization failed closed: " + err.Error())
	}
	reason, _ := decision.Context["reason"].(string)
	reasonCode, _ := decision.Context["reason_code"].(string)
	if !decision.Decision {
		if reason == "" {
			reason = "Denied by BAP policy"
		}
		_ = edgeLogger.Log(bapedge.EdgeEvent{Event: "authorization_result", TraceID: operationTrace.TraceID, SpanID: operationTrace.SpanID, HookEvent: input.HookEventName, Tool: input.ToolName, Action: request.Action.Name, Decision: "deny", ReasonCode: reasonCode, Source: "bap_service"})
		deny(reason)
	}
	if grant, ok := decision.Context["grant"].(string); ok && grant != "" {
		if err := cache.Store(requestHash, grant); err != nil {
			deny("BAP Edge could not safely cache the signed grant")
		}
	}
	_ = edgeLogger.Log(bapedge.EdgeEvent{Event: "authorization_result", TraceID: operationTrace.TraceID, SpanID: operationTrace.SpanID, HookEvent: input.HookEventName, Tool: input.ToolName, Action: request.Action.Name, Decision: "allow", ReasonCode: reasonCode, Source: "bap_service"})
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
