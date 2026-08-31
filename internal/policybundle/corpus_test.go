package policybundle

import (
	"encoding/json"
	"os"
	"reflect"
	"testing"
	"time"
)

type commandCorpusCase struct {
	Name       string   `json:"name"`
	Command    string   `json:"command"`
	Allowed    bool     `json:"allowed"`
	ReasonCode string   `json:"reason_code"`
	RuleIDs    []string `json:"rule_ids,omitempty"`
}

func TestCommandPolicyCorpus(t *testing.T) {
	data, err := os.ReadFile("testdata/command-policy-corpus.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []commandCorpusCase
	if err := json.Unmarshal(data, &cases); err != nil {
		t.Fatal(err)
	}
	if len(cases) < 30 {
		t.Fatalf("command policy corpus is unexpectedly small: %d cases", len(cases))
	}
	now := time.Date(2026, 8, 30, 12, 0, 0, 0, time.UTC)
	bundle := testBundle(t, now)
	for _, testCase := range cases {
		t.Run(testCase.Name, func(t *testing.T) {
			decision, err := Authorize(bundle, commandRequest(testCase.Command), now.Add(time.Minute))
			if err != nil {
				t.Fatal(err)
			}
			if decision.Allowed != testCase.Allowed || decision.ReasonCode != testCase.ReasonCode {
				t.Fatalf("command=%q allowed=%v reason=%s; want allowed=%v reason=%s", testCase.Command, decision.Allowed, decision.ReasonCode, testCase.Allowed, testCase.ReasonCode)
			}
			if testCase.RuleIDs != nil && !reflect.DeepEqual(decision.RuleIDs, testCase.RuleIDs) {
				t.Fatalf("command=%q rule IDs=%v; want %v", testCase.Command, decision.RuleIDs, testCase.RuleIDs)
			}
		})
	}
}
