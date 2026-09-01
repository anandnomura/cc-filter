# BAP API Gateway PEP (Spring Cloud)

This Java 21 Spring Cloud Gateway component is the resource-side policy
enforcement point for the protected Orders API example. It accepts only the
configured route, validates the exact AgentGrant-bound request, consumes the
grant using its own Agent STS identity, strips BAP transport fields, and uses a
PEP-owned backend credential.

Use the [resource PEP guide](../docs/bap-system/resource-peps.md) for native,
Docker, and Podman build, test, start, and demo commands.

