package server

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"bap-system/bap-service/internal/agentsts"
	"bap-system/bap-service/internal/audit"
	"bap-system/bap-service/internal/cedaradapter"
	"bap-system/bap-service/internal/metrics"
	"bap-system/internal/agentgrant"
	"bap-system/internal/auditwire"
	"bap-system/internal/authzen"
	"bap-system/internal/grants"
	"bap-system/internal/policybundle"
	"bap-system/internal/tracecontext"
)

type Server struct {
	engine              *cedaradapter.Engine
	privateKey          ed25519.PrivateKey
	issuer              string
	audience            string
	grantTTL            time.Duration
	proposals           ProposalStore
	audit               AuditStore
	apiKey              string
	principal           string
	metrics             *metrics.Registry
	readinessMu         sync.Mutex
	readinessFailed     bool
	lastReadinessLog    time.Time
	readinessSuppressed uint64
	policyEnvelope      policybundle.Envelope
	policyBundle        policybundle.Bundle
	agentSTS            *agentsts.Service
	role                string
	stsIssueAPIKey      string
	stsIssuePrincipal   string
	stsConsumeAPIKey    string
	stsConsumePrincipal string
	stsConsumers        []AgentSTSConsumer
	legacyAuthZEN       bool
}

// AgentSTSConsumer is one independently authenticated resource PEP. Audiences
// are exact AgentGrant audience values assigned to that PEP.
type AgentSTSConsumer struct {
	APIKey    string
	Principal string
	Audiences []string
}

type ProposalStore interface {
	Record(authzen.EvaluationRequest) (string, error)
}

type AuditStore interface {
	Append(audit.Event) error
	HasEvent(string) (bool, error)
	HasAllowedOperation(string, string, string, string) (bool, error)
	Ready(context.Context) error
}

func New(engine *cedaradapter.Engine, privateKey ed25519.PrivateKey, issuer, audience string, grantTTL time.Duration, proposalStore ProposalStore, auditStore AuditStore, apiKey, principal string) *Server {
	return &Server{engine: engine, privateKey: privateKey, issuer: issuer, audience: audience, grantTTL: grantTTL, proposals: proposalStore, audit: auditStore, apiKey: apiKey, principal: principal, metrics: metrics.New(), agentSTS: agentsts.New(privateKey, issuer), role: "combined", stsIssueAPIKey: apiKey, stsIssuePrincipal: principal, stsConsumeAPIKey: apiKey, stsConsumePrincipal: principal}
}

func (s *Server) SetRole(role string) error {
	if role != "combined" && role != "agent-sts" {
		return fmt.Errorf("unsupported BAP Service role %q", role)
	}
	s.role = role
	return nil
}

func (s *Server) SetAgentSTSClients(issueAPIKey, issuePrincipal, consumeAPIKey, consumePrincipal string) error {
	if issuePrincipal == "" || consumePrincipal == "" {
		return fmt.Errorf("Agent STS issue and consume principals are required")
	}
	if issuePrincipal == consumePrincipal {
		return fmt.Errorf("Agent STS Edge and gateway principals must be distinct")
	}
	s.stsIssueAPIKey, s.stsIssuePrincipal = issueAPIKey, issuePrincipal
	s.stsConsumeAPIKey, s.stsConsumePrincipal = consumeAPIKey, consumePrincipal
	s.stsConsumers = []AgentSTSConsumer{{APIKey: consumeAPIKey, Principal: consumePrincipal}}
	return nil
}

func (s *Server) SetAgentSTSConsumers(consumers []AgentSTSConsumer) error {
	if len(consumers) == 0 {
		return fmt.Errorf("at least one Agent STS resource PEP is required")
	}
	principals, audiences := map[string]bool{}, map[string]bool{}
	for index, consumer := range consumers {
		principal := strings.TrimSpace(consumer.Principal)
		if principal == "" || principal == s.stsIssuePrincipal || principals[strings.ToLower(principal)] || len(consumer.Audiences) == 0 {
			return fmt.Errorf("Agent STS resource PEP %d has an invalid or duplicate principal", index)
		}
		principals[strings.ToLower(principal)] = true
		for _, audience := range consumer.Audiences {
			audience = strings.TrimSpace(audience)
			if audience == "" || audiences[strings.ToLower(audience)] {
				return fmt.Errorf("Agent STS resource PEP %d has an invalid or duplicate audience", index)
			}
			audiences[strings.ToLower(audience)] = true
		}
	}
	s.stsConsumers = append([]AgentSTSConsumer(nil), consumers...)
	return nil
}

