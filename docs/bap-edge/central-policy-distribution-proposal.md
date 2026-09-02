# Control-plane policy and Edge traffic-authority design

## Decision

BAP Service is the control plane and the source of truth for rules. BAP Edge is
the endpoint data plane and the source of truth for decisions about traffic it
intercepts. BAP Edge contains the local PDP and PEP: it verifies centrally
signed policy, classifies the raw operation, evaluates Cedar locally, and
returns allow or deny to Claude without a network decision in the hot path.

The distinction is intentional:

- BAP Service owns what rules exist, their version, scope, approval, expiry,
  revocation, signature, and rollout.
- BAP Edge owns the decision for a particular intercepted tool operation, using
  only a valid signed snapshot of those centrally owned rules.
- A local user cannot add an allow rule. Unknown, ambiguous, stale, malformed,
  expired, unsigned, or rolled-back policy denies.

The first implementation slice is present. It includes the signed source file,
separate bundle signing key, authenticated sync API, local bundle store,
rollback/equivocation checks, refresh and offline leases, local Cedar decision,
centrally configured shell registry, kill switch, forced-update directive,
durable local decision spool, and central audit ingestion. The persistent Edge
Agent, proactive background channel, governed admin API/database workflow, and
hardware-bound production identity remain follow-up work.

## Architecture

```text
BAP Service control plane
  - validates and versions rule sources
  - signs immutable policy bundles
  - sets refresh/offline limits and revocation epoch
  - distributes update, force-update, and kill-switch directives
  - receives asynchronously delivered Edge decision/outcome audit
                  |
                  | authenticated TLS policy synchronization
                  | signed policy bundle
                  v
BAP Edge data plane: local PDP + PEP
  - verifies signature, schema, version, digest, epoch, and expiry
  - rejects rollback and same-version rule equivocation
  - parses and classifies the raw operation
  - evaluates bundled Cedar locally
  - enforces allow/deny in the Claude PreToolUse hook
  - durably spools audit before allowing
                  |
                  v
Claude tool execution
```

The legacy central AuthZEN evaluation/grant endpoints were removed. Regression
tests require those routes to return 404, leaving one tool-call authorization
path: signed policy evaluated by BAP Edge. AgentGrant issuance and consumption
are separate resource-access transactions.

## Rule bundle

The administrator-owned source is
`bap-service/policies/edge-policy-source.json`. The Cedar document remains
`bap-service/policies/agent-tools.cedar`. BAP Service validates both, combines
them, calculates a rules digest, and signs the bundle with the dedicated
Ed25519 bundle key.

The bundle includes:

```text
schema_version
version
rules_digest
issued_at / expires_at
refresh_after_seconds
max_offline_seconds
minimum_edge_version
revocation_epoch
force_update / kill_switch
policy_profile
network, MCP, and delegation registries
Cedar policy text
structured command rules with owner and approval
```

Changing any rule requires incrementing `version`. A changed rule set under the
same version is equivocation and is rejected by an Edge that has already seen
that version. Lower versions and revocation epochs are rollback and are also
rejected.

Command rules are structured around executable, optional subcommand, ordered
argument patterns, profile, effect, owner, approval, and validity. A rule with
`eligible-for-permit` does not bypass Cedar; it only makes that command eligible
for normal Cedar evaluation. A matching forbid overrides permits.

## Expiry and disconnected behavior

Three lifetimes have different purposes:

- A rule/bundle approval lifetime can be up to 30 days. Renewal creates a new
  reviewed version rather than silently extending permission.
- `refresh_after_seconds` controls normal contact with the control plane. The
  development source currently uses 15 minutes.
- `max_offline_seconds` bounds disconnected authorization. The development
  source currently uses one hour. Once this lease expires, Edge fails closed
  until it receives and verifies a control-plane response.

There is no 30-day per-tool grant. Each intercepted operation is evaluated
locally against current signed rules. A fresh bundle permits bounded operation
during a temporary BAP Service or network outage. This is now expected behavior,
not a fail-open defect.

No system can push to a powered-off or disconnected endpoint. The offline lease
defines the maximum stale-policy exposure. High-risk operations may later be
configured to require online posture confirmation if a shorter bound is needed.

## Synchronization

The authenticated endpoint is:

```text
POST /bap/v1/edge/sync
```

Edge sends its persistent instance ID, protocol version, installed bundle
version and digest, revocation epoch, and a fresh nonce. Identity comes from the
authenticated channel rather than body claims.

The response directive is one of:

- `CURRENT`: the installed version/digest/epoch matches;
- `UPDATE`: a newer or different signed bundle is available;
- `UPDATE_REQUIRED`: `force_update` is active and the caller is stale; or
- `KILL_SWITCH`: local tool traffic must deny.

The response carries a signed envelope. Edge verifies it before atomically
activating it and records the highest accepted version/digest/epoch. Deleting
local policy state forces synchronization; it never falls back to allow.

