package bapedge

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"os"
	"path/filepath"
	"testing"
	"time"

	"bap-system/internal/grants"
	"bap-system/internal/policybundle"
)

func policyTestConfig(t *testing.T, publicKey ed25519.PublicKey) Config {
	t.Helper()
	directory := t.TempDir()
	grantPrivate := filepath.Join(directory, "grant-private.pem")
	grantPublic := filepath.Join(directory, "grant-public.pem")
	if err := grants.GenerateKeyPair(grantPrivate, grantPublic); err != nil {
		t.Fatal(err)
	}
	bundlePublic := filepath.Join(directory, "bundle-public.pem")
	if err := writePublicKeyForTest(bundlePublic, publicKey); err != nil {
		t.Fatal(err)
	}
	return Config{StateDirectory: directory, PublicKeyPath: grantPublic, BundlePublicKeyPath: bundlePublic}
}

func writePublicKeyForTest(path string, key ed25519.PublicKey) error {
	der, err := x509.MarshalPKIXPublicKey(key)
	if err != nil {
		return err
	}
	return os.WriteFile(path, pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der}), 0600)
}

func TestPolicyStoreRejectsRollbackEquivocationAndExpiredLease(t *testing.T) {
	now := time.Date(2026, 8, 30, 12, 0, 0, 0, time.UTC)
	publicKey, privateKey, _ := ed25519.GenerateKey(rand.Reader)
	config := policyTestConfig(t, publicKey)
	store, err := NewPolicyStore(config)
	if err != nil {
		t.Fatal(err)
	}
	bundle := testStoreBundle(now, 2, "sha256:rules-two")
	envelope, _ := policybundle.Sign(privateKey, "test", bundle)
	if _, err := store.Accept(envelope, now); err != nil {
		t.Fatal(err)
	}
	rollback := testStoreBundle(now, 1, "sha256:rules-one")
	rollbackEnvelope, _ := policybundle.Sign(privateKey, "test", rollback)
	if _, err := store.Accept(rollbackEnvelope, now); err == nil {
		t.Fatal("rollback bundle was accepted")
	}
	equivocation := testStoreBundle(now, 2, "sha256:different-rules")
	equivocationEnvelope, _ := policybundle.Sign(privateKey, "test", equivocation)
	if _, err := store.Accept(equivocationEnvelope, now); err == nil {
		t.Fatal("same-version different-rules bundle was accepted")
	}
	if _, _, err := store.Current(now.Add(2 * time.Hour)); err == nil {
		t.Fatal("expired offline synchronization lease was accepted")
	}
}

func TestPolicyStoreRejectsTamperedEnvelope(t *testing.T) {
	now := time.Now().UTC()
	publicKey, privateKey, _ := ed25519.GenerateKey(rand.Reader)
	store, _ := NewPolicyStore(policyTestConfig(t, publicKey))
	envelope, _ := policybundle.Sign(privateKey, "test", testStoreBundle(now, 1, "sha256:rules"))
	envelope.Payload[0] ^= 1
	if _, err := store.Accept(envelope, now); err == nil {
		t.Fatal("tampered envelope was accepted")
	}
}

func TestProtocolVersionsUseNumericOrdering(t *testing.T) {
	if err := checkProtocolVersion("2", "10"); err == nil {
		t.Fatal("protocol version 2 satisfied required version 10")
	}
	if err := checkProtocolVersion("10", "2"); err != nil {
		t.Fatal("protocol version 10 did not satisfy required version 2")
	}
	if err := checkProtocolVersion("invalid", "2"); err == nil {
		t.Fatal("invalid protocol version was accepted")
	}
}

func testStoreBundle(now time.Time, version uint64, digest string) policybundle.Bundle {
	policy, _ := os.ReadFile(filepath.Join("..", "..", "bap-service", "policies", "agent-tools.cedar"))
	return policybundle.Bundle{SchemaVersion: policybundle.SchemaVersion, Version: version, RulesDigest: digest, IssuedAt: now, ExpiresAt: now.Add(24 * time.Hour), RefreshAfterSeconds: 60, MaxOfflineSeconds: 3600, MinimumEdgeVersion: "1", RevocationEpoch: version, PolicyProfile: "standard-developer", CedarPolicy: string(policy)}
}
