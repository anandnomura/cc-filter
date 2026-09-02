package policybundle

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"bap-system/internal/authzen"
	"bap-system/internal/resourceindicator"
	cedar "github.com/cedar-policy/cedar-go"
)

const SchemaVersion = 1

type Source struct {
	SchemaVersion       int              `json:"schema_version"`
	Version             uint64           `json:"version"`
	ValidForSeconds     int64            `json:"valid_for_seconds"`
	RefreshAfterSeconds int64            `json:"refresh_after_seconds"`
	MaxOfflineSeconds   int64            `json:"max_offline_seconds"`
	MinimumEdgeVersion  string           `json:"minimum_edge_version"`
	RevocationEpoch     uint64           `json:"revocation_epoch"`
	ForceUpdate         bool             `json:"force_update"`
	KillSwitch          bool             `json:"kill_switch"`
	PolicyProfile       string           `json:"policy_profile"`
	AllowedNetwork      []string         `json:"allowed_network_domains"`
	ApprovedMCP         []string         `json:"approved_mcp_tools"`
	ApprovedDelegates   []string         `json:"approved_subagent_types"`
	PromptRules         []PromptRule     `json:"prompt_rules,omitempty"`
	CommandRules        []CommandRule    `json:"command_rules"`
	AgentGrantRules     []AgentGrantRule `json:"agent_grant_rules,omitempty"`
	SessionPolicy       SessionPolicy    `json:"session_policy"`
}

// SessionPolicy is signed with the bundle. Capability labels and limits are
// configuration; the Edge only implements the generic state machine.
type SessionPolicy struct {
	MaxEvents          int                      `json:"max_events"`
	MaxLifetimeSeconds int64                    `json:"max_lifetime_seconds"`
	IdleTimeoutSeconds int64                    `json:"idle_timeout_seconds"`
	Capabilities       []SessionCapability      `json:"capabilities,omitempty"`
	CompositionRules   []SessionCompositionRule `json:"composition_rules,omitempty"`
	BudgetRules        []SessionBudgetRule      `json:"budget_rules,omitempty"`
}

type SessionCapability struct {
	ID         string              `json:"id"`
	Actions    []string            `json:"actions,omitempty"`
	Tools      []string            `json:"tools,omitempty"`
	Properties map[string][]string `json:"property_equals,omitempty"`
	Profiles   []string            `json:"profiles,omitempty"`
	Owner      string              `json:"owner"`
	Approval   string              `json:"approval"`
}

type SessionCompositionRule struct {
	ID                  string   `json:"id"`
	PriorCapabilities   []string `json:"prior_capabilities"`
	CurrentCapabilities []string `json:"current_capabilities"`
	WithinSeconds       int64    `json:"within_seconds"`
	Reason              string   `json:"reason"`
	Profiles            []string `json:"profiles,omitempty"`
	Owner               string   `json:"owner"`
	Approval            string   `json:"approval"`
}

type SessionBudgetRule struct {
	ID            string   `json:"id"`
	Capabilities  []string `json:"capabilities"`
	MaxCount      int      `json:"max_count"`
	WindowSeconds int64    `json:"window_seconds"`
	Scope         string   `json:"scope"`
	Reason        string   `json:"reason"`
	Profiles      []string `json:"profiles,omitempty"`
	Owner         string   `json:"owner"`
	Approval      string   `json:"approval"`
}

// PromptRule is an advisory natural-language signal. It can make handling
// more conservative, but it is never an authorization permit.
type PromptRule struct {
	ID       string   `json:"id"`
	Effect   string   `json:"effect"`
	Patterns []string `json:"patterns"`
	Profiles []string `json:"profiles,omitempty"`
	Owner    string   `json:"owner"`
	Approval string   `json:"approval"`
}

// AgentGrantRule selects concrete operations that require an online Agent STS
// decision. It never overrides a Cedar forbid or a manual-only command rule.
type AgentGrantRule struct {
	ID                  string   `json:"id"`
	ResourceType        string   `json:"resource_type"`
	Action              string   `json:"action"`
	Tool                string   `json:"tool"`
	Methods             []string `json:"methods,omitempty"`
	Hosts               []string `json:"hosts,omitempty"`
	Paths               []string `json:"paths,omitempty"`
	MCPServers          []string `json:"mcp_servers,omitempty"`
	MCPTools            []string `json:"mcp_tools,omitempty"`
	IntentRuleIDs       []string `json:"intent_rule_ids"`
	Resource            string   `json:"resource"`
	MaxTTLSeconds       int64    `json:"max_ttl_seconds"`
	MaxIntentAgeSeconds int64    `json:"max_intent_age_seconds"`
	MaxGrantsPerIntent  int      `json:"max_grants_per_intent"`
	Profiles            []string `json:"profiles,omitempty"`
	Owner               string   `json:"owner"`
	Approval            string   `json:"approval"`
}

type PromptMatch struct {
	ID     string
	Effect string
}

