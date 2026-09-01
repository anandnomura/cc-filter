# BAP MCP PEP

This Go service is the resource-side policy enforcement point for protected
MCP tools. It validates the exact tool and arguments, consumes an AgentGrant
with its own Agent STS identity, strips BAP transport fields, and calls the
upstream MCP server with a PEP-owned identity.

Use the [resource PEP guide](../docs/bap-system/resource-peps.md) for native,
Docker, and Podman commands. The example configuration is
`mcp-pep.example.json`.

