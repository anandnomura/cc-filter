# Production deployment and rollback runbook

This is a promotion runbook, not a single-machine launcher. Native binaries,
Docker images and Podman images use the same security contract. The company
platform team supplies orchestration, PKI, workload identity, secret manager,
database, network controls, audit transport and service supervision.

## Current production-readiness statement

The core Edge, signed policy, AgentGrant, one-use ledger, API PEP and MCP PEP
reference transactions are implemented and tested. Promotion remains blocked
until the deployment supplies every external control below and resolves the
applicable MVP gaps:

- interactive API use requires a managed structured-tool adapter;
- COAZ-MCP Binding 1.0 is not implemented;
- reference PEP-to-STS authentication is distinct bearer workload credentials
  over TLS, not client mTLS;
- the second credential is currently PEP-owned; dynamic downstream workload
  token exchange is not yet implemented;
- central audit export/alerting is external to the reference;
- Linux lacks a one-command full certification runner.

Do not waive a listed gap when it is a stated company production requirement.

## Required deployment inventory

Record these values in the change ticket without recording secret contents:

| Item | Required record |
|---|---|
| Artifact | Immutable digest, version, source commit and SBOM result |
| Policy | Bundle version/digest, approvers and expiry |
| Service | DNS name, port, replicas and readiness endpoint |
| Agent STS | Combined/separate role, issuer and signing-key identifier |
| Edge | Package version, managed-settings source and subject mapping |
| API PEP | Resource URI, external operation, fixed backend and workload identity |
| MCP PEP | Resource URI, managed server name, tool mappings and upstream identity |
| Database | TLS server name, CA, backup/PITR and connection limits |
| PKI | Certificate SANs, issuer, expiry and rotation owner |
| Audit | Sink, retention, chain-verification job and alert owner |
| Rollback | Previous immutable digests and compatible policy/key version |

## Build and artifact promotion

Use a clean source commit and internally approved digest-pinned base images:

```powershell
.\Build-CompanyArtifacts.ps1 `
  -Runtime Docker `
  -Version '1.0.0' `
  -Registry 'registry.company.example/security' `
  -BuildImage 'registry.company.example/build/golang@sha256:APPROVED' `
  -RuntimeImage 'registry.company.example/runtime/debian@sha256:APPROVED'

