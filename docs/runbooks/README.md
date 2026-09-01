# BAP operator runbooks

Use the runbook matching the runtime. These are procedural documents: every
one includes prerequisites, build, deployment, verification, shutdown and
rollback. The [production runbook](production.md) is the promotion gate for any
runtime; passing a local demo does not make a deployment production-ready.

| Runtime | Runbook | Intended use |
|---|---|---|
| Native Go/Java on Windows | [Native](native-windows.md) | Company laptop development and acceptance without containers |
| Docker Desktop | [Docker](docker-windows.md) | Windows development, OCI build and full boundary proof |
| Podman Desktop/WSL | [Podman](podman-windows.md) | Docker-independent Windows OCI build and proof |
| Company infrastructure | [Production](production.md) | PKI, identities, state, network, rollout and rollback |

Supporting references:

- [Build/test command matrix](../bap-edge/deployment-test-matrix.md)
- [Resource PEP commands](../bap-system/resource-peps.md)
- [Protected-resource acceptance](../bap-system/protected-resource-acceptance.md)
- [Complete deployment configuration](../bap-system/deployment-guide.md)
- [AgentGrant and Agent STS](../bap-edge/agent-grant-sts.md)

## Common success criteria

All runtime demonstrations must show:

```text
PASS: Claude-style API tool -> Edge -> Agent STS -> Spring Cloud API PEP -> protected Orders API
PASS: Spring Cloud API PEP rejected exact AgentGrant replay
PASS: Claude-style MCP call -> Edge -> Agent STS -> MCP PEP -> protected upstream MCP server
PASS: MCP PEP rejected exact AgentGrant replay
PASS: protected resources accepted only PEP-owned identities and executed once each
```

The transaction uses two credentials: Claude transports only the scoped,
short-lived, one-use AgentGrant; the PEP owns a separate backend credential and
removes the AgentGrant before forwarding. A dynamic downstream workload-token
exchange is a future hardening step, not part of the current reference.

## Support boundaries

- Windows native and Docker end-to-end paths are automated.
- Podman end-to-end is automated after enabling Windows user-mode networking.
- Linux binaries and OCI images are buildable, but a Linux equivalent of the
  full PowerShell certification runner is not yet supplied.
- COAZ-MCP Binding 1.0 conformance is not implemented.
- The Agent STS enforces BAP's strict
  [RFC 8707 resource-indicator profile](https://www.rfc-editor.org/rfc/rfc8707.html):
  exactly one policy-derived HTTPS resource, no query/fragment, and `aud`
  equal to that resource on every issue and consume transaction.
- Interactive Claude use of the API example still needs a company-managed
  structured-tool adapter. Interactive MCP PEP use is supported.
