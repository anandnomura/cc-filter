package mysqlstore

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"cc-filter/bap-service/internal/audit"
	"cc-filter/bap-service/internal/proposals"
	"cc-filter/internal/authzen"
	mysqldriver "github.com/go-sql-driver/mysql"
)

const schemaVersion = 2

type Config struct {
	DSN                   string
	CABundlePath          string
	TLSServerName         string
	AllowInsecure         bool
	MaxOpenConnections    int
	MaxIdleConnections    int
	ConnectionMaxLifetime time.Duration
}

type Store struct {
	db       *sql.DB
	auditKey ed25519.PrivateKey
}

func Open(ctx context.Context, config Config, auditKey ed25519.PrivateKey) (*Store, error) {
	if config.DSN == "" {
		return nil, errors.New("MySQL DSN is required")
	}
	if len(auditKey) != ed25519.PrivateKeySize {
		return nil, errors.New("audit signing key is invalid")
	}
	dsn, err := securedDSN(config)
	if err != nil {
		return nil, err
	}
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return nil, fmt.Errorf("open MySQL: %w", err)
	}
	if config.MaxOpenConnections <= 0 {
		config.MaxOpenConnections = 20
	}
	if config.MaxIdleConnections < 0 {
		config.MaxIdleConnections = 0
	} else if config.MaxIdleConnections == 0 {
		config.MaxIdleConnections = 10
	}
	if config.ConnectionMaxLifetime <= 0 {
		config.ConnectionMaxLifetime = 5 * time.Minute
	}
	db.SetMaxOpenConns(config.MaxOpenConnections)
	db.SetMaxIdleConns(config.MaxIdleConnections)
	db.SetConnMaxLifetime(config.ConnectionMaxLifetime)

	store := &Store{db: db, auditKey: auditKey}
	if err := store.Ready(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("connect to MySQL: %w", err)
	}
	if err := store.migrate(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("migrate MySQL: %w", err)
	}
	if _, err := store.Events(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("verify MySQL audit chain: %w", err)
	}
	return store, nil
}

func securedDSN(config Config) (string, error) {
	parsed, err := mysqldriver.ParseDSN(config.DSN)
	if err != nil {
		return "", fmt.Errorf("parse MySQL DSN: %w", err)
	}
	parsed.ParseTime = true
	parsed.Loc = time.UTC
	if parsed.Timeout == 0 {
		parsed.Timeout = 3 * time.Second
	}
	if parsed.ReadTimeout == 0 {
		parsed.ReadTimeout = 5 * time.Second
	}
	if parsed.WriteTimeout == 0 {
		parsed.WriteTimeout = 5 * time.Second
	}

	if parsed.TLSConfig == "skip-verify" || parsed.TLSConfig == "preferred" {
		if !config.AllowInsecure {
			return "", errors.New("MySQL tls=skip-verify/preferred is forbidden outside explicit development mode")
		}
	}
	if config.CABundlePath != "" {
		caPEM, err := os.ReadFile(config.CABundlePath)
		if err != nil {
			return "", fmt.Errorf("read MySQL CA bundle: %w", err)
		}
		roots, err := x509.SystemCertPool()
		if err != nil || roots == nil {
			roots = x509.NewCertPool()
		}
		if !roots.AppendCertsFromPEM(caPEM) {
			return "", errors.New("MySQL CA bundle contains no certificates")
		}
		nameHash := sha256.Sum256([]byte(config.CABundlePath + "\x00" + config.TLSServerName))
		tlsName := "bap-" + hex.EncodeToString(nameHash[:8])
		if err := mysqldriver.RegisterTLSConfig(tlsName, &tls.Config{
			MinVersion: tls.VersionTLS12,
			RootCAs:    roots,
			ServerName: config.TLSServerName,
		}); err != nil && !strings.Contains(err.Error(), "already registered") {
			return "", fmt.Errorf("register MySQL TLS configuration: %w", err)
		}
		parsed.TLSConfig = tlsName
	}
	if parsed.TLSConfig == "" && !config.AllowInsecure {
		return "", errors.New("MySQL TLS is required; configure tls=true or BAP_DATABASE_TLS_CA_PATH")
	}
	return parsed.FormatDSN(), nil
}

func (s *Store) Close() error { return s.db.Close() }

func (s *Store) Ready(ctx context.Context) error { return s.db.PingContext(ctx) }

