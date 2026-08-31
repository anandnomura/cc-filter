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
	"cc-filter/internal/policybundle"
	"cc-filter/internal/tracecontext"
)

var version = "dev"

func main() {
	configPath := flag.String("config", defaultConfigPath(), "administrator-controlled BAP Edge configuration")
	showVersion := flag.Bool("version", false, "print version and exit")
	flag.Parse()
	if *showVersion {
		fmt.Println(version)
		return
	}
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
		matchedRuleIDs, classifierErr := classifyPrompt(config, input.Prompt)
		if classifierErr != nil {
			// Prompt classification improves UX but is not an authorization
			// boundary. PreToolUse remains fail-closed and authoritative.
			_ = edgeLogger.Log(bapedge.EdgeEvent{Event: "prompt_intent_classification", Level: "error", TraceID: promptTrace.TraceID, SpanID: promptTrace.SpanID, HookEvent: input.HookEventName, Decision: "unavailable", Source: "signed_policy_bundle"})
			fmt.Print(local.Output)
			return
		}
		if len(matchedRuleIDs) == 0 {
			_ = edgeLogger.Log(bapedge.EdgeEvent{Event: "prompt_intent_classification", TraceID: promptTrace.TraceID, SpanID: promptTrace.SpanID, HookEvent: input.HookEventName, Decision: "no_match", Source: "signed_policy_bundle"})
			fmt.Print(local.Output)
			return
		}
		_ = edgeLogger.Log(bapedge.EdgeEvent{Event: "prompt_intent_classification", TraceID: promptTrace.TraceID, SpanID: promptTrace.SpanID, HookEvent: input.HookEventName, Decision: "manual_only_advisory", ReasonCode: strings.Join(matchedRuleIDs, ","), Source: "signed_policy_bundle"})
		promptAdvisoryOutput(local.Output)
		return
	}
	client, err := bapedge.NewClient(config)
	if err != nil {
		deny("BAP Edge trust configuration error: " + err.Error())
	}
	policyStore, err := bapedge.NewPolicyStore(config)
	if err != nil {
		deny("BAP Edge policy store error: " + err.Error())
	}
	edgeInstanceID, err := bapedge.LoadOrCreateEdgeInstanceID(config.StateDirectory)
	if err != nil {
		deny("BAP Edge instance identity error: " + err.Error())
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
	_ = spool.WriteMetrics(time.Now().UTC())

	if input.HookEventName == "SessionStart" {
		if _, err := bapedge.EnsurePolicy(context.Background(), client, policyStore, edgeInstanceID, true, time.Now().UTC()); err != nil {
			contextOutput("BAP policy synchronization is unavailable. Tool calls will fail closed: " + err.Error())
		}
		contextOutput("BAP Edge is active with a verified signed policy. Workload " + workloadID + " is bound to this Claude session; tool traffic is decided locally and fails closed on stale policy.")
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
		_ = spool.WriteMetrics(time.Now().UTC())
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
	bundle, err := bapedge.EnsurePolicy(context.Background(), client, policyStore, edgeInstanceID, false, time.Now().UTC())
	if err != nil {
		deny("BAP Edge policy is unavailable or stale: " + err.Error())
	}
	request, err := bapedge.NormalizeWithPolicy(input, config.SubjectID, workloadID, bapedge.NormalizationPolicy{Profile: bundle.PolicyProfile, AllowedNetworkDomains: bundle.AllowedNetwork, ApprovedMCPTools: bundle.ApprovedMCP, ApprovedSubagentTypes: bundle.ApprovedDelegates})
	if err != nil {
		deny("BAP Edge rejected malformed or unsupported tool input: " + err.Error())
	}
	_ = edgeLogger.Log(bapedge.EdgeEvent{Event: "operation_normalized", TraceID: operationTrace.TraceID, SpanID: operationTrace.SpanID, HookEvent: input.HookEventName, Tool: input.ToolName, Action: request.Action.Name})
	if isDeny(local.Output) {
		if err := bapedge.RecordFixtureFromEnvironment(raw, input, request, "deny", "LOCAL_FILTER_DENY", nil, bundle); err != nil {
			deny("BAP fixture capture failed closed: " + err.Error())
		}
		denialID := bapedge.AuditEventID("edge-denial", input.SessionID, workloadID, input.ToolUseID)
		denial := auditwire.EdgeDenial{EventID: denialID, Request: request, Reason: "Denied by local cc-filter rules", TraceParent: operationTrace.TraceParent()}
		if err := client.ReportEdgeDenial(context.Background(), denial); err != nil {
			if queueErr := spool.QueueEdgeDenial(denial); queueErr != nil {
				fmt.Fprintln(os.Stderr, "BAP Edge could not report or queue local denial:", queueErr)
			}
		}
		_ = spool.WriteMetrics(time.Now().UTC())
		_ = edgeLogger.Log(bapedge.EdgeEvent{Event: "authorization_result", TraceID: operationTrace.TraceID, SpanID: operationTrace.SpanID, HookEvent: input.HookEventName, Tool: input.ToolName, Action: request.Action.Name, Decision: "deny", ReasonCode: "LOCAL_FILTER_DENY", Source: "local_filter"})
		fmt.Print(local.Output)
		return
	}
	decision, err := policybundle.Authorize(bundle, request, time.Now().UTC())
	if err != nil {
		_ = edgeLogger.Log(bapedge.EdgeEvent{Event: "authorization_result", Level: "error", TraceID: operationTrace.TraceID, SpanID: operationTrace.SpanID, HookEvent: input.HookEventName, Tool: input.ToolName, Action: request.Action.Name, Decision: "deny", ReasonCode: "LOCAL_POLICY_ERROR", Source: "signed_policy_bundle"})
		deny("BAP local authorization failed closed: " + err.Error())
	}
	fixtureDecision := "deny"
	if decision.Allowed {
		fixtureDecision = "allow"
	}
	if err := bapedge.RecordFixtureFromEnvironment(raw, input, request, fixtureDecision, decision.ReasonCode, decision.RuleIDs, bundle); err != nil {
		deny("BAP fixture capture failed closed: " + err.Error())
	}
	auditDecision := auditwire.EdgeDecision{EventID: bapedge.AuditEventID("edge-policy-decision", input.SessionID, workloadID, input.ToolUseID), Request: request, Allowed: decision.Allowed, ReasonCode: decision.ReasonCode, PolicyVersion: decision.PolicyVersion, BundleVersion: bundle.Version, BundleDigest: bundle.RulesDigest, RuleIDs: decision.RuleIDs, TraceParent: operationTrace.TraceParent()}
	if _, err := spool.RecordEdgeDecision(context.Background(), client, auditDecision); err != nil {
		deny("BAP Edge could not durably record its local authorization decision")
	}
	_ = spool.WriteMetrics(time.Now().UTC())
	result := "deny"
	if decision.Allowed {
		result = "allow"
	}
	_ = edgeLogger.Log(bapedge.EdgeEvent{Event: "authorization_result", TraceID: operationTrace.TraceID, SpanID: operationTrace.SpanID, HookEvent: input.HookEventName, Tool: input.ToolName, Action: request.Action.Name, Decision: result, ReasonCode: decision.ReasonCode, Source: "signed_policy_bundle"})
	if !decision.Allowed {
		if decision.ManualOnly {
			manualExecutionRequired()
		}
		deny(decision.Reason)
	}
	allow(decision.Reason)
}

func classifyPrompt(config bapedge.Config, prompt string) ([]string, error) {
	client, err := bapedge.NewClient(config)
	if err != nil {
		return nil, err
	}
	store, err := bapedge.NewPolicyStore(config)
	if err != nil {
		return nil, err
	}
	edgeInstanceID, err := bapedge.LoadOrCreateEdgeInstanceID(config.StateDirectory)
	if err != nil {
		return nil, err
	}
	bundle, err := bapedge.EnsurePolicy(context.Background(), client, store, edgeInstanceID, false, time.Now().UTC())
	if err != nil {
		return nil, err
	}
	return policybundle.MatchPrompt(bundle, prompt), nil
}

func promptAdvisoryOutput(parentOutput string) {
	message := "BAP detected a request that may involve privileged manual-only access. Claude may explain, research, draft, or review the operation, but must not invoke a direct database, remote-shell, or cluster client. If execution is requested, provide a safe manual handoff for the employee to review, obtain any required A2P approval, and run in a separate terminal. This advisory is not authorization; PreToolUse policy remains authoritative."
	value := map[string]any{}
	if strings.TrimSpace(parentOutput) != "" {
		_ = json.Unmarshal([]byte(parentOutput), &value)
	}
	hookOutput, _ := value["hookSpecificOutput"].(map[string]any)
	if hookOutput == nil {
		hookOutput = map[string]any{}
	}
	hookOutput["hookEventName"] = "UserPromptSubmit"
	hookOutput["additionalContext"] = appendHookMessage(hookOutput["additionalContext"], message)
	value["hookSpecificOutput"] = hookOutput
	value["systemMessage"] = appendHookMessage(value["systemMessage"], message)
	_ = json.NewEncoder(os.Stdout).Encode(value)
}

func appendHookMessage(existing any, message string) string {
	if current, ok := existing.(string); ok && strings.TrimSpace(current) != "" {
		return current + "\n\n" + message
	}
	return message
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

func manualExecutionRequired() {
	hookDecisionWithPrefix("deny", manualExecutionMessage(), "")
	os.Exit(0)
}

func manualExecutionMessage() string {
	return "BAP EDGE REQUIRES MANUAL EXECUTION; THIS TOOL CALL DID NOT EXECUTE. Review the command, confirm the required access or A2P approval, and run it yourself in a separate terminal. BAP intentionally does not reproduce the command because it may contain credentials or sensitive connection details."
}

func hookDecision(decision, reason string) {
	prefix := ""
	if decision == "deny" {
		prefix = "BAP EDGE BLOCKED THIS TOOL CALL; IT DID NOT EXECUTE. "
	}
	hookDecisionWithPrefix(decision, reason, prefix)
}

func hookDecisionWithPrefix(decision, reason, prefix string) {
	reason = prefix + reason
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
