# BAP Edge MVP roadmap and readiness ledger

This document is the working product ledger. It distinguishes what is complete,
what is implemented with an intentional interim deviation, what a solid MVP
requires, and what remains for enterprise production. Update it in every change
that materially alters security, identity, policy, storage, audit, deployment,
or operations.

Use the [MVP technical architecture](mvp-technical-architecture.md) for the
component diagram, trust boundaries, technology choices, cc-filter rationale,
managed-settings enforcement, AuthZEN contract, identity model, and API-key
design.

## Readiness verdict

The current release is a strong security-oriented reference implementation and
local/distributed demonstrator. It is **not enterprise production ready**.

The next achievable release is a **bounded internal-pilot MVP** for approved
Claude Code clients on managed Windows endpoints. That MVP can be considered
production-like only inside the scope below; it must not be represented as a
general endpoint DLP product, a resource gateway, or a highly available
enterprise authorization service.

## How to use this ledger

This is the single source of truth for the march to the internal-pilot MVP. The
status words have these meanings:

- **complete**: implemented and verified for the stated scope;
- **interim**: deliberately usable for the pilot only with the documented risk;
- **blocker**: must be completed before the pilot release gate can pass;
- **not started**: designed or scoped, but not implemented.

Use the two separate executable readiness runbooks:

- [Local laptop MVP test](local-laptop-mvp-test.md) — development acceptance on
  one Windows laptop with local containers and development credentials;
- [Company internal-pilot MVP test](company-pilot-mvp-test.md) — formal company
  environment, managed-endpoint, operational, security, and go/no-go evidence.

Passing the local runbook never implies that the company runbook passes.

Keep Edge and Service progress in this document so their shared contracts do not
drift. Use these component views when ownership is split:

| Component | Done now | Pilot blockers / next work |
|---|---|---|
| BAP Edge (endpoint PEP) | Six managed Claude hooks, local cc-filter, normalized AuthZEN requests, exact-request signed-grant cache, outcome retry spool, fail-closed behavior, W3C operation traces, privacy-safe JSONL | Sonnet/Opus and full-tool certification; bounded cache cleanup and spool metrics; OTLP export; per-device revocable identity; signed Windows release and endpoint application control |
| BAP Service (network PDP) | Authenticated HTTPS/AuthZEN API, Cedar permit/forbid/default-deny, signed request-bound grants, MySQL audit/proposals with trace index, structured decision logs, bounded Prometheus metrics, `/healthz` and database-backed `/readyz`, fail-closed database behavior | Versioned policy lifecycle; client registry/revocation; admin read/workflow APIs; OpenTelemetry Collector export; backup/restore/checkpoints/retention; UI; HA/DR and signed OCI release |
| Shared deployment contract | Company TLS and managed-settings design, correlation IDs, privacy-safe audit model, local Docker MySQL path, documented enterprise MySQL cutover | Company PKI/secret distribution, enterprise MySQL exercise, supported-version matrix, SLOs/alerts/runbooks, independent security review |

The capability ledger below is authoritative when a summary conflicts with a
work package. The [technical architecture](mvp-technical-architecture.md) is the
separate design document for both components; component-specific implementation
notes live in the [BAP Service README](../../bap-service/README.md) and the
[BAP Edge configuration reference](configuration.md).

## MVP scope and threat model

The internal-pilot MVP will protect tool calls made by the approved official
Claude Code client when all of these conditions hold:

- Windows endpoints are centrally managed and developers cannot elevate to
  local Administrator;
- Claude managed settings, BAP Edge, and its configuration are deployed by IT;
- application control permits only the approved Claude and BAP binaries;
- BAP Service runs on a protected company network with company TLS;
- each endpoint/device has a distinct revocable BAP identity;
- administrators own Cedar policy and the policy deployment process;
- audit storage, retention, backup, alerting, and incident ownership exist;
- direct access to protected databases, APIs, and MCP services is either out of
  scope or separately protected.

