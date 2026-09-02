// Package agentgrant defines the short-lived, one-operation capability issued
// by BAP Agent STS. Tokens are transported by trusted runtime code and must
// never be placed in model context, prompts, or generated shell commands.
package agentgrant

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"bap-system/internal/authzen"
	"bap-system/internal/resourceindicator"
)

const Type = "BAP-AgentGrant-EdDSA-v3"
const GatewayToolName = "mcp__bap_gateway__execute"

var ErrInvalidTarget = errors.New("invalid_target")

type IntentEvidence struct {
	IntentID   string   `json:"intent_id"`
	SessionID  string   `json:"session_id"`
	WorkloadID string   `json:"workload_id"`
	IntentHash string   `json:"intent_hash"`
	RuleIDs    []string `json:"rule_ids"`
	CapturedAt int64    `json:"captured_at"`
}

type Claims struct {
	Issuer                string   `json:"iss"`
	Audience              string   `json:"aud"`
	GrantID               string   `json:"jti"`
	Subject               string   `json:"sub"`
	Principal             string   `json:"principal"`
	CredentialFingerprint string   `json:"credential_fingerprint"`
	EdgeInstanceID        string   `json:"edge_instance_id"`
	SessionID             string   `json:"session_id"`
	WorkloadID            string   `json:"workload_id"`
	ToolUseID             string   `json:"tool_use_id"`
	Tool                  string   `json:"tool"`
	Action                string   `json:"action"`
	Resource              string   `json:"resource"`
	OperationResourceID   string   `json:"operation_resource_id"`
	RequestHash           string   `json:"request_hash"`
	IntentHash            string   `json:"intent_hash"`
	IntentID              string   `json:"intent_id"`
	IntentRuleIDs         []string `json:"intent_rule_ids"`
	PolicyRuleIDs         []string `json:"policy_rule_ids"`
	PolicyVersion         uint64   `json:"policy_version"`
	PolicyDigest          string   `json:"policy_digest"`
	RevocationEpoch       uint64   `json:"revocation_epoch"`
	MaxUses               uint32   `json:"max_uses"`
	IssuedAt              int64    `json:"iat"`
	NotBefore             int64    `json:"nbf"`
	ExpiresAt             int64    `json:"exp"`
}

type IssueRequest struct {
	EdgeInstanceID string                    `json:"edge_instance_id"`
	Resource       string                    `json:"resource"`
	Operation      authzen.EvaluationRequest `json:"operation"`
	Intent         IntentEvidence            `json:"intent"`
}

type IssueResponse struct {
	Token     string `json:"agent_grant"`
	GrantID   string `json:"grant_id"`
	ExpiresAt string `json:"expires_at"`
	Audience  string `json:"audience"`
	Resource  string `json:"resource"`
}

type ConsumeRequest struct {
	Token     string                    `json:"agent_grant"`
	Resource  string                    `json:"resource"`
	Operation authzen.EvaluationRequest `json:"operation"`
}

type ConsumeResponse struct {
	Consumed   bool   `json:"consumed"`
	GrantID    string `json:"grant_id"`
	DecisionID string `json:"decision_id"`
}

type VerifyOptions struct {
	Issuer          string
	Audience        string
	Resource        string
	RequestHash     string
	PolicyVersion   uint64
	PolicyDigest    string
	RevocationEpoch uint64
	Now             time.Time
}

type header struct {
	Algorithm string `json:"alg"`
	Type      string `json:"typ"`
}

func (r IssueRequest) Validate() error {
	if r.EdgeInstanceID == "" || r.Operation.Validate() != nil {
		return errors.New("edge_instance_id and a valid operation are required")
	}
	if err := ValidateResource(r.Resource); err != nil {
		return err
	}
	sessionID, _ := r.Operation.Context["session_id"].(string)
	workloadID, _ := r.Operation.Context["workload_id"].(string)
	if r.Intent.IntentID == "" || r.Intent.SessionID == "" || r.Intent.WorkloadID == "" || r.Intent.IntentHash == "" || len(r.Intent.RuleIDs) == 0 {
		return errors.New("classified intent evidence is required")
	}
	if r.Intent.SessionID != sessionID || r.Intent.WorkloadID != workloadID {
		return errors.New("intent evidence is not bound to the operation session and workload")
	}
	return nil
}

// ValidateResource applies BAP's mandatory single-resource RFC 8707 profile.
func ValidateResource(resource string) error {
	if err := resourceindicator.Validate(resource); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidTarget, err)
	}
	return nil
}

