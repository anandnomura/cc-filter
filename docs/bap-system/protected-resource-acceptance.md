# Protected-resource acceptance guide

This guide tells a tester what to do and what evidence proves the result. A
green health check alone is not acceptance.

## Automated certification

Run from the repository root on a company Windows laptop:

```powershell
# Complete container-free build and protected-resource proof
.\Build-Bap.ps1 -Runtime Native
.\Build-ResourcePEPs.ps1 -Runtime Native
.\Test-AgentGrant.ps1 -Runtime Native
.\Test-ResourcePEPs.ps1 -Runtime Native
.\Demo-ResourcePEPs.ps1 -Runtime Native

# Repeat the resource boundary in available container runtimes
.\Test-ResourcePEPs.ps1 -Runtime Docker
.\Demo-ResourcePEPs.ps1 -Runtime Docker -Rebuild
.\Test-ResourcePEPs.ps1 -Runtime Podman
.\Demo-ResourcePEPs.ps1 -Runtime Podman -Rebuild
```

For the wider Edge/Service regression also run:

```powershell
.\Test-MVP0.ps1 -Runtime Native
```

The demo succeeds only when it prints all five lines:

```text
PASS: Claude-style API tool -> Edge -> Agent STS -> Spring Cloud API PEP -> protected Orders API
PASS: Spring Cloud API PEP rejected exact AgentGrant replay
PASS: Claude-style MCP call -> Edge -> Agent STS -> MCP PEP -> protected upstream MCP server
PASS: MCP PEP rejected exact AgentGrant replay
PASS: protected resources accepted only PEP-owned identities and executed once each
```

This proves policy/intent binding, one-use consumption, audience isolation,
replay rejection, transport stripping, PEP-owned backend identity and direct
backend denial. It does not prove company PKI, secret manager, network ACLs or
Claude MCP registration; validate those separately below.

## Human MCP test with Claude Code

Use a development MCP PEP connected to a non-production upstream. Register it
under the exact name `bap_mcp_pep`, which is part of the signed rule:

```json
{
  "mcpServers": {
    "bap_mcp_pep": {
      "type": "http",
      "url": "https://bap-mcp-pep.dev.company.example/mcp"
    }
  }
}
```

An enterprise administrator puts this in
`C:\Program Files\ClaudeCode\managed-mcp.json`. Restart Claude, open `/mcp`,
and verify `bap_mcp_pep` is connected and exposes `change_create`.

Enter this prompt; never paste a bearer grant:

```text
Create a change request for orders release 2026.08 in staging with summary "Orders staging acceptance test".
```

Expected evidence:

- Claude selects `mcp__bap_mcp_pep__change_create`;
- managed `UserPromptSubmit` stores matching intent;
- managed `PreToolUse` injects a short-lived one-use grant;
- MCP PEP returns the upstream change ID;
- Service audit has `AGENT_GRANT_ISSUED` and `AGENT_GRANT_CONSUMED` for the same
  grant ID and MCP audience;
- upstream audit has exactly one creation by the MCP PEP identity.

Approved negative checks against a development resource:

1. Ask only `Create a change request` without Orders/staging intent; the tool
   must not receive a grant.
2. Change `service` or `environment`; Edge or the PEP must deny it.
3. Stop Agent STS and retry; execution must fail closed with no upstream call.
4. Call the upstream without the PEP identity; it must return unauthorized.

Do not replay tokens through the UI. The demo tests replay without exposing a
grant to a prompt, shell argument or employee.

## Human API-resource test status

The Spring gateway/API path is covered end to end by the automated harness.
Interactive Claude testing is not complete because the repository lacks the
approved structured-tool adapter that presents `mcp__bap_gateway__execute` and
forwards its trusted envelope to Spring. Do not substitute raw shell `curl`;
that bypasses the intended structured contract. After adding and managing that
adapter, test with:

```text
Deploy orders release 2026.08 to staging.
```

Acceptance requires one deployment, one matching STS issue/consume pair, no
BAP transport fields at the backend, and denial of tamper, replay and direct
backend access.

## Evidence to retain

Retain artifact digests, policy version/digest, runtimes and versions, test
output, signed Service audit, Edge decisions, PEP logs, resource call counts,
TLS chain/expiry, and managed hook/MCP configuration source. Never retain
AgentGrant bearer values.

## AuthZEN and COAZ-MCP status

The current system is **not conformant with AuthZEN COAZ-MCP Binding 1.0**. It
is conceptually aligned only:

- BAP has a SARC-shaped request internally; the legacy
  `/access/v1/evaluation` PDP endpoint has been removed;
- MCP AgentGrants bind exact tool/server and canonical argument digest;
- Agent STS re-evaluates the operation at issue and consume.

Missing binding requirements include per-method default mappings,
`x-authzen-mapping` discovery in `tools/list`, CEL evaluation,
`evaluation`/`evaluations` envelopes, token-anchored human subject plus agent
context, standard JSON-RPC authorization errors, and fail-closed coverage of all
MCP methods. Current action/resource names and digest-only argument binding are
BAP-specific.

COAZ-MCP is an OpenID AuthZEN Working Group Draft; AuthZEN Authorization API
1.0 is final. Adopt it as a separate reviewed feature: put a COAZ mapping engine
in the MCP PEP, validate the caller token, construct the standard AuthZEN
request, call the PDP, and require both the AuthZEN permit and one-use
AgentGrant. Standard evaluation must not replace exact grant consumption.

Authoritative references:

- https://openid.net/wg/authzen/specifications/
- https://openid.github.io/authzen/authzen-coaz-mcp-binding-1_0.html
- https://openid.net/specs/authorization-api-1_0.html