The hook does not mediate a different executable, a direct database client, or a
user with local-administrator control. Those boundaries must remain visible in
product documentation and pilot approval.

## Capability ledger

| Capability | Current implementation | Interim deviation or limitation | Solid MVP target | Enterprise final state | Status |
|---|---|---|---|---|---|
| Endpoint enforcement | Six administrator-managed Claude hooks; lower-scope hooks and permission rules disabled; fail closed | A local administrator or modified client can bypass the hook | Managed endpoints without local admin plus application allowlisting and signed artifacts | Endpoint attestation and resource-side enforcement | Complete for lab; pilot controls required |
| Local filtering | Embedded cc-filter blocks protected paths, risky commands, and sensitive prompts | Rule coverage is heuristic and tool-specific | Versioned rules with bypass regression corpus for every supported Claude release | Centrally distributed, signed rule bundles plus defense in depth at resources | Complete baseline |
| Policy decision | AuthZEN evaluation with Cedar permit, forbid, and default deny | Single policy bundle loaded at service startup | Validated, versioned policy bundles with staging, rollback, ownership, and approval | Multi-tenant policy control plane with canary deployment | Engine complete; lifecycle missing |
| Claude/model/tool coverage | Local Qwen harness and a small modeled tool set prove the hook path | Company Sonnet/Opus usage, full built-in tools, MCP, delegation, shell variants, and versioned tool schemas are not certified | Exact Claude Code/Sonnet/Opus support matrix, owned normalizers, policy profiles, and bypass corpus | Continuous certification for approved client/model/tool releases | MVP blocker |
| Edge identity | Dedicated bearer key, principal mapping, and credential fingerprint | One copyable credential/principal per service instance; no revocation registry | Unique per-device credentials with registration, expiration, rotation overlap, and revocation | Short-lived enterprise workload identity or mTLS bound to device/user posture | Interim; MVP blocker |
| Session identity | Claude session ID plus random Edge workload ID and tool-use ID | User-local mapping can be deleted; asserted human identity is not strongly verified | Device credential plus stable enterprise user/device claims | Attested workload and resource-enforced identity chain | Strong correlation, interim trust |
| Transport and grants | HTTPS; Ed25519, request-bound 30-second allow grants | Development CA/key files in local demo; one active grant key | Company PKI, protected secret storage, documented rotation, overlapping public keys | Automated PKI/KMS/HSM rotation and revocation | Protocol complete; operations missing |
| Permission cache | Only signed allow grants; exact request hash; central acknowledgement on reuse | Exact retry only; expired files are not proactively removed | Expiry cleanup, size/age limits, metrics, corruption tests | Bounded in-memory/disk cache with fleet telemetry | Secure semantics complete; hygiene missing |
| Authorization audit | MySQL append-only events with transactional chain head, Ed25519 signatures, policy hash, indexed correlations, startup verification, and fail-closed commits | Local single MySQL instance; no backup/restore proof, external checkpoint, retention job, or SIEM export | Company managed MySQL, backup/restore test, retention, external checkpoints, searchable export | Replicated durable store plus SIEM/WORM archive | Pilot database baseline complete; operations blocker remains |
| Outcome correlation | PostToolUse/PostToolUseFailure with idempotent retry and prior-allow check | User-local retry spool can be deleted | Queue depth/age metrics, alerts, retry limits, operational runbook | Durable endpoint/service messaging with delivery SLO | Complete baseline; observability missing |
| End-to-end tracing | Stable operation trace IDs, W3C `traceparent`, Edge/Service spans, response trace headers, signed-audit persistence and MySQL trace index | No direct OTLP export, sampling controls, downstream MCP/API propagation, or trace UI | Export to company collector and searchable correlated timeline | Traces continue into MCP/API/database gateways and centralized observability | Propagation complete; export/UI missing |
| Safe learning | Sanitized MySQL proposals only for `NO_MATCHING_POLICY`; explicit forbids never propose bypass; duplicates increment evidence | No ownership, review transition API, approval, or policy-draft workflow | Proposal review states, admin API, audit, and manual policy draft/test flow | Governed recommendation models with explainability; never autonomous activation | Durable evidence baseline complete; workflow missing |
| Service APIs | Health, AuthZEN evaluation, grant-consumption audit, outcome, and edge-denial endpoints | No admin API; runtime bearer credential model only | Separately authenticated read APIs for audit/proposals and controlled workflow APIs | RBAC/ABAC admin control plane with separation of duties | Runtime API complete; control plane missing |
| UI | CLI scripts list and verify audit/proposals | No dashboard or investigation workflow | Read-only operations dashboard after admin APIs exist | Full trace, policy, proposal, identity, rollout, and incident UI | Not started |
| Availability | Non-root service, MySQL-backed `/readyz`, database outage fail-closed test | One local service and MySQL instance; outage blocks tools | Defined pilot SLO, resource limits, managed-MySQL backup/restore, monitored replicas | Multi-zone stateless PDP with replicated durable stores and tested DR | Readiness baseline complete; HA/DR blocker remains |
| Supply chain | Vendored Go dependencies, pinned toolchain path, repeatable builds and tests | Images not digest-pinned; no signed releases, SBOM, provenance, or enforced application control | Checksums, SBOM, vulnerability scan, signed binaries/images, protected release workflow | Reproducible attestations and organization-wide enforcement | MVP blocker |
| Security validation | Unit/integration, managed ACL, allow/deny, fail-closed, audit integrity, and performance tests | No independent review, fuzzing, chaos, formal threat model, or release-specific bypass certification | Written threat model, parser fuzzing, negative corpus, disk-full/network/key-rotation tests, pilot review | Continuous conformance, penetration tests, red team, and chaos program | MVP blocker |