func (s *Server) SetAgentSTSLedger(ledger agentsts.Ledger) error {
	if ledger == nil {
		return fmt.Errorf("Agent STS ledger is required")
	}
	s.agentSTS = agentsts.NewWithLedger(s.privateKey, s.issuer, ledger)
	return nil
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", s.health)
	mux.HandleFunc("GET /readyz", s.ready)
	mux.HandleFunc("GET /metrics", s.prometheusMetrics)
	mux.Handle("POST /bap/v1/agent-sts/issue", s.authenticateAgentSTS(s.stsIssueAPIKey, s.stsIssuePrincipal, http.HandlerFunc(s.issueAgentGrant)))
	mux.Handle("POST /bap/v1/agent-sts/consume", s.authenticateAgentSTSConsumers(http.HandlerFunc(s.consumeAgentGrant)))
	if s.role != "agent-sts" {
		mux.Handle("POST /bap/v1/edge/sync", s.authenticate(http.HandlerFunc(s.syncPolicy)))
		mux.Handle("POST /bap/v1/audit/outcome", s.authenticate(http.HandlerFunc(s.auditOutcome)))
		mux.Handle("POST /bap/v1/audit/edge-denial", s.authenticate(http.HandlerFunc(s.auditEdgeDenial)))
		mux.Handle("POST /bap/v1/audit/edge-decision", s.authenticate(http.HandlerFunc(s.auditEdgeDecision)))
		if s.legacyAuthZEN {
			mux.HandleFunc("GET /.well-known/authzen-configuration", s.metadata)
			mux.Handle("POST /access/v1/evaluation", s.authenticate(http.HandlerFunc(s.evaluate)))
			mux.Handle("POST /bap/v1/audit/grant-consumption", s.authenticate(http.HandlerFunc(s.auditGrantConsumption)))
		}
	}
	return requestLogging(s.traceRequest(s.observeHTTP(mux)))
}

func (s *Server) issueAgentGrant(w http.ResponseWriter, r *http.Request) {
	if s.policyBundle.Version == 0 || s.agentSTS == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "agent_sts_unavailable"})
		return
	}
	var request agentgrant.IssueRequest
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_request"})
		return
	}
	caller := callerFrom(r.Context())
	response, claims, err := s.agentSTS.Issue(request, caller.Principal, caller.Fingerprint, s.policyBundle, time.Now().UTC())
	if err != nil {
		s.metrics.Decision(false, "AGENT_GRANT_ISSUE_DENY", "agent_sts")
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "agent_grant_denied", "reason": err.Error()})
		return
	}
	if s.audit != nil {
		allowed := true
		event := authorizationEvent(request.Operation, "agent_sts_issue", claims.GrantID, "AGENT_GRANT_ISSUED", fmt.Sprintf("bundle:%d:%s", claims.PolicyVersion, claims.PolicyDigest), &allowed, caller, traceFrom(r.Context()))
		event.GrantID, event.IntentHash = claims.GrantID, claims.IntentHash
		if err := s.audit.Append(event); err != nil {
			s.metrics.AuditFailure("agent_grant_issue")
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "audit_unavailable"})
			return
		}
	}
	s.metrics.Decision(true, "AGENT_GRANT_ISSUED", "agent_sts")
	writeJSON(w, http.StatusOK, response)
}

