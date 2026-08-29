package audit

import (
	"crypto/ed25519"
	"crypto/rand"
	"os"
	"testing"
)

func TestSignedHashChainDetectsTampering(t *testing.T) {
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	path := t.TempDir() + "/audit.jsonl"
	store := New(path, privateKey)
	if err := store.Append(Event{EventID: "one", EventType: "authorization_decision"}); err != nil {
		t.Fatal(err)
	}
	if err := store.Append(Event{EventID: "two", EventType: "tool_outcome"}); err != nil {
		t.Fatal(err)
	}
	if events, err := ReadAndVerify(path, publicKey); err != nil || len(events) != 2 {
		t.Fatalf("expected valid two-event chain, events=%d err=%v", len(events), err)
	}
	data, _ := os.ReadFile(path)
	data[20] ^= 1
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadAndVerify(path, publicKey); err == nil {
		t.Fatal("expected tampering to be detected")
	}
	restarted := New(path, privateKey)
	if err := restarted.Initialize(); err == nil {
		t.Fatal("expected service startup to fail closed on a tampered existing chain")
	}
}
