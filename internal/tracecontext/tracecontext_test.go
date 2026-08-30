package tracecontext

import "testing"

func TestOperationTraceStableWithFreshSpans(t *testing.T) {
	first := ForOperation("session", "workload", "tool")
	second := ForOperation("session", "workload", "tool")
	if first.TraceID != second.TraceID {
		t.Fatal("operation trace ID must be stable across hook processes")
	}
	if first.SpanID == second.SpanID {
		t.Fatal("each hook process needs a fresh span")
	}
	parsed, ok := Parse(first.TraceParent())
	if !ok || parsed.TraceID != first.TraceID || parsed.SpanID != first.SpanID {
		t.Fatal("traceparent did not round trip")
	}
}

func TestParseRejectsInvalidTraceparent(t *testing.T) {
	for _, value := range []string{"", "00-zero", "00-00000000000000000000000000000000-1111111111111111-01", "01-11111111111111111111111111111111-2222222222222222-01"} {
		if _, ok := Parse(value); ok {
			t.Fatalf("accepted invalid traceparent %q", value)
		}
	}
}
