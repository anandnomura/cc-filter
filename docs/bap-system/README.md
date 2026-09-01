# BAP System

BAP System is the complete policy-enforcement system. The repository may be
cloned into a directory named `bap-system`; changing the checkout directory
does not alter Git history, branches, or remotes.

The original cc-filter source stays at the repository root so upstream fork
synchronization remains straightforward. BAP components are isolated beside
it:

```text
bap-system/
  main.go, configs/, internal/       inherited cc-filter + shared Go contracts
  bap-edge/                          Claude hook PEP and local PDP
  bap-service/                       policy distribution and Agent STS
  bap-api-gateway-springcloud/       protected API PEP (Java/Spring Cloud)
  bap-mcp-pep/                       protected MCP PEP (Go)
  examples/protected-resources/      test-only protected API and MCP resources
```

The Go module is `bap-system`. Root `internal` packages are private to this
repository and include both inherited cc-filter implementation and contracts
shared by multiple BAP components. A component-specific implementation belongs
inside that component.

For an existing checkout named `bap-edge`, close programs using the directory
and rename it from its parent PowerShell directory:

```powershell
Rename-Item -LiteralPath .\bap-edge -NewName bap-system
cd .\bap-system
git status
```

The rename is outside the repository and therefore creates no Git change.

Start with:

- [Resource PEP build, test, start, and demo commands](resource-peps.md)
- [Complete development and production deployment guide](deployment-guide.md)
- [Protected-resource automated and human acceptance guide](protected-resource-acceptance.md)
- [Native, Docker, Podman and production operator runbooks](../runbooks/README.md)
- [BAP Edge operator guide](../bap-edge/README.md)
- [AgentGrant and Agent STS](../bap-edge/agent-grant-sts.md)
