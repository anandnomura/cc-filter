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
	"time"

	"cc-filter/bap-service/internal/audit"
	"cc-filter/bap-service/internal/cedaradapter"
	"cc-filter/internal/auditwire"
	"cc-filter/internal/authzen"
	"cc-filter/internal/grants"
)

type Server struct {
	engine     *cedaradapter.Engine
	privateKey ed25519.PrivateKey
	issuer     string
	audience   string
	grantTTL   time.Duration
	proposals  ProposalStore
	audit      AuditStore
	apiKey     string
	principal  string
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
	return &Server{engine: engine, privateKey: privateKey, issuer: issuer, audience: audience, grantTTL: grantTTL, proposals: proposalStore, audit: auditStore, apiKey: apiKey, principal: principal}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", s.health)
	mux.HandleFunc("GET /readyz", s.ready)
	mux.HandleFunc("GET /.well-known/authzen-configuration", s.metadata)
	mux.Handle("POST /access/v1/evaluation", s.authenticate(http.HandlerFunc(s.evaluate)))
	mux.Handle("POST /bap/v1/audit/grant-consumption", s.authenticate(http.HandlerFunc(s.auditGrantConsumption)))
	mux.Handle("POST /bap/v1/audit/outcome", s.authenticate(http.HandlerFunc(s.auditOutcome)))
	mux.Handle("POST /bap/v1/audit/edge-denial", s.authenticate(http.HandlerFunc(s.auditEdgeDenial)))
	return requestLogging(mux)
}

func (s *Server) ready(w http.ResponseWriter, r *http.Request) {
	if s.audit == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "not_ready", "reason": "audit_unavailable"})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	if err := s.audit.Ready(ctx); err != nil {
		log.Printf("readiness_error request_id=%s error=%q", requestID(r), err)
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "not_ready", "reason": "storage_unavailable"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ready"})
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
		log.Printf("cedar_error request_id=%s error=%q", requestID(r), err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "evaluation_error"})
		return
	}
	decisionID := randomID()
	context := map[string]any{"reason": reason, "reason_code": reasonCode, "decision_id": decisionID, "policy_version": s.engine.PolicyVersion()}
	if !allowed && reasonCode == "NO_MATCHING_POLICY" && s.proposals != nil {
		if proposalID, err := s.proposals.Record(request); err != nil {
			log.Printf("proposal_record_error request_id=%s error=%q", requestID(r), err)
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
	log.Printf("decision request_id=%s decision_id=%s subject=%q action=%q resource_type=%q allowed=%t",
		requestID(r), decisionID, request.Subject.ID, request.Action.Name, request.Resource.Type, allowed)
	if s.audit != nil {
		allowedValue := allowed
		event := authorizationEvent(request, "pdp_evaluation", decisionID, reasonCode, s.engine.PolicyVersion(), &allowedValue, caller)
		if err := s.audit.Append(event); err != nil {
			log.Printf("audit_write_error request_id=%s error=%q", requestID(r), err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "audit_unavailable"})
			return
		}
	}
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
	if err != nil || claims.CredentialFingerprint != caller.Fingerprint || claims.Principal != caller.Principal {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "invalid_grant"})
		return
	}
	allowed := true
	event := authorizationEvent(consumption.Request, "cached_grant_consumption", claims.DecisionID, "CACHED_SIGNED_GRANT", claims.PolicyVersion, &allowed, caller)
	if err := s.audit.Append(event); err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "audit_unavailable"})
		return
	}
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
		SessionID: outcome.SessionID, WorkloadID: outcome.WorkloadID, ToolUseID: outcome.ToolUseID,
		Tool: outcome.Tool, Outcome: outcome.Outcome, Principal: caller.Principal,
		CredentialFingerprint: caller.Fingerprint,
	}
	if err := s.audit.Append(event); err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "audit_unavailable"})
		return
	}
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
	event := authorizationEvent(denial.Request, "local_edge_filter", "", "LOCAL_FILTER_DENY", "embedded-cc-filter", &allowed, caller)
	event.EventID = denial.EventID
	if err := s.audit.Append(event); err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "audit_unavailable"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"recorded": true, "event_id": event.EventID})
}

func authorizationEvent(request authzen.EvaluationRequest, source, decisionID, reasonCode, policyVersion string, allowed *bool, caller callerIdentity) audit.Event {
	tool, _ := request.Resource.Properties["tool"].(string)
	sessionID, _ := request.Context["session_id"].(string)
	toolUseID, _ := request.Context["tool_use_id"].(string)
	assertedUser, _ := request.Context["asserted_user"].(string)
	workloadID, _ := request.Context["workload_id"].(string)
	return audit.Event{
		EventID: randomID(), EventType: "authorization_decision", Source: source,
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

func (s *Server) authenticate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		provided := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		if provided == "" || len(provided) != len(s.apiKey) || subtle.ConstantTimeCompare([]byte(provided), []byte(s.apiKey)) != 1 {
			w.Header().Set("WWW-Authenticate", "Bearer")
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid_client"})
			return
		}
		sum := sha256.Sum256([]byte(provided))
		caller := callerIdentity{Principal: s.principal, Fingerprint: "sha256:" + hex.EncodeToString(sum[:])}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), callerContextKey{}, caller)))
	})
}

func callerFrom(ctx context.Context) callerIdentity {
	caller, _ := ctx.Value(callerContextKey{}).(callerIdentity)
	return caller
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
