package main

import (
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