func (s *Server) consumeAgentGrant(w http.ResponseWriter, r *http.Request) {
	if s.policyBundle.Version == 0 || s.agentSTS == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "agent_sts_unavailable"})
		return
	}
	var request agentgrant.ConsumeRequest
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_request"})
		return
	}
	response, claims, err := s.agentSTS.ConsumeForAudiences(request, s.policyBundle, time.Now().UTC(), stsAudiencesFrom(r.Context()))
	if err != nil {
		s.metrics.Decision(false, "AGENT_GRANT_CONSUME_DENY", "agent_sts")
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "invalid_agent_grant", "reason": err.Error()})
		return
	}
	if s.audit != nil {
		allowed := true
		caller := callerFrom(r.Context())
		event := authorizationEvent(request.Operation, "agent_sts_consume", claims.GrantID, "AGENT_GRANT_CONSUMED", fmt.Sprintf("bundle:%d:%s", claims.PolicyVersion, claims.PolicyDigest), &allowed, caller, traceFrom(r.Context()))
		event.GrantID, event.IntentHash = claims.GrantID, claims.IntentHash
		if err := s.audit.Append(event); err != nil {
			s.metrics.AuditFailure("agent_grant_consume")
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "audit_unavailable"})
			return
		}
	}
	s.metrics.Decision(true, "AGENT_GRANT_CONSUMED", "agent_sts")
	writeJSON(w, http.StatusOK, response)
}

func (s *Server) SetPolicyBundle(bundle policybundle.Bundle, envelope policybundle.Envelope) {
	s.policyBundle = bundle
	s.policyEnvelope = envelope
}

func (s *Server) syncPolicy(w http.ResponseWriter, r *http.Request) {
	if s.policyBundle.Version == 0 || len(s.policyEnvelope.Payload) == 0 {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "policy_bundle_unavailable"})
		return
	}
	if s.audit == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "control_plane_not_ready"})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	if err := s.audit.Ready(ctx); err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "control_plane_not_ready"})
		return
	}
	var request policybundle.SyncRequest
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil || request.EdgeInstanceID == "" || request.EdgeVersion == "" || request.Nonce == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_sync_request"})
		return
	}
	directive := "UPDATE"
	if s.policyBundle.KillSwitch {
		directive = "KILL_SWITCH"
	} else if s.policyBundle.ForceUpdate && (request.InstalledVersion != s.policyBundle.Version || request.InstalledDigest != s.policyBundle.RulesDigest || request.RevocationEpoch < s.policyBundle.RevocationEpoch) {
		directive = "UPDATE_REQUIRED"
	} else if request.InstalledVersion == s.policyBundle.Version && request.InstalledDigest == s.policyBundle.RulesDigest && request.RevocationEpoch == s.policyBundle.RevocationEpoch {
		directive = "CURRENT"
	}
	writeJSON(w, http.StatusOK, policybundle.SyncResponse{Directive: directive, Envelope: s.policyEnvelope})
}

func (s *Server) ready(w http.ResponseWriter, r *http.Request) {
	if s.audit == nil {
		s.metrics.SetReady(false)
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "not_ready", "reason": "audit_unavailable"})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	if err := s.audit.Ready(ctx); err != nil {
		s.metrics.SetReady(false)
		s.noteReadinessFailure(r, err)
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "not_ready", "reason": "storage_unavailable"})
		return
	}
	s.metrics.SetReady(true)
	s.noteReadinessReady(r)
	writeJSON(w, http.StatusOK, map[string]string{"status": "ready"})
}

func (s *Server) noteReadinessFailure(r *http.Request, err error) {
	s.readinessMu.Lock()
	defer s.readinessMu.Unlock()
	now := time.Now()
	s.readinessFailed = true
	if !s.lastReadinessLog.IsZero() && now.Sub(s.lastReadinessLog) < 10*time.Second {
		s.readinessSuppressed++
		return
	}
	fields := map[string]any{"error": err.Error()}
	if s.readinessSuppressed > 0 {
		fields["suppressed_since_previous"] = s.readinessSuppressed
	}
	s.logEvent("readiness_error", r, fields)
	s.lastReadinessLog = now
	s.readinessSuppressed = 0
}

func (s *Server) noteReadinessReady(r *http.Request) {
	s.readinessMu.Lock()
	defer s.readinessMu.Unlock()
	if !s.readinessFailed {
		return
	}
	s.logEvent("readiness_recovered", r, map[string]any{"suppressed_readiness_errors": s.readinessSuppressed})
	s.readinessFailed = false
	s.lastReadinessLog = time.Time{}
	s.readinessSuppressed = 0
}

