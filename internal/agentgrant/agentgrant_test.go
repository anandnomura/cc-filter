package agentgrant

import (
	"crypto/ed25519"
	"crypto/rand"
	"testing"
	"time"
)

func TestAgentGrantBindsRequestPolicyAndLifetime(t *testing.T) {
	publicKey, privateKey, _ := ed25519.GenerateKey(rand.Reader)
	now := time.Now().UTC().Truncate(time.Second)
	claims := Claims{
		Issuer: "bap-agent-sts", Audience: "https://gateway.company.example/", Resource: "https://gateway.company.example/", GrantID: "ag_test", Subject: "agent",
		Principal: "device", CredentialFingerprint: "fp", EdgeInstanceID: "edge", SessionID: "session",
		WorkloadID: "workload", ToolUseID: "tool-use", Tool: "GatewayRequest", Action: "gateway.execute",
		OperationResourceID: "resource", RequestHash: "request", IntentID: "intent-1", IntentHash: "intent", IntentRuleIDs: []string{"intent.deploy"}, PolicyRuleIDs: []string{"agentgrant.deploy"},
		PolicyVersion: 7, PolicyDigest: "sha256:policy", RevocationEpoch: 3, MaxUses: 1,
		IssuedAt: now.Unix(), NotBefore: now.Unix(), ExpiresAt: now.Add(time.Minute).Unix(),
	}
	token, err := Sign(privateKey, claims)
	if err != nil {
		t.Fatal(err)
	}
	options := VerifyOptions{Issuer: "bap-agent-sts", Audience: "https://gateway.company.example/", Resource: "https://gateway.company.example/", RequestHash: "request", PolicyVersion: 7, PolicyDigest: "sha256:policy", RevocationEpoch: 3, Now: now.Add(time.Second)}
	if _, err := Verify(publicKey, token, options); err != nil {
		t.Fatal(err)
	}
	for name, mutate := range map[string]func(*VerifyOptions){
		"request":  func(v *VerifyOptions) { v.RequestHash = "other" },
		"resource": func(v *VerifyOptions) { v.Resource = "https://other.company.example/" },
		"policy":   func(v *VerifyOptions) { v.PolicyVersion++ },
		"epoch":    func(v *VerifyOptions) { v.RevocationEpoch++ },
		"expiry":   func(v *VerifyOptions) { v.Now = now.Add(time.Minute) },
	} {
		t.Run(name, func(t *testing.T) {
			changed := options
			mutate(&changed)
			if _, err := Verify(publicKey, token, changed); err == nil {
				t.Fatal("changed binding was accepted")
			}
		})
	}
}
