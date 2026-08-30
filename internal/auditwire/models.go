package auditwire

import "cc-filter/internal/authzen"

type GrantConsumption struct {
	Request     authzen.EvaluationRequest `json:"request"`
	Grant       string                    `json:"grant"`
	TraceParent string                    `json:"traceparent,omitempty"`
}

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