func (s *Server) prometheusMetrics(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	s.metrics.WritePrometheus(w)
}

func (s *Server) health(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) metadata(w http.ResponseWriter, r *http.Request) {
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	baseURL := scheme + "://" + r.Host
	writeJSON(w, http.StatusOK, map[string]any{
		"policy_decision_point":      baseURL,
		"access_evaluation_endpoint": baseURL + "/access/v1/evaluation",
		"capabilities":               []string{},
	})
}

func (s *Server) evaluate(w http.ResponseWriter, r *http.Request) {
	caller := callerFrom(r.Context())
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	var request authzen.EvaluationRequest
	if err := decoder.Decode(&request); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_request", "error_description": err.Error()})
		return
	}
	if err := request.Validate(); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_request", "error_description": err.Error()})
		return
	}

	allowed, reason, reasonCode, err := s.engine.Authorize(request)
	if err != nil {
		s.logEvent("cedar_error", r, map[string]any{"error": err.Error()})
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "evaluation_error"})
		return
	}
	decisionID := randomID()
	context := map[string]any{"reason": reason, "reason_code": reasonCode, "decision_id": decisionID, "policy_version": s.engine.PolicyVersion()}
	if !allowed && reasonCode == "NO_MATCHING_POLICY" && s.proposals != nil {
		if proposalID, err := s.proposals.Record(request); err != nil {
			s.logEvent("proposal_record_error", r, map[string]any{"error": err.Error()})
		} else {
			context["proposal_id"] = proposalID
			context["proposal_status"] = "pending_admin_review"
		}
	}
	if allowed {
		hash, err := grants.HashRequest(request)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "grant_error"})
			return
		}
		now := time.Now().UTC()
		sessionID, _ := request.Context["session_id"].(string)
		claims := grants.Claims{
			Issuer: s.issuer, Audience: s.audience, Subject: request.Subject.ID,
			Action: request.Action.Name, Resource: request.Resource.ID, SessionID: sessionID,
			RequestHash: hash, DecisionID: decisionID, Principal: caller.Principal,
			CredentialFingerprint: caller.Fingerprint, PolicyVersion: s.engine.PolicyVersion(), IssuedAt: now.Unix(), ExpiresAt: now.Add(s.grantTTL).Unix(),
		}
		grant, err := grants.Sign(s.privateKey, claims)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "grant_error"})
			return
		}
		context["grant"] = grant
		context["grant_type"] = grants.Type
		context["expires_at"] = time.Unix(claims.ExpiresAt, 0).UTC().Format(time.RFC3339)
	}
	if s.audit != nil {
		allowedValue := allowed
		event := authorizationEvent(request, "pdp_evaluation", decisionID, reasonCode, s.engine.PolicyVersion(), &allowedValue, caller, traceFrom(r.Context()))
		if err := s.audit.Append(event); err != nil {
			s.metrics.AuditFailure("authorization_decision")
			s.logEvent("audit_write_error", r, map[string]any{"operation": "authorization_decision", "error": err.Error()})
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "audit_unavailable"})
			return
		}
	}
	s.metrics.Decision(allowed, reasonCode, "pdp_evaluation")
	s.logEvent("authorization_committed", r, map[string]any{
		"decision_id": decisionID, "action": request.Action.Name, "resource_type": request.Resource.Type,
		"decision": map[bool]string{true: "allow", false: "deny"}[allowed], "reason_code": reasonCode,
		"policy_version": s.engine.PolicyVersion(),
	})
	writeJSON(w, http.StatusOK, authzen.Decision{Decision: allowed, Context: context})
}

