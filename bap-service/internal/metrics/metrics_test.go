package metrics

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

func TestPrometheusOutputUsesBoundedLabels(t *testing.T) {
	registry := New()
	registry.SetReady(true)
	registry.Decision(false, "EXPLICIT_FORBID", "pdp_evaluation")
	registry.Decision(false, "unsafe value", "pdp_evaluation")
	registry.ObserveHTTP("/access/v1/evaluation", "POST", 200, 20*time.Millisecond)
	var output bytes.Buffer
	registry.WritePrometheus(&output)
	text := output.String()
	for _, expected := range []string{
		"bap_service_ready 1",
		`reason_code="EXPLICIT_FORBID"`,
		`reason_code="other"`,
		"bap_http_request_duration_seconds_count",
	} {
		if !strings.Contains(text, expected) {
			t.Fatalf("metrics output missing %q:\n%s", expected, text)
		}
	}
}