type CommandRule struct {
	ID                        string    `json:"id"`
	Executable                string    `json:"executable"`
	Subcommand                string    `json:"subcommand,omitempty"`
	Effect                    string    `json:"effect"`
	ArgumentPatterns          []string  `json:"argument_patterns,omitempty"`
	AllowAdditionalArguments  bool      `json:"allow_additional_arguments,omitempty"`
	AdditionalArgumentPattern string    `json:"additional_argument_pattern,omitempty"`
	Profiles                  []string  `json:"profiles,omitempty"`
	Owner                     string    `json:"owner"`
	Approval                  string    `json:"approval"`
	NotBefore                 time.Time `json:"not_before"`
	ExpiresAt                 time.Time `json:"expires_at"`
}

type Bundle struct {
	SchemaVersion       int              `json:"schema_version"`
	Version             uint64           `json:"version"`
	RulesDigest         string           `json:"rules_digest"`
	IssuedAt            time.Time        `json:"issued_at"`
	ExpiresAt           time.Time        `json:"expires_at"`
	RefreshAfterSeconds int64            `json:"refresh_after_seconds"`
	MaxOfflineSeconds   int64            `json:"max_offline_seconds"`
	MinimumEdgeVersion  string           `json:"minimum_edge_version"`
	RevocationEpoch     uint64           `json:"revocation_epoch"`
	ForceUpdate         bool             `json:"force_update"`
	KillSwitch          bool             `json:"kill_switch"`
	PolicyProfile       string           `json:"policy_profile"`
	AllowedNetwork      []string         `json:"allowed_network_domains"`
	ApprovedMCP         []string         `json:"approved_mcp_tools"`
	ApprovedDelegates   []string         `json:"approved_subagent_types"`
	PromptRules         []PromptRule     `json:"prompt_rules,omitempty"`
	CedarPolicy         string           `json:"cedar_policy"`
	CommandRules        []CommandRule    `json:"command_rules"`
	AgentGrantRules     []AgentGrantRule `json:"agent_grant_rules,omitempty"`
	SessionPolicy       SessionPolicy    `json:"session_policy"`
}

type Envelope struct {
	KeyID     string          `json:"key_id"`
	Payload   json.RawMessage `json:"payload"`
	Signature string          `json:"signature"`
}

type SyncRequest struct {
	EdgeInstanceID   string `json:"edge_instance_id"`
	EdgeVersion      string `json:"edge_version"`
	InstalledVersion uint64 `json:"installed_version"`
	InstalledDigest  string `json:"installed_digest,omitempty"`
	RevocationEpoch  uint64 `json:"revocation_epoch"`
	Nonce            string `json:"nonce"`
}

type SyncResponse struct {
	Directive string   `json:"directive"`
	Envelope  Envelope `json:"envelope"`
}

type Decision struct {
	Allowed               bool
	ManualOnly            bool
	AgentGrantRequired    bool
	Reason                string
	ReasonCode            string
	RuleIDs               []string
	PolicyVersion         string
	GrantAudience         string
	GrantResource         string
	GrantTTL              time.Duration
	GrantIntentMaxAge     time.Duration
	GrantMaxPerIntent     int
	RequiredIntentRuleIDs []string
}

type SessionEvent struct {
	Capabilities []string
	ResourceID   string
	Status       string
	OccurredAt   time.Time
}

type SessionDecision struct {
	Allowed      bool
	Capabilities []string
	Reason       string
	ReasonCode   string
	RuleIDs      []string
}

func validateSessionPolicy(policy SessionPolicy, profile string) error {
	if policy.MaxEvents <= 0 || policy.MaxEvents > 10000 || policy.MaxLifetimeSeconds <= 0 || policy.MaxLifetimeSeconds > 86400 || policy.IdleTimeoutSeconds <= 0 || policy.IdleTimeoutSeconds > policy.MaxLifetimeSeconds {
		return errors.New("session_policy limits are invalid")
	}
	capabilities := map[string]bool{}
	for i, capability := range policy.Capabilities {
		if capability.ID == "" || capability.Owner == "" || capability.Approval == "" || len(capability.Actions) == 0 || capabilities[capability.ID] {
			return fmt.Errorf("session capability %d is incomplete or duplicated", i)
		}
		capabilities[capability.ID] = true
	}
	validateRefs := func(ruleID string, refs []string) error {
		if len(refs) == 0 {
			return fmt.Errorf("session rule %s has no capabilities", ruleID)
		}
		for _, ref := range refs {
			if !capabilities[ref] {
				return fmt.Errorf("session rule %s references unknown capability %q", ruleID, ref)
			}
		}
		return nil
	}
	ids := map[string]bool{}
	for _, rule := range policy.CompositionRules {
		if rule.ID == "" || ids[rule.ID] || rule.WithinSeconds <= 0 || rule.WithinSeconds > policy.MaxLifetimeSeconds || rule.Reason == "" || rule.Owner == "" || rule.Approval == "" {
			return fmt.Errorf("session composition rule %q is incomplete", rule.ID)
		}
		if err := validateRefs(rule.ID, rule.PriorCapabilities); err != nil {
			return err
		}
		if err := validateRefs(rule.ID, rule.CurrentCapabilities); err != nil {
			return err
		}
		ids[rule.ID] = true
	}
	for _, rule := range policy.BudgetRules {
		if rule.ID == "" || ids[rule.ID] || rule.MaxCount <= 0 || rule.WindowSeconds <= 0 || rule.WindowSeconds > policy.MaxLifetimeSeconds || (rule.Scope != "session" && rule.Scope != "resource") || rule.Reason == "" || rule.Owner == "" || rule.Approval == "" {
			return fmt.Errorf("session budget rule %q is incomplete", rule.ID)
		}
		if err := validateRefs(rule.ID, rule.Capabilities); err != nil {
			return err
		}
		ids[rule.ID] = true
	}
	_ = profile
	return nil
}

