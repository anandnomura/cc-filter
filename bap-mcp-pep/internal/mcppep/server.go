// Package mcppep implements a resource-side MCP policy enforcement point.
// It terminates the client MCP call, validates the trusted Edge envelope,
// consumes the one-use AgentGrant, strips BAP fields, and calls an upstream MCP
// server with a PEP-owned identity.
package mcppep

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"bap-system/internal/agentgrant"
	"bap-system/internal/authzen"
)

const ProtocolVersion = "2025-06-18"

type Config struct {
	ListenAddress                        string       `json:"listen_address"`
	STSURL                               string       `json:"agent_sts_url"`
	STSAPIKeyEnv                         string       `json:"agent_sts_api_key_env"`
	STSCAPath                            string       `json:"agent_sts_ca_path,omitempty"`
	UpstreamURL                          string       `json:"upstream_url"`
	UpstreamKeyEnv                       string       `json:"upstream_api_key_env"`
	UpstreamCAPath                       string       `json:"upstream_ca_path,omitempty"`
	AllowedOrigins                       []string     `json:"allowed_origins,omitempty"`
	TLSCertPath                          string       `json:"tls_cert_path,omitempty"`
	TLSKeyPath                           string       `json:"tls_key_path,omitempty"`
	AllowDevelopmentCleartextHostGateway bool         `json:"allow_development_cleartext_host_gateway,omitempty"`
	Tools                                []ToolPolicy `json:"tools"`
}

type ToolPolicy struct {
	PublicName        string         `json:"public_name"`
	UpstreamName      string         `json:"upstream_name"`
	ClaudeToolName    string         `json:"claude_tool_name"`
	MCPServer         string         `json:"mcp_server"`
	Description       string         `json:"description"`
	InputSchema       map[string]any `json:"input_schema"`
	RequiredArguments []string       `json:"required_arguments,omitempty"`
	ExactArguments    map[string]any `json:"exact_arguments,omitempty"`
}

func LoadConfig(path string) (Config, error) {
	var config Config
	data, err := os.ReadFile(path)
	if err != nil {
		return config, err
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&config); err != nil {
		return config, fmt.Errorf("decode MCP PEP config: %w", err)
	}
	if config.ListenAddress == "" {
		config.ListenAddress = "127.0.0.1:8765"
	}
	if config.STSAPIKeyEnv == "" || config.UpstreamKeyEnv == "" {
		return config, errors.New("MCP PEP credential environment-variable names are required")
	}
	for _, raw := range []string{config.STSURL, config.UpstreamURL} {
		parsed, parseErr := url.Parse(raw)
		if parseErr != nil || parsed.Hostname() == "" || (parsed.Scheme != "https" && parsed.Scheme != "http") {
			return config, fmt.Errorf("invalid MCP PEP URL %q", raw)
		}
		developmentHostGateway := config.AllowDevelopmentCleartextHostGateway && strings.EqualFold(parsed.Hostname(), "host.containers.internal")
		if parsed.Scheme != "https" && parsed.Hostname() != "127.0.0.1" && parsed.Hostname() != "localhost" && !developmentHostGateway {
			return config, fmt.Errorf("cleartext MCP PEP URL must be loopback: %q", raw)
		}
	}
	if len(config.Tools) == 0 {
		return config, errors.New("at least one protected MCP tool is required")
	}
	seen := map[string]bool{}
	for index, tool := range config.Tools {
		if tool.PublicName == "" || tool.UpstreamName == "" || tool.ClaudeToolName == "" || tool.MCPServer == "" || tool.Description == "" || len(tool.InputSchema) == 0 || seen[tool.PublicName] {
			return config, fmt.Errorf("protected MCP tool %d is incomplete or duplicate", index)
		}
		seen[tool.PublicName] = true
	}
	if (config.TLSCertPath == "") != (config.TLSKeyPath == "") {
		return config, errors.New("both MCP PEP TLS certificate and key are required together")
	}
	return config, nil
}

type Server struct {
	config      Config
	stsKey      string
	upstreamKey string
	stsClient   *http.Client
	upstream    *http.Client
	tools       map[string]ToolPolicy
}

func New(config Config) (*Server, error) {
	stsKey, upstreamKey := os.Getenv(config.STSAPIKeyEnv), os.Getenv(config.UpstreamKeyEnv)
	if stsKey == "" || upstreamKey == "" {
		return nil, errors.New("MCP PEP credentials are not available in the configured environment variables")
	}
	stsClient, err := secureClient(config.STSCAPath)
	if err != nil {
		return nil, fmt.Errorf("configure Agent STS client: %w", err)
	}
	upstreamClient, err := secureClient(config.UpstreamCAPath)
	if err != nil {
		return nil, fmt.Errorf("configure upstream MCP client: %w", err)
	}
	tools := make(map[string]ToolPolicy, len(config.Tools))
	for _, tool := range config.Tools {
		tools[tool.PublicName] = tool
	}
	return &Server{config: config, stsKey: stsKey, upstreamKey: upstreamKey, stsClient: stsClient, upstream: upstreamClient, tools: tools}, nil
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	mux.HandleFunc("GET /mcp", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusMethodNotAllowed) })
	mux.HandleFunc("POST /mcp", s.handleMCP)
	return mux
}