func (s *Server) auditGrantConsumption(w http.ResponseWriter, r *http.Request) {
	if s.audit == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "audit_unavailable"})
		return
	}
	caller := callerFrom(r.Context())
	var consumption auditwire.GrantConsumption
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&consumption); err != nil || consumption.Request.Validate() != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_request"})
		return
	}
	hash, err := grants.HashRequest(consumption.Request)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_request"})
		return
	}
	publicKey := s.privateKey.Public().(ed25519.PublicKey)
	claims, err := grants.Verify(publicKey, consumption.Grant, s.audience, hash, time.Now().UTC())
	if err != nil || claims.CredentialFingerprint != caller.Fingerprint || claims.Principal != caller.Principal || claims.PolicyVersion != s.engine.PolicyVersion() {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "invalid_grant"})
		return
	}
	allowed := true
	event := authorizationEvent(consumption.Request, "cached_grant_consumption", claims.DecisionID, "CACHED_SIGNED_GRANT", claims.PolicyVersion, &allowed, caller, traceFrom(r.Context()))
	if err := s.audit.Append(event); err != nil {
		s.metrics.AuditFailure("grant_consumption")
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "audit_unavailable"})
		return
	}
	s.metrics.Decision(true, "CACHED_SIGNED_GRANT", "cached_grant_consumption")
	s.logEvent("authorization_committed", r, map[string]any{"decision_id": claims.DecisionID, "decision": "allow", "reason_code": "CACHED_SIGNED_GRANT", "source": "cached_grant_consumption", "policy_version": claims.PolicyVersion})
	writeJSON(w, http.StatusOK, map[string]any{"recorded": true, "event_id": event.EventID})
}

func (s *Server) auditOutcome(w http.ResponseWriter, r *http.Request) {
	if s.audit == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "audit_unavailable"})
		return
	}
	caller := callerFrom(r.Context())
	var outcome auditwire.Outcome
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&outcome); err != nil || outcome.EventID == "" || outcome.SessionID == "" || outcome.WorkloadID == "" || outcome.ToolUseID == "" || (outcome.Outcome != "success" && outcome.Outcome != "failure") {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_outcome"})
		return
	}
	if exists, err := s.audit.HasEvent(outcome.EventID); err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "audit_unavailable"})
		return
	} else if exists {
		writeJSON(w, http.StatusOK, map[string]any{"recorded": true, "event_id": outcome.EventID, "duplicate": true})
		return
	}
	if allowed, err := s.audit.HasAllowedOperation(outcome.SessionID, outcome.WorkloadID, outcome.ToolUseID, caller.Fingerprint); err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "audit_unavailable"})
		return
	} else if !allowed {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "authorization_event_not_found"})
		return
	}
	event := audit.Event{
		EventID: outcome.EventID, EventType: "tool_outcome", Source: "bap_edge_report",
		TraceID: traceFrom(r.Context()).TraceID, SpanID: traceFrom(r.Context()).SpanID, ParentSpanID: traceFrom(r.Context()).ParentSpanID,
		SessionID: outcome.SessionID, WorkloadID: outcome.WorkloadID, ToolUseID: outcome.ToolUseID,
		Tool: outcome.Tool, Outcome: outcome.Outcome, Principal: caller.Principal,
		CredentialFingerprint: caller.Fingerprint,
	}
	if err := s.audit.Append(event); err != nil {
		s.metrics.AuditFailure("tool_outcome")
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "audit_unavailable"})
		return
	}
	s.metrics.Outcome(outcome.Outcome)
	s.logEvent("tool_outcome_committed", r, map[string]any{"outcome": outcome.Outcome})
	writeJSON(w, http.StatusOK, map[string]any{"recorded": true, "event_id": event.EventID})
}