// EvaluateSession applies signed capability mappings, ordered composition
// forbids, and rolling budgets. Pending events count conservatively so two
// concurrent Claude hook processes cannot race through the same limit.
func EvaluateSession(bundle Bundle, request authzen.EvaluationRequest, history []SessionEvent, now time.Time) SessionDecision {
	current := make([]string, 0)
	tool, _ := request.Resource.Properties["tool"].(string)
	for _, capability := range bundle.SessionPolicy.Capabilities {
		if !profileMatches(bundle.PolicyProfile, capability.Profiles) || !matchesExact(request.Action.Name, capability.Actions) || (len(capability.Tools) > 0 && !matchesExact(tool, capability.Tools)) {
			continue
		}
		matched := true
		for key, allowed := range capability.Properties {
			value, ok := request.Resource.Properties[key]
			if !ok || !matchesExact(fmt.Sprint(value), allowed) {
				matched = false
				break
			}
		}
		if matched {
			current = append(current, capability.ID)
		}
	}
	decision := SessionDecision{Allowed: true, Capabilities: current, ReasonCode: "SESSION_POLICY_PERMIT"}
	if len(current) == 0 {
		return decision
	}
	currentSet := stringSet(current)
	for _, rule := range bundle.SessionPolicy.CompositionRules {
		if !profileMatches(bundle.PolicyProfile, rule.Profiles) || !intersectsSet(currentSet, rule.CurrentCapabilities) {
			continue
		}
		cutoff := now.Add(-time.Duration(rule.WithinSeconds) * time.Second)
		for _, event := range history {
			if event.Status == "failure" || event.OccurredAt.Before(cutoff) {
				continue
			}
			if intersectsSet(stringSet(event.Capabilities), rule.PriorCapabilities) {
				return SessionDecision{Capabilities: current, Reason: rule.Reason, ReasonCode: "SESSION_COMPOSITION_FORBID", RuleIDs: []string{rule.ID}}
			}
		}
	}
	for _, rule := range bundle.SessionPolicy.BudgetRules {
		if !profileMatches(bundle.PolicyProfile, rule.Profiles) || !intersectsSet(currentSet, rule.Capabilities) {
			continue
		}
		count := 0
		cutoff := now.Add(-time.Duration(rule.WindowSeconds) * time.Second)
		for _, event := range history {
			if event.Status == "failure" || event.OccurredAt.Before(cutoff) || !intersectsSet(stringSet(event.Capabilities), rule.Capabilities) {
				continue
			}
			if rule.Scope == "resource" && event.ResourceID != request.Resource.ID {
				continue
			}
			count++
		}
		if count >= rule.MaxCount {
			return SessionDecision{Capabilities: current, Reason: rule.Reason, ReasonCode: "SESSION_BUDGET_EXCEEDED", RuleIDs: []string{rule.ID}}
		}
	}
	return decision
}

func stringSet(values []string) map[string]bool {
	result := map[string]bool{}
	for _, value := range values {
		result[value] = true
	}
	return result
}
func intersectsSet(set map[string]bool, values []string) bool {
	for _, value := range values {
		if set[value] {
			return true
		}
	}
	return false
}

