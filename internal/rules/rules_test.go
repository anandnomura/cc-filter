package rules

import (
	"testing"

	"cc-filter/configs"
)

func TestDefaultCommandRulesDoNotMatchEveryCommand(t *testing.T) {
	loaded, err := loadDefaultRules(configs.DefaultRulesYAML)
	if err != nil {
		t.Fatal(err)
	}
	loaded, err = loaded.compile()
	if err != nil {
		t.Fatal(err)
	}

	if blocked, reason := loaded.ShouldBlockCommand("git status --short"); blocked {
		t.Fatalf("safe command was blocked: %s", reason)
	}
	for _, command := range []string{"env | sort", "export | grep PATH"} {
		if blocked, _ := loaded.ShouldBlockCommand(command); !blocked {
			t.Fatalf("sensitive environment pipeline was not blocked: %s", command)
		}
	}
}
