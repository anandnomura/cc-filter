package tracecontext

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"strings"
)

// Context is the subset of W3C Trace Context BAP persists and propagates.
type Context struct {
	TraceID      string
	SpanID       string
	ParentSpanID string
	Flags        string
}

// ForOperation returns a stable trace with a fresh Edge span. Claude invokes
// lifecycle hooks in separate processes, so the stable ID is derived from the
// operation correlation fields rather than held in process memory.
func ForOperation(sessionID, workloadID, toolUseID string) Context {
	sum := sha256.Sum256([]byte("bap-operation-trace\x00" + sessionID + "\x00" + workloadID + "\x00" + toolUseID))
	return Context{TraceID: hex.EncodeToString(sum[:16]), SpanID: randomHex(8), Flags: "01"}
}

// NewRoot returns a new trace for requests that have no operation identity.
func NewRoot() Context {
	return Context{TraceID: randomHex(16), SpanID: randomHex(8), Flags: "01"}
}

// Parse validates a version 00 W3C traceparent value.
func Parse(value string) (Context, bool) {
	parts := strings.Split(strings.TrimSpace(value), "-")
	if len(parts) != 4 || parts[0] != "00" || len(parts[1]) != 32 || len(parts[2]) != 16 || len(parts[3]) != 2 {
		return Context{}, false
	}
	if !validHex(parts[1]) || !validHex(parts[2]) || !validHex(parts[3]) || allZero(parts[1]) || allZero(parts[2]) {
		return Context{}, false
	}
	return Context{TraceID: strings.ToLower(parts[1]), SpanID: strings.ToLower(parts[2]), Flags: strings.ToLower(parts[3])}, true
}

// Child creates a fresh span whose parent is the supplied span.
func (c Context) Child() Context {
	flags := c.Flags
	if flags == "" {
		flags = "01"
	}
	return Context{TraceID: c.TraceID, SpanID: randomHex(8), ParentSpanID: c.SpanID, Flags: flags}
}

func (c Context) TraceParent() string {
	if len(c.TraceID) != 32 || len(c.SpanID) != 16 {
		return ""
	}
	flags := c.Flags
	if flags == "" {
		flags = "01"
	}
	return "00-" + c.TraceID + "-" + c.SpanID + "-" + flags
}

func validHex(value string) bool {
	_, err := hex.DecodeString(value)
	return err == nil
}

func allZero(value string) bool { return strings.Trim(value, "0") == "" }

func randomHex(size int) string {
	value := make([]byte, size)
	if _, err := rand.Read(value); err != nil {
		panic("operating system random source unavailable")
	}
	return hex.EncodeToString(value)
}