func LoadSource(data []byte) (Source, error) {
	var source Source
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&source); err != nil {
		return source, fmt.Errorf("parse policy bundle source: %w", err)
	}
	if source.SchemaVersion != SchemaVersion || source.Version == 0 {
		return source, errors.New("policy bundle source has unsupported schema or zero version")
	}
	if source.ValidForSeconds <= 0 || source.ValidForSeconds > int64((30*24*time.Hour)/time.Second) {
		return source, errors.New("policy bundle valid_for_seconds must be between 1 second and 30 days")
	}
	if source.RefreshAfterSeconds <= 0 || source.MaxOfflineSeconds < source.RefreshAfterSeconds || source.MaxOfflineSeconds > source.ValidForSeconds {
		return source, errors.New("policy refresh/offline intervals are invalid")
	}
	if source.PolicyProfile != "standard-developer" && source.PolicyProfile != "read-only" {
		return source, errors.New("policy_profile must be standard-developer or read-only")
	}
	ids := map[string]bool{}
	for i, rule := range source.PromptRules {
		if rule.ID == "" || (rule.Effect != "manual-only-advisory" && rule.Effect != "agent-grant-intent") || len(rule.Patterns) == 0 || rule.Owner == "" || rule.Approval == "" {
			return source, fmt.Errorf("prompt rule %d is incomplete", i)
		}
		if ids[rule.ID] {
			return source, fmt.Errorf("duplicate policy rule id %q", rule.ID)
		}
		ids[rule.ID] = true
		for _, pattern := range rule.Patterns {
			if pattern == "" || len(pattern) > 1024 {
				return source, fmt.Errorf("prompt rule %s has an invalid pattern length", rule.ID)
			}
			if _, err := regexp.Compile(pattern); err != nil {
				return source, fmt.Errorf("prompt rule %s has invalid pattern: %w", rule.ID, err)
			}
		}
	}
	for i := range source.CommandRules {
		rule := &source.CommandRules[i]
		if rule.ID == "" || rule.Executable == "" || rule.Owner == "" || rule.Approval == "" || (rule.Effect != "eligible-for-permit" && rule.Effect != "forbid" && rule.Effect != "manual-only") {
			return source, fmt.Errorf("command rule %d is incomplete", i)
		}
		if ids[rule.ID] {
			return source, fmt.Errorf("duplicate policy rule id %q", rule.ID)
		}
		ids[rule.ID] = true
		for _, pattern := range rule.ArgumentPatterns {
			if _, err := regexp.Compile("^(?:" + pattern + ")$"); err != nil {
				return source, fmt.Errorf("command rule %s has invalid argument pattern: %w", rule.ID, err)
			}
		}
		if rule.AllowAdditionalArguments && rule.AdditionalArgumentPattern == "" {
			return source, fmt.Errorf("command rule %s allows additional arguments without constraining them", rule.ID)
		}
		if rule.AdditionalArgumentPattern != "" {
			if _, err := regexp.Compile("^(?:" + rule.AdditionalArgumentPattern + ")$"); err != nil {
				return source, fmt.Errorf("command rule %s has invalid additional argument pattern: %w", rule.ID, err)
			}
		}
	}
	for i, rule := range source.AgentGrantRules {
		if rule.ID == "" || (rule.ResourceType != "api" && rule.ResourceType != "mcp") || rule.Action == "" || rule.Tool == "" || len(rule.IntentRuleIDs) == 0 || rule.Resource == "" || rule.MaxTTLSeconds <= 0 || rule.MaxTTLSeconds > 300 || rule.MaxIntentAgeSeconds <= 0 || rule.MaxIntentAgeSeconds > 900 || rule.MaxGrantsPerIntent <= 0 || rule.MaxGrantsPerIntent > 10 || rule.Owner == "" || rule.Approval == "" {
			return source, fmt.Errorf("agent grant rule %d is incomplete", i)
		}
		if err := resourceindicator.Validate(rule.Resource); err != nil {
			return source, fmt.Errorf("agent grant rule %s has invalid resource: %w", rule.ID, err)
		}
		if rule.ResourceType == "api" && (len(rule.Methods) == 0 || len(rule.Hosts) == 0 || len(rule.Paths) == 0 || len(rule.MCPServers) != 0 || len(rule.MCPTools) != 0) {
			return source, fmt.Errorf("agent grant API rule %s must define only exact methods, hosts, and paths", rule.ID)
		}
		if rule.ResourceType == "mcp" && (rule.Action != "mcp.invoke" || len(rule.MCPServers) == 0 || len(rule.MCPTools) == 0 || len(rule.Methods) != 0 || len(rule.Hosts) != 0 || len(rule.Paths) != 0) {
			return source, fmt.Errorf("agent grant MCP rule %s must define only exact MCP servers and tools", rule.ID)
		}
		if ids[rule.ID] {
			return source, fmt.Errorf("duplicate policy rule id %q", rule.ID)
		}
		ids[rule.ID] = true
		for _, method := range rule.Methods {
			if method != strings.ToUpper(method) || !regexp.MustCompile(`^[A-Z]+$`).MatchString(method) {
				return source, fmt.Errorf("agent grant rule %s has invalid HTTP method", rule.ID)
			}
		}
		for _, host := range rule.Hosts {
			if host != strings.ToLower(host) || net.ParseIP(host) != nil || !regexp.MustCompile(`^(?:[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?\.)+[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$`).MatchString(host) {
				return source, fmt.Errorf("agent grant rule %s has invalid exact DNS host %q", rule.ID, host)
			}
		}
		for _, path := range rule.Paths {
			if !strings.HasPrefix(path, "/") || strings.ContainsAny(path, "?#") {
				return source, fmt.Errorf("agent grant rule %s has invalid exact path %q", rule.ID, path)
			}
		}
		for _, intentRuleID := range rule.IntentRuleIDs {
			found := false
			for _, promptRule := range source.PromptRules {
				if promptRule.ID == intentRuleID && promptRule.Effect == "agent-grant-intent" {
					found = true
					break
				}
			}
			if !found {
				return source, fmt.Errorf("agent grant rule %s references unknown grant intent rule %q", rule.ID, intentRuleID)
			}
		}
	}
	if err := validateSessionPolicy(source.SessionPolicy, source.PolicyProfile); err != nil {
		return source, err
	}
	return source, nil
}