## MVP work packages

The following order reduces security risk before adding a UI.

### MVP-0: Expand Cedar policy and certify Sonnet/Opus tool coverage

Priority: P0 and required before the company pilot.

Use the detailed [Cedar MVP policy and Claude tool coverage plan](cedar-mvp-policy-plan.md).

MVP-0A is implemented: the documented built-in tool inventory has owned action
classification, malformed required fields fail closed, read-only and standard
developer profiles exist, exact endpoint registries gate network/MCP/delegation,
shell execution is restricted to a small safe classifier, stale-policy grants
are rejected, and a data-driven corpus plus `Test-MVP0.ps1` certifies the local
path. Central registry attributes, deeper shell parsing, identity attributes,
company fixtures, and exact release certification remain.

- inventory every company-enabled built-in tool, MCP tool, plugin, shell, and
  delegation mechanism in the approved Claude Code version;
- capture sanitized hook fixtures from both company-approved Sonnet and Opus;
- expand normalization and the Cedar schema for roles, device state,
  environments, repositories, path class, shell risk, network destination, MCP
  server/tool classification, mutation, delegation, and approvals;
- replace the universal broad permit with read-only and standard-developer
  profiles plus explicit forbid modules;
- default deny every unknown/malformed tool or classification;
- build a data-driven company bypass and policy regression corpus.

Exit criteria:

- every enabled tool has an owned, versioned normalization contract;
- equivalent operations receive equivalent decisions under Sonnet and Opus;
- broad wildcard permits for MCP, network, shell, and unknown tools are gone;
- explicit forbids cover company secrets, workspace escape, destructive actions,
  privilege/persistence, exfiltration, production mutation, BAP self-protection,
  and delegation escalation;
- the exact Claude Code and model versions pass the release corpus.

### MVP-1: Stabilize the existing Edge and audit path

Priority: P0. This is the next implementation slice.

