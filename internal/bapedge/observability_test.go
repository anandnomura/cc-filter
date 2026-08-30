package bapedge

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEdgeLoggerWritesBoundedEvent(t *testing.T) {
	directory := t.TempDir()
	logger, err := NewEdgeLogger(directory)
	if err != nil {
		t.Fatal(err)
	}
	if err := logger.Log(EdgeEvent{Event: "authorization_result", TraceID: "trace", Tool: "Read", Decision: "allow"}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(directory, "observability", "edge.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	for _, expected := range []string{`"event":"authorization_result"`, `"trace_id":"trace"`, `"decision":"allow"`} {
		if !strings.Contains(text, expected) {
			t.Fatalf("log missing %s: %s", expected, text)
		}
	}
}

func TestEdgeLoggerRotatesAtBound(t *testing.T) {
	directory := t.TempDir()
	logger, err := NewEdgeLogger(directory)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Truncate(logger.path, edgeLogMaxBytes); err != nil {
		file, createErr := os.Create(logger.path)
		if createErr != nil {
			t.Fatal(createErr)
		}
		_ = file.Close()
		if err := os.Truncate(logger.path, edgeLogMaxBytes); err != nil {
			t.Fatal(err)
		}
	}
	if err := logger.Log(EdgeEvent{Event: "after_rotation"}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(logger.path + ".1"); err != nil {
		t.Fatal("rotated Edge log was not retained")
	}
}