func Build(source Source, cedarPolicy []byte, now time.Time) (Bundle, error) {
	if _, err := LoadSource(mustJSON(source)); err != nil {
		return Bundle{}, err
	}
	if _, err := cedar.NewPolicyListFromBytes("bundle.cedar", cedarPolicy); err != nil {
		return Bundle{}, fmt.Errorf("parse bundled Cedar policy: %w", err)
	}
	sourceDigest := sha256.Sum256(append(mustJSON(source), cedarPolicy...))
	expires := now.UTC().Add(time.Duration(source.ValidForSeconds) * time.Second)
	rules := append([]CommandRule(nil), source.CommandRules...)
	for i := range rules {
		if rules[i].NotBefore.IsZero() {
			rules[i].NotBefore = now.UTC()
		}
		if rules[i].ExpiresAt.IsZero() || rules[i].ExpiresAt.After(expires) {
			rules[i].ExpiresAt = expires
		}
	}
	return Bundle{
		SchemaVersion: SchemaVersion, Version: source.Version, RulesDigest: "sha256:" + hex.EncodeToString(sourceDigest[:]),
		IssuedAt: now.UTC(), ExpiresAt: expires, RefreshAfterSeconds: source.RefreshAfterSeconds,
		MaxOfflineSeconds: source.MaxOfflineSeconds, MinimumEdgeVersion: source.MinimumEdgeVersion,
		RevocationEpoch: source.RevocationEpoch, ForceUpdate: source.ForceUpdate, KillSwitch: source.KillSwitch,
		PolicyProfile: source.PolicyProfile, AllowedNetwork: source.AllowedNetwork, ApprovedMCP: source.ApprovedMCP,
		ApprovedDelegates: source.ApprovedDelegates, PromptRules: append([]PromptRule(nil), source.PromptRules...), CedarPolicy: string(cedarPolicy), CommandRules: rules,
		AgentGrantRules: append([]AgentGrantRule(nil), source.AgentGrantRules...),
		SessionPolicy:   source.SessionPolicy,
	}, nil
}

// MatchPrompt returns signed advisory rule IDs whose patterns all occur in the
// prompt. The result is a risk signal only; authorization continues to happen
// against the eventual structured tool invocation.
func MatchPrompt(bundle Bundle, prompt string) []string {
	matched := make([]string, 0)
	for _, match := range MatchPromptRules(bundle, prompt) {
		if match.Effect == "manual-only-advisory" {
			matched = append(matched, match.ID)
		}
	}
	return matched
}

// MatchPromptRules returns signed intent classifications without retaining or
// returning prompt text. Authorization still requires a concrete tool request.
func MatchPromptRules(bundle Bundle, prompt string) []PromptMatch {
	matched := make([]PromptMatch, 0)
	for _, rule := range bundle.PromptRules {
		if !profileMatches(bundle.PolicyProfile, rule.Profiles) {
			continue
		}
		matchesAll := true
		for _, pattern := range rule.Patterns {
			expression, err := regexp.Compile(pattern)
			if err != nil || !expression.MatchString(prompt) {
				matchesAll = false
				break
			}
		}
		if matchesAll {
			matched = append(matched, PromptMatch{ID: rule.ID, Effect: rule.Effect})
		}
	}
	return matched
}

func Sign(privateKey ed25519.PrivateKey, keyID string, bundle Bundle) (Envelope, error) {
	payload, err := json.Marshal(bundle)
	if err != nil {
		return Envelope{}, err
	}
	signature := ed25519.Sign(privateKey, payload)
	return Envelope{KeyID: keyID, Payload: payload, Signature: base64.RawURLEncoding.EncodeToString(signature)}, nil
}

func Activate(source Source, cedarPolicy []byte, privateKey ed25519.PrivateKey, keyID, statePath string, now time.Time) (Bundle, Envelope, error) {
	candidate, err := Build(source, cedarPolicy, now)
	if err != nil {
		return Bundle{}, Envelope{}, err
	}
	if data, readErr := os.ReadFile(statePath); readErr == nil {
		var existingEnvelope Envelope
		var untrusted Bundle
		if json.Unmarshal(data, &existingEnvelope) != nil || json.Unmarshal(existingEnvelope.Payload, &untrusted) != nil {
			return Bundle{}, Envelope{}, errors.New("stored active policy bundle is invalid")
		}
		existing, verifyErr := Verify(privateKey.Public().(ed25519.PublicKey), existingEnvelope, untrusted.IssuedAt.Add(time.Second))
		if verifyErr != nil {
			return Bundle{}, Envelope{}, fmt.Errorf("verify stored active policy bundle: %w", verifyErr)
		}
		if source.Version < existing.Version {
			return Bundle{}, Envelope{}, errors.New("control-plane policy source rollback rejected")
		}
		if source.Version == existing.Version {
			if candidate.RulesDigest != existing.RulesDigest {
				return Bundle{}, Envelope{}, errors.New("control-plane rule content changed without a version increment")
			}
			if !now.UTC().Before(existing.ExpiresAt) {
				return Bundle{}, Envelope{}, errors.New("active policy bundle expired; approve and increment the source version")
			}
			return existing, existingEnvelope, nil
		}
	} else if !os.IsNotExist(readErr) {
		return Bundle{}, Envelope{}, readErr
	}
	envelope, err := Sign(privateKey, keyID, candidate)
	if err != nil {
		return Bundle{}, Envelope{}, err
	}
	data, _ := json.Marshal(envelope)
	temporary := statePath + ".tmp"
	if err := os.WriteFile(temporary, data, 0600); err != nil {
		return Bundle{}, Envelope{}, err
	}
	if err := os.Rename(temporary, statePath); err != nil {
		_ = os.Remove(temporary)
		return Bundle{}, Envelope{}, err
	}
	return candidate, envelope, nil
}

