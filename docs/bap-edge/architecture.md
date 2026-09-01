# Architecture and security design

## Components and trust boundaries

```text
Official Claude Code
  managed hooks (admin-controlled)
        |
        | Claude session_id + tool_use_id
        v
BAP Edge / data plane / local PDP + PEP
  - inherited cc-filter hard blocks and redaction
  - signed local prompt-intent advisories (never permits)
  - random per-session workload_id
  - signed policy bundle verification and rollback state
  - local command classification and Cedar evaluation
  - durable outcome/denial retry spool
        |
        | authenticated HTTPS policy sync and asynchronous audit
        v
BAP Service / control plane
  - authenticates the edge credential
  - validates, versions, signs, distributes, expires, and revokes rules
  - sends update, forced-update, and kill-switch directives
  - transactionally records delivered signed/hash-chained audit events in MySQL
```

BAP Edge lives in `bap-edge/`. BAP Service is intentionally
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

## Policy synchronization and PreToolUse flow

For `UserPromptSubmit`, inherited cc-filter secret detection runs first and
retains its existing exit-code-2 blocking behavior. Prompts that pass are
compared locally with signed intent rules. A match adds manual-only context for
Claude and a privacy-safe rule-ID log event; the prompt is neither rewritten
nor sent to BAP Service. The advisory is merged into any successful parent
cc-filter hook context; a no-match returns the parent's output unchanged. This
advisory flow cannot produce an authorization permit.

1. The admin-managed hook invokes BAP Edge with Claude's JSON.
2. Edge loads or creates its persistent instance ID and session workload ID,
   then retries queued audit events.
3. At SessionStart, when no policy exists, or after `refresh_after_seconds`,
   Edge calls `POST /bap/v1/edge/sync` with its installed version/digest/epoch.
4. Edge verifies the returned Ed25519 envelope, Cedar, schema, expiry, minimum
   protocol, version, digest, and revocation epoch before atomic activation.
5. Inherited cc-filter rules run. A local denial remains a denial and is queued
   for central reporting.
6. Edge normalizes the raw tool contract without creating a command allow.
7. The signed bundle classifies command/network/MCP/delegation eligibility;
   unknown or ambiguous inputs remain unapproved.
8. Edge applies command precedence: explicit forbid, `manual-only`, then normal
   Cedar evaluation. A manual-only match is denied with a safe employee handoff;
   explicit forbids still win and no matching permit denies.
9. Edge durably spools the local decision, attempts asynchronous central audit
   delivery, and returns allow or deny to Claude.

The company hook is installed in administrator-owned managed settings with a
`PreToolUse` matcher covering every Claude tool. Bash, network, MCP, browser,
delegation, and unknown tool paths therefore reach Edge before execution;
unknown paths fail closed. See the [manual execution boundary](manual-execution-boundary.md)
for privileged database, SSH, and platform clients.

If BAP Service is temporarily unavailable, a verified bundle remains usable only
until `max_offline_seconds`. After that lease, missing synchronization fails
closed. The development policy refreshes after 15 minutes and permits at most
one hour offline.

## Post-tool outcome flow

Managed `PostToolUse` and `PostToolUseFailure` hooks report `success` or `failure`
using the same session, workload, and tool-use IDs. Tool output, prompts, errors,
file content, and command plaintext are never included. If delivery fails, Edge
writes a minimal user-local spool record and retries on the next hook. A user can
delete their spool, which is why this interim identity model is not equivalent to
an OS service or protected workload identity; authorization events remain
central and fail-closed.

## Policy state semantics

The local state contains only a signed control-plane bundle, its highest
accepted version/digest/revocation epoch, last successful synchronization, and
queued audit. A lower version or epoch is rollback; a different digest under the
same version is equivocation. Both deny. Deleting policy state forces a new
synchronization and cannot create an allow.

Rules may be approved for up to 30 days, while refresh and offline leases are
much shorter. These are policy lifetimes, not reusable tool grants.

## Client/model compatibility evidence

Model names never grant authority. Exact Claude Code and approved model
combinations are compatibility evidence: Edge captures only privacy-safe hook
schema shapes and local results, then certification regenerates representative
inputs and replays normalization and bundled Cedar. A manifest binds every
fixture hash to the active policy version/digest and requires equivalent
decisions across required model families. Unknown schemas fail certification
and remain default deny at runtime.

## AuthZEN and BAP endpoints

- `GET /healthz` — unauthenticated liveness only
- `GET /readyz` — unauthenticated MySQL-backed readiness
- `GET /.well-known/authzen-configuration` — AuthZEN discovery
- `POST /bap/v1/edge/sync` — authenticated signed policy synchronization
- `POST /bap/v1/audit/edge-decision` — asynchronous local decision ingestion
- `POST /bap/v1/audit/outcome` — authenticated post-tool outcome
- `POST /bap/v1/audit/edge-denial` — authenticated local-filter denial
- `POST /access/v1/evaluation` and grant consumption — legacy migration APIs,
  not used by the local traffic-decision path

The evaluation request and `decision` response follow AuthZEN Authorization API
1.0. Grant, decision ID, reason code, and proposal fields are documented BAP
extensions inside AuthZEN `context`.

## Policy learning boundary

Only a Cedar default deny caused by `NO_MATCHING_POLICY` creates a sanitized
proposal. Explicit forbids never propose a bypass. The service does not
self-modify or auto-enforce learned policy. An administrator reviews, edits,
tests, and deploys Cedar policy deliberately.
