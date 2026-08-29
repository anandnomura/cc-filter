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

	"cc-filter/internal/auditwire"
	"cc-filter/internal/authzen"
	"cc-filter/internal/grants"
)

type Client struct {
	baseURL   string
	audience  string
	publicKey ed25519.PublicKey
	apiKey    string
	http      *http.Client
}

func NewClient(config Config) (*Client, error) {
	if config.APIKey() == "" {
		return nil, fmt.Errorf("required BAP credential environment variable %s is empty", config.APIKeyEnv)
	}
	publicKey, err := grants.LoadPublicKey(config.PublicKeyPath)
	if err != nil {
		return nil, fmt.Errorf("load BAP grant verification key: %w", err)
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
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
		transport.TLSClientConfig = &tls.Config{MinVersion: tls.VersionTLS12, RootCAs: roots}
	}
	return &Client{
		baseURL: strings.TrimRight(config.ServiceURL, "/"), audience: "bap-edge", publicKey: publicKey, apiKey: config.APIKey(),
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

func (c *Client) Evaluate(ctx context.Context, request authzen.EvaluationRequest) (authzen.Decision, error) {
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

func (c *Client) post(ctx context.Context, path string, value any) error {
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

func (c *Client) AuditGrantConsumption(ctx context.Context, request authzen.EvaluationRequest, grant string) error {
	return c.post(ctx, "/bap/v1/audit/grant-consumption", auditwire.GrantConsumption{Request: request, Grant: grant})
}

func (c *Client) ReportOutcome(ctx context.Context, outcome auditwire.Outcome) error {
	return c.post(ctx, "/bap/v1/audit/outcome", outcome)
}

func (c *Client) ReportEdgeDenial(ctx context.Context, denial auditwire.EdgeDenial) error {
	return c.post(ctx, "/bap/v1/audit/edge-denial", denial)
}

func (c *Client) authorize(request *http.Request) {
	request.Header.Set("Authorization", "Bearer "+c.apiKey)
}

func (c *Client) VerifyGrant(token, requestHash string) error {
	_, err := grants.Verify(c.publicKey, token, c.audience, requestHash, time.Now().UTC())
	return err
}

func requestID() string {
	value := make([]byte, 16)
	if _, err := rand.Read(value); err != nil {
		return fmt.Sprintf("fallback-%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(value)
}
