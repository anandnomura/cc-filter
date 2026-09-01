package bapedge

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"cc-filter/internal/agentgrant"
	"cc-filter/internal/auditwire"
	"cc-filter/internal/authzen"
	"cc-filter/internal/grants"
	"cc-filter/internal/policybundle"
	"cc-filter/internal/tracecontext"
)

type Client struct {
	baseURL    string
	stsBaseURL string
	stsIssuer  string
	audience   string
	publicKey  ed25519.PublicKey
	apiKey     string
	stsAPIKey  string
	http       *http.Client
}

func (c *Client) IssueAgentGrant(ctx context.Context, request agentgrant.IssueRequest, trace tracecontext.Context) (agentgrant.IssueResponse, error) {
	var result agentgrant.IssueResponse
	body, err := json.Marshal(request)
	if err != nil {
		return result, err
	}
	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodPost, c.stsBaseURL+"/bap/v1/agent-sts/issue", bytes.NewReader(body))
	if err != nil {
		return result, err
	}
	httpRequest.Header.Set("Content-Type", "application/json")
	httpRequest.Header.Set("X-Request-ID", requestID())
	applyTrace(httpRequest, trace.TraceParent())
	c.authorizeWithKey(httpRequest, c.stsAPIKey)
	response, err := c.http.Do(httpRequest)
	if err != nil {
		return result, fmt.Errorf("call BAP Agent STS: %w", err)
	}
	defer response.Body.Close()
	responseBody, _ := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if response.StatusCode != http.StatusOK {
		return result, fmt.Errorf("BAP Agent STS denied issuance with HTTP %d", response.StatusCode)
	}
	if err := json.Unmarshal(responseBody, &result); err != nil || result.Token == "" || result.GrantID == "" {
		return result, fmt.Errorf("decode BAP Agent STS response")
	}
	return result, nil
}

func NewClient(config Config) (*Client, error) {
	stsURL := config.AgentSTSURL
	if stsURL == "" {
		stsURL = config.ServiceURL
	}
	stsIssuer := config.AgentSTSIssuer
	if stsIssuer == "" {
		stsIssuer = "bap-agent-sts-local"
	}
	stsAPIKey := config.APIKey()
	if config.AgentSTSAPIKeyEnv != "" {
		stsAPIKey = config.AgentSTSAPIKey()
	}
	if (config.APIKey() == "" || stsAPIKey == "") && config.ClientCertificatePath == "" {
		return nil, fmt.Errorf("required BAP credential environment variable %s is empty", config.APIKeyEnv)
	}
	var publicKey ed25519.PublicKey
	var err error
	if config.PublicKeyPath != "" {
		publicKey, err = grants.LoadPublicKey(config.PublicKeyPath)
		if err != nil {
			return nil, fmt.Errorf("load BAP AgentGrant verification key: %w", err)
		}
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	tlsConfig := &tls.Config{MinVersion: tls.VersionTLS12}
	if config.CABundlePath != "" {
		caPEM, err := os.ReadFile(config.CABundlePath)
		if err != nil {
			return nil, fmt.Errorf("read BAP Service CA bundle: %w", err)
		}
		roots, err := x509.SystemCertPool()
		if err != nil || roots == nil {
			roots = x509.NewCertPool()
		}
		if !roots.AppendCertsFromPEM(caPEM) {
			return nil, fmt.Errorf("BAP Service CA bundle contains no certificates")
		}
		tlsConfig.RootCAs = roots
	}
	if config.ClientCertificatePath != "" {
		certificate, err := tls.LoadX509KeyPair(config.ClientCertificatePath, config.ClientKeyPath)
		if err != nil {
			return nil, fmt.Errorf("load BAP Edge client identity: %w", err)
		}
		tlsConfig.Certificates = []tls.Certificate{certificate}
	}
	transport.TLSClientConfig = tlsConfig
	return &Client{
		baseURL: strings.TrimRight(config.ServiceURL, "/"), stsBaseURL: strings.TrimRight(stsURL, "/"), stsIssuer: stsIssuer, audience: "bap-edge", publicKey: publicKey, apiKey: config.APIKey(), stsAPIKey: stsAPIKey,
		http: &http.Client{Timeout: config.Timeout(), Transport: transport},
	}, nil
}

func validateServiceURL(value string) error {
	parsed, err := url.Parse(value)
	if err != nil || parsed.Hostname() == "" {
		return fmt.Errorf("service_url must be an absolute URL")
	}
	if parsed.Scheme == "https" {
		return nil
	}
	if parsed.Scheme != "http" {
		return fmt.Errorf("service_url must use https, except for localhost development")
	}
	host := strings.ToLower(parsed.Hostname())
	if host != "localhost" && host != "127.0.0.1" && host != "::1" {
		return fmt.Errorf("cleartext BAP Service URLs are allowed only on loopback; use https for network deployment")
	}
	return nil
}

func (c *Client) Health(ctx context.Context) error {
	request, _ := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/healthz", nil)
	response, err := c.http.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("health endpoint returned HTTP %d", response.StatusCode)
	}
	return nil
}

