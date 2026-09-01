# BAP System deployment guide

This guide separates the runnable development reference from a production
deployment. Do not promote generated development certificates, JSONL storage,
the in-memory Agent STS ledger, mock resources, or literal shell secrets.

For procedural start, verification, stop and rollback instructions, use the
[runtime operator runbooks](../runbooks/README.md).

## Deployment boundary

The deployed path has five independently owned boundaries:

1. managed Claude Code configuration invokes BAP Edge hooks and registers the
   approved MCP PEPs;
2. BAP Edge verifies centrally signed policy and requests AgentGrants only for
   exact escalated operations;
3. BAP Service distributes policy and, by default, hosts Agent STS on the same
   HTTPS listener;
4. API and MCP PEPs consume a one-use, audience-bound AgentGrant;
5. protected resources accept only a PEP-owned backend identity.

The API gateway and MCP PEP are examples for one Orders API route and one
change-management MCP tool. They are not general open proxies.

## Development deployment

### Native Windows

Prerequisites are Go 1.23.12+, Java 21, Maven 3.9+, and `curl.exe`. From the
repository root:

```powershell
.\Build-Bap.ps1 -Runtime Native
.\Build-ResourcePEPs.ps1 -Runtime Native
.\Test-ResourcePEPs.ps1 -Runtime Native
.\Demo-ResourcePEPs.ps1 -Runtime Native
```

The demo creates isolated state, random credentials, a local CA and certificate,
starts all reference processes, proves both protected-resource paths and their
failure cases, then stops them. Evidence remains under
`.bap\resource-pep-demo\<run-id>\`.

### Docker or Podman

```powershell
.\Test-ResourcePEPs.ps1 -Runtime Docker
.\Demo-ResourcePEPs.ps1 -Runtime Docker -Rebuild

