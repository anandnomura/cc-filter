# BAP Service

BAP Service is intentionally isolated in this top-level folder. It is the rule
control plane; the BAP Edge local PDP/PEP remains in the repository root.

It validates and signs centrally owned Cedar/registry bundles, distributes
version, refresh, revocation, forced-update, and kill-switch state over HTTPS,
and ingests asynchronously delivered Edge decisions and outcomes. MySQL stores
the transactional signed/hash-chained audit trail. The obsolete central
AuthZEN/Cedar decision and legacy grant endpoints have been removed. BAP Edge
is the only tool-call policy decision point; escalated operations use the
separate AgentGrant STS contract.

See the [network deployment guide](../docs/bap-edge/network-deployment.md),
[MySQL storage guide](../docs/bap-edge/storage.md),
[certificate guide](../docs/bap-edge/certificates.md), and
[policy guide](../docs/bap-edge/policies-and-proposals.md). Audit operation is
documented in the [audit guide](../docs/bap-edge/audit-trail.md).
