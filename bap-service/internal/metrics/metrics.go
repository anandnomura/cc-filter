package metrics

import (
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

var durationBuckets = []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5}

// Registry is a deliberately small, bounded-label Prometheus registry. It
// never records prompts, commands, paths, principals, credentials, or IDs.
type Registry struct {
	mu         sync.Mutex
	counters   map[string]uint64
	durations  map[string]*histogram
	ready      float64
	readyKnown bool
}

type histogram struct {
	Count   uint64
	Sum     float64
	Buckets []uint64
}

func New() *Registry {
	return &Registry{counters: map[string]uint64{}, durations: map[string]*histogram{}}
}

func (r *Registry) Decision(allowed bool, reasonCode, source string) {
	r.increment("bap_authorization_decisions_total", labels(
		"decision", map[bool]string{true: "allow", false: "deny"}[allowed],
		"reason_code", bounded(reasonCode), "source", bounded(source)))
}

func (r *Registry) AuthenticationFailure() {
	r.increment("bap_authentication_failures_total", "")
}

func (r *Registry) AuditFailure(operation string) {
	r.increment("bap_audit_failures_total", labels("operation", bounded(operation)))
}

func (r *Registry) Outcome(outcome string) {
	r.increment("bap_tool_outcomes_total", labels("outcome", bounded(outcome)))
}

func (r *Registry) ObserveHTTP(route, method string, status int, elapsed time.Duration) {
	key := "bap_http_request_duration_seconds" + labels("method", method, "route", route, "status", strconv.Itoa(status))
	r.mu.Lock()
	defer r.mu.Unlock()
	h := r.durations[key]
	if h == nil {
		h = &histogram{Buckets: make([]uint64, len(durationBuckets))}
		r.durations[key] = h
	}
	seconds := elapsed.Seconds()
	h.Count++
	h.Sum += seconds
	for index, upper := range durationBuckets {
		if seconds <= upper {
			h.Buckets[index]++
		}
	}
}

func (r *Registry) SetReady(ready bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.readyKnown = true
	if ready {
		r.ready = 1
	} else {
		r.ready = 0
	}
}

func (r *Registry) WritePrometheus(w io.Writer) {
	r.mu.Lock()
	defer r.mu.Unlock()
	_, _ = io.WriteString(w, "# HELP bap_service_ready Whether durable authorization storage is ready.\n")
	_, _ = io.WriteString(w, "# TYPE bap_service_ready gauge\n")
	_, _ = io.WriteString(w, "# HELP bap_authorization_decisions_total Committed authorization decisions by bounded outcome labels.\n")
	_, _ = io.WriteString(w, "# TYPE bap_authorization_decisions_total counter\n")
	_, _ = io.WriteString(w, "# HELP bap_authentication_failures_total Rejected BAP client authentication attempts.\n")
	_, _ = io.WriteString(w, "# TYPE bap_authentication_failures_total counter\n")
	_, _ = io.WriteString(w, "# HELP bap_audit_failures_total Failed durable audit operations.\n")
	_, _ = io.WriteString(w, "# TYPE bap_audit_failures_total counter\n")
	_, _ = io.WriteString(w, "# HELP bap_tool_outcomes_total Committed tool outcomes reported by BAP Edge.\n")
	_, _ = io.WriteString(w, "# TYPE bap_tool_outcomes_total counter\n")
	_, _ = io.WriteString(w, "# HELP bap_http_request_duration_seconds BAP Service request latency by bounded route labels.\n")
	_, _ = io.WriteString(w, "# TYPE bap_http_request_duration_seconds histogram\n")
	ready := r.ready
	if !r.readyKnown {
		ready = 0
	}
	_, _ = fmt.Fprintf(w, "bap_service_ready %s\n", strconv.FormatFloat(ready, 'f', 0, 64))

	counterKeys := make([]string, 0, len(r.counters))
	for key := range r.counters {
		counterKeys = append(counterKeys, key)
	}
	sort.Strings(counterKeys)
	for _, key := range counterKeys {
		_, _ = fmt.Fprintf(w, "%s %d\n", key, r.counters[key])
	}

	histogramKeys := make([]string, 0, len(r.durations))
	for key := range r.durations {
		histogramKeys = append(histogramKeys, key)
	}
	sort.Strings(histogramKeys)
	for _, key := range histogramKeys {
		h := r.durations[key]
		name, labelSet := splitMetric(key)
		for index, upper := range durationBuckets {
			_, _ = fmt.Fprintf(w, "%s_bucket%s %d\n", name, addLabel(labelSet, "le", strconv.FormatFloat(upper, 'f', -1, 64)), h.Buckets[index])
		}
		_, _ = fmt.Fprintf(w, "%s_bucket%s %d\n", name, addLabel(labelSet, "le", "+Inf"), h.Count)
		_, _ = fmt.Fprintf(w, "%s_sum%s %s\n", name, labelSet, strconv.FormatFloat(h.Sum, 'f', 6, 64))
		_, _ = fmt.Fprintf(w, "%s_count%s %d\n", name, labelSet, h.Count)
	}
}

func (r *Registry) increment(name, labelSet string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.counters[name+labelSet]++
}

func bounded(value string) string {
	if value == "" {
		return "unknown"
	}
	if len(value) > 64 {
		return "other"
	}
	for _, character := range value {
		if !(character == '_' || character == '-' || character == '.' || character >= 'A' && character <= 'Z' || character >= 'a' && character <= 'z' || character >= '0' && character <= '9') {
			return "other"
		}
	}
	return value
}

func labels(values ...string) string {
	parts := make([]string, 0, len(values)/2)
	for index := 0; index+1 < len(values); index += 2 {
		parts = append(parts, values[index]+"=\""+values[index+1]+"\"")
	}
	return "{" + strings.Join(parts, ",") + "}"
}

func splitMetric(value string) (string, string) {
	index := strings.IndexByte(value, '{')
	if index < 0 {
		return value, ""
	}
	return value[:index], value[index:]
}

func addLabel(labelSet, name, value string) string {
	if labelSet == "" {
		return labels(name, value)
	}
	return strings.TrimSuffix(labelSet, "}") + "," + name + "=\"" + value + "\"}"
}
