// Package agentgateway is an executable reference for the enforcement filter
// that belongs in the customized Spring Cloud Gateway. It validates the
// trusted BAP transport envelope, consumes the AgentGrant, strips all BAP
// metadata, and only then invokes the protected backend.
package agentgateway

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"bap-system/internal/agentgrant"
	"bap-system/internal/authzen"
)

type Input struct {
	Method    string                    `json:"method"`
	URL       string                    `json:"url"`
	Body      any                       `json:"body,omitempty"`
	Grant     string                    `json:"_bap_agent_grant"`
	Operation authzen.EvaluationRequest `json:"_bap_operation"`
}

type Consumer interface {
	Consume(context.Context, agentgrant.ConsumeRequest) (agentgrant.ConsumeResponse, error)
}

type Backend interface {
	Execute(context.Context, string, string, any, string) (any, error)
}

type Gateway struct {
	consumer Consumer
	backend  Backend
	resource string
}

func New(consumer Consumer, backend Backend, resource string) *Gateway {
	return &Gateway{consumer: consumer, backend: backend, resource: resource}
}

func (g *Gateway) Execute(ctx context.Context, input Input) (any, error) {
	if g.consumer == nil || g.backend == nil {
		return nil, errors.New("gateway is not configured")
	}
	if err := validateEnvelope(input); err != nil {
		return nil, err
	}
	if err := agentgrant.ValidateResource(g.resource); err != nil {
		return nil, errors.New("gateway resource indicator is invalid")
	}
	consumed, err := g.consumer.Consume(ctx, agentgrant.ConsumeRequest{Token: input.Grant, Resource: g.resource, Operation: input.Operation})
	if err != nil || !consumed.Consumed || consumed.GrantID == "" {
		return nil, errors.New("AgentGrant consumption was denied")
	}
	// The backend receives business fields only. Grant metadata is never
	// forwarded, and the grant ID doubles as a stable idempotency key.
	return g.backend.Execute(ctx, strings.ToUpper(input.Method), input.URL, input.Body, consumed.GrantID)
}

func validateEnvelope(input Input) error {
	if input.Grant == "" || input.Operation.Validate() != nil || input.Operation.Action.Name != "gateway.execute" {
		return errors.New("trusted BAP gateway envelope is incomplete")
	}
	properties := input.Operation.Resource.Properties
	tool, _ := properties["tool"].(string)
	method, _ := properties["httpMethod"].(string)
	target, _ := properties["target"].(string)
	digest, _ := properties["bodyDigest"].(string)
	if tool != agentgrant.GatewayToolName || method != strings.ToUpper(input.Method) || target != input.URL {
		return errors.New("business request does not match the AgentGrant operation")
	}
	body, err := json.Marshal(input.Body)
	if err != nil {
		return errors.New("gateway body is not JSON-compatible")
	}
	sum := sha256.Sum256(body)
	if digest != "sha256:"+hex.EncodeToString(sum[:]) {
		return fmt.Errorf("gateway body does not match the AgentGrant operation")
	}
	return nil
}