func Sign(privateKey ed25519.PrivateKey, claims Claims) (string, error) {
	if err := validateClaims(claims); err != nil {
		return "", err
	}
	headerJSON, _ := json.Marshal(header{Algorithm: "EdDSA", Type: Type})
	claimsJSON, err := json.Marshal(claims)
	if err != nil {
		return "", err
	}
	unsigned := encode(headerJSON) + "." + encode(claimsJSON)
	signature := ed25519.Sign(privateKey, []byte(unsigned))
	return unsigned + "." + encode(signature), nil
}

func Verify(publicKey ed25519.PublicKey, token string, options VerifyOptions) (Claims, error) {
	var claims Claims
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return claims, errors.New("AgentGrant must contain three segments")
	}
	headerJSON, err := decode(parts[0])
	if err != nil {
		return claims, errors.New("invalid AgentGrant header")
	}
	var h header
	if json.Unmarshal(headerJSON, &h) != nil || h.Algorithm != "EdDSA" || h.Type != Type {
		return claims, errors.New("unsupported AgentGrant header")
	}
	signature, err := decode(parts[2])
	if err != nil || !ed25519.Verify(publicKey, []byte(parts[0]+"."+parts[1]), signature) {
		return claims, errors.New("invalid AgentGrant signature")
	}
	claimsJSON, err := decode(parts[1])
	if err != nil || json.Unmarshal(claimsJSON, &claims) != nil || validateClaims(claims) != nil {
		return claims, errors.New("invalid AgentGrant claims")
	}
	now := options.Now.UTC()
	if now.IsZero() {
		now = time.Now().UTC()
	}
	if claims.Audience != options.Audience {
		return claims, errors.New("AgentGrant audience mismatch")
	}
	if options.Resource == "" || claims.Resource != options.Resource || claims.Audience != options.Resource {
		return claims, fmt.Errorf("%w: AgentGrant resource mismatch", ErrInvalidTarget)
	}
	if options.Issuer == "" || claims.Issuer != options.Issuer {
		return claims, errors.New("AgentGrant issuer mismatch")
	}
	if claims.RequestHash != options.RequestHash {
		return claims, errors.New("AgentGrant request binding mismatch")
	}
	if claims.PolicyVersion != options.PolicyVersion || claims.PolicyDigest != options.PolicyDigest || claims.RevocationEpoch != options.RevocationEpoch {
		return claims, errors.New("AgentGrant policy binding is stale")
	}
	if now.Unix() < claims.NotBefore || now.Unix() >= claims.ExpiresAt || claims.IssuedAt > now.Add(30*time.Second).Unix() {
		return claims, errors.New("AgentGrant is expired or not yet valid")
	}
	return claims, nil
}

func HashOperation(operation authzen.EvaluationRequest) (string, error) {
	data, err := json.Marshal(operation)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(data)
	return base64.RawURLEncoding.EncodeToString(digest[:]), nil
}

func HashIntent(prompt string) string {
	digest := sha256.Sum256([]byte(strings.TrimSpace(prompt)))
	return "sha256:" + fmt.Sprintf("%x", digest[:])
}

func NewID() (string, error) {
	value := make([]byte, 18)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	return "ag_" + base64.RawURLEncoding.EncodeToString(value), nil
}

func NewIntentID() (string, error) {
	value := make([]byte, 18)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	return "agi_" + base64.RawURLEncoding.EncodeToString(value), nil
}

func validateClaims(claims Claims) error {
	if claims.Issuer == "" || claims.Audience == "" || claims.GrantID == "" || claims.Subject == "" || claims.Principal == "" || claims.CredentialFingerprint == "" || claims.EdgeInstanceID == "" || claims.SessionID == "" || claims.WorkloadID == "" || claims.ToolUseID == "" || claims.Tool == "" || claims.Action == "" || claims.OperationResourceID == "" || claims.RequestHash == "" || claims.IntentID == "" || claims.IntentHash == "" || len(claims.IntentRuleIDs) == 0 || len(claims.PolicyRuleIDs) == 0 || claims.PolicyVersion == 0 || claims.PolicyDigest == "" || claims.MaxUses != 1 || claims.IssuedAt == 0 || claims.NotBefore == 0 || claims.ExpiresAt <= claims.NotBefore {
		return errors.New("AgentGrant claims are incomplete")
	}
	if err := resourceindicator.Validate(claims.Resource); err != nil || claims.Audience != claims.Resource {
		return errors.New("AgentGrant resource and audience binding is invalid")
	}
	return nil
}

func encode(value []byte) string          { return base64.RawURLEncoding.EncodeToString(value) }
func decode(value string) ([]byte, error) { return base64.RawURLEncoding.DecodeString(value) }
