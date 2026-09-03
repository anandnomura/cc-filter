package bapedge

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"bap-system/internal/agentgrant"
	"bap-system/internal/auditwire"
	"bap-system/internal/authzen"
	"bap-system/internal/policybundle"
)

func stateDirectory(configured string) (string, error) {
	if configured != "" {
		return configured, nil
	}
	base, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "BAP Edge"), nil
}

type SessionStore struct{ directory string }

type sessionState struct {
	SchemaVersion int                        `json:"schema_version"`
	WorkloadID    string                     `json:"workload_id"`
	CreatedAt     time.Time                  `json:"created_at"`
	LastActivity  time.Time                  `json:"last_activity"`
	PolicyVersion uint64                     `json:"policy_version,omitempty"`
	PolicyDigest  string                     `json:"policy_digest,omitempty"`
	Intent        *agentgrant.IntentEvidence `json:"intent,omitempty"`
	Events        []sessionEvent             `json:"events,omitempty"`
}

type sessionEvent struct {
	ToolUseID    string    `json:"tool_use_id"`
	Capabilities []string  `json:"capabilities"`
	ResourceID   string    `json:"resource_id"`
	Status       string    `json:"status"`
	OccurredAt   time.Time `json:"occurred_at"`
}

type SessionReservation struct {
	Reserved bool
	Decision policybundle.SessionDecision
}

func NewSessionStore(configured string) (*SessionStore, error) {
	base, err := stateDirectory(configured)
	if err != nil {
		return nil, err
	}
	directory := filepath.Join(base, "sessions")
	if err := os.MkdirAll(directory, 0700); err != nil {
		return nil, err
	}
	return &SessionStore{directory: directory}, nil
}

func (s *SessionStore) LoadOrCreate(sessionID string) (string, error) {
	if sessionID == "" {
		return "", fmt.Errorf("Claude session_id is missing")
	}
	var workloadID string
	err := s.mutateWithTouch(sessionID, true, false, func(state *sessionState) error { workloadID = state.WorkloadID; return nil })
	return workloadID, err
}

// RecordIntent stores only a random nonce, prompt hash, and signed-policy rule
// IDs. Prompt text is deliberately excluded from local state and STS requests.
func (s *SessionStore) RecordIntent(sessionID, workloadID, prompt string, ruleIDs []string, now time.Time) (agentgrant.IntentEvidence, error) {
	intentID, err := agentgrant.NewIntentID()
	if err != nil {
		return agentgrant.IntentEvidence{}, err
	}
	intent := agentgrant.IntentEvidence{IntentID: intentID, SessionID: sessionID, WorkloadID: workloadID, IntentHash: agentgrant.HashIntent(prompt), RuleIDs: append([]string(nil), ruleIDs...), CapturedAt: now.UTC().Unix()}
	if err := s.mutateWithTouch(sessionID, false, false, func(state *sessionState) error {
		if state.WorkloadID != workloadID {
			return errors.New("session workload binding changed")
		}
		state.Intent = &intent
		return nil
	}); err != nil {
		return intent, err
	}
	return intent, nil
}

func (s *SessionStore) ClearIntent(sessionID, workloadID string) error {
	return s.mutateWithTouch(sessionID, false, false, func(state *sessionState) error {
		if state.WorkloadID != workloadID {
			return errors.New("session workload binding changed")
		}
		state.Intent = nil
		return nil
	})
}

func (s *SessionStore) LoadIntent(sessionID string) (agentgrant.IntentEvidence, error) {
	var intent agentgrant.IntentEvidence
	err := s.mutateWithTouch(sessionID, false, false, func(state *sessionState) error {
		if state.Intent == nil {
			return fmt.Errorf("no classified AgentGrant intent exists for this session")
		}
		intent = *state.Intent
		return nil
	})
	return intent, err
}

