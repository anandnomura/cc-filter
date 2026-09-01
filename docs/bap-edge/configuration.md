# Configuration reference

## BAP Edge YAML

Policy profiles and command/network/MCP/delegation allow registries are not
endpoint configuration. Unknown YAML fields are rejected; these values arrive
only in a verified BAP Service policy bundle.

Fixture capture is disabled unless `Capture-ClaudeFixtures.ps1` sets all five
process-scoped variables below. They are evidence metadata, not authority, and
are never configured machine-wide:

| Variable | Purpose |
|---|---|
| `BAP_FIXTURE_CAPTURE_DIRECTORY` | Opt-in destination for privacy-safe fixtures |
| `BAP_FIXTURE_SCENARIO` | Reviewed stable scenario identifier |
| `BAP_FIXTURE_MODEL` | Exact model label under certification |
| `BAP_FIXTURE_CLAUDE_VERSION` | Exact `claude --version` output |
| `BAP_FIXTURE_EXPECTED_DECISION` | Reviewed `allow` or `deny` expectation |

The managed Windows installer writes `C:\Program Files\BAP Edge\bap-edge.yaml`:

```yaml
service_url: "https://127.0.0.1:8443"
agent_sts_url: "https://127.0.0.1:8443"
agent_sts_issuer: "bap-agent-sts-local"
public_key_path: "C:\\Program Files\\BAP Edge\\grant-public.pem"
bundle_public_key_path: "C:\\Program Files\\BAP Edge\\bundle-public.pem"
ca_bundle_path: "C:\\Program Files\\BAP Edge\\service-ca-bundle.pem"
client_certificate_path: ""
client_key_path: ""
subject_id: "claude-code-local"
timeout_ms: 3000
state_directory: ""
api_key_env: "BAP_EDGE_API_KEY"
agent_sts_api_key_env: "BAP_EDGE_API_KEY"
```

| Setting | Meaning |
|---|---|
| `service_url` | BAP Service base URL; network URLs must use HTTPS |
| `agent_sts_url` | Separate Agent STS URL; defaults to `service_url` for combined/local deployment |
| `agent_sts_issuer` | Exact expected signed-token issuer; must match Service `BAP_AGENT_STS_ISSUER` |
| `public_key_path` | Agent STS Ed25519 public key; required when signed policy can return `AGENT_GRANT_REQUIRED` |
| `bundle_public_key_path` | Ed25519 policy-bundle verification public key |
| `ca_bundle_path` | Private/company CA bundle; omit when system trust is sufficient |
| `client_certificate_path`, `client_key_path` | Optional per-device mTLS identity; configure both together |
| `subject_id` | Cedar/AuthZEN agent subject configured by the administrator |
| `timeout_ms` | Per-service-call timeout; defaults to 3000 |
| `state_directory` | Signed bundle/rollback state, synchronization lease, instance/session mappings, audit retry spool, and privacy-safe logs; empty uses OS user cache |
| `agent_sts_api_key_env` | Edge-to-STS issue credential variable; defaults to `api_key_env` only for combined/local deployment |
| `api_key_env` | Name—not value—of the dedicated credential environment variable |

The secret is deliberately absent from YAML. The installer provisions
`BAP_EDGE_API_KEY` as a machine environment variable. A network secret-management
agent may inject the named variable instead.

Policy profile and command/network/MCP/delegation registries no longer belong in
endpoint YAML. Administrators modify the control-plane source:

```text
bap-service/policies/edge-policy-source.json
```

Changing rule content requires incrementing its `version`. Reusing a version
with different content is rejected as equivocation.

Command rule `effect` accepts `eligible-for-permit`, `manual-only`, or `forbid`.
`manual-only` is for direct privileged client executables that Claude may help
prepare but must not execute. It returns `MANUAL_EXECUTION_REQUIRED`; a matching
`forbid` always takes precedence. Add native and `.exe` names explicitly when a
client must be covered on both Linux/macOS and Windows.

`prompt_rules` are signed and centrally distributed. Every regular expression
in a rule's `patterns` array must match before the rule fires. Supported effects
are `manual-only-advisory` and `agent-grant-intent`. A match adds guidance or
records hashed short-lived intent evidence in the `UserPromptSubmit` hook; it
never returns an allow decision and never weakens
`PreToolUse`. Keep the patterns broad enough to recognize normal phrasing but
require both an operation signal and a resource/tool signal to avoid warning on
purely explanatory questions. Prompt text is classified locally and is not
included in Edge audit events.