func Verify(publicKey ed25519.PublicKey, envelope Envelope, now time.Time) (Bundle, error) {
	var bundle Bundle
	signature, err := base64.RawURLEncoding.DecodeString(envelope.Signature)
	if err != nil || !ed25519.Verify(publicKey, envelope.Payload, signature) {
		return bundle, errors.New("invalid policy bundle signature")
	}
	if err := json.Unmarshal(envelope.Payload, &bundle); err != nil {
		return bundle, errors.New("invalid policy bundle payload")
	}
	if bundle.SchemaVersion != SchemaVersion || bundle.Version == 0 || bundle.RulesDigest == "" {
		return bundle, errors.New("unsupported policy bundle schema or identity")
	}
	if !bundle.IssuedAt.Before(bundle.ExpiresAt) || !now.UTC().Before(bundle.ExpiresAt) {
		return bundle, errors.New("policy bundle is expired")
	}
	if bundle.IssuedAt.After(now.UTC().Add(5 * time.Minute)) {
		return bundle, errors.New("policy bundle is not yet valid")
	}
	if _, err := cedar.NewPolicyListFromBytes("bundle.cedar", []byte(bundle.CedarPolicy)); err != nil {
		return bundle, errors.New("policy bundle contains invalid Cedar")
	}
	for _, rule := range bundle.AgentGrantRules {
		if err := resourceindicator.Validate(rule.Resource); err != nil {
			return bundle, fmt.Errorf("policy bundle contains invalid AgentGrant resource for %s: %w", rule.ID, err)
		}
		if rule.MaxGrantsPerIntent <= 0 || rule.MaxGrantsPerIntent > 10 {
			return bundle, fmt.Errorf("policy bundle contains invalid AgentGrant intent budget for %s", rule.ID)
		}
	}
	if err := validateSessionPolicy(bundle.SessionPolicy, bundle.PolicyProfile); err != nil {
		return bundle, fmt.Errorf("policy bundle contains invalid session policy: %w", err)
	}
	return bundle, nil
}

