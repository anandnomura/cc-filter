# Architecture and security design

## Components and trust boundaries

```text
Official Claude Code
  managed hooks (admin-controlled)
        |
        | Claude session_id + tool_use_id
        v
BAP Edge / PEP
  - inherited cc-filter hard blocks and redaction
  - random per-session workload_id
  - exact-operation signed-grant cache
  - durable outcome/denial retry spool
        |
        | HTTPS + dedicated BAP bearer credential
        | AuthZEN request / BAP audit extensions
        v
BAP Service / PDP
  - authenticates the edge credential
  - evaluates Cedar permit/forbid/default-deny policies
  - issues 30-second Ed25519 grants
  - transactionally records signed, hash-chained audit events in MySQL
  - records deduplicated missing-policy proposals in MySQL for administrator review
```

BAP Edge lives in the fork root and `cmd/bap-edge`. BAP Service is intentionally
separate under `bap-service/`; it can run on a company network without installing
Claude Code there. Conversely, the prebuilt Windows Edge requires no container
runtime or Go. Shared wire models contain only protocol contracts.

## Identity model used in this version

Claude supplies `session_id`; a hook cannot replace it. At the first hook for a
session, BAP Edge creates a cryptographically random `bapw_...` workload ID and
stores this mapping in the user's BAP state directory. Every tool operation also
has Claude's `tool_use_id`.

BAP Edge reads the dedicated `BAP_EDGE_API_KEY` environment variable and sends it
only in the HTTPS Authorization header. BAP Service compares it in constant time,
maps it to `BAP_EDGE_PRINCIPAL`, and records only `sha256:<fingerprint>`—never the
key. Do not use `ANTHROPIC_API_KEY` for this purpose.

This is an explicit interim workload-identity assumption. A copied bearer key can
impersonate its registered principal. Use one key per user/device, protect its
delivery, and replace it with mTLS or an enterprise identity token later. A
shared key identifies the shared deployment, not an individual human.

## PreToolUse authorization flow

1. The admin-managed hook invokes BAP Edge with Claude's JSON.
2. Edge loads or creates the session workload ID and retries queued audit events.
3. Inherited cc-filter rules run. A local denial remains a denial and is reported
   centrally; if the service is offline it is queued for later delivery.
4. Edge normalizes the tool into an AuthZEN subject/action/resource/context.
5. For an exact cached grant, Edge verifies signature, audience, complete request
   hash, and expiry locally.
6. Before a cached grant can authorize execution, Edge calls
   `POST /bap/v1/audit/grant-consumption`. Service re-verifies the grant and its
   credential binding, durably records the event, and acknowledges it. No
   acknowledgement means no cached authorization.
7. On cache miss or failed acknowledgement, Edge calls the AuthZEN
   `POST /access/v1/evaluation` endpoint.
8. BAP Service evaluates Cedar. Before returning a decision it writes the signed
   audit event. An allowed decision carries a short-lived request-bound grant.
9. Edge independently verifies the grant and returns allow or deny to Claude.

If BAP Service is unavailable, a fresh operation fails closed. Claude can still
reason and answer without tools.

## Post-tool outcome flow

Managed `PostToolUse` and `PostToolUseFailure` hooks report `success` or `failure`
using the same session, workload, and tool-use IDs. Tool output, prompts, errors,
file content, and command plaintext are never included. If delivery fails, Edge
writes a minimal user-local spool record and retries on the next hook. A user can
delete their spool, which is why this interim identity model is not equivalent to
an OS service or protected workload identity; authorization events remain
central and fail-closed.

## Cache semantics

The cache stores only service-signed allow grants—not generic allow decisions.
The cache key hashes the complete AuthZEN request, including session,
workload, and tool-use IDs. Consequently it is normally used for an exact hook
retry, not to authorize a different invocation. Each reuse still contacts BAP
Service for the audit acknowledgement. Caching avoids Cedar re-evaluation but
does not create an audit blind spot.

The current lifetime is 30 seconds. A user may delete or corrupt the cache and
cause a service evaluation, but cannot forge a valid allow grant.

## AuthZEN and BAP endpoints

- `GET /healthz` — unauthenticated liveness only
- `GET /readyz` — unauthenticated MySQL-backed readiness
- `GET /.well-known/authzen-configuration` — AuthZEN discovery
- `POST /access/v1/evaluation` — authenticated AuthZEN evaluation
- `POST /bap/v1/audit/grant-consumption` — authenticated cached-grant receipt
- `POST /bap/v1/audit/outcome` — authenticated post-tool outcome
- `POST /bap/v1/audit/edge-denial` — authenticated local-filter denial

The evaluation request and `decision` response follow AuthZEN Authorization API
1.0. Grant, decision ID, reason code, and proposal fields are documented BAP
extensions inside AuthZEN `context`.

## Policy learning boundary

Only a Cedar default deny caused by `NO_MATCHING_POLICY` creates a sanitized
proposal. Explicit forbids never propose a bypass. The service does not
self-modify or auto-enforce learned policy. An administrator reviews, edits,
tests, and deploys Cedar policy deliberately.