func (s *Server) auditEdgeDenial(w http.ResponseWriter, r *http.Request) {
	if s.audit == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "audit_unavailable"})
		return
	}
	caller := callerFrom(r.Context())
	var denial auditwire.EdgeDenial
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&denial); err != nil || denial.EventID == "" || denial.Request.Validate() != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_denial"})
		return
	}
	if exists, err := s.audit.HasEvent(denial.EventID); err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "audit_unavailable"})
		return
	} else if exists {
		writeJSON(w, http.StatusOK, map[string]any{"recorded": true, "event_id": denial.EventID, "duplicate": true})
		return
	}
	allowed := false
	event := authorizationEvent(denial.Request, "local_edge_filter", "", "LOCAL_FILTER_DENY", "embedded-cc-filter", &allowed, caller, traceFrom(r.Context()))
	event.EventID = denial.EventID
	if err := s.audit.Append(event); err != nil {
		s.metrics.AuditFailure("edge_denial")
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "audit_unavailable"})
		return
	}
	s.metrics.Decision(false, "LOCAL_FILTER_DENY", "local_edge_filter")
	s.logEvent("authorization_committed", r, map[string]any{"decision": "deny", "reason_code": "LOCAL_FILTER_DENY", "source": "local_edge_filter", "policy_version": "embedded-cc-filter"})
	writeJSON(w, http.StatusOK, map[string]any{"recorded": true, "event_id": event.EventID})
}

func (s *Server) auditEdgeDecision(w http.ResponseWriter, r *http.Request) {
	if s.audit == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "audit_unavailable"})
		return
	}
	caller := callerFrom(r.Context())
	var decision auditwire.EdgeDecision
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&decision); err != nil || decision.EventID == "" || decision.Request.Validate() != nil || decision.PolicyVersion == "" || decision.BundleVersion == 0 || decision.BundleDigest == "" || decision.ReasonCode == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_edge_decision"})
		return
	}
	if exists, err := s.audit.HasEvent(decision.EventID); err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "audit_unavailable"})
		return
	} else if exists {
		writeJSON(w, http.StatusOK, map[string]any{"recorded": true, "event_id": decision.EventID, "duplicate": true})
		return
	}
	event := authorizationEvent(decision.Request, "edge_policy_evaluation", "", decision.ReasonCode, decision.PolicyVersion, &decision.Allowed, caller, traceFrom(r.Context()))
	event.EventID = decision.EventID
	if err := s.audit.Append(event); err != nil {
		s.metrics.AuditFailure("edge_decision")
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "audit_unavailable"})
		return
	}
	s.metrics.Decision(decision.Allowed, decision.ReasonCode, "edge_policy_evaluation")
	writeJSON(w, http.StatusOK, map[string]any{"recorded": true, "event_id": event.EventID})
}

func authorizationEvent(request authzen.EvaluationRequest, source, decisionID, reasonCode, policyVersion string, allowed *bool, caller callerIdentity, trace tracecontext.Context) audit.Event {
	tool, _ := request.Resource.Properties["tool"].(string)
	sessionID, _ := request.Context["session_id"].(string)
	toolUseID, _ := request.Context["tool_use_id"].(string)
	assertedUser, _ := request.Context["asserted_user"].(string)
	workloadID, _ := request.Context["workload_id"].(string)
	return audit.Event{
		EventID: randomID(), EventType: "authorization_decision", Source: source,
		TraceID: trace.TraceID, SpanID: trace.SpanID, ParentSpanID: trace.ParentSpanID,
		ToolUseID: toolUseID, SessionID: sessionID, WorkloadID: workloadID, AssertedUser: assertedUser,
		Principal: caller.Principal, CredentialFingerprint: caller.Fingerprint,
		Subject: request.Subject.ID, Action: request.Action.Name, Tool: tool,
		ResourceID: request.Resource.ID, TargetSummary: targetSummary(request),
		DecisionID: decisionID, Allowed: allowed, ReasonCode: reasonCode,
		PolicyVersion: policyVersion,
	}
}

func targetSummary(request authzen.EvaluationRequest) string {
	if request.Action.Name == "command.execute" {
		command, _ := request.Resource.Properties["command"].(string)
		hash := sha256.Sum256([]byte(command))
		return "command-sha256:" + hex.EncodeToString(hash[:])
	}
	target, _ := request.Resource.Properties["target"].(string)
	workspace, _ := request.Context["workspace"].(string)
	if strings.HasPrefix(request.Action.Name, "file.") && workspace != "" {
		targetSlash := strings.ReplaceAll(target, "\\", "/")
		workspaceSlash := strings.TrimRight(strings.ReplaceAll(workspace, "\\", "/"), "/")
		if len(targetSlash) >= len(workspaceSlash) && strings.EqualFold(targetSlash[:len(workspaceSlash)], workspaceSlash) {
			relative := strings.TrimPrefix(targetSlash[len(workspaceSlash):], "/")
			if relative != "" {
				return relative
			}
			return "."
		}
		hash := sha256.Sum256([]byte(targetSlash))
		return "outside-workspace-sha256:" + hex.EncodeToString(hash[:])
	}
	return target
}

