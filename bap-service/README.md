# BAP Service

BAP Service is intentionally isolated in this top-level folder. It is the
network Policy Decision Point; BAP Edge remains in the repository root.

It provides AuthZEN evaluation over HTTPS, evaluates Cedar policies, issues
short-lived request-bound grants, and records sanitized missing-policy proposals
for manual administrator review. MySQL stores durable proposals and the
transactional signed/hash-chained audit trail; database failure makes readiness
and new authorization fail closed.

See the [network deployment guide](../docs/bap-edge/network-deployment.md),
[MySQL storage guide](../docs/bap-edge/storage.md),
[certificate guide](../docs/bap-edge/certificates.md), and
[policy guide](../docs/bap-edge/policies-and-proposals.md). Audit operation is
documented in the [audit guide](../docs/bap-edge/audit-trail.md).