.\Build-ResourcePEPs.ps1 -Runtime Docker
```

The existing resource-PEP build uses its configured Maven/Java and Go base
images. Mirror and digest-pin those images in the company pipeline before
production promotion. Sign final artifacts/images and record their digests.

For native deployment, build explicit targets and package them with the same
signing/scanning controls:

```powershell
.\Build-BapEdge.ps1 -Runtime Native -Targets All -Version '1.0.0'
.\Build-BapService-Native.ps1 -Target All -Architecture All -Version '1.0.0'
.\Build-ResourcePEPs.ps1 -Runtime Native -Target All -Architecture amd64
```

## Provision external controls

1. Create company DNS names and server certificates containing their exact SANs.
2. Provision three separate signing keys: AgentGrant, policy bundle and audit.
3. Provision TLS MySQL; enable backups/PITR and least-privilege schema access.
4. Create distinct identities for Edge issue, API PEP consume and MCP PEP
   consume. Bind each consume identity to only its exact HTTPS resource URI.
5. Create distinct API-PEP-to-backend and MCP-PEP-to-upstream identities.
6. Restrict each protected resource to its PEP identity and remove direct
   employee/agent routing.
7. Permit only required flows: Edge to Service/STS, PEP to STS, and each PEP to
   its fixed backend. Deny arbitrary PEP egress.
8. Configure central signed-audit ingestion, retention and alerts.
9. Verify time synchronization on every issuer, verifier and PEP host.

Never use `BAP_DEVELOPMENT_TLS=true`, generated `.bap` keys,
`BAP_DATABASE_ALLOW_INSECURE=true`, JSONL grant state, or the in-memory ledger.

## Configure BAP Service and Agent STS

Required production environment/file inputs are detailed in the
[deployment guide](../bap-system/deployment-guide.md#bap-service-and-embedded-agent-sts).
At minimum configure:

```text
BAP_LISTEN_ADDRESS
BAP_TLS_CERT_PATH / BAP_TLS_KEY_PATH
BAP_STATE_DIRECTORY
BAP_POLICY_PATH / BAP_BUNDLE_SOURCE_PATH
BAP_GRANT_PRIVATE_KEY_PATH / BAP_GRANT_PUBLIC_KEY_PATH
BAP_BUNDLE_PRIVATE_KEY_PATH / BAP_BUNDLE_PUBLIC_KEY_PATH
BAP_AUDIT_PRIVATE_KEY_PATH / BAP_AUDIT_PUBLIC_KEY_PATH
BAP_AGENT_STS_EDGE_PRINCIPAL
BAP_AGENT_STS_CONSUMERS_JSON
BAP_DATABASE_DSN_FILE
BAP_DATABASE_TLS_CA_PATH
BAP_DATABASE_TLS_SERVER_NAME
```

Use `BAP_SERVICE_ROLE=combined` by default. Use the separately built
`bap-agent-sts` only when independent scaling/ownership is required. The
STS-only role requires TLS and exposes only operational plus issue/consume
endpoints.

## Configure Edge and resource PEPs

1. Distribute only public verification keys and company CA certificates to
   Edge; keep signing keys in Service/KMS custody.
2. Install BAP hooks through administrator-managed Claude settings.
3. Deploy `managed-mcp.json` with the exact MCP PEP server name used by signed
   policy. Never store bearer secrets in it.
4. Configure the Spring PEP with its fixed route, STS credential, exact
   `BAP_API_PEP_RESOURCE`, verified STS CA, backend URL and backend identity.
5. Configure the MCP PEP with TLS listener, allowed origins, exact schemas,
   STS/upstream CA files, exact argument constraints and its two identities.
6. Keep `allow_development_cleartext_host_gateway=false` or absent.

## Canary verification

Before enabling employee traffic:

1. verify Service `/healthz`, `/readyz`, certificate chain/name and MySQL;
2. verify unauthenticated issue/consume and direct backend calls are rejected;
3. verify Edge syncs the intended bundle version/digest;
4. run unit/integration gates against the exact promoted artifacts;
5. perform the human MCP test from the acceptance guide;
6. if an API tool adapter is deployed, perform the human API test;
7. verify exact operation tampering, wrong audience, stale intent, expiry,
   replay and STS outage all fail closed; also verify missing, malformed,
   query-bearing, and cross-resource indicators return `invalid_target`;
8. verify one issue and one consume audit event correlate to one backend event;
9. confirm no AgentGrant or downstream credential appears in prompts, process
   arguments, application logs or retained test evidence;
10. keep the canary until audit/latency/error alerts remain healthy for the
   company-defined observation window.

## Runtime operations

- Rotate PEP service credentials independently; never share API and MCP PEP
  consumers.
- Rotate TLS before expiry and test full chain/name validation from every
  caller.
- Roll signing keys using an overlap window that distributes new public keys
  before issuance changes.
- Monitor issuance/consumption ratio, replay denials, wrong-audience attempts,
  policy staleness, audit failures, DB conditional-update errors and clock skew.
- Treat failure of policy, STS, ledger, audit or TLS validation as fail closed.

## Rollback

1. Stop new protected-operation issuance or activate the signed kill switch.
2. Preserve audit, Service, Edge and PEP evidence before changing artifacts.
3. Roll PEPs and Service to previously approved immutable digests only when
   their policy schema and public keys remain compatible.
4. Do not lower the signed policy version on Edge state that observed a newer
   version. Publish a new higher-version corrective bundle instead.
5. Do not restore consumed grants to `ISSUED`; they remain consumed evidence.
6. Restore routing only after readiness, policy sync, deny tests and one-use
   acceptance pass on the rollback canary.
7. Reconcile issue/consume/backend audit counts and close the change record.

## Production sign-off

Security, platform, resource owner and operations must sign off the inventory,
external controls, applicable MVP gaps, canary evidence and rollback rehearsal.
Only then is the selected native/container runtime deployable for production.