.\Test-ResourcePEPs.ps1 -Runtime Podman
.\Demo-ResourcePEPs.ps1 -Runtime Podman -Rebuild
```

On Windows, a Podman WSL machine must expose published ports to Windows:

```powershell
podman machine inspect --format '{{.UserModeNetworking}}'
podman machine stop
podman machine set --user-mode-networking=true
podman machine start
```

The first command must report `true`. Changing the setting restarts the Podman
VM, so stop other Podman workloads first.

`BAP_DEVELOPMENT_TLS=true` generates a certificate for `localhost`,
`127.0.0.1`, `::1`, and `host.containers.internal`. Incompatible older local
certificates are regenerated. This PKI is for local tests only.

## Production prerequisites and owners

| Control | Required production input | Typical owner |
|---|---|---|
| DNS/routing | Stable HTTPS names for Service/STS, PEPs and backends | Platform/network |
| Server PKI | Company certificates with every actual service DNS SAN | PKI/platform |
| Client identity | Separate Edge issue identity and consume identity per PEP | IAM/security |
| Secrets | Secret-manager injection, never policy/images/prompts/arguments | IAM/platform |
| Signing keys | Separate AgentGrant, bundle and audit keys | Security/KMS |
| One-use state | TLS MySQL with durable state and conditional updates | Database/platform |
| Network policy | Resources accept only PEP traffic; PEP egress is fixed | Network/resource owner |
| Policy | Reviewed resource, audience, intent and parameter constraints | Security/resource owner |
| Time | Enterprise time synchronization on Edge, Service and PEPs | Platform |
| Audit | Central append-only collection, retention and alerting | SOC/compliance |

## Production configuration

### BAP Service and embedded Agent STS

Use the combined role unless independent scaling or ownership justifies the
optional STS-only executable. One executable safely serves all endpoints on one
HTTPS port.

| Variable | Production meaning |
|---|---|
| `BAP_LISTEN_ADDRESS` | Explicit private listener and port |
| `BAP_TLS_CERT_PATH`, `BAP_TLS_KEY_PATH` | Company server certificate/key |
| `BAP_STATE_DIRECTORY` | Protected persistent state directory |
| `BAP_POLICY_PATH`, `BAP_BUNDLE_SOURCE_PATH` | Reviewed Cedar policy/bundle source |
| `BAP_GRANT_PRIVATE_KEY_PATH`, `BAP_GRANT_PUBLIC_KEY_PATH` | AgentGrant signing keypair |
| `BAP_BUNDLE_PRIVATE_KEY_PATH`, `BAP_BUNDLE_PUBLIC_KEY_PATH` | Policy signing keypair |
| `BAP_AUDIT_PRIVATE_KEY_PATH`, `BAP_AUDIT_PUBLIC_KEY_PATH` | Audit signing keypair |
| `BAP_EDGE_PRINCIPAL` | Authenticated policy-sync principal |
| `BAP_AGENT_STS_EDGE_PRINCIPAL` | Identity allowed to issue grants |
| `BAP_AGENT_STS_CONSUMERS_JSON` | PEP principals, secret env names and audiences |
| `BAP_DATABASE_DSN_FILE` | File containing MySQL DSN |
| `BAP_DATABASE_TLS_CA_PATH`, `BAP_DATABASE_TLS_SERVER_NAME` | DB CA and verified name |

Set `BAP_ALLOW_KEY_GENERATION=false`, `BAP_DEVELOPMENT_TLS=false`, and
`BAP_DATABASE_ALLOW_INSECURE=false`. Production must not use the JSONL grant
store or in-memory one-use ledger. Multiple replicas must share transactional
MySQL and consistent keys/policy.

Example consumers (the values name injected secret variables, not secrets):

```json
[
  {"principal":"orders-api-pep","api_key_env":"BAP_API_PEP_STS_API_KEY","audiences":["https://api.staging.company.example/orders/deploy"]},
  {"principal":"change-mcp-pep","api_key_env":"BAP_MCP_PEP_STS_API_KEY","audiences":["https://bap-mcp-pep.company.example/mcp"]}
]
```

The reference PEPs authenticate to STS with distinct bearer service credentials
over verified TLS. Service can globally require client mTLS using
`BAP_CLIENT_CA_PATH`, and Edge supports a client certificate/key, but the two
reference PEP clients do not yet present client certificates. Do not enable
global mTLS until those clients are extended; use rotated secret-manager
credentials plus verified TLS for this MVP.

### BAP Edge

Distribute protected Edge configuration like:

```yaml
service_url: "https://bap-service.company.example"
agent_sts_url: "https://bap-service.company.example"
public_key_path: "C:\\ProgramData\\BAP\\grant-public.pem"
bundle_public_key_path: "C:\\ProgramData\\BAP\\bundle-public.pem"
ca_bundle_path: "C:\\ProgramData\\BAP\\company-ca.pem"
api_key_env: "BAP_EDGE_API_KEY"
agent_sts_api_key_env: "BAP_AGENT_STS_EDGE_API_KEY"
subject_id: "claude-code-company"
timeout_ms: 3000
state_directory: "C:\\ProgramData\\BAP\\edge-state"
```

For Edge mTLS add both `client_certificate_path` and `client_key_path`. Install
hooks through managed settings; never rely on project-local hooks in production.

### Spring Cloud API PEP

Inject `BAP_AGENT_STS_URL`, `BAP_AGENT_STS_CA_PATH`,
`BAP_API_PEP_RESOURCE`,
`BAP_API_PEP_STS_API_KEY`, `BAP_ORDERS_BACKEND_URL`, and
`BAP_ORDERS_BACKEND_API_KEY`. Replace the fixed example route, external URL,
method, backend path and identity with reviewed resource-specific values before
building. The backend must reject direct traffic and require the gateway-owned
identity plus an idempotency key.

The repository does not yet include the company structured-tool adapter that
exposes `mcp__bap_gateway__execute` to Claude. The harness drives that exact
tool envelope directly. Real Claude API-resource rollout requires an approved
adapter connected to the Spring gateway; raw `curl` is not equivalent.

### MCP PEP

Copy `bap-mcp-pep/mcp-pep.example.json` into protected configuration. Set its
exact HTTPS `resource`, HTTPS
URLs, STS/upstream CA paths, listener certificate/key, allowed origins, and
reviewed public/upstream tool mappings. Keep
`allow_development_cleartext_host_gateway` absent or `false` in production.

Deploy the MCP endpoint using administrator-owned
`C:\Program Files\ClaudeCode\managed-mcp.json`:

```json
{
  "mcpServers": {
    "bap_mcp_pep": {
      "type": "http",
      "url": "https://bap-mcp-pep.company.example/mcp"
    }
  }
}
```

Do not place bearer secrets in this readable file. Use company OAuth, per-user
headers or a headers helper when client authentication is added.

## Ordered production rollout

1. Build signed/versioned artifacts from a clean commit and scan SBOMs.
2. Provision DNS, network policy, PKI, secret identities, MySQL TLS and storage.
3. Load signing keys and reviewed policy; verify public-key distribution.
4. Deploy Service/STS privately and verify readiness, TLS, DB and audit append.
5. Deploy each PEP with a distinct STS consumer/audience and backend identity.
6. Restrict protected resources to PEP identities and remove direct routes.
7. Deploy managed MCP registration and managed hooks to a canary group.
8. Run human and automated acceptance, including dependency outages.
9. Verify central audit ingestion and alerts before widening rollout.
10. Rehearse certificate, credential and signing-key rotation plus rollback.

Fail closed if policy, STS, ledger, audit, TLS validation or backend is
unavailable. Never convert a failed consume into a bypass or token reuse.

## Known MVP deployment gaps

- COAZ-MCP Binding 1.0 is not implemented; see the acceptance guide.
- the API example needs a structured-tool adapter for actual Claude use;
- reference PEP-to-STS identity is TLS plus distinct bearer credentials, not
  client mTLS;
- production audit export/dashboards are deployment integrations;
- Linux lacks a one-command PowerShell-equivalent full runner.