func (c *Client) SyncPolicy(ctx context.Context, request policybundle.SyncRequest) (policybundle.SyncResponse, error) {
	var result policybundle.SyncResponse
	body, err := json.Marshal(request)
	if err != nil {
		return result, err
	}
	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/bap/v1/edge/sync", bytes.NewReader(body))
	if err != nil {
		return result, err
	}
	httpRequest.Header.Set("Content-Type", "application/json")
	c.authorize(httpRequest)
	response, err := c.http.Do(httpRequest)
	if err != nil {
		return result, fmt.Errorf("synchronize BAP policy: %w", err)
	}
	defer response.Body.Close()
	responseBody, _ := io.ReadAll(io.LimitReader(response.Body, 4<<20))
	if response.StatusCode != http.StatusOK {
		return result, fmt.Errorf("BAP policy sync returned HTTP %d", response.StatusCode)
	}
	if err := json.Unmarshal(responseBody, &result); err != nil {
		return result, fmt.Errorf("decode BAP policy sync: %w", err)
	}
	return result, nil
}

func (c *Client) Evaluate(ctx context.Context, request authzen.EvaluationRequest, trace tracecontext.Context) (authzen.Decision, error) {
	body, err := json.Marshal(request)
	if err != nil {
		return authzen.Decision{}, err
	}
	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/access/v1/evaluation", bytes.NewReader(body))
	if err != nil {
		return authzen.Decision{}, err
	}
	httpRequest.Header.Set("Content-Type", "application/json")
	c.authorize(httpRequest)
	httpRequest.Header.Set("X-Request-ID", requestID())
	applyTrace(httpRequest, trace.TraceParent())
	response, err := c.http.Do(httpRequest)
	if err != nil {
		return authzen.Decision{}, fmt.Errorf("call BAP Service: %w", err)
	}
	defer response.Body.Close()
	responseBody, _ := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if response.StatusCode != http.StatusOK {
		return authzen.Decision{}, fmt.Errorf("BAP Service returned HTTP %d", response.StatusCode)
	}
	var decision authzen.Decision
	if err := json.Unmarshal(responseBody, &decision); err != nil {
		return authzen.Decision{}, fmt.Errorf("decode BAP decision: %w", err)
	}
	if !decision.Decision {
		return decision, nil
	}
	grant, ok := decision.Context["grant"].(string)
	if !ok || grant == "" {
		return authzen.Decision{}, fmt.Errorf("allow decision did not contain a grant")
	}
	hash, err := grants.HashRequest(request)
	if err != nil {
		return authzen.Decision{}, err
	}
	if err := c.VerifyGrant(grant, hash); err != nil {
		return authzen.Decision{}, fmt.Errorf("verify BAP grant: %w", err)
	}
	return decision, nil
}

func (c *Client) post(ctx context.Context, path string, value any, traceParent string) error {
	body, err := json.Marshal(value)
	if err != nil {
		return err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, bytes.NewReader(body))
	if err != nil {
		return err
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Request-ID", requestID())
	applyTrace(request, traceParent)
	c.authorize(request)
	response, err := c.http.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("BAP Service returned HTTP %d", response.StatusCode)
	}
	return nil
}

func (c *Client) AuditGrantConsumption(ctx context.Context, request authzen.EvaluationRequest, grant string, trace tracecontext.Context) error {
	return c.post(ctx, "/bap/v1/audit/grant-consumption", auditwire.GrantConsumption{Request: request, Grant: grant, TraceParent: trace.TraceParent()}, trace.TraceParent())
}

func (c *Client) ReportOutcome(ctx context.Context, outcome auditwire.Outcome) error {
	return c.post(ctx, "/bap/v1/audit/outcome", outcome, outcome.TraceParent)
}

func (c *Client) ReportEdgeDenial(ctx context.Context, denial auditwire.EdgeDenial) error {
	return c.post(ctx, "/bap/v1/audit/edge-denial", denial, denial.TraceParent)
}

func (c *Client) ReportEdgeDecision(ctx context.Context, decision auditwire.EdgeDecision) error {
	return c.post(ctx, "/bap/v1/audit/edge-decision", decision, decision.TraceParent)
}

func applyTrace(request *http.Request, traceParent string) {
	if _, ok := tracecontext.Parse(traceParent); ok {
		request.Header.Set("traceparent", traceParent)
	}
}

func (c *Client) authorize(request *http.Request) {
	c.authorizeWithKey(request, c.apiKey)
}

func (c *Client) authorizeWithKey(request *http.Request, key string) {
	if key != "" {
		request.Header.Set("Authorization", "Bearer "+key)
	}
}

func (c *Client) VerifyGrant(token, requestHash string) error {
	if len(c.publicKey) == 0 {
		return fmt.Errorf("legacy grant verification is not configured")
	}
	_, err := grants.Verify(c.publicKey, token, c.audience, requestHash, time.Now().UTC())
	return err
}

func (c *Client) VerifyAgentGrant(token string, operation authzen.EvaluationRequest, bundle policybundle.Bundle, audience string) error {
	if len(c.publicKey) == 0 {
		return fmt.Errorf("AgentGrant verification key is not configured")
	}
	hash, err := agentgrant.HashOperation(operation)
	if err != nil {
		return err
	}
	_, err = agentgrant.Verify(c.publicKey, token, agentgrant.VerifyOptions{Issuer: c.stsIssuer, Audience: audience, RequestHash: hash, PolicyVersion: bundle.Version, PolicyDigest: bundle.RulesDigest, RevocationEpoch: bundle.RevocationEpoch, Now: time.Now().UTC()})
	return err
}

func requestID() string {
	value := make([]byte, 16)
	if _, err := rand.Read(value); err != nil {
		return fmt.Sprintf("fallback-%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(value)
}
