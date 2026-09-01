package cedaradapter

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"

	"cc-filter/internal/authzen"
	cedar "github.com/cedar-policy/cedar-go"
)

// Engine evaluates AuthZEN requests with the BAP Service policy set.
type Engine struct {
	policies      *cedar.PolicySet
	policyVersion string
}

func New(policyPath string) (*Engine, error) {
	document, err := os.ReadFile(policyPath)
	if err != nil {
		return nil, fmt.Errorf("read Cedar policies: %w", err)
	}
	list, err := cedar.NewPolicyListFromBytes(policyPath, document)
	if err != nil {
		return nil, fmt.Errorf("parse Cedar policies: %w", err)
	}
	set := cedar.NewPolicySet()
	for index, policy := range list {
		set.Add(cedar.PolicyID(fmt.Sprintf("policy-%d", index+1)), policy)
	}
	sum := sha256.Sum256(document)
	return &Engine{policies: set, policyVersion: "sha256:" + hex.EncodeToString(sum[:])}, nil
}

func (e *Engine) PolicyVersion() string { return e.policyVersion }

func (e *Engine) Authorize(request authzen.EvaluationRequest) (bool, string, string, error) {
	entitiesJSON, err := json.Marshal([]map[string]any{
		{
			"uid":     map[string]string{"type": "Agent", "id": request.Subject.ID},
			"attrs":   map[string]any{"enabled": true},
			"parents": []any{},
		},
		{
			"uid":     map[string]string{"type": "ToolInvocation", "id": request.Resource.ID},
			"attrs":   cedarAttributes(request.Resource.Properties),
			"parents": []any{},
		},
	})
	if err != nil {
		return false, "", "EVALUATION_ERROR", fmt.Errorf("encode Cedar entities: %w", err)
	}
	var entities cedar.EntityMap
	if err := json.Unmarshal(entitiesJSON, &entities); err != nil {
		return false, "", "EVALUATION_ERROR", fmt.Errorf("construct Cedar entities: %w", err)
	}

	cedarRequest := cedar.Request{
		Principal: cedar.NewEntityUID("Agent", cedar.String(request.Subject.ID)),
		Action:    cedar.NewEntityUID("Action", cedar.String(request.Action.Name)),
		Resource:  cedar.NewEntityUID("ToolInvocation", cedar.String(request.Resource.ID)),
		Context:   cedar.NewRecord(cedar.RecordMap{}),
	}
	decision, diagnostic := cedar.Authorize(e.policies, entities, cedarRequest)
	if len(diagnostic.Errors) > 0 {
		return false, "", "EVALUATION_ERROR", fmt.Errorf("Cedar evaluation error: %v", diagnostic.Errors)
	}
	if decision == cedar.Allow {
		return true, "Allowed by Cedar policy", "POLICY_PERMIT", nil
	}
	if len(diagnostic.Reasons) > 0 {
		return false, "An explicit Cedar forbid policy applied", "EXPLICIT_FORBID", nil
	}
	return false, "No Cedar permit matched", "NO_MATCHING_POLICY", nil
}

func cedarAttributes(properties map[string]any) map[string]any {
	result := map[string]any{
		"tool":             "",
		"target":           "",
		"path":             "",
		"command":          "",
		"protected":        false,
		"outsideWorkspace": false,
		"securityControl":  false,
		"destructive":      false,
		"privileged":       false,
		"exfiltration":     false,
		"obfuscated":       false,
		"shellApproved":    false,
		"policyProfile":    "standard-developer",
		"approvedNetwork":  false,
		"approvedMCP":      false,
		"approvedDelegate": false,
		"networkHost":      "",
		"mcpServer":        "",
		"mcpTool":          "",
		"httpMethod":       "",
		"bodyDigest":       "",
	}
	for key := range result {
		if value, ok := properties[key]; ok {
			result[key] = value
		}
	}
	return result
}
