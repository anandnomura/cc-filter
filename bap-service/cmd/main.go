package main

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"bap-system/bap-service/internal/audit"
	"bap-system/bap-service/internal/cedaradapter"
	"bap-system/bap-service/internal/devcert"
	"bap-system/bap-service/internal/mysqlstore"
	"bap-system/bap-service/internal/proposals"
	"bap-system/bap-service/internal/server"
	"bap-system/internal/grants"
	"bap-system/internal/policybundle"
)

var (
	version     = "dev"
	defaultRole = "combined"
)

func main() {
	role := env("BAP_SERVICE_ROLE", defaultRole)
	if role != "combined" && role != "agent-sts" {
		log.Fatalf("BAP_SERVICE_ROLE must be combined or agent-sts, got %q", role)
	}
	log.Printf("starting BAP Service version=%s role=%s", version, role)
	address := env("BAP_LISTEN_ADDRESS", ":8080")
	policyPath := env("BAP_POLICY_PATH", "policies/agent-tools.cedar")
	keyDirectory := env("BAP_STATE_DIRECTORY", env("BAP_KEY_DIRECTORY", ".bap/runtime"))
	proposalPath := env("BAP_PROPOSAL_PATH", filepath.Join(keyDirectory, "policy-proposals.jsonl"))
	auditPath := env("BAP_AUDIT_PATH", filepath.Join(keyDirectory, "audit.jsonl"))
	privatePath := env("BAP_GRANT_PRIVATE_KEY_PATH", filepath.Join(keyDirectory, "grant-private.pem"))
	publicPath := env("BAP_GRANT_PUBLIC_KEY_PATH", filepath.Join(keyDirectory, "grant-public.pem"))
	auditPrivatePath := env("BAP_AUDIT_PRIVATE_KEY_PATH", filepath.Join(keyDirectory, "audit-private.pem"))
	auditPublicPath := env("BAP_AUDIT_PUBLIC_KEY_PATH", filepath.Join(keyDirectory, "audit-public.pem"))
	bundlePrivatePath := env("BAP_BUNDLE_PRIVATE_KEY_PATH", filepath.Join(keyDirectory, "bundle-private.pem"))
	bundlePublicPath := env("BAP_BUNDLE_PUBLIC_KEY_PATH", filepath.Join(keyDirectory, "bundle-public.pem"))
	bundleSourcePath := env("BAP_BUNDLE_SOURCE_PATH", "policies/edge-policy-source.json")

	if err := os.MkdirAll(keyDirectory, 0700); err != nil {
		log.Fatal(err)
	}
	if len(os.Args) > 1 && os.Args[1] == "initialize-certificates" {
		_, _, caPath, err := devcert.Ensure(keyDirectory)
		if err != nil {
			log.Fatalf("initialize local TLS certificates: %v", err)
		}
		if _, err := os.Stat(privatePath); os.IsNotExist(err) {
			if err := grants.GenerateKeyPair(privatePath, publicPath); err != nil {
				log.Fatalf("initialize grant signing key: %v", err)
			}
		}
		if _, err := os.Stat(auditPrivatePath); os.IsNotExist(err) {
			if err := grants.GenerateKeyPair(auditPrivatePath, auditPublicPath); err != nil {
				log.Fatalf("initialize audit signing key: %v", err)
			}
		}
		if _, err := os.Stat(bundlePrivatePath); os.IsNotExist(err) {
			if err := grants.GenerateKeyPair(bundlePrivatePath, bundlePublicPath); err != nil {
				log.Fatalf("initialize policy bundle signing key: %v", err)
			}
		}
		log.Printf("certificates initialized; distribute CA %s, bundle public key %s, and AgentGrant public key %s", caPath, bundlePublicPath, publicPath)
		return
	}
	if env("BAP_DEVELOPMENT_TLS", "false") == "true" {
		certPath, keyPath, caPath, err := devcert.Ensure(keyDirectory)
		if err != nil {
			log.Fatalf("generate local development TLS certificate: %v", err)
		}
		_ = os.Setenv("BAP_TLS_CERT_PATH", certPath)
		_ = os.Setenv("BAP_TLS_KEY_PATH", keyPath)
		log.Printf("local development TLS CA: %s", caPath)
	}
	if _, err := os.Stat(privatePath); os.IsNotExist(err) {
		if env("BAP_ALLOW_KEY_GENERATION", "false") != "true" {
			log.Fatal("grant signing key is missing; initialize it explicitly rather than generating authority at service startup")
		}
		if err := grants.GenerateKeyPair(privatePath, publicPath); err != nil {
			log.Fatalf("generate explicitly enabled development grant signing key: %v", err)
		}
		log.Printf("generated development grant signing key in %s", keyDirectory)
	}
	privateKey, err := grants.LoadPrivateKey(privatePath)
	if err != nil {
		log.Fatalf("load grant signing key: %v", err)
	}
	auditPrivateKey, err := grants.LoadPrivateKey(auditPrivatePath)
	if err != nil {
		log.Fatalf("load audit signing key: %v; run initialize-certificates first", err)
	}
	var bundlePrivateKey ed25519.PrivateKey
	if role != "agent-sts" {
		bundlePrivateKey, err = grants.LoadPrivateKey(bundlePrivatePath)
		if err != nil {
			log.Fatalf("load policy bundle signing key: %v; run initialize-certificates first", err)
		}
	}
	var auditStore server.AuditStore
	var proposalStore server.ProposalStore
	var databaseStore *mysqlstore.Store
	if databaseDSN := envOrFile("BAP_DATABASE_DSN", "BAP_DATABASE_DSN_FILE"); databaseDSN != "" {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		databaseStore, err = mysqlstore.Open(ctx, mysqlstore.Config{
			DSN: databaseDSN, CABundlePath: os.Getenv("BAP_DATABASE_TLS_CA_PATH"),
			TLSServerName:         os.Getenv("BAP_DATABASE_TLS_SERVER_NAME"),
			AllowInsecure:         env("BAP_DATABASE_ALLOW_INSECURE", "false") == "true",
			MaxOpenConnections:    envInt("BAP_DATABASE_MAX_OPEN_CONNECTIONS", 20),
			MaxIdleConnections:    envInt("BAP_DATABASE_MAX_IDLE_CONNECTIONS", 10),
			ConnectionMaxLifetime: time.Duration(envInt("BAP_DATABASE_CONNECTION_MAX_LIFETIME_SECONDS", 300)) * time.Second,
		}, auditPrivateKey)
		cancel()
		if err != nil {
			log.Fatalf("initialize MySQL storage: %v", err)
		}
		defer databaseStore.Close()
		auditStore, proposalStore = databaseStore, databaseStore
		log.Printf("MySQL storage initialized")
	} else {
		log.Printf("WARNING: BAP_DATABASE_DSN is unset; using development-only JSONL storage")
		fileAuditStore := audit.New(auditPath, auditPrivateKey)
		if err := fileAuditStore.Initialize(); err != nil {
			log.Fatalf("audit chain failed startup verification: %v", err)
		}
		auditStore, proposalStore = fileAuditStore, proposals.New(proposalPath)
	}

	if len(os.Args) > 2 && os.Args[1] == "proposals" && os.Args[2] == "list" {
		var summaries []proposals.Summary
		if databaseStore != nil {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			summaries, err = databaseStore.Proposals(ctx)
			cancel()
		} else {
			summaries, err = proposals.Summarize(proposalPath)
		}
		if err != nil {
			log.Fatal(err)
		}
		encoder := json.NewEncoder(os.Stdout)
		encoder.SetIndent("", "  ")
		if err := encoder.Encode(summaries); err != nil {
			log.Fatal(err)
		}
		return
	}
	if len(os.Args) > 2 && os.Args[1] == "audit" && (os.Args[2] == "list" || os.Args[2] == "verify") {
		var events []audit.Event
		if databaseStore != nil {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			events, err = databaseStore.Events(ctx)
			cancel()
		} else {
			events, err = audit.ReadAndVerify(auditPath, auditPrivateKey.Public().(ed25519.PublicKey))
		}
		if err != nil {
			log.Fatalf("audit verification failed: %v", err)
		}
		if os.Args[2] == "verify" {
			log.Printf("audit chain verified: %d signed events", len(events))
			return
		}
		encoder := json.NewEncoder(os.Stdout)
		encoder.SetIndent("", "  ")
		if err := encoder.Encode(events); err != nil {
			log.Fatal(err)
		}
		return
	}
	engine, err := cedaradapter.New(policyPath)
	if err != nil {
		log.Fatal(err)
	}
	activeBundlePath := env("BAP_ACTIVE_POLICY_BUNDLE_PATH", filepath.Join(keyDirectory, "active-policy-bundle.json"))
	var bundle policybundle.Bundle
	var envelope policybundle.Envelope
	if role == "agent-sts" {
		envelopeData, readErr := os.ReadFile(activeBundlePath)
		if readErr != nil {
			log.Fatalf("load signed active policy bundle for Agent STS: %v", readErr)
		}
		if decodeErr := json.Unmarshal(envelopeData, &envelope); decodeErr != nil {
			log.Fatalf("decode signed active policy bundle for Agent STS: %v", decodeErr)
		}
		bundlePublicKey, loadErr := grants.LoadPublicKey(bundlePublicPath)
		if loadErr != nil {
			log.Fatalf("load policy bundle verification key: %v", loadErr)
		}
		bundle, err = policybundle.Verify(bundlePublicKey, envelope, time.Now().UTC())
		if err != nil {
			log.Fatalf("verify signed active policy bundle for Agent STS: %v", err)
		}
	} else {
		bundleSourceData, readErr := os.ReadFile(bundleSourcePath)
		if readErr != nil {
			log.Fatalf("read policy bundle source: %v", readErr)
		}
		bundleSource, loadErr := policybundle.LoadSource(bundleSourceData)
		if loadErr != nil {
			log.Fatal(loadErr)
		}
		cedarPolicy, readErr := os.ReadFile(policyPath)
		if readErr != nil {
			log.Fatal(readErr)
		}
		bundle, envelope, err = policybundle.Activate(bundleSource, cedarPolicy, bundlePrivateKey, "bap-bundle-local", activeBundlePath, time.Now().UTC())
		if err != nil {
			log.Fatal(err)
		}
	}
	apiKey := os.Getenv("BAP_EDGE_API_KEY")
	clientCAPath := os.Getenv("BAP_CLIENT_CA_PATH")
	if role == "combined" && apiKey == "" && clientCAPath == "" {
		log.Fatal("BAP_EDGE_API_KEY is required unless mutual TLS client authentication is configured")
	}
	principal := env("BAP_EDGE_PRINCIPAL", "local-user")
	service := server.New(engine, privateKey, env("BAP_AGENT_STS_ISSUER", "bap-agent-sts-local"), "bap-edge", 30*time.Second, proposalStore, auditStore, apiKey, principal)
	if err := service.SetRole(role); err != nil {
		log.Fatal(err)
	}
	stsEdgeKey, stsGatewayKey := os.Getenv("BAP_AGENT_STS_EDGE_API_KEY"), os.Getenv("BAP_AGENT_STS_GATEWAY_API_KEY")
	stsEdgePrincipal, stsGatewayPrincipal := os.Getenv("BAP_AGENT_STS_EDGE_PRINCIPAL"), os.Getenv("BAP_AGENT_STS_GATEWAY_PRINCIPAL")
	configureSeparateSTSAuth := role == "agent-sts" || stsEdgeKey != "" || stsGatewayKey != "" || stsEdgePrincipal != "" || stsGatewayPrincipal != ""
	if configureSeparateSTSAuth {
		if stsEdgePrincipal == "" || stsGatewayPrincipal == "" {
			log.Fatal("BAP_AGENT_STS_EDGE_PRINCIPAL and BAP_AGENT_STS_GATEWAY_PRINCIPAL are required for separate STS authentication")
		}
		if clientCAPath == "" && (stsEdgeKey == "" || stsGatewayKey == "") {
			log.Fatal("separate Agent STS requires both client API keys unless mutual TLS is configured")
		}
		if err := service.SetAgentSTSClients(stsEdgeKey, stsEdgePrincipal, stsGatewayKey, stsGatewayPrincipal); err != nil {
			log.Fatal(err)
		}
		if consumersJSON := os.Getenv("BAP_AGENT_STS_CONSUMERS_JSON"); consumersJSON != "" {
			var configured []struct {
				Principal string   `json:"principal"`
				APIKeyEnv string   `json:"api_key_env"`
				Audiences []string `json:"audiences"`
			}
			if err := json.Unmarshal([]byte(consumersJSON), &configured); err != nil {
				log.Fatalf("decode BAP_AGENT_STS_CONSUMERS_JSON: %v", err)
			}
			consumers := make([]server.AgentSTSConsumer, 0, len(configured))
			for _, entry := range configured {
				key := ""
				if entry.APIKeyEnv != "" {
					key = os.Getenv(entry.APIKeyEnv)
				}
				if clientCAPath == "" && key == "" {
					log.Fatalf("Agent STS consumer %q API key environment variable is empty", entry.Principal)
				}
				consumers = append(consumers, server.AgentSTSConsumer{APIKey: key, Principal: entry.Principal, Audiences: entry.Audiences})
			}
			if err := service.SetAgentSTSConsumers(consumers); err != nil {
				log.Fatal(err)
			}
		}
	}
	if databaseStore != nil {
		if err := service.SetAgentSTSLedger(databaseStore); err != nil {
			log.Fatal(err)
		}
		log.Printf("Agent STS uses the transactional MySQL one-use ledger")
	} else {
		log.Printf("WARNING: Agent STS uses an in-memory one-use ledger; this is local development only")
	}
	service.SetPolicyBundle(bundle, envelope)
	log.Printf("BAP Service listening on %s", address)
	certPath := os.Getenv("BAP_TLS_CERT_PATH")
	keyPath := os.Getenv("BAP_TLS_KEY_PATH")
	if (certPath == "") != (keyPath == "") {
		log.Fatal("BAP_TLS_CERT_PATH and BAP_TLS_KEY_PATH must be configured together")
	}
	if role == "agent-sts" && certPath == "" {
		log.Fatal("the separate Agent STS role requires TLS; configure BAP_TLS_CERT_PATH and BAP_TLS_KEY_PATH")
	}
	if certPath != "" {
		log.Printf("TLS is enabled")
		minimumTLS := uint16(tls.VersionTLS12)
		if role == "agent-sts" {
			minimumTLS = tls.VersionTLS13
		}
		tlsConfig := &tls.Config{MinVersion: minimumTLS}
		if clientCAPath := os.Getenv("BAP_CLIENT_CA_PATH"); clientCAPath != "" {
			caPEM, err := os.ReadFile(clientCAPath)
			if err != nil {
				log.Fatalf("read BAP client CA: %v", err)
			}
			clientCAs := x509.NewCertPool()
			if !clientCAs.AppendCertsFromPEM(caPEM) {
				log.Fatal("BAP client CA contains no certificates")
			}
			tlsConfig.ClientCAs = clientCAs
			tlsConfig.ClientAuth = tls.RequireAndVerifyClientCert
			log.Printf("mutual TLS client authentication is required")
		}
		httpServer := &http.Server{Addr: address, Handler: service.Handler(), TLSConfig: tlsConfig, ReadHeaderTimeout: 10 * time.Second}
		if err := httpServer.ListenAndServeTLS(certPath, keyPath); err != nil {
			log.Fatal(err)
		}
		return
	}
	if err := http.ListenAndServe(address, service.Handler()); err != nil {
		log.Fatal(err)
	}
}

func env(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}

func envInt(name string, fallback int) int {
	value := os.Getenv(name)
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed < 0 {
		log.Fatalf("%s must be a non-negative integer", name)
	}
	return parsed
}

func envOrFile(name, fileName string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	path := os.Getenv(fileName)
	if path == "" {
		return ""
	}
	data, err := os.ReadFile(path)
	if err != nil {
		log.Fatalf("read %s: %v", fileName, err)
	}
	return string(bytes.TrimSpace(data))
}