func EnvelopeDigest(envelope Envelope) string {
	data, _ := json.Marshal(envelope)
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func Authorize(bundle Bundle, request authzen.EvaluationRequest, now time.Time) (Decision, error) {
	policyVersion := fmt.Sprintf("bundle:%d:%s", bundle.Version, bundle.RulesDigest)
	if bundle.KillSwitch {
		return Decision{Reason: "BAP policy kill switch is active", ReasonCode: "KILL_SWITCH", PolicyVersion: policyVersion}, nil
	}
	if !now.UTC().Before(bundle.ExpiresAt) {
		return Decision{Reason: "BAP policy bundle is expired", ReasonCode: "BUNDLE_EXPIRED", PolicyVersion: policyVersion}, nil
	}
	properties := cloneMap(request.Resource.Properties)
	properties["policyProfile"] = bundle.PolicyProfile
	ruleIDs := []string{}
	action := request.Action.Name
	manualOnly := false
	agentRule := matchAgentGrantRule(bundle, request)
	// A signed, exact MCP AgentGrant rule is the registry approval for reaching
	// the protected MCP PEP. Cedar still evaluates every other forbid, and the
	// call cannot execute locally because the result below requires Agent STS.
	if agentRule != nil && agentRule.ResourceType == "mcp" {
		properties["approvedMCP"] = true
	}
	if action == "command.execute" {
		command, _ := properties["command"].(string)
		approved, forbidden, commandManualOnly, matched, err := classifyCommand(bundle, command, now)
		if err != nil {
			return Decision{Reason: err.Error(), ReasonCode: "COMMAND_PARSE_DENY", PolicyVersion: policyVersion}, nil
		}
		manualOnly = commandManualOnly
		properties["shellApproved"] = (approved || manualOnly) && !forbidden
		if forbidden {
			properties["destructive"] = true
		}
		ruleIDs = matched
	}
	if action == "network.fetch" {
		host, _ := properties["networkHost"].(string)
		properties["approvedNetwork"] = matchesDomain(host, bundle.AllowedNetwork)
	}
	if action == "mcp.invoke" {
		tool, _ := properties["tool"].(string)
		// A grant-required MCP tool is intentionally absent from the ordinary
		// local-permit registry. Let Cedar evaluate every other forbid, then
		// convert its otherwise-permit result into an online AgentGrant decision.
		properties["approvedMCP"] = matchesExact(tool, bundle.ApprovedMCP) || agentRule != nil
	}
	if action == "agent.delegate" {
		target, _ := properties["target"].(string)
		properties["approvedDelegate"] = matchesExact(target, bundle.ApprovedDelegates)
	}
	list, err := cedar.NewPolicyListFromBytes("bundle.cedar", []byte(bundle.CedarPolicy))
	if err != nil {
		return Decision{}, err
	}
	set := cedar.NewPolicySet()
	for index, policy := range list {
		set.Add(cedar.PolicyID(fmt.Sprintf("policy-%d", index+1)), policy)
	}
	entitiesJSON, _ := json.Marshal([]map[string]any{
		{"uid": map[string]string{"type": "Agent", "id": request.Subject.ID}, "attrs": map[string]any{"enabled": true}, "parents": []any{}},
		{"uid": map[string]string{"type": "ToolInvocation", "id": request.Resource.ID}, "attrs": cedarAttributes(properties), "parents": []any{}},
	})
	var entities cedar.EntityMap
	if err := json.Unmarshal(entitiesJSON, &entities); err != nil {
		return Decision{}, err
	}
	cedarRequest := cedar.Request{Principal: cedar.NewEntityUID("Agent", cedar.String(request.Subject.ID)), Action: cedar.NewEntityUID("Action", cedar.String(action)), Resource: cedar.NewEntityUID("ToolInvocation", cedar.String(request.Resource.ID)), Context: cedar.NewRecord(cedar.RecordMap{})}
	decision, diagnostic := cedar.Authorize(set, entities, cedarRequest)
	if len(diagnostic.Errors) > 0 {
		return Decision{}, fmt.Errorf("Cedar evaluation error: %v", diagnostic.Errors)
	}
	if decision == cedar.Allow {
		if manualOnly {
			return Decision{
				ManualOnly:    true,
				Reason:        "This privileged access tool requires deliberate manual execution in a separate terminal after the user reviews the command and confirms the required access",
				ReasonCode:    "MANUAL_EXECUTION_REQUIRED",
				RuleIDs:       ruleIDs,
				PolicyVersion: policyVersion,
			}, nil
		}
		if agentRule == nil {
			return Decision{Allowed: true, Reason: "Allowed locally by signed BAP policy bundle", ReasonCode: "LOCAL_POLICY_PERMIT", RuleIDs: ruleIDs, PolicyVersion: policyVersion}, nil
		}
		return Decision{
			AgentGrantRequired:    true,
			Reason:                "This operation requires a short-lived, one-use BAP AgentGrant",
			ReasonCode:            "AGENT_GRANT_REQUIRED",
			RuleIDs:               []string{agentRule.ID},
			PolicyVersion:         policyVersion,
			GrantAudience:         agentRule.Resource,
			GrantResource:         agentRule.Resource,
			GrantTTL:              time.Duration(agentRule.MaxTTLSeconds) * time.Second,
			GrantIntentMaxAge:     time.Duration(agentRule.MaxIntentAgeSeconds) * time.Second,
			GrantMaxPerIntent:     agentRule.MaxGrantsPerIntent,
			RequiredIntentRuleIDs: append([]string(nil), agentRule.IntentRuleIDs...),
		}, nil
	}
	if len(diagnostic.Reasons) > 0 {
		return Decision{Reason: "An explicit signed BAP policy forbid applied", ReasonCode: "LOCAL_EXPLICIT_FORBID", RuleIDs: ruleIDs, PolicyVersion: policyVersion}, nil
	}
	if agentRule != nil {
		return Decision{
			AgentGrantRequired:    true,
			Reason:                "This operation requires a short-lived, one-use BAP AgentGrant",
			ReasonCode:            "AGENT_GRANT_REQUIRED",
			RuleIDs:               []string{agentRule.ID},
			PolicyVersion:         policyVersion,
			GrantAudience:         agentRule.Resource,
			GrantResource:         agentRule.Resource,
			GrantTTL:              time.Duration(agentRule.MaxTTLSeconds) * time.Second,
			GrantIntentMaxAge:     time.Duration(agentRule.MaxIntentAgeSeconds) * time.Second,
			GrantMaxPerIntent:     agentRule.MaxGrantsPerIntent,
			RequiredIntentRuleIDs: append([]string(nil), agentRule.IntentRuleIDs...),
		}, nil
	}
	return Decision{Reason: "No signed BAP policy permit matched", ReasonCode: "LOCAL_NO_MATCHING_POLICY", RuleIDs: ruleIDs, PolicyVersion: policyVersion}, nil
}

func matchAgentGrantRule(bundle Bundle, request authzen.EvaluationRequest) *AgentGrantRule {
	tool, _ := request.Resource.Properties["tool"].(string)
	method, _ := request.Resource.Properties["httpMethod"].(string)
	host, _ := request.Resource.Properties["networkHost"].(string)
	target, _ := request.Resource.Properties["target"].(string)
	parsedTarget, _ := url.Parse(target)
	mcpServer, _ := request.Resource.Properties["mcpServer"].(string)
	mcpTool, _ := request.Resource.Properties["mcpTool"].(string)
	for index := range bundle.AgentGrantRules {
		rule := &bundle.AgentGrantRules[index]
		if rule.Action != request.Action.Name || !strings.EqualFold(rule.Tool, tool) || !profileMatches(bundle.PolicyProfile, rule.Profiles) {
			continue
		}
		if rule.ResourceType == "api" && matchesExact(strings.ToUpper(method), rule.Methods) && matchesExact(host, rule.Hosts) && matchesExact(parsedTarget.EscapedPath(), rule.Paths) {
			return rule
		}
		if rule.ResourceType == "mcp" && matchesExact(mcpServer, rule.MCPServers) && matchesExact(mcpTool, rule.MCPTools) {
			return rule
		}
	}
	return nil
}

func classifyCommand(bundle Bundle, command string, now time.Time) (bool, bool, bool, []string, error) {
	args, err := splitCommand(command)
	if err != nil || len(args) == 0 {
		return false, false, false, nil, errors.New("unsupported or ambiguous shell command")
	}
	executable := strings.ToLower(filepath.Base(strings.ReplaceAll(args[0], "\\", "/")))
	remaining := args[1:]
	approved, forbidden, manualOnly := false, false, false
	matched := []string{}
	for _, rule := range bundle.CommandRules {
		if now.Before(rule.NotBefore) || !now.Before(rule.ExpiresAt) || !strings.EqualFold(executable, rule.Executable) || !profileMatches(bundle.PolicyProfile, rule.Profiles) {
			continue
		}
		candidate := remaining
		if rule.Subcommand != "" {
			if len(candidate) == 0 || !strings.EqualFold(candidate[0], rule.Subcommand) {
				continue
			}
			candidate = candidate[1:]
		}
		if !argumentsMatch(candidate, rule) {
			continue
		}
		matched = append(matched, rule.ID)
		if rule.Effect == "forbid" {
			forbidden = true
		} else if rule.Effect == "manual-only" {
			manualOnly = true
		} else if rule.Effect == "eligible-for-permit" {
			approved = true
		}
	}
	return approved, forbidden, manualOnly, matched, nil
}

func argumentsMatch(args []string, rule CommandRule) bool {
	if len(args) < len(rule.ArgumentPatterns) || (!rule.AllowAdditionalArguments && len(args) != len(rule.ArgumentPatterns)) {
		return false
	}
	for index, pattern := range rule.ArgumentPatterns {
		matched, _ := regexp.MatchString("^(?:"+pattern+")$", args[index])
		if !matched {
			return false
		}
	}
	for _, argument := range args[len(rule.ArgumentPatterns):] {
		matched, _ := regexp.MatchString("^(?:"+rule.AdditionalArgumentPattern+")$", argument)
		if !matched {
			return false
		}
	}
	return true
}

func splitCommand(command string) ([]string, error) {
	if strings.ContainsAny(command, ";&|<>`\r\n") || strings.Contains(command, "$(") {
		return nil, errors.New("shell operators are not supported")
	}
	var result []string
	var current strings.Builder
	var quote rune
	escaped := false
	flush := func() {
		if current.Len() > 0 {
			result = append(result, current.String())
			current.Reset()
		}
	}
	for _, char := range command {
		if escaped {
			current.WriteRune(char)
			escaped = false
			continue
		}
		if char == '\\' && quote != '\'' {
			escaped = true
			continue
		}
		if quote != 0 {
			if char == quote {
				quote = 0
			} else {
				current.WriteRune(char)
			}
			continue
		}
		if char == '\'' || char == '"' {
			quote = char
			continue
		}
		if char == ' ' || char == '\t' {
			flush()
			continue
		}
		current.WriteRune(char)
	}
	if escaped || quote != 0 {
		return nil, errors.New("unterminated shell quoting")
	}
	flush()
	return result, nil
}

func cedarAttributes(properties map[string]any) map[string]any {
	result := map[string]any{"tool": "", "target": "", "path": "", "command": "", "protected": false, "outsideWorkspace": false, "securityControl": false, "destructive": false, "privileged": false, "exfiltration": false, "obfuscated": false, "shellApproved": false, "policyProfile": "read-only", "approvedNetwork": false, "approvedMCP": false, "approvedDelegate": false, "networkHost": "", "mcpServer": "", "mcpTool": "", "httpMethod": "", "bodyDigest": "", "argumentsDigest": ""}
	for key := range result {
		if value, ok := properties[key]; ok {
			result[key] = value
		}
	}
	return result
}

func profileMatches(profile string, profiles []string) bool {
	return len(profiles) == 0 || matchesExact(profile, profiles)
}
func matchesExact(value string, entries []string) bool {
	for _, entry := range entries {
		if strings.EqualFold(strings.TrimSpace(entry), value) {
			return true
		}
	}
	return false
}
func matchesDomain(host string, entries []string) bool {
	host = strings.ToLower(host)
	for _, entry := range entries {
		entry = strings.ToLower(strings.TrimSpace(entry))
		if strings.HasPrefix(entry, "*.") {
			suffix := strings.TrimPrefix(entry, "*")
			if strings.HasSuffix(host, suffix) && host != strings.TrimPrefix(suffix, ".") {
				return true
			}
		} else if host == entry {
			return true
		}
	}
	return false
}
func cloneMap(source map[string]any) map[string]any {
	result := map[string]any{}
	for key, value := range source {
		result[key] = value
	}
	return result
}
func mustJSON(value any) []byte { data, _ := json.Marshal(value); return data }
