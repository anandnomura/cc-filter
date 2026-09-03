# Production readiness assessment

The repository is a functional, security-oriented reference implementation and
distributed demonstrator. It is not yet an enterprise production control plane.

For the prioritized path from the current implementation to a bounded internal
pilot, use the [MVP roadmap and readiness ledger](mvp-roadmap.md). This page
describes the broader enterprise target; the roadmap defines the next work
packages and measurable release gates.

## Implemented and tested

- admin-managed Claude hooks with lower-scope hooks/rules disabled;
- inherited cc-filter blocks/redaction plus Cedar permit/forbid/default deny;
- one signed-policy decision path in BAP Edge; obsolete central AuthZEN/Cedar
  evaluation and legacy grant routes are removed and 404-tested;
- dedicated bearer credential, principal fingerprint, Claude session, random
  workload, and tool-use correlation;
- signed session capability mappings, composition forbids and rolling budgets,
  with atomic multi-process Edge state and transactional STS intent budgets;
- short-lived exact-operation grants and centrally acknowledged cache use;
- decisions fail when durable audit fails;
- post-tool outcomes with durable retry, idempotency, and prior-allow correlation;
- dedicated signed/hash-chained audit key, startup verification, policy hash, and
  command/outside-path privacy;
- recommendation-only missing-policy proposals requiring administrator approval;
- signed, automatically expiring shadow observation with production refusal,
  verified multi-runtime log collection, and offline explainable ML ranking that
  can only emit human-review candidates;
- non-root Docker/Podman service, native Windows source build, vendored modules,
  and case-by-case automated tests.

## Required before enterprise production

### Supported Claude releases and complete policy coverage

The local Qwen bridge is a development harness, not the company deployment
target. Inventory every enabled built-in, MCP, plugin, and delegation tool for
the exact approved Claude Code and Sonnet release. Normalize equivalent
operations identically, default-deny unknown or malformed payloads, and run the
same allow, forbid, bypass, and schema corpus against every supported
combination. Model identity may be diagnostic metadata, but must not grant
authority. The detailed design and delivery sequence are in the
[Cedar MVP policy plan](cedar-mvp-policy-plan.md).

### 1. Complete enterprise identity lifecycle

Pilot/production mode now requires company mTLS, an explicit enrolled Edge
certificate-CN registry, and distinct Agent STS issue/consume principals. This
supports bounded pilot enrollment and configuration-driven revocation. Add
automated enrollment, CRL/OCSP or short-lived workload identity, rotation
overlap, device/user binding, protected keys, and identity-provider audit before
enterprise-wide deployment.

### 2. Move authority to protected resources

A Claude hook controls the approved Claude client; it cannot stop a user or
another process calling a database/API directly. Production DB/API/MCP/tool
gateways must verify bounded grants and enforce action/resource constraints.
Also require application allowlisting, managed Claude distribution, restricted
local administration, and binary signing/attestation.

### 3. Operationalize and replicate MySQL audit storage

The pilot baseline now commits indexed, signed/hash-chained events to MySQL in a
transaction and fails closed when the commit cannot complete. Deploy company
managed MySQL with replication, point-in-time recovery, tested restore,
retention, capacity/SLO measurements, external chain checkpoints, and
SIEM/WORM export. Then run multiple stateless control-plane replicas and prove concurrent
chain-head behavior at expected peak load.

### 4. High availability and disaster recovery

Run multiple instances across failure domains behind an authenticated load
balancer. Define readiness, capacity limits, backup/restore, audit recovery,
policy rollback, RTO/RPO, and a tested break-glass process that cannot silently
become allow-all.

### 5. Enterprise key and secret management

Move TLS, grant, audit, and client credentials to company PKI plus KMS/HSM/secret
manager integrations. Add automated rotation with overlapping verification keys,
revocation, least-privilege service identities, and key-access alerts.

### 6. Policy lifecycle and approvals

Add signed/versioned policy bundles, schema validation, regression and negative
tests, staging/canary rollout, two-person approval for sensitive changes,
rollback, ownership, and periodic review. Proposals remain advisory.

### 7. Operational observability

Publish metrics for decision latency/count, cache source, deny reasons, audit
acknowledgement, queue age/depth, proposals, authentication failures, policy
version, and Cedar errors. Add structured logs, traces, dashboards, alerts, and
runbooks.

### 8. Formal security and resilience validation

Perform threat modeling, independent review and penetration testing,
Cedar and applicable AuthZEN COAZ-MCP conformance tests, parser fuzzing, race testing, load/soak tests,
network partition/slow backend/disk-full/key-rotation chaos tests, and bypass
testing against every supported Claude release and managed-settings source.

### 9. Supply chain and release engineering

Pin approved images by digest, generate SBOM/provenance, scan and sign Windows/OCI
artifacts, protect CI/release branches, review vendored licenses/dependencies,
publish checksums, and support reproducible internal builds.

### 10. Data governance

Define retention, regional placement, access reviews, incident/legal hold,
encrypted backup, and SIEM mappings. Ensure new tools never add prompts,
plaintext commands, outputs, secrets, or unnecessary personal paths to records.

## Recommended rollout

1. Keep this version in a lab and establish functional/policy tests.
2. Deploy to an internal test network with company TLS/KMS and durable audit.
3. Add enterprise workload identity and protected-resource grant enforcement.
4. Pilot with managed endpoints and explicit SLO/alert coverage.
5. Complete security review, failure/rotation/restore exercises, and load gates.