Example:

```json
{
  "id": "prompt.manual.database",
  "effect": "manual-only-advisory",
  "patterns": [
    "(?i)\\b(connect|run|execute|query|reindex|alter)\\b",
    "(?i)\\b(mysql|postgres(?:ql)?|sql[ -]?server|oracle|database|db)\\b"
  ],
  "profiles": ["standard-developer", "read-only"],
  "owner": "security",
  "approval": "manual-access-boundary-v2"
}
```

## BAP Service environment

| Variable | Default | Purpose |
|---|---|---|
| `BAP_LISTEN_ADDRESS` | `:8080` (image sets `:8443`) | Listener |
| `BAP_POLICY_PATH` | `policies/agent-tools.cedar` | Cedar policy |
| `BAP_STATE_DIRECTORY` | `.bap/runtime` (image sets `/var/lib/bap`) | Development key/state root |
| `BAP_TLS_CERT_PATH`, `BAP_TLS_KEY_PATH` | empty | Production TLS pair |
| `BAP_DEVELOPMENT_TLS` | `false` | Generate/use local development TLS only |
| `BAP_GRANT_PRIVATE_KEY_PATH` | under key directory | Grant signing key |
| `BAP_GRANT_PUBLIC_KEY_PATH` | under key directory | Grant public key |
| `BAP_BUNDLE_PRIVATE_KEY_PATH` | under key directory | Dedicated policy-bundle signing key |
| `BAP_BUNDLE_PUBLIC_KEY_PATH` | under key directory | Policy-bundle public key distributed to Edge |
| `BAP_BUNDLE_SOURCE_PATH` | `policies/edge-policy-source.json` | Versioned control-plane rule source |
| `BAP_CLIENT_CA_PATH` | empty | When set, require Edge mTLS certificates chaining to this CA |
| `BAP_AUDIT_PRIVATE_KEY_PATH` | under key directory | Audit signing key |
| `BAP_AUDIT_PUBLIC_KEY_PATH` | under key directory | Audit verification key |
| `BAP_DATABASE_DSN` | empty | MySQL Go-driver DSN; use only when secret injection is protected |
| `BAP_DATABASE_DSN_FILE` | empty | Preferred path to a mounted MySQL DSN secret |
| `BAP_DATABASE_TLS_CA_PATH` | empty | Optional company MySQL CA bundle |
| `BAP_DATABASE_TLS_SERVER_NAME` | DSN host | Expected MySQL certificate DNS name |
| `BAP_DATABASE_ALLOW_INSECURE` | `false` | Local development only; permits non-TLS MySQL |
| `BAP_DATABASE_MAX_OPEN_CONNECTIONS` | `20` | MySQL pool open-connection limit |
| `BAP_DATABASE_MAX_IDLE_CONNECTIONS` | `10` | MySQL pool idle-connection limit |
| `BAP_DATABASE_CONNECTION_MAX_LIFETIME_SECONDS` | `300` | MySQL connection lifetime |
| `BAP_AUDIT_PATH` | `audit.jsonl` under key directory | Development fallback only when no MySQL DSN is set |
| `BAP_PROPOSAL_PATH` | `policy-proposals.jsonl` | Development fallback only when no MySQL DSN is set |
| `BAP_EDGE_API_KEY` | required unless mTLS is configured | Dedicated local-development Edge bearer credential |
| `BAP_EDGE_PRINCIPAL` | `local-user` | Registered name for that credential |

`BAP_ALLOW_KEY_GENERATION=true` is development-only. Normal service startup
refuses to invent a missing signing authority. Use the explicit initialization
command or company secret provisioning.

Network MySQL requires verified TLS. The service rejects a DSN without TLS
unless `BAP_DATABASE_ALLOW_INSECURE=true`; the local launcher is the only normal
place that sets this override. See [MySQL storage and enterprise
configuration](storage.md).

`BAP_KEY_DIRECTORY` remains a compatibility alias; new deployments should use
`BAP_STATE_DIRECTORY`.
