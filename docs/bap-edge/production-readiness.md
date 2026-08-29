# Production readiness assessment

The repository is a functional, security-oriented reference implementation and
distributed demonstrator. It is not yet an enterprise production control plane.

## Implemented and tested

- admin-managed Claude hooks with lower-scope hooks/rules disabled;
- inherited cc-filter blocks/redaction plus Cedar permit/forbid/default deny;
- AuthZEN 1.0 evaluation endpoint over HTTPS;
- dedicated bearer credential, principal fingerprint, Claude session, random
  workload, and tool-use correlation;
- short-lived exact-operation grants and centrally acknowledged cache use;
- decisions fail when durable audit fails;
- post-tool outcomes with durable retry, idempotency, and prior-allow correlation;
- dedicated signed/hash-chained audit key, startup verification, policy hash, and
  command/outside-path privacy;
- recommendation-only missing-policy proposals requiring administrator approval;
- non-root Docker/Podman service, native Windows source build, vendored modules,
  and case-by-case automated tests.

## Required before enterprise production

### 1. Replace interim identity

The machine environment bearer key can be copied by a user who can inspect their
environment, and this version accepts one credential/principal per instance. Add
mTLS or short-lived enterprise workload tokens, multi-client registration,
revocation, rotation overlap, device/user binding, and identity-provider audit.

### 2. Move authority to protected resources

A Claude hook controls the approved Claude client; it cannot stop a user or
another process calling a database/API directly. Production DB/API/MCP/tool
gateways must verify bounded grants and enforce action/resource constraints.
Also require application allowlisting, managed Claude distribution, restricted
local administration, and binary signing/attestation.

### 3. Replace single-node JSONL audit throughput

The current serialized fsync path measured about 58 decisions/second on the
development volume. Introduce a durable replicated log/event service that
acknowledges only after quorum/WAL durability, supports idempotency and indexes,
exports to SIEM/WORM storage, retains signatures, and publishes external chain
checkpoints. Then make PDP replicas stateless and horizontally scalable.

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
AuthZEN/Cedar conformance tests, parser fuzzing, race testing, load/soak tests,
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