func (s *Server) handleMCP(w http.ResponseWriter, r *http.Request) {
	if !s.originAllowed(r.Header.Get("Origin")) {
		writeJSON(w, http.StatusForbidden, rpcError(nil, -32001, "origin is not allowed"))
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<20))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, rpcError(nil, -32700, "invalid JSON-RPC request"))
		return
	}
	if len(bytes.TrimSpace(body)) > 0 && bytes.TrimSpace(body)[0] == '[' {
		var batch []json.RawMessage
		if json.Unmarshal(body, &batch) != nil || len(batch) == 0 {
			writeJSON(w, http.StatusBadRequest, rpcError(nil, -32600, "invalid JSON-RPC batch"))
			return
		}
		responses := make([]any, 0, len(batch))
		for _, item := range batch {
			if response := s.process(r.Context(), item); response != nil {
				responses = append(responses, response)
			}
		}
		if len(responses) == 0 {
			w.WriteHeader(http.StatusAccepted)
			return
		}
		writeJSON(w, http.StatusOK, responses)
		return
	}
	response := s.process(r.Context(), body)
	if response == nil {
		w.WriteHeader(http.StatusAccepted)
		return
	}
	writeJSON(w, http.StatusOK, response)
}

type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      any             `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

func (s *Server) process(ctx context.Context, raw json.RawMessage) any {
	var request rpcRequest
	if json.Unmarshal(raw, &request) != nil || request.JSONRPC != "2.0" || request.Method == "" {
		return rpcError(request.ID, -32600, "invalid JSON-RPC request")
	}
	if request.ID == nil {
		return nil
	}
	switch request.Method {
	case "initialize":
		return rpcResult(request.ID, map[string]any{"protocolVersion": ProtocolVersion, "capabilities": map[string]any{"tools": map[string]any{"listChanged": false}}, "serverInfo": map[string]string{"name": "bap-mcp-pep", "version": "dev"}})
	case "tools/list":
		tools := make([]any, 0, len(s.config.Tools))
		for _, tool := range s.config.Tools {
			tools = append(tools, map[string]any{"name": tool.PublicName, "description": tool.Description, "inputSchema": tool.InputSchema})
		}
		return rpcResult(request.ID, map[string]any{"tools": tools})
	case "tools/call":
		result, err := s.callTool(ctx, request)
		if err != nil {
			return rpcError(request.ID, -32003, err.Error())
		}
		return result
	default:
		return rpcError(request.ID, -32601, "method not found")
	}
}

func (s *Server) callTool(ctx context.Context, request rpcRequest) (any, error) {
	var params struct {
		Name      string         `json:"name"`
		Arguments map[string]any `json:"arguments"`
	}
	if json.Unmarshal(request.Params, &params) != nil || params.Name == "" || params.Arguments == nil {
		return nil, errors.New("invalid tools/call parameters")
	}
	policy, ok := s.tools[params.Name]
	if !ok {
		return nil, errors.New("protected MCP tool is not registered")
	}
	token, tokenOK := params.Arguments["_bap_agent_grant"].(string)
	operationValue, operationOK := params.Arguments["_bap_operation"]
	if !tokenOK || token == "" || !operationOK {
		return nil, errors.New("trusted BAP envelope is required")
	}
	delete(params.Arguments, "_bap_agent_grant")
	delete(params.Arguments, "_bap_operation")
	if err := validateArguments(policy, params.Arguments); err != nil {
		return nil, err
	}
	operationJSON, _ := json.Marshal(operationValue)
	var operation authzen.EvaluationRequest
	if json.Unmarshal(operationJSON, &operation) != nil || operation.Validate() != nil {
		return nil, errors.New("trusted BAP operation is invalid")
	}
	if err := validateOperation(policy, params.Arguments, operation); err != nil {
		return nil, err
	}
	grantID, err := s.consume(ctx, token, operation)
	if err != nil {
		return nil, errors.New("AgentGrant consumption was denied")
	}
	upstreamParams, _ := json.Marshal(map[string]any{"name": policy.UpstreamName, "arguments": params.Arguments})
	upstreamRequest := rpcRequest{JSONRPC: "2.0", ID: request.ID, Method: "tools/call", Params: upstreamParams}
	payload, _ := json.Marshal(upstreamRequest)
	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodPost, s.config.UpstreamURL, bytes.NewReader(payload))
	if err != nil {
		return nil, errors.New("create upstream MCP request")
	}
	httpRequest.Header.Set("Authorization", "Bearer "+s.upstreamKey)
	httpRequest.Header.Set("Content-Type", "application/json")
	httpRequest.Header.Set("Accept", "application/json")
	httpRequest.Header.Set("MCP-Protocol-Version", ProtocolVersion)
	httpRequest.Header.Set("X-BAP-Grant-ID", grantID)
	response, err := s.upstream.Do(httpRequest)
	if err != nil {
		return nil, errors.New("upstream MCP server is unavailable")
	}
	defer response.Body.Close()
	responseBody, _ := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if response.StatusCode != http.StatusOK {
		return nil, errors.New("upstream MCP server rejected the PEP identity")
	}
	var result any
	if json.Unmarshal(responseBody, &result) != nil {
		return nil, errors.New("upstream MCP response is invalid")
	}
	return result, nil
}

func validateArguments(policy ToolPolicy, arguments map[string]any) error {
	for _, name := range policy.RequiredArguments {
		if _, ok := arguments[name]; !ok {
			return fmt.Errorf("required MCP argument %q is missing", name)
		}
	}
	for name, expected := range policy.ExactArguments {
		actual, ok := arguments[name]
		if !ok || !jsonEqual(actual, expected) {
			return fmt.Errorf("MCP argument %q is outside protected tool policy", name)
		}
	}
	return nil
}

func validateOperation(policy ToolPolicy, arguments map[string]any, operation authzen.EvaluationRequest) error {
	if operation.Action.Name != "mcp.invoke" {
		return errors.New("AgentGrant is not for an MCP invocation")
	}
	properties := operation.Resource.Properties
	tool, _ := properties["tool"].(string)
	server, _ := properties["mcpServer"].(string)
	name, _ := properties["mcpTool"].(string)
	digest, _ := properties["argumentsDigest"].(string)
	if tool != policy.ClaudeToolName || server != policy.MCPServer || name != policy.PublicName {
		return errors.New("MCP request does not match the AgentGrant operation")
	}
	data, err := json.Marshal(arguments)
	if err != nil {
		return errors.New("MCP arguments are not JSON-compatible")
	}
	sum := sha256.Sum256(data)
	if digest != "sha256:"+hex.EncodeToString(sum[:]) {
		return errors.New("MCP arguments do not match the AgentGrant operation")
	}
	return nil
}

func (s *Server) consume(ctx context.Context, token string, operation authzen.EvaluationRequest) (string, error) {
	payload, _ := json.Marshal(agentgrant.ConsumeRequest{Token: token, Operation: operation})
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(s.config.STSURL, "/")+"/bap/v1/agent-sts/consume", bytes.NewReader(payload))
	if err != nil {
		return "", err
	}
	request.Header.Set("Authorization", "Bearer "+s.stsKey)
	request.Header.Set("Content-Type", "application/json")
	response, err := s.stsClient.Do(request)
	if err != nil {
		return "", err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return "", errors.New("consume denied")
	}
	var consumed agentgrant.ConsumeResponse
	if json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&consumed) != nil || !consumed.Consumed || consumed.GrantID == "" {
		return "", errors.New("invalid consume response")
	}
	return consumed.GrantID, nil
}

func (s *Server) originAllowed(origin string) bool {
	if origin == "" {
		return true
	}
	for _, allowed := range s.config.AllowedOrigins {
		if strings.EqualFold(origin, allowed) {
			return true
		}
	}
	return false
}

func secureClient(caPath string) (*http.Client, error) {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.TLSClientConfig = &tls.Config{MinVersion: tls.VersionTLS13}
	if caPath != "" {
		data, err := os.ReadFile(caPath)
		if err != nil {
			return nil, err
		}
		roots, _ := x509.SystemCertPool()
		if roots == nil {
			roots = x509.NewCertPool()
		}
		if !roots.AppendCertsFromPEM(data) {
			return nil, errors.New("CA bundle has no certificates")
		}
		transport.TLSClientConfig.RootCAs = roots
	}
	return &http.Client{Transport: transport, Timeout: 10 * time.Second}, nil
}

func rpcResult(id any, result any) map[string]any {
	return map[string]any{"jsonrpc": "2.0", "id": id, "result": result}
}
func rpcError(id any, code int, message string) map[string]any {
	return map[string]any{"jsonrpc": "2.0", "id": id, "error": map[string]any{"code": code, "message": message}}
}
func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
func jsonEqual(left, right any) bool {
	a, _ := json.Marshal(left)
	b, _ := json.Marshal(right)
	return bytes.Equal(a, b)
}
