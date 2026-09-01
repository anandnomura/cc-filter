package audit

import (
	"bufio"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type Event struct {
	EventID               string    `json:"event_id"`
	EventType             string    `json:"event_type"`
	Timestamp             time.Time `json:"timestamp"`
	TraceID               string    `json:"trace_id,omitempty"`
	SpanID                string    `json:"span_id,omitempty"`
	ParentSpanID          string    `json:"parent_span_id,omitempty"`
	Source                string    `json:"source,omitempty"`
	ToolUseID             string    `json:"tool_use_id,omitempty"`
	SessionID             string    `json:"session_id,omitempty"`
	WorkloadID            string    `json:"workload_id,omitempty"`
	CredentialFingerprint string    `json:"credential_fingerprint,omitempty"`
	Principal             string    `json:"principal,omitempty"`
	AssertedUser          string    `json:"asserted_user,omitempty"`
	Subject               string    `json:"subject,omitempty"`
	Action                string    `json:"action,omitempty"`
	Tool                  string    `json:"tool,omitempty"`
	ResourceID            string    `json:"resource_id,omitempty"`
	TargetSummary         string    `json:"target_summary,omitempty"`
	DecisionID            string    `json:"decision_id,omitempty"`
	GrantID               string    `json:"grant_id,omitempty"`
	IntentHash            string    `json:"intent_hash,omitempty"`
	Allowed               *bool     `json:"allowed,omitempty"`
	ReasonCode            string    `json:"reason_code,omitempty"`
	PolicyVersion         string    `json:"policy_version,omitempty"`
	Outcome               string    `json:"outcome,omitempty"`
	PreviousHash          string    `json:"previous_hash,omitempty"`
	EventHash             string    `json:"event_hash"`
	Signature             string    `json:"signature"`
}

type Store struct {
	path        string
	key         ed25519.PrivateKey
	mu          sync.Mutex
	initialized bool
	lastHash    string
	eventIDs    map[string]struct{}
	allowedOps  map[string]struct{}
}

func New(path string, key ed25519.PrivateKey) *Store { return &Store{path: path, key: key} }

func (s *Store) Initialize() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.initializeLocked()
}

func (s *Store) initializeLocked() error {
	if s.initialized {
		return nil
	}
	if len(s.key) != ed25519.PrivateKeySize {
		return fmt.Errorf("audit signing key is invalid")
	}
	events, err := ReadAndVerify(s.path, s.key.Public().(ed25519.PublicKey))
	if err != nil {
		return err
	}
	s.eventIDs = make(map[string]struct{}, len(events))
	s.allowedOps = make(map[string]struct{})
	for _, event := range events {
		s.index(event)
		s.lastHash = event.EventHash
	}
	s.initialized = true
	return nil
}

func (s *Store) index(event Event) {
	s.eventIDs[event.EventID] = struct{}{}
	if event.EventType == "authorization_decision" && event.Allowed != nil && *event.Allowed {
		s.allowedOps[operationKey(event.SessionID, event.WorkloadID, event.ToolUseID, event.CredentialFingerprint)] = struct{}{}
	}
}

func operationKey(sessionID, workloadID, toolUseID, fingerprint string) string {
	return sessionID + "\x00" + workloadID + "\x00" + toolUseID + "\x00" + fingerprint
}

func (s *Store) Append(event Event) error {
	if event.Timestamp.IsZero() {
		event.Timestamp = time.Now().UTC()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.initializeLocked(); err != nil {
		return err
	}
	event, err := SignEvent(event, s.lastHash, s.key)
	if err != nil {
		return err
	}
	data, err := json.Marshal(event)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0700); err != nil {
		return err
	}
	file, err := os.OpenFile(s.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	if _, err := file.Write(append(data, '\n')); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	s.lastHash = event.EventHash
	s.index(event)
	return nil
}

func (s *Store) Ready(_ context.Context) error { return s.Initialize() }

func (s *Store) Events() ([]Event, error) {
	return ReadAndVerify(s.path, s.key.Public().(ed25519.PublicKey))
}

func (s *Store) HasEvent(eventID string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.initializeLocked(); err != nil {
		return false, err
	}
	_, exists := s.eventIDs[eventID]
	return exists, nil
}

func (s *Store) HasAllowedOperation(sessionID, workloadID, toolUseID, fingerprint string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.initializeLocked(); err != nil {
		return false, err
	}
	_, exists := s.allowedOps[operationKey(sessionID, workloadID, toolUseID, fingerprint)]
	return exists, nil
}

func ReadAndVerify(path string, key ed25519.PublicKey) ([]Event, error) {
	events, err := read(path)
	if err != nil {
		return nil, err
	}
	if err := VerifyEvents(events, key); err != nil {
		return nil, err
	}
	return events, nil
}

func SignEvent(event Event, previousHash string, key ed25519.PrivateKey) (Event, error) {
	if event.Timestamp.IsZero() {
		event.Timestamp = time.Now().UTC()
	}
	event.PreviousHash = previousHash
	event.EventHash, event.Signature = "", ""
	payload, err := json.Marshal(event)
	if err != nil {
		return Event{}, err
	}
	event.Signature = hex.EncodeToString(ed25519.Sign(key, payload))
	sum := sha256.Sum256(append(payload, []byte(event.Signature)...))
	event.EventHash = hex.EncodeToString(sum[:])
	return event, nil
}

func VerifyEvents(events []Event, key ed25519.PublicKey) error {
	previous := ""
	for index, stored := range events {
		if stored.PreviousHash != previous {
			return fmt.Errorf("audit event %d breaks the hash chain", index+1)
		}
		eventHash, signatureHex := stored.EventHash, stored.Signature
		stored.EventHash, stored.Signature = "", ""
		payload, err := json.Marshal(stored)
		if err != nil {
			return err
		}
		signature, err := hex.DecodeString(signatureHex)
		if err != nil || !ed25519.Verify(key, payload, signature) {
			return fmt.Errorf("audit event %d has an invalid signature", index+1)
		}
		sum := sha256.Sum256(append(payload, []byte(signatureHex)...))
		if hex.EncodeToString(sum[:]) != eventHash {
			return fmt.Errorf("audit event %d has an invalid hash", index+1)
		}
		previous = eventHash
	}
	return nil
}

func read(path string) ([]Event, error) {
	file, err := os.Open(path)
	if os.IsNotExist(err) {
		return []Event{}, nil
	}
	if err != nil {
		return nil, err
	}
	defer file.Close()
	var events []Event
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	for scanner.Scan() {
		var event Event
		if err := json.Unmarshal(scanner.Bytes(), &event); err != nil {
			return nil, err
		}
		events = append(events, event)
	}
	return events, scanner.Err()
}