func (s *SessionStore) ReserveOperation(sessionID, workloadID, toolUseID string, request authzen.EvaluationRequest, bundle policybundle.Bundle, now time.Time) (SessionReservation, error) {
	result := SessionReservation{}
	err := s.mutate(sessionID, false, func(state *sessionState) error {
		if state.WorkloadID != workloadID {
			return errors.New("session workload binding changed")
		}
		if state.PolicyDigest != "" && (state.PolicyDigest != bundle.RulesDigest || state.PolicyVersion != bundle.Version) {
			return errors.New("signed policy changed during this session; start a new Claude session")
		}
		if now.Sub(state.CreatedAt) > time.Duration(bundle.SessionPolicy.MaxLifetimeSeconds)*time.Second || now.Sub(state.LastActivity) > time.Duration(bundle.SessionPolicy.IdleTimeoutSeconds)*time.Second {
			return errors.New("session lifetime or idle limit exceeded; start a new Claude session")
		}
		for _, event := range state.Events {
			if event.ToolUseID == toolUseID {
				return errors.New("duplicate tool_use_id replay in this session")
			}
		}
		history := make([]policybundle.SessionEvent, 0, len(state.Events))
		for _, event := range state.Events {
			history = append(history, policybundle.SessionEvent{Capabilities: event.Capabilities, ResourceID: event.ResourceID, Status: event.Status, OccurredAt: event.OccurredAt})
		}
		result.Decision = policybundle.EvaluateSession(bundle, request, history, now)
		if !result.Decision.Allowed {
			return nil
		}
		if len(result.Decision.Capabilities) == 0 {
			return nil
		}
		if len(state.Events) >= bundle.SessionPolicy.MaxEvents {
			return errors.New("session capability ledger is full; start a new Claude session")
		}
		state.PolicyVersion, state.PolicyDigest = bundle.Version, bundle.RulesDigest
		state.Events = append(state.Events, sessionEvent{ToolUseID: toolUseID, Capabilities: result.Decision.Capabilities, ResourceID: request.Resource.ID, Status: "pending", OccurredAt: now.UTC()})
		result.Reserved = true
		return nil
	})
	return result, err
}

func (s *SessionStore) CompleteOperation(sessionID, workloadID, toolUseID, status string) error {
	if status != "success" && status != "failure" {
		return errors.New("invalid session operation outcome")
	}
	return s.mutate(sessionID, false, func(state *sessionState) error {
		if state.WorkloadID != workloadID {
			return errors.New("session workload binding changed")
		}
		for i := range state.Events {
			if state.Events[i].ToolUseID == toolUseID {
				if state.Events[i].Status != "pending" {
					return errors.New("session operation was already completed")
				}
				state.Events[i].Status = status
				return nil
			}
		}
		return nil // operations without a configured capability have no reservation
	})
}