type callerIdentity struct {
	Principal   string
	Fingerprint string
}

type callerContextKey struct{}
type stsAudiencesContextKey struct{}

func (s *Server) authenticate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.TLS != nil && len(r.TLS.PeerCertificates) > 0 {
			certificate := r.TLS.PeerCertificates[0]
			sum := sha256.Sum256(certificate.Raw)
			principal := certificate.Subject.CommonName
			if principal == "" {
				principal = "mtls-device"
			}
			caller := callerIdentity{Principal: principal, Fingerprint: "sha256:" + hex.EncodeToString(sum[:])}
			next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), callerContextKey{}, caller)))
			return
		}
		provided := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		if provided == "" || len(provided) != len(s.apiKey) || subtle.ConstantTimeCompare([]byte(provided), []byte(s.apiKey)) != 1 {
			s.metrics.AuthenticationFailure()
			s.logEvent("authentication_failed", r, nil)
			w.Header().Set("WWW-Authenticate", "Bearer")
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid_client"})
			return
		}
		sum := sha256.Sum256([]byte(provided))
		caller := callerIdentity{Principal: s.principal, Fingerprint: "sha256:" + hex.EncodeToString(sum[:])}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), callerContextKey{}, caller)))
	})
}

func (s *Server) authenticateAgentSTS(apiKey, expectedPrincipal string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.TLS != nil && len(r.TLS.PeerCertificates) > 0 {
			certificate := r.TLS.PeerCertificates[0]
			if expectedPrincipal == "" || certificate.Subject.CommonName != expectedPrincipal {
				s.rejectAuthentication(w, r)
				return
			}
			sum := sha256.Sum256(certificate.Raw)
			caller := callerIdentity{Principal: certificate.Subject.CommonName, Fingerprint: "sha256:" + hex.EncodeToString(sum[:])}
			next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), callerContextKey{}, caller)))
			return
		}
		provided := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		if apiKey == "" || provided == "" || len(provided) != len(apiKey) || subtle.ConstantTimeCompare([]byte(provided), []byte(apiKey)) != 1 {
			s.rejectAuthentication(w, r)
			return
		}
		sum := sha256.Sum256([]byte(provided))
		caller := callerIdentity{Principal: expectedPrincipal, Fingerprint: "sha256:" + hex.EncodeToString(sum[:])}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), callerContextKey{}, caller)))
	})
}

