package bapedge

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"cc-filter/internal/auditwire"
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
	path := s.path(sessionID)
	if data, err := os.ReadFile(path); err == nil {
		return decodeWorkload(data)
	} else if !os.IsNotExist(err) {
		return "", err
	}
	workloadID := "bapw_" + randomHex(24)
	data, _ := json.Marshal(map[string]string{"workload_id": workloadID})
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if os.IsExist(err) {
		return s.LoadOrCreate(sessionID)
	}
	if err != nil {
		return "", err
	}
	if _, err := file.Write(data); err != nil {
		_ = file.Close()
		return "", err
	}
	if err := file.Close(); err != nil {
		return "", err
	}
	return workloadID, nil
}

func (s *SessionStore) Remove(sessionID string) error {
	err := os.Remove(s.path(sessionID))
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

func (s *SessionStore) path(sessionID string) string {
	sum := sha256.Sum256([]byte(sessionID))
	return filepath.Join(s.directory, hex.EncodeToString(sum[:])+".json")
}

func decodeWorkload(data []byte) (string, error) {
	var value struct {
		WorkloadID string `json:"workload_id"`
	}
	if err := json.Unmarshal(data, &value); err != nil || value.WorkloadID == "" {
		return "", fmt.Errorf("invalid BAP session state")
	}
	return value.WorkloadID, nil
}

type queuedAudit struct {
	Kind    string          `json:"kind"`
	Payload json.RawMessage `json:"payload"`
}

type AuditSpool struct{ directory string }

func NewAuditSpool(configured string) (*AuditSpool, error) {
	base, err := stateDirectory(configured)
	if err != nil {
		return nil, err
	}
	directory := filepath.Join(base, "audit-spool")
	if err := os.MkdirAll(directory, 0700); err != nil {
		return nil, err
	}
	return &AuditSpool{directory: directory}, nil
}

func (s *AuditSpool) QueueOutcome(value auditwire.Outcome) error {
	return s.queue("outcome", value)
}

func (s *AuditSpool) QueueEdgeDenial(value auditwire.EdgeDenial) error {
	return s.queue("edge-denial", value)
}

func (s *AuditSpool) queue(kind string, value any) error {
	payload, err := json.Marshal(value)
	if err != nil {
		return err
	}
	data, _ := json.Marshal(queuedAudit{Kind: kind, Payload: payload})
	path := filepath.Join(s.directory, randomHex(16)+".json")
	return os.WriteFile(path, data, 0600)
}

func (s *AuditSpool) Flush(ctx context.Context, client *Client) error {
	entries, err := os.ReadDir(s.directory)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		path := filepath.Join(s.directory, entry.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		var queued queuedAudit
		if err := json.Unmarshal(data, &queued); err != nil {
			return err
		}
		switch queued.Kind {
		case "outcome":
			var value auditwire.Outcome
			if err := json.Unmarshal(queued.Payload, &value); err != nil {
				return err
			}
			err = client.ReportOutcome(ctx, value)
		case "edge-denial":
			var value auditwire.EdgeDenial
			if err := json.Unmarshal(queued.Payload, &value); err != nil {
				return err
			}
			err = client.ReportEdgeDenial(ctx, value)
		default:
			return fmt.Errorf("unknown queued audit kind %q", queued.Kind)
		}
		if err != nil {
			return err
		}
		if err := os.Remove(path); err != nil {
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
