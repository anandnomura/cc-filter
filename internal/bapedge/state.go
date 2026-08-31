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
	"sort"
	"strings"
	"time"

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
