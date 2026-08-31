package main

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func TestManualExecutionMessageIsActionableAndDoesNotEchoCommand(t *testing.T) {
	message := manualExecutionMessage()
	for _, required := range []string{"DID NOT EXECUTE", "A2P", "separate terminal", "does not reproduce the command"} {
		if !strings.Contains(message, required) {
			t.Fatalf("manual handoff message is missing %q: %s", required, message)
		}
	}
	for _, sensitive := range []string{"orders-prod", "dba@", "--password", "mysql -h"} {
		if strings.Contains(message, sensitive) {
			t.Fatalf("manual handoff message contains command detail %q", sensitive)
		}
	}
}

func TestPromptAdvisoryIsUserPromptContextNotAuthorization(t *testing.T) {
	original := os.Stdout
	readEnd, writeEnd, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = writeEnd
	promptAdvisoryOutput(`{"hookSpecificOutput":{"hookEventName":"UserPromptSubmit","additionalContext":"parent cc-filter context"},"systemMessage":"parent message","parentField":true}`)
	_ = writeEnd.Close()
	os.Stdout = original
	var output map[string]any
	if err := json.NewDecoder(readEnd).Decode(&output); err != nil {
		t.Fatal(err)
	}
	hookOutput := output["hookSpecificOutput"].(map[string]any)
	if hookOutput["hookEventName"] != "UserPromptSubmit" {
		t.Fatalf("unexpected hook event: %#v", hookOutput)
	}
	if _, exists := hookOutput["permissionDecision"]; exists {
		t.Fatal("prompt advisory must not make an authorization decision")
	}
	context, _ := hookOutput["additionalContext"].(string)
	for _, expected := range []string{"parent cc-filter context", "manual-only", "A2P", "PreToolUse policy remains authoritative"} {
		if !strings.Contains(context, expected) {
			t.Fatalf("advisory is missing %q: %s", expected, context)
		}
	}
	if output["parentField"] != true || !strings.Contains(output["systemMessage"].(string), "parent message") {
		t.Fatalf("parent cc-filter output was not preserved: %#v", output)
	}
}