Implemented in the observability slice: stable operation trace IDs, W3C
propagation, Edge and Service spans, signed-audit trace persistence, MySQL trace
indexing, response trace headers, privacy-safe Edge JSONL, structured Service
decision logs, `/metrics`, and automated trace/log/metric privacy checks.
Remaining MVP-1 work is cache cleanup and bounds, retry-spool metrics and
limits, direct OTLP export/sampling, collector integration, and expanded
disk/corruption/slow-response failure tests.

- add grant-cache expiry cleanup, maximum age, maximum entry count, and metrics;
- add structured JSON operational logs without prompts, plaintext commands,
  file contents, absolute outside paths, or credentials;
- introduce `trace_id`, `span_id`, and `parent_span_id` fields and propagate
  W3C `traceparent` between Edge and Service;
- persist trace correlation in signed audit records;
- add readiness separate from liveness and report policy/audit readiness;
- expose Prometheus-format metrics for decisions, latency, cache source, deny
  reason, authentication failure, audit failure, and retry-spool age/depth;
- add automated tests for cache expiry/cleanup, trace propagation, log privacy,
  service outage, slow response, corrupted grants, and disk-write failure.

Exit criteria:

- expired grants do not accumulate indefinitely;
- every authorization and outcome is searchable by one trace ID;
- logs and metrics contain no prohibited sensitive fields;
- a failure to persist an allow audit still denies;
- readiness becomes unhealthy when policy, keys, or audit durability is unusable.

### MVP-2: Replace the shared bearer identity

Priority: P0.

- add an Edge/client registry with unique IDs and credential fingerprints;
- support credential expiration, disable/revoke, rotation overlap, and last-used
  metadata;
- bind grants and audit events to the registered client and principal;
- protect administration with a separate mTLS or OIDC identity and RBAC;
- document provisioning, rotation, loss, compromise, and offboarding runbooks;
- choose the migration target: per-device client credentials for the pilot and
  enterprise mTLS/short-lived workload tokens for the final state.

Exit criteria:

- revoking one endpoint does not interrupt others;
- a revoked or expired credential cannot evaluate or consume a grant;
- rotations work without an allow-all window;
- no admin endpoint accepts an Edge runtime credential.

### MVP-3: Add a governed policy and proposal control plane

Priority: P0 before policy changes are delegated beyond the core team.

- create versioned immutable policy bundles with schema and test metadata;
- add proposal states: `pending`, `investigating`, `drafted`, `rejected`,
  `approved`, and `deployed`;
- provide authenticated admin APIs to list/filter proposals and create a policy
  draft, but never activate policy automatically;
- validate Cedar and run positive/negative regression tests before staging;
- require an approver distinct from the author for sensitive policy activation;
- record every admin action in a separate immutable administrative audit trail;
- support activation, canary, rollback, and last-known-good recovery.

Exit criteria:

- no endpoint can mutate the active policy directly;
- every active policy maps to source, tests, author, approver, and deployment;
- explicit forbids can never generate or approve an automatic bypass;
- rollback is exercised successfully in a test environment.

### MVP-4: Provide read APIs and an operations UI

Priority: P1, after identity and admin authorization exist.

Start read-only:

- audit event search by time, principal, endpoint, session, workload, tool,
  action, decision, reason, policy version, and trace ID;
- correlated authorization-to-outcome timeline;
- proposal queue and occurrence trends;
- service health, latency, cache source, authentication failures, audit-chain
  state, and Edge spool status;
- export links for incident response and SIEM correlation.

Do not expose audit update/delete operations. Policy activation and credential
changes require stronger workflow controls than ordinary CRUD.

Exit criteria:

- operators can explain a denial and trace an allowed operation to its outcome
  without reading container files;
- UI/API authorization is tested for cross-role and cross-tenant access;
- UI unavailability cannot change authorization behavior.

### MVP-5: Operationalize storage and service reliability

Priority: P0 for pilot launch.

- define pilot decision volume, latency SLO, availability SLO, RTO, and RPO;
- choose a protected durable store and searchable index while retaining signed
  event integrity and idempotency;
