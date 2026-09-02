package auditwire

import "bap-system/internal/authzen"

type Outcome struct {
	EventID     string `json:"event_id"`
	SessionID   string `json:"session_id"`
	WorkloadID  string `json:"workload_id"`
	ToolUseID   string `json:"tool_use_id"`
	Tool        string `json:"tool"`
	Outcome     string `json:"outcome"`
	TraceParent string `json:"traceparent,omitempty"`
}

type EdgeDenial struct {
	EventID     string                    `json:"event_id"`
	Request     authzen.EvaluationRequest `json:"request"`
	Reason      string                    `json:"reason"`
	TraceParent string                    `json:"traceparent,omitempty"`
}

type EdgeDecision struct {
	EventID             string                    `json:"event_id"`
	Request             authzen.EvaluationRequest `json:"request"`
	Allowed             bool                      `json:"allowed"`
	ReasonCode          string                    `json:"reason_code"`
	EvaluatedAllowed    bool                      `json:"evaluated_allowed"`
	EvaluatedReasonCode string                    `json:"evaluated_reason_code"`
	EnforcementMode     string                    `json:"enforcement_mode"`
	PolicyVersion       string                    `json:"policy_version"`
	BundleVersion       uint64                    `json:"bundle_version"`
	BundleDigest        string                    `json:"bundle_digest"`
	RuleIDs             []string                  `json:"rule_ids,omitempty"`
	TraceParent         string                    `json:"traceparent,omitempty"`
}