func (s *SessionStore) Remove(sessionID string) error {
	release, err := s.lock(sessionID)
	if err != nil {
		return err
	}
	defer release()
	err = os.Remove(s.path(sessionID))
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

func (s *SessionStore) mutate(sessionID string, create bool, fn func(*sessionState) error) error {
	return s.mutateWithTouch(sessionID, create, true, fn)
}

func (s *SessionStore) mutateWithTouch(sessionID string, create, touch bool, fn func(*sessionState) error) error {
	release, err := s.lock(sessionID)
	if err != nil {
		return err
	}
	defer release()
	path := s.path(sessionID)
	var state sessionState
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) && create {
		now := time.Now().UTC()
		state = sessionState{SchemaVersion: 1, WorkloadID: "bapw_" + randomHex(24), CreatedAt: now, LastActivity: now}
	} else if err != nil {
		return err
	} else if json.Unmarshal(data, &state) != nil || state.SchemaVersion != 1 || state.WorkloadID == "" {
		return errors.New("invalid BAP session state")
	}
	if err := fn(&state); err != nil {
		return err
	}
	if touch {
		state.LastActivity = time.Now().UTC()
	}
	encoded, err := json.Marshal(state)
	if err != nil {
		return err
	}
	temporary := path + "." + randomHex(6) + ".tmp"
	file, err := os.OpenFile(temporary, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	if _, err = file.Write(encoded); err == nil {
		err = file.Sync()
	}
	closeErr := file.Close()
	if err == nil {
		err = closeErr
	}
	if err == nil {
		for attempt := 0; attempt < 10; attempt++ {
			err = os.Rename(temporary, path)
			if err == nil {
				break
			}
			time.Sleep(10 * time.Millisecond)
		}
	}
	if err != nil {
		_ = os.Remove(temporary)
	}
	return err
}

func (s *SessionStore) lock(sessionID string) (func(), error) {
	path := s.path(sessionID) + ".lock"
	deadline := time.Now().Add(5 * time.Second)
	for {
		if err := os.Mkdir(path, 0700); err == nil {
			return func() { _ = os.Remove(path) }, nil
		} else {
			// On Windows a concurrent CreateDirectory can surface as access
			// denied rather than os.ErrExist. The directory's presence is the
			// authoritative indication that another Edge process owns the lock.
			if _, statErr := os.Stat(path); statErr != nil {
				if os.IsNotExist(statErr) {
					continue
				}
				return nil, err
			}
		}
		if info, err := os.Stat(path); err == nil && time.Since(info.ModTime()) > 30*time.Second {
			_ = os.Remove(path)
			continue
		}
		if time.Now().After(deadline) {
			return nil, errors.New("timed out acquiring session state lock")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func (s *SessionStore) path(sessionID string) string {
	sum := sha256.Sum256([]byte(sessionID))
	return filepath.Join(s.directory, hex.EncodeToString(sum[:])+".json")
}

func decodeWorkload(data []byte) (string, error) {
	var value sessionState
	if err := json.Unmarshal(data, &value); err != nil || value.WorkloadID == "" {
		return "", fmt.Errorf("invalid BAP session state")
	}
	return value.WorkloadID, nil
}

type queuedAudit struct {
	Kind    string          `json:"kind"`
	Payload json.RawMessage `json:"payload"`
}

type AuditSpool struct {
	directory  string
	maxEntries int
	maxBytes   int64
}

const (
	auditSpoolMaxEntries    = 10000
	auditSpoolMaxBytes      = 64 * 1024 * 1024
	auditSpoolMaxEntryBytes = 1024 * 1024
	auditClaimStaleAfter    = 5 * time.Minute
)

type AuditSpoolStats struct {
	Depth     int
	Bytes     int64
	OldestAge time.Duration
}

func NewAuditSpool(configured string) (*AuditSpool, error) {
	base, err := stateDirectory(configured)
	if err != nil {
		return nil, err
	}
	directory := filepath.Join(base, "audit-spool")
	if err := os.MkdirAll(directory, 0700); err != nil {
		return nil, err
	}
	spool := &AuditSpool{directory: directory, maxEntries: auditSpoolMaxEntries, maxBytes: auditSpoolMaxBytes}
	if err := spool.recoverStaleClaims(time.Now().UTC()); err != nil {
		return nil, err
	}
	return spool, nil
}

func (s *AuditSpool) QueueOutcome(value auditwire.Outcome) error {
	return s.queue("outcome", value)
}

func (s *AuditSpool) QueueEdgeDenial(value auditwire.EdgeDenial) error {
	return s.queue("edge-denial", value)
}

func (s *AuditSpool) QueueEdgeDecision(value auditwire.EdgeDecision) error {
	return s.queue("edge-decision", value)
}

func (s *AuditSpool) RecordEdgeDecision(ctx context.Context, client *Client, value auditwire.EdgeDecision) (bool, error) {
	path, err := s.queueFile("edge-decision", value)
	if err != nil {
		return false, err
	}
	if err := client.ReportEdgeDecision(ctx, value); err != nil {
		return false, nil
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return true, err
	}
	return true, nil
}

func (s *AuditSpool) queue(kind string, value any) error {
	_, err := s.queueFile(kind, value)
	return err
}

func (s *AuditSpool) queueFile(kind string, value any) (string, error) {
	payload, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	data, err := json.Marshal(queuedAudit{Kind: kind, Payload: payload})
	if err != nil {
		return "", err
	}
	if len(data) > auditSpoolMaxEntryBytes {
		return "", fmt.Errorf("audit event exceeds spool entry limit")
	}
	stats, err := s.Stats(time.Now().UTC())
	if err != nil {
		return "", err
	}
	if s.maxEntries > 0 && stats.Depth >= s.maxEntries {
		return "", fmt.Errorf("audit spool entry limit reached")
	}
	if s.maxBytes > 0 && stats.Bytes+int64(len(data)) > s.maxBytes {
		return "", fmt.Errorf("audit spool byte limit reached")
	}
	path := filepath.Join(s.directory, randomHex(16)+".json")
	temporary, err := os.CreateTemp(s.directory, "audit-*.tmp")
	if err != nil {
		return "", err
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(0600); err != nil {
		_ = temporary.Close()
		return "", err
	}
	if _, err := temporary.Write(data); err != nil {
		_ = temporary.Close()
		return "", err
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return "", err
	}
	if err := temporary.Close(); err != nil {
		return "", err
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return "", err
	}
	return path, nil
}

func (s *AuditSpool) Flush(ctx context.Context, client *Client) error {
	entries, err := os.ReadDir(s.directory)
	if err != nil {
		return err
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		path := filepath.Join(s.directory, entry.Name())
		claimPath := strings.TrimSuffix(path, ".json") + ".sending"
		if err := os.Rename(path, claimPath); err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return err
		}
		data, err := os.ReadFile(claimPath)
		if err != nil {
			_ = os.Rename(claimPath, path)
			return err
		}
		var queued queuedAudit
		if err := json.Unmarshal(data, &queued); err != nil {
			_ = os.Rename(claimPath, path)
			return err
		}
		switch queued.Kind {
		case "outcome":
			var value auditwire.Outcome
			if err := json.Unmarshal(queued.Payload, &value); err != nil {
				_ = os.Rename(claimPath, path)
				return err
			}
			err = client.ReportOutcome(ctx, value)
		case "edge-denial":
			var value auditwire.EdgeDenial
			if err := json.Unmarshal(queued.Payload, &value); err != nil {
				_ = os.Rename(claimPath, path)
				return err
			}
			err = client.ReportEdgeDenial(ctx, value)
		case "edge-decision":
			var value auditwire.EdgeDecision
			if err := json.Unmarshal(queued.Payload, &value); err != nil {
				_ = os.Rename(claimPath, path)
				return err
			}
			err = client.ReportEdgeDecision(ctx, value)
		default:
			_ = os.Rename(claimPath, path)
			return fmt.Errorf("unknown queued audit kind %q", queued.Kind)
		}
		if err != nil {
			_ = os.Rename(claimPath, path)
			return err
		}
		if err := os.Remove(claimPath); err != nil {
			return err
		}
	}
	return nil
}

func (s *AuditSpool) Stats(now time.Time) (AuditSpoolStats, error) {
	entries, err := os.ReadDir(s.directory)
	if err != nil {
		return AuditSpoolStats{}, err
	}
	var stats AuditSpoolStats
	var oldest time.Time
	for _, entry := range entries {
		extension := filepath.Ext(entry.Name())
		if entry.IsDir() || extension != ".json" && extension != ".sending" {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			return AuditSpoolStats{}, err
		}
		stats.Depth++
		stats.Bytes += info.Size()
		if oldest.IsZero() || info.ModTime().Before(oldest) {
			oldest = info.ModTime()
		}
	}
	if !oldest.IsZero() {
		stats.OldestAge = now.Sub(oldest)
		if stats.OldestAge < 0 {
			stats.OldestAge = 0
		}
	}
	return stats, nil
}

func (s *AuditSpool) recoverStaleClaims(now time.Time) error {
	entries, err := os.ReadDir(s.directory)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".sending" {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if now.Sub(info.ModTime()) < auditClaimStaleAfter {
			continue
		}
		claimPath := filepath.Join(s.directory, entry.Name())
		queuePath := strings.TrimSuffix(claimPath, ".sending") + ".json"
		if err := os.Rename(claimPath, queuePath); err != nil && !os.IsExist(err) {
			return err
		}
	}
	return nil
}

func randomHex(bytes int) string {
	value := make([]byte, bytes)
	if _, err := rand.Read(value); err != nil {
		panic("operating system random source unavailable")
	}
	return hex.EncodeToString(value)
}

func AuditEventID(kind string, values ...string) string {
	sum := sha256.Sum256([]byte(kind + "\x00" + strings.Join(values, "\x00")))
	return hex.EncodeToString(sum[:16])
}
