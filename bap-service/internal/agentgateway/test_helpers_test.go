package agentgateway

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"

	"cc-filter/internal/authzen"
)

func testGatewayOperation(body any) authzen.EvaluationRequest {
	data, _ := json.Marshal(body)
	digest := sha256.Sum256(data)
	return authzen.EvaluationRequest{
		Subject: authzen.Entity{Type: "agent", ID: "claude-code-local"},
		Action:  authzen.Action{Name: "gateway.execute"},
		Resource: authzen.Entity{Type: "tool-invocation", ID: "resource", Properties: map[string]any{
			"tool": "mcp__bap_gateway__execute", "target": "https://api.staging.company.example/orders/deploy", "networkHost": "api.staging.company.example", "httpMethod": "POST", "bodyDigest": "sha256:" + hex.EncodeToString(digest[:]),
		}},
	}
}