The current hook implementation synchronizes at `SessionStart`, when no bundle
exists, and when the refresh interval elapses. A persistent BAP Edge Agent is
required for unsolicited background delivery. That agent should run as a
protected Windows service, maintain an mTLS event stream with polling fallback,
own policy/audit state, and expose a restricted named-pipe API to the hook.

Until the agent exists, "push" means a mandatory directive on the next Edge
communication. The one-hour offline lease is the hard bound in the development
configuration.

## Identity and impersonation

Local development continues to support the dedicated bearer credential. It is
not sufficient for the company threat model because a user process can copy it.

The transport now supports per-device mutual TLS:

- BAP Service sets `BAP_CLIENT_CA_PATH` and requires a verified client
  certificate.
- Edge config sets `client_certificate_path` and `client_key_path`.
- The service derives principal and fingerprint from the verified certificate,
  not from request-body identity fields.

Production still requires enrollment, revocation, short certificate lifetime,
rotation, TPM/CNG non-exportable keys where available, signed binaries,
application control, no local administrator rights, and eventually workload
attestation. A PEM client key readable by the interactive user is transport
authentication, not hardware-backed endpoint identity.

No endpoint design can guarantee enforcement against a local administrator who
can replace Claude, the hook, BAP Edge, trust roots, or the operating system.
Resource-side controls remain necessary for the strongest boundary.

## Local decision and audit

Before returning an allow, Edge writes the decision to its durable local spool.
It then attempts asynchronous delivery to
`POST /bap/v1/audit/edge-decision`. The service records the bundle policy
version, privacy-safe resource summary, endpoint credential fingerprint,
session/workload/tool correlation, result, and trace information in the signed
central audit chain.

A temporary audit/control-plane outage does not block traffic while the signed
bundle lease is fresh. Unsent decisions and outcomes remain queued. The current
user-local spool can be deleted by that user; moving it into the protected Edge
Agent is required for the company pilot assurance target.

## Default deny

Edge denies when:

- it has no valid signed bundle and synchronization fails;
- signature, schema, Cedar, digest, version, epoch, or expiry verification
  fails;
- the maximum offline lease has elapsed;
- the bundle requires a newer Edge protocol;
- force-update cannot install the required version;
- kill switch is active;
- raw tool input is malformed or unsupported;
- shell syntax is ambiguous, chained, redirected, encoded, or unregistered;
- a rule is not yet valid or has expired;
- Cedar has an explicit forbid or no matching permit; or
- the local decision cannot be durably spooled.

The inherited cc-filter remains a non-relaxable early-deny layer. It never
creates an allow.

## Administrative lifecycle target

The source file is the MVP development control. The next control-plane slice
must store immutable versions and lifecycle state in the service database:

```text
draft -> validated -> staged -> active -> superseded/revoked
```

Activation must validate schema and Cedar, run the complete bypass corpus,
require policy-owner approval, sign the immutable version, audit the transition,
then advance the active pointer. Every rule needs owner, approval/ticket, scope,
not-before, expiry, and renewal history. The admin API must be separately
authenticated and authorized from runtime Edge synchronization.

## Test requirements

Automated tests must prove:

- signed bundle verification succeeds and any payload/signature tamper fails;
- expired, lower-version, lower-epoch, and same-version/different-digest bundles
  fail closed;
- refresh and maximum-offline leases behave independently;
- `UPDATE`, `CURRENT`, `UPDATE_REQUIRED`, and `KILL_SWITCH` directives work;
- unauthenticated sync is rejected and verified mTLS identity is authoritative;
- `ls -al` is allowed only because it exists in the central rule source;
- deleting that rule and incrementing the version denies it after update;
- client-supplied `shellApproved=true` cannot authorize an unknown or forbidden
  command;
- unknown commands, destructive Git, chaining, encoding, and unsupported flags
  deny;
- local Cedar permits normal file reads and denies protected/outside resources;
- a fresh bundle continues local decisions through a bounded service outage;
- an expired offline lease denies during an outage;
- a local decision is durably queued before allow and central ingestion is
  idempotent;
- audit never stores plaintext commands or outside-workspace paths; and
- all existing managed-hook, outcome, trace, database, Docker/Podman, and
  approved-model fixture gates remain green.

The focused executable gate is `Test-PolicyRollout.ps1`. Its command cases are
reviewable data in `internal/policybundle/testdata/command-policy-corpus.json`,
and `Test-MVP0.ps1` runs it before the live local-service acceptance suite.
Exact model fixtures contain schemas rather than values, replay through the
normalizer and bundle, require expected approved-model outcomes, and are bound
to the same version/digest in a hashed certification manifest.

## Remaining work before company pilot

1. Persistent protected Edge Agent and proactive update channel.
2. Database-backed rule lifecycle, admin APIs, approvals, staging, and rollout.
3. Per-device enrollment/revocation and hardware-backed mTLS keys.
4. Shell parser and bypass corpus beyond the current strict MVP parser.
5. Protected audit queue with delivery health/SLO and gap detection.
6. Signed release artifacts, application control, and device/workload
   attestation.
