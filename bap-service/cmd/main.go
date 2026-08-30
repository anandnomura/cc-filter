package main

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"cc-filter/bap-service/internal/audit"
	"cc-filter/bap-service/internal/cedaradapter"
	"cc-filter/bap-service/internal/devcert"
	"cc-filter/bap-service/internal/mysqlstore"
	"cc-filter/bap-service/internal/proposals"
	"cc-filter/bap-service/internal/server"
	"cc-filter/internal/grants"
)

func main() {
	address := env("BAP_LISTEN_ADDRESS", ":8080")
	policyPath := env("BAP_POLICY_PATH", "policies/agent-tools.cedar")
	keyDirectory := env("BAP_STATE_DIRECTORY", env("BAP_KEY_DIRECTORY", ".bap/runtime"))
	proposalPath := env("BAP_PROPOSAL_PATH", filepath.Join(keyDirectory, "policy-proposals.jsonl"))
	auditPath := env("BAP_AUDIT_PATH", filepath.Join(keyDirectory, "audit.jsonl"))
	privatePath := env("BAP_GRANT_PRIVATE_KEY_PATH", filepath.Join(keyDirectory, "grant-private.pem"))
	publicPath := env("BAP_GRANT_PUBLIC_KEY_PATH", filepath.Join(keyDirectory, "grant-public.pem"))
	auditPrivatePath := env("BAP_AUDIT_PRIVATE_KEY_PATH", filepath.Join(keyDirectory, "audit-private.pem"))
	auditPublicPath := env("BAP_AUDIT_PUBLIC_KEY_PATH", filepath.Join(keyDirectory, "audit-public.pem"))

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
		log.Printf("certificates initialized; distribute CA %s and grant public key %s", caPath, publicPath)
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
	apiKey := os.Getenv("BAP_EDGE_API_KEY")
	if apiKey == "" {
		log.Fatal("BAP_EDGE_API_KEY is required")
	}
	principal := env("BAP_EDGE_PRINCIPAL", "local-user")
	service := server.New(engine, privateKey, "bap-service-local", "bap-edge", 30*time.Second, proposalStore, auditStore, apiKey, principal)
	log.Printf("BAP Service listening on %s", address)
	certPath := os.Getenv("BAP_TLS_CERT_PATH")
	keyPath := os.Getenv("BAP_TLS_KEY_PATH")
	if (certPath == "") != (keyPath == "") {
		log.Fatal("BAP_TLS_CERT_PATH and BAP_TLS_KEY_PATH must be configured together")
	}
	if certPath != "" {
		log.Printf("TLS is enabled")
		if err := http.ListenAndServeTLS(address, certPath, keyPath, service.Handler()); err != nil {
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
