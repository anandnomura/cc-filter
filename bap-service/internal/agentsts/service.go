// Package agentsts implements the online issuance and atomic consumption of
// short-lived, one-operation AgentGrants. The in-memory ledger is suitable for
// the MVP reference implementation; production replicas must use a shared,
// strongly consistent store for the ISSUED -> CONSUMED transition.
package agentsts

import (
	"crypto/ed25519"
	"errors"
	"fmt"
	"sync"
	"time"

	"cc-filter/internal/agentgrant"
	"cc-filter/internal/policybundle"
)

type grantState struct {
	status    string
	expiresAt time.Time
}

// Ledger owns the atomic one-use state transition. A production implementation
// can use a transactional database without changing issuance or token logic.
type Ledger interface {
	Reserve(grantID string, expiresAt time.Time, now time.Time) error
	Consume(grantID string, now time.Time) error
}

type MemoryLedger struct {
	mu     sync.Mutex
	grants map[string]grantState
}

func NewMemoryLedger() *MemoryLedger { return &MemoryLedger{grants: map[string]grantState{}} }

type Service struct {
	privateKey ed25519.PrivateKey
	issuer     string
	ledger     Ledger
}

func New(privateKey ed25519.PrivateKey, issuer string) *Service {
	return NewWithLedger(privateKey, issuer, NewMemoryLedger())
}

func NewWithLedger(privateKey ed25519.PrivateKey, issuer string, ledger Ledger) *Service {
	return &Service{privateKey: privateKey, issuer: issuer, ledger: ledger}
}

func (s *Service) Issue(request agentgrant.IssueRequest, principal, fingerprint string, bundle policybundle.Bundle, now time.Time) (agentgrant.IssueResponse, agentgrant.Claims, error) {
	var empty agentgrant.Claims
	if len(s.privateKey) != ed25519.PrivateKeySize || s.issuer == "" || s.ledger == nil || principal == "" || fingerprint == "" {
		return agentgrant.IssueResponse{}, empty, errors.New("Agent STS is not configured")
	}
	if err := request.Validate(); err != nil {
		return agentgrant.IssueResponse{}, empty, err
	}
	now = now.UTC()
	capturedAt := time.Unix(request.Intent.CapturedAt, 0).UTC()
	decision, err := policybundle.Authorize(bundle, request.Operation, now)
	if err != nil {
		return agentgrant.IssueResponse{}, empty, err
	}
	if !decision.AgentGrantRequired {
		return agentgrant.IssueResponse{}, empty, fmt.Errorf("operation is not eligible for AgentGrant: %s", decision.ReasonCode)
	}
	if request.Intent.CapturedAt <= 0 || capturedAt.After(now.Add(30*time.Second)) || decision.GrantIntentMaxAge <= 0 || now.Sub(capturedAt) > decision.GrantIntentMaxAge {
		return agentgrant.IssueResponse{}, empty, errors.New("intent evidence is stale or not yet valid")
	}
	matchedIntent := intersection(request.Intent.RuleIDs, decision.RequiredIntentRuleIDs)
	if len(matchedIntent) == 0 {
		return agentgrant.IssueResponse{}, empty, errors.New("classified intent does not satisfy the current AgentGrant policy")
	}
	requestHash, err := agentgrant.HashOperation(request.Operation)
	if err != nil {
		return agentgrant.IssueResponse{}, empty, err
	}
	grantID, err := agentgrant.NewID()
	if err != nil {
		return agentgrant.IssueResponse{}, empty, err
	}
	sessionID, _ := request.Operation.Context["session_id"].(string)
	workloadID, _ := request.Operation.Context["workload_id"].(string)
	toolUseID, _ := request.Operation.Context["tool_use_id"].(string)
	tool, _ := request.Operation.Resource.Properties["tool"].(string)
	claims := agentgrant.Claims{
		Issuer: s.issuer, Audience: decision.GrantAudience, GrantID: grantID,
		Subject: request.Operation.Subject.ID, Principal: principal, CredentialFingerprint: fingerprint,
		EdgeInstanceID: request.EdgeInstanceID, SessionID: sessionID, WorkloadID: workloadID,
		ToolUseID: toolUseID, Tool: tool, Action: request.Operation.Action.Name,
		Resource: request.Operation.Resource.ID, RequestHash: requestHash,
		IntentHash: request.Intent.IntentHash, IntentRuleIDs: matchedIntent, PolicyRuleIDs: append([]string(nil), decision.RuleIDs...),
		PolicyVersion: bundle.Version, PolicyDigest: bundle.RulesDigest, RevocationEpoch: bundle.RevocationEpoch,
		MaxUses: 1, IssuedAt: now.Unix(), NotBefore: now.Add(-2 * time.Second).Unix(), ExpiresAt: now.Add(decision.GrantTTL).Unix(),
	}
	token, err := agentgrant.Sign(s.privateKey, claims)
	if err != nil {
		return agentgrant.IssueResponse{}, empty, err
	}
	if err := s.ledger.Reserve(grantID, time.Unix(claims.ExpiresAt, 0).UTC(), now); err != nil {
		return agentgrant.IssueResponse{}, empty, err
	}
	return agentgrant.IssueResponse{Token: token, GrantID: grantID, ExpiresAt: time.Unix(claims.ExpiresAt, 0).UTC().Format(time.RFC3339), Audience: claims.Audience}, claims, nil
}