func (s *Server) authenticateAgentSTSConsumers(next http.Handler) http.Handler {
	consumers := s.stsConsumers
	if len(consumers) == 0 {
		consumers = []AgentSTSConsumer{{APIKey: s.stsConsumeAPIKey, Principal: s.stsConsumePrincipal}}
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var matched *AgentSTSConsumer
		fingerprint := ""
		if r.TLS != nil && len(r.TLS.PeerCertificates) > 0 {
			certificate := r.TLS.PeerCertificates[0]
			for index := range consumers {
				if certificate.Subject.CommonName == consumers[index].Principal {
					matched = &consumers[index]
					break
				}
			}
			if matched != nil {
				sum := sha256.Sum256(certificate.Raw)
				fingerprint = "sha256:" + hex.EncodeToString(sum[:])
			}
		} else {
			provided := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
			for index := range consumers {
				expected := consumers[index].APIKey
				if expected != "" && len(provided) == len(expected) && subtle.ConstantTimeCompare([]byte(provided), []byte(expected)) == 1 {
					matched = &consumers[index]
				}
			}
			if matched != nil {
				sum := sha256.Sum256([]byte(provided))
				fingerprint = "sha256:" + hex.EncodeToString(sum[:])
			}
		}
		if matched == nil {
			s.rejectAuthentication(w, r)
			return
		}
		ctx := context.WithValue(r.Context(), callerContextKey{}, callerIdentity{Principal: matched.Principal, Fingerprint: fingerprint})
		ctx = context.WithValue(ctx, stsAudiencesContextKey{}, append([]string(nil), matched.Audiences...))
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func (s *Server) rejectAuthentication(w http.ResponseWriter, r *http.Request) {
	s.metrics.AuthenticationFailure()
	s.logEvent("authentication_failed", r, nil)
	w.Header().Set("WWW-Authenticate", "Bearer")
	writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid_client"})
}

func callerFrom(ctx context.Context) callerIdentity {
	caller, _ := ctx.Value(callerContextKey{}).(callerIdentity)
	return caller
}

func stsAudiencesFrom(ctx context.Context) []string {
	audiences, _ := ctx.Value(stsAudiencesContextKey{}).([]string)
	return audiences
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func requestID(r *http.Request) string {
	if value := strings.TrimSpace(r.Header.Get("X-Request-ID")); value != "" {
		return value
	}
	return "not-provided"
}

type traceContextKey struct{}

func traceFrom(ctx context.Context) tracecontext.Context {
	trace, _ := ctx.Value(traceContextKey{}).(tracecontext.Context)
	return trace
}

func (s *Server) traceRequest(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		parent, ok := tracecontext.Parse(r.Header.Get("traceparent"))
		var current tracecontext.Context
		if ok {
			current = parent.Child()
		} else {
			current = tracecontext.NewRoot()
		}
		w.Header().Set("traceparent", current.TraceParent())
		w.Header().Set("X-Trace-ID", current.TraceID)
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), traceContextKey{}, current)))
	})
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (w *statusRecorder) WriteHeader(status int) {
	if w.status == 0 {
		w.status = status
	}
	w.ResponseWriter.WriteHeader(status)
}

func (w *statusRecorder) Write(data []byte) (int, error) {
	if w.status == 0 {
		w.WriteHeader(http.StatusOK)
	}
	return w.ResponseWriter.Write(data)
}

func (s *Server) observeHTTP(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started := time.Now()
		recorder := &statusRecorder{ResponseWriter: w}
		next.ServeHTTP(recorder, r)
		status := recorder.status
		if status == 0 {
			status = http.StatusOK
		}
		if r.URL.Path != "/metrics" {
			s.metrics.ObserveHTTP(metricRoute(r.URL.Path), r.Method, status, time.Since(started))
		}
	})
}

func metricRoute(path string) string {
	switch path {
	case "/healthz", "/readyz", "/metrics", "/.well-known/authzen-configuration",
		"/access/v1/evaluation", "/bap/v1/agent-sts/issue", "/bap/v1/agent-sts/consume", "/bap/v1/audit/grant-consumption", "/bap/v1/audit/outcome", "/bap/v1/audit/edge-denial":
		return path
	default:
		return "other"
	}
}

func (s *Server) logEvent(event string, r *http.Request, fields map[string]any) {
	trace := traceFrom(r.Context())
	entry := map[string]any{
		"timestamp": time.Now().UTC().Format(time.RFC3339Nano),
		"level":     "info", "event": event, "request_id": requestID(r),
		"trace_id": trace.TraceID, "span_id": trace.SpanID, "parent_span_id": trace.ParentSpanID,
	}
	if strings.HasSuffix(event, "error") || event == "authentication_failed" {
		entry["level"] = "error"
	}
	for name, value := range fields {
		entry[name] = value
	}
	encoded, err := json.Marshal(entry)
	if err == nil {
		log.Print(string(encoded))
	}
}

func randomID() string {
	value := make([]byte, 16)
	if _, err := rand.Read(value); err != nil {
		return fmt.Sprintf("fallback-%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(value)
}

func requestLogging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestIDValue := strings.TrimSpace(r.Header.Get("X-Request-ID"))
		if requestIDValue == "" {
			requestIDValue = randomID()
			r.Header.Set("X-Request-ID", requestIDValue)
		}
		w.Header().Set("X-Request-ID", requestIDValue)
		next.ServeHTTP(w, r)
	})
}
