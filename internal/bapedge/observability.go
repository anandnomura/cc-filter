package bapedge

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

// EdgeEvent contains only bounded operational metadata. Callers must never add
// prompts, command text, paths, tool input, model output, or credentials.
type EdgeEvent struct {
	Timestamp  string `json:"timestamp"`
	Level      string `json:"level"`
	Event      string `json:"event"`
	TraceID    string `json:"trace_id,omitempty"`
	SpanID     string `json:"span_id,omitempty"`
	HookEvent  string `json:"hook_event,omitempty"`
	Tool       string `json:"tool,omitempty"`
	Action     string `json:"action,omitempty"`
	Decision   string `json:"decision,omitempty"`
	ReasonCode string `json:"reason_code,omitempty"`
	Source     string `json:"source,omitempty"`
}

type EdgeLogger struct{ path string }

const edgeLogMaxBytes int64 = 10 * 1024 * 1024

func NewEdgeLogger(configured string) (*EdgeLogger, error) {
	base, err := stateDirectory(configured)
	if err != nil {
		return nil, err
	}
	directory := filepath.Join(base, "observability")
	if err := os.MkdirAll(directory, 0700); err != nil {
		return nil, err
	}
	return &EdgeLogger{path: filepath.Join(directory, "edge.jsonl")}, nil
}

func (l *EdgeLogger) Log(event EdgeEvent) error {
	if l == nil {
		return nil
	}
	if event.Timestamp == "" {
		event.Timestamp = time.Now().UTC().Format(time.RFC3339Nano)
	}
	if event.Level == "" {
		event.Level = "info"
	}
	data, err := json.Marshal(event)
	if err != nil {
		return err
	}
	if info, err := os.Stat(l.path); err == nil && info.Size()+int64(len(data)+1) > edgeLogMaxBytes {
		rotated := l.path + ".1"
		_ = os.Remove(rotated)
		if err := os.Rename(l.path, rotated); err != nil {
			return err
		}
	}
	file, err := os.OpenFile(l.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	if _, err := file.Write(append(data, '\n')); err != nil {
		_ = file.Close()
		return err
	}
	return file.Close()
}