func (s *Store) migrate(ctx context.Context) error {
	statements := []string{
		`CREATE TABLE IF NOT EXISTS bap_schema_migrations (
			version INT NOT NULL PRIMARY KEY,
			applied_at DATETIME(6) NOT NULL
		) ENGINE=InnoDB`,
		`CREATE TABLE IF NOT EXISTS bap_audit_chain (
			chain_id TINYINT UNSIGNED NOT NULL PRIMARY KEY,
			last_hash CHAR(64) NOT NULL
		) ENGINE=InnoDB`,
		`INSERT IGNORE INTO bap_audit_chain (chain_id, last_hash) VALUES (1, '')`,
		`CREATE TABLE IF NOT EXISTS bap_audit_events (
			sequence_id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
			event_id VARCHAR(128) NOT NULL,
			event_type VARCHAR(64) NOT NULL,
			timestamp_utc DATETIME(6) NOT NULL,
			trace_id CHAR(32) NOT NULL DEFAULT '',
			span_id CHAR(16) NOT NULL DEFAULT '',
			parent_span_id CHAR(16) NOT NULL DEFAULT '',
			session_id VARCHAR(255) NOT NULL DEFAULT '',
			workload_id VARCHAR(255) NOT NULL DEFAULT '',
			tool_use_id VARCHAR(255) NOT NULL DEFAULT '',
			credential_fingerprint VARCHAR(128) NOT NULL DEFAULT '',
			allowed BOOLEAN NULL,
			previous_hash CHAR(64) NOT NULL,
			event_hash CHAR(64) NOT NULL,
			signature CHAR(128) NOT NULL,
			payload JSON NOT NULL,
			UNIQUE KEY uq_bap_audit_event_id (event_id),
			UNIQUE KEY uq_bap_audit_event_hash (event_hash),
			KEY ix_bap_audit_operation (session_id(64), workload_id(64), tool_use_id(64), credential_fingerprint(64), event_type, allowed),
			KEY ix_bap_audit_trace (trace_id, timestamp_utc),
			KEY ix_bap_audit_timestamp (timestamp_utc)
		) ENGINE=InnoDB`,
		`CREATE TABLE IF NOT EXISTS bap_policy_proposals (
			proposal_id VARCHAR(64) NOT NULL PRIMARY KEY,
			subject_type VARCHAR(128) NOT NULL,
			action_name VARCHAR(128) NOT NULL,
			tool VARCHAR(255) NOT NULL,
			resource_type VARCHAR(128) NOT NULL,
			status ENUM('new','reviewing','accepted','rejected','expired') NOT NULL DEFAULT 'new',
			occurrences BIGINT UNSIGNED NOT NULL DEFAULT 1,
			first_seen DATETIME(6) NOT NULL,
			last_seen DATETIME(6) NOT NULL,
			KEY ix_bap_proposal_status_last_seen (status, last_seen),
			KEY ix_bap_proposal_occurrences (occurrences)
		) ENGINE=InnoDB`,
		`INSERT IGNORE INTO bap_schema_migrations (version, applied_at) VALUES (1, UTC_TIMESTAMP(6))`,
	}
	for _, statement := range statements {
		if _, err := s.db.ExecContext(ctx, statement); err != nil {
			return err
		}
	}
	for _, column := range []struct {
		name       string
		definition string
	}{
		{"trace_id", "CHAR(32) NOT NULL DEFAULT '' AFTER timestamp_utc"},
		{"span_id", "CHAR(16) NOT NULL DEFAULT '' AFTER trace_id"},
		{"parent_span_id", "CHAR(16) NOT NULL DEFAULT '' AFTER span_id"},
	} {
		if err := s.ensureAuditColumn(ctx, column.name, column.definition); err != nil {
			return err
		}
	}
	if err := s.ensureAuditIndex(ctx, "ix_bap_audit_trace", "(trace_id, timestamp_utc)"); err != nil {
		return err
	}
	_, err := s.db.ExecContext(ctx, `INSERT IGNORE INTO bap_schema_migrations (version, applied_at) VALUES (?, UTC_TIMESTAMP(6))`, schemaVersion)
	return err
}

func (s *Store) ensureAuditColumn(ctx context.Context, name, definition string) error {
	var exists bool
	err := s.db.QueryRowContext(ctx, `SELECT EXISTS(
		SELECT 1 FROM information_schema.columns
		WHERE table_schema = DATABASE() AND table_name = 'bap_audit_events' AND column_name = ?
	)`, name).Scan(&exists)
	if err != nil || exists {
		return err
	}
	_, err = s.db.ExecContext(ctx, `ALTER TABLE bap_audit_events ADD COLUMN `+name+` `+definition)
	return err
}

func (s *Store) ensureAuditIndex(ctx context.Context, name, definition string) error {
	var exists bool
	err := s.db.QueryRowContext(ctx, `SELECT EXISTS(
		SELECT 1 FROM information_schema.statistics
		WHERE table_schema = DATABASE() AND table_name = 'bap_audit_events' AND index_name = ?
	)`, name).Scan(&exists)
	if err != nil || exists {
		return err
	}
	_, err = s.db.ExecContext(ctx, `CREATE INDEX `+name+` ON bap_audit_events `+definition)
	return err
}