- publish audit-chain checkpoints outside the primary store;
- implement retention, backup, restore, corruption, and legal-hold procedures;
- add graceful shutdown, bounded request concurrency, rate limits, timeouts,
  resource limits, and alerting;
- test network partition, slow storage, disk full, damaged audit tail, key
  rotation, certificate expiry, and restore from backup.

Exit criteria:

- a documented restore exercise meets RTO/RPO;
- no failure mode silently becomes allow;
- alerting identifies authentication, policy, audit, capacity, and latency
  failures before the pilot SLO is exhausted.

### MVP-6: Release and endpoint hardening

Priority: P0 for pilot launch.

- produce signed Windows and OCI artifacts with checksums, SBOM, provenance,
  and vulnerability reports;
- pin deployment images by digest and protect build/release branches;
- deploy managed settings and credentials through endpoint management;
- enforce approved Claude/BAP binaries with WDAC, AppLocker, or equivalent;
- remove developer local-admin capability in the pilot group;
- test each supported Claude release against the bypass corpus before approval;
- define installation, upgrade, rollback, incident, and break-glass runbooks.

Exit criteria:

- a standard pilot user cannot replace policy, Edge, settings, or the approved
  Claude executable;
- artifacts are traceable to reviewed source and reproducible build inputs;
- rollback restores both compatible Edge and policy versions.

## Solid MVP release gates

The internal-pilot MVP is ready only when every gate has named evidence and an
owner.

| Gate | Required evidence |
|---|---|
| Scope | Approved threat model, supported tools/platforms, exclusions, pilot users, and data classification |
| Functional | Certified Claude Code/Sonnet/Opus tool fixtures plus automated allow, forbid, default deny, outside-workspace, local-filter, cache, outcome, proposal, and fail-closed tests |
| Identity | Per-device credential provisioning, revocation, expiry, rotation, and offboarding tests |
| Policy | Versioned bundles, regression suite, approval separation, staged activation, and rollback proof |
| Audit | Signed-chain verification, external checkpoint, searchable export, retention, backup, restore, and privacy test |
| Reliability | Capacity result, latency/availability SLO, disk/network/key/certificate failure tests, and monitored readiness |
| Endpoint | Managed policy, no local admin, application allowlist, signed artifacts, and supported Claude version certification |
| Operations | Dashboards, actionable alerts, ownership, on-call/runbooks, incident drill, and break-glass review |
| Security | Threat-model review, dependency/artifact scan, parser fuzzing, bypass corpus, and independent pilot assessment |
| Governance | Data owner, retention, access review, proposal/policy owners, and explicit risk acceptance for remaining deviations |

## What may be deferred beyond the bounded MVP

These items remain necessary for broad enterprise production but may be deferred
from a small, explicitly risk-accepted internal pilot:

- multi-region active-active service;
- very high-throughput replicated authorization/event infrastructure;
- HSM-backed signing for every environment;
- generalized enforcement at every protected resource gateway;
- advanced recommendation models for proposal classification;
- multi-tenant delegated administration;
- fleet-wide posture attestation and continuous device trust.

Deferral must be explicit. It must not weaken fail-closed authorization, unique
revocable identity, policy governance, audit durability, endpoint control, or
the ability to investigate an allowed operation.

## Immediate next iteration

Finish MVP-0 and the remaining MVP-1 work before starting the UI. The
recommended change sequence is:

1. company tool inventory and Sonnet/Opus hook fixtures;
2. expanded normalization, schema, policy profiles, and forbid corpus;
3. cache cleanup, bounded storage, and retry-spool metrics;
4. OTLP export, sampling controls, and company collector integration;
5. slow-service, disk, corruption, and telemetry-export failure tests;
6. update this ledger with measured evidence.

After MVP-1, implement the identity registry and revocation model in MVP-2. An
admin UI built before those foundations would expose sensitive operational data
without a suitable administrative trust boundary.
