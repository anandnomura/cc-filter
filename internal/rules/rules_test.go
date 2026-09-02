package rules

import (
	"testing"

	"bap-system/configs"
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

func TestShouldBlockSearchCatchesLiteralAndObfuscatedPatterns(t *testing.T) {
	loaded, err := loadDefaultRules(configs.DefaultRulesYAML)
	if err != nil {
		t.Fatal(err)
	}
	loaded, err = loaded.compile()
	if err != nil {
		t.Fatal(err)
	}

	obfuscated := []string{
		"secret",     // literal
		"s.cret",     // wildcard-obfuscated literal
		"p[a]ssword", // character-class-obfuscated literal
		"[Aa][Pp][Ii]",
		"to[k]en",
		"bear.r",
	}
	for _, pattern := range obfuscated {
		if blocked, _ := loaded.ShouldBlockSearch(pattern); !blocked {
			t.Fatalf("obfuscated search pattern was not blocked: %q", pattern)
		}
	}

	benign := []string{
		"TODO",
		"func TestFoo",
		"import \\(",
	}
	for _, pattern := range benign {
		if blocked, reason := loaded.ShouldBlockSearch(pattern); blocked {
			t.Fatalf("benign search pattern %q was blocked: %s", pattern, reason)
		}
	}

	// An invalid regex that still contains a blocked literal must still be
	// caught by the substring fallback even though it fails to compile.
	if blocked, _ := loaded.ShouldBlockSearch("secret["); !blocked {
		t.Fatalf("uncompilable pattern containing a blocked literal was not blocked")
	}
}
