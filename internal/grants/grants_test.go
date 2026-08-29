package grants

import (
	"crypto/ed25519"
	"crypto/rand"
	"strings"
	"testing"
	"time"
)

func TestGrantIsSignedBoundAndExpires(t *testing.T) {
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	claims := Claims{Audience: "bap-edge", RequestHash: "request-a", IssuedAt: now.Unix(), ExpiresAt: now.Add(time.Minute).Unix()}
	token, err := Sign(privateKey, claims)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Verify(publicKey, token, "bap-edge", "request-a", now); err != nil {
		t.Fatalf("valid grant rejected: %v", err)
	}
	if _, err := Verify(publicKey, token, "bap-edge", "request-b", now); err == nil {
		t.Fatal("grant was accepted for a different request")
	}
	if _, err := Verify(publicKey, token, "wrong-audience", "request-a", now); err == nil {
		t.Fatal("grant was accepted for a different audience")
	}
	parts := strings.Split(token, ".")
	parts[1] = parts[1] + "A"
	if _, err := Verify(publicKey, strings.Join(parts, "."), "bap-edge", "request-a", now); err == nil {
		t.Fatal("tampered grant was accepted")
	}
	if _, err := Verify(publicKey, token, "bap-edge", "request-a", now.Add(2*time.Minute)); err == nil {
		t.Fatal("expired grant was accepted")
	}
}
