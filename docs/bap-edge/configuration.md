# Configuration reference

## BAP Edge YAML

The managed Windows installer writes `C:\Program Files\BAP Edge\bap-edge.yaml`:

```yaml
service_url: "https://127.0.0.1:8443"
public_key_path: "C:\\Program Files\\BAP Edge\\grant-public.pem"
ca_bundle_path: "C:\\Program Files\\BAP Edge\\service-ca-bundle.pem"
subject_id: "claude-code-local"
timeout_ms: 3000
cache_directory: ""
state_directory: ""
api_key_env: "BAP_EDGE_API_KEY"
```

| Setting | Meaning |
|---|---|
| `service_url` | BAP Service base URL; network URLs must use HTTPS |
| `public_key_path` | Ed25519 grant verification public key |
| `ca_bundle_path` | Private/company CA bundle; omit when system trust is sufficient |
| `subject_id` | Cedar/AuthZEN agent subject configured by the administrator |
| `timeout_ms` | Per-service-call timeout; defaults to 3000 |
| `cache_directory` | Signed grant cache; empty uses the OS user cache |
| `state_directory` | Session mappings and outcome retry spool; empty uses OS user cache |
| `api_key_env` | Name—not value—of the dedicated credential environment variable |

The secret is deliberately absent from YAML. The installer provisions
`BAP_EDGE_API_KEY` as a machine environment variable. A network secret-management
agent may inject the named variable instead.

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
| `BAP_AUDIT_PRIVATE_KEY_PATH` | under key directory | Audit signing key |
| `BAP_AUDIT_PUBLIC_KEY_PATH` | under key directory | Audit verification key |
| `BAP_AUDIT_PATH` | `audit.jsonl` under key directory | Durable signed event log |
| `BAP_PROPOSAL_PATH` | `policy-proposals.jsonl` | Pending policy suggestions |
| `BAP_EDGE_API_KEY` | required | Dedicated Edge bearer credential |
| `BAP_EDGE_PRINCIPAL` | `local-user` | Registered name for that credential |

`BAP_ALLOW_KEY_GENERATION=true` is development-only. Normal service startup
refuses to invent a missing grant authority. Use the explicit initialization
command or company secret provisioning.

`BAP_KEY_DIRECTORY` remains a compatibility alias; new deployments should use
`BAP_STATE_DIRECTORY`.
