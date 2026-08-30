# MVP-0A certification and remaining model gate

MVP-0A is the model-independent policy foundation. It is testable without
working Sonnet or Opus access because Claude Code hooks, normalized operations,
and Cedar decisions—not model prose—form the authorization boundary.

## Run the complete local certification

From an ordinary PowerShell terminal in the repository:

```powershell
.\Test-MVP0.ps1 -Runtime Docker
```

Use `-Runtime Podman` instead when that is the selected local engine. The script
builds current sources, switches the other local runtime off port 8443 when
needed, restarts BAP Service against MySQL, runs unit and end-to-end tests,
verifies signed audit and trace correlation, then directly checks these hook outcomes:

```text
git status --short  -> allow
git reset --hard    -> deny and does not execute
```

The automated corpus also covers missing/wrongly typed fields, protected and
outside-workspace paths, unclassified/chained/encoded shell commands, HTTPS
destination validation, exact MCP registration, exact subagent registration,
unknown tools, Cedar profiles, and rejection of grants from an older policy.

## Administrator registries

The protected Edge YAML defaults to no WebFetch, MCP, or delegation approval.
See [configuration](configuration.md) for exact entries. Registry values must
be installed by administrators and reviewed like policy. They must never be
generated from model requests or auto-learning suggestions.

For the bounded pilot, these classifications originate in the managed Edge
configuration. The enterprise target is a service-owned registry bound to an
authenticated SPIFFE device/workload identity.

## What still requires real company Claude

Before admitting pilot users, capture sanitized `PreToolUse` payloads from the
exact approved Claude Code client with each approved Sonnet and Opus model.
Replay those payloads through the same corpus and record:

- exact Claude Code and model identifiers;
- every enabled built-in, MCP, plugin, and subagent tool schema;
- equivalent operation decisions across Sonnet and Opus;
- company shell commands, destinations, repositories, and MCP classifications;
- negative/bypass outcomes and an approval from the policy owner.

This live step is compatibility certification. It should not change the
authorization design; a new or changed payload must fail closed until its
contract and tests are reviewed.
