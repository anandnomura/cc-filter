# BAP Service

BAP Service is intentionally isolated in this top-level folder. It is the
network Policy Decision Point; BAP Edge remains in the repository root.

It provides AuthZEN evaluation over HTTPS, evaluates Cedar policies, issues
short-lived request-bound grants, and records sanitized missing-policy proposals
for manual administrator review. Authenticated authorization, cached-grant use,
local denials, and tool outcomes form one signed, hash-chained audit trail.

See the [network deployment guide](../docs/bap-edge/network-deployment.md),
[certificate guide](../docs/bap-edge/certificates.md), and
[policy guide](../docs/bap-edge/policies-and-proposals.md). Audit operation is
documented in the [audit guide](../docs/bap-edge/audit-trail.md).