func (s *Store) Append(event audit.Event) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	var existingHash string
	err = tx.QueryRowContext(ctx, `SELECT event_hash FROM bap_audit_events WHERE event_id = ?`, event.EventID).Scan(&existingHash)
	if err == nil {
		return tx.Commit()
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return err
	}

	var previousHash string
	if err := tx.QueryRowContext(ctx, `SELECT last_hash FROM bap_audit_chain WHERE chain_id = 1 FOR UPDATE`).Scan(&previousHash); err != nil {
		return err
	}
	event, err = audit.SignEvent(event, previousHash, s.auditKey)
	if err != nil {
		return err
	}
	payload, err := json.Marshal(event)
	if err != nil {
		return err
	}
	var allowed any
	if event.Allowed != nil {
		allowed = *event.Allowed
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO bap_audit_events
		(event_id, event_type, timestamp_utc, trace_id, span_id, parent_span_id, session_id, workload_id, tool_use_id, credential_fingerprint, allowed, previous_hash, event_hash, signature, payload)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		event.EventID, event.EventType, event.Timestamp.UTC(), event.TraceID, event.SpanID, event.ParentSpanID, event.SessionID, event.WorkloadID,
		event.ToolUseID, event.CredentialFingerprint, allowed, event.PreviousHash, event.EventHash, event.Signature, payload)
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE bap_audit_chain SET last_hash = ? WHERE chain_id = 1`, event.EventHash); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) HasEvent(eventID string) (bool, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	var exists bool
	err := s.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM bap_audit_events WHERE event_id = ?)`, eventID).Scan(&exists)
	return exists, err
}

func (s *Store) HasAllowedOperation(sessionID, workloadID, toolUseID, fingerprint string) (bool, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	var exists bool
	err := s.db.QueryRowContext(ctx, `SELECT EXISTS(
		SELECT 1 FROM bap_audit_events
		WHERE session_id = ? AND workload_id = ? AND tool_use_id = ? AND credential_fingerprint = ?
		AND event_type = 'authorization_decision' AND allowed = TRUE
	)`, sessionID, workloadID, toolUseID, fingerprint).Scan(&exists)
	return exists, err
}

func (s *Store) Events(ctx context.Context) ([]audit.Event, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT payload FROM bap_audit_events ORDER BY sequence_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var events []audit.Event
	for rows.Next() {
		var payload []byte
		if err := rows.Scan(&payload); err != nil {
			return nil, err
		}
		var event audit.Event
		if err := json.Unmarshal(payload, &event); err != nil {
			return nil, err
		}
		events = append(events, event)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if err := audit.VerifyEvents(events, s.auditKey.Public().(ed25519.PublicKey)); err != nil {
		return nil, err
	}
	var storedHead string
	if err := s.db.QueryRowContext(ctx, `SELECT last_hash FROM bap_audit_chain WHERE chain_id = 1`).Scan(&storedHead); err != nil {
		return nil, err
	}
	expectedHead := ""
	if len(events) > 0 {
		expectedHead = events[len(events)-1].EventHash
	}
	if storedHead != expectedHead {
		return nil, errors.New("MySQL audit chain head does not match the final event")
	}
	return events, nil
}

func (s *Store) Record(request authzen.EvaluationRequest) (string, error) {
	tool, _ := request.Resource.Properties["tool"].(string)
	id := proposalID(request.Subject.Type, request.Action.Name, tool, request.Resource.Type)
	now := time.Now().UTC()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_, err := s.db.ExecContext(ctx, `INSERT INTO bap_policy_proposals
		(proposal_id, subject_type, action_name, tool, resource_type, occurrences, first_seen, last_seen)
		VALUES (?, ?, ?, ?, ?, 1, ?, ?)
		ON DUPLICATE KEY UPDATE occurrences = occurrences + 1, last_seen = VALUES(last_seen)`,
		id, request.Subject.Type, request.Action.Name, tool, request.Resource.Type, now, now)
	if err != nil {
		return "", err
	}
	return id, nil
}

func (s *Store) Proposals(ctx context.Context) ([]proposals.Summary, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT proposal_id, subject_type, action_name, tool, resource_type, first_seen, occurrences, last_seen
		FROM bap_policy_proposals ORDER BY occurrences DESC, last_seen DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []proposals.Summary
	for rows.Next() {
		var summary proposals.Summary
		if err := rows.Scan(&summary.ID, &summary.SubjectType, &summary.Action, &summary.Tool, &summary.ResourceType,
			&summary.FirstSeen, &summary.Occurrences, &summary.LastSeen); err != nil {
			return nil, err
		}
		result = append(result, summary)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	sort.SliceStable(result, func(i, j int) bool { return result[i].Occurrences > result[j].Occurrences })
	return result, nil
}

func proposalID(parts ...string) string {
	hash := sha256.New()
	for _, part := range parts {
		_, _ = hash.Write([]byte(part))
		_, _ = hash.Write([]byte{0})
	}
	return "proposal-" + hex.EncodeToString(hash.Sum(nil))[:16]
}