func (s *Service) Consume(request agentgrant.ConsumeRequest, bundle policybundle.Bundle, now time.Time) (agentgrant.ConsumeResponse, agentgrant.Claims, error) {
	var empty agentgrant.Claims
	if request.Token == "" || request.Operation.Validate() != nil {
		return agentgrant.ConsumeResponse{}, empty, errors.New("a valid operation and AgentGrant are required")
	}
	hash, err := agentgrant.HashOperation(request.Operation)
	if err != nil {
		return agentgrant.ConsumeResponse{}, empty, err
	}
	decision, err := policybundle.Authorize(bundle, request.Operation, now)
	if err != nil || !decision.AgentGrantRequired {
		return agentgrant.ConsumeResponse{}, empty, errors.New("operation is no longer AgentGrant eligible")
	}
	claims, err := agentgrant.Verify(s.privateKey.Public().(ed25519.PublicKey), request.Token, agentgrant.VerifyOptions{
		Issuer: s.issuer, Audience: decision.GrantAudience, RequestHash: hash, PolicyVersion: bundle.Version,
		PolicyDigest: bundle.RulesDigest, RevocationEpoch: bundle.RevocationEpoch, Now: now,
	})
	if err != nil {
		return agentgrant.ConsumeResponse{}, empty, err
	}
	if err := s.ledger.Consume(claims.GrantID, now.UTC()); err != nil {
		return agentgrant.ConsumeResponse{}, empty, err
	}
	return agentgrant.ConsumeResponse{Consumed: true, GrantID: claims.GrantID}, claims, nil
}

func (l *MemoryLedger) Reserve(grantID string, expiresAt time.Time, now time.Time) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.purgeExpiredLocked(now)
	if _, exists := l.grants[grantID]; exists {
		return errors.New("AgentGrant ID already exists")
	}
	l.grants[grantID] = grantState{status: "ISSUED", expiresAt: expiresAt.UTC()}
	return nil
}

func (l *MemoryLedger) Consume(grantID string, now time.Time) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	state, found := l.grants[grantID]
	if !found || state.status != "ISSUED" || !now.UTC().Before(state.expiresAt) {
		return errors.New("AgentGrant is unknown, expired, or already consumed")
	}
	state.status = "CONSUMED"
	l.grants[grantID] = state
	return nil
}

func (l *MemoryLedger) purgeExpiredLocked(now time.Time) {
	for id, state := range l.grants {
		if !now.Before(state.expiresAt) {
			delete(l.grants, id)
		}
	}
}

func intersection(left, right []string) []string {
	allowed := make(map[string]struct{}, len(right))
	for _, value := range right {
		allowed[value] = struct{}{}
	}
	result := make([]string, 0)
	for _, value := range left {
		if _, ok := allowed[value]; ok {
			result = append(result, value)
		}
	}
	return result
}
