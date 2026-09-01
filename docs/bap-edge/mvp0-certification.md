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
builds current sources, runs `Test-PolicyRollout.ps1`, switches the other local
runtime off port 8443 when needed, restarts BAP Service against MySQL, runs unit and end-to-end tests,
verifies signed audit and trace correlation, then directly checks these hook outcomes:

```text
git status --short  -> allow
ls -al              -> allow from the central rule source
git reset --hard    -> deny and does not execute
```

If company policy permits approved Go but not Docker or Podman, run:

```powershell
.\Test-MVP0.ps1 -Runtime Native
```

Native mode builds both Windows EXEs, runs the full vendored Go suite, starts
an isolated native Service, verifies Edge synchronization plus command and
prompt decisions, and uses the native fixture verifier. It explicitly reports
live MySQL lifecycle, container networking, OCI packaging, and container
failure-recovery checks as not run. Those remain Docker/Podman-only gates.

To replace the fixture `PENDING` result, follow the three native Sonnet
captures in [Exact Claude fixture certification](claude-fixture-certification.md#exact-container-free-company-baseline),
create the manifest, and rerun the strict native gate shown there.

The automated corpus also covers missing/wrongly typed fields, protected and
outside-workspace paths, unclassified/chained/encoded shell commands, HTTPS
destination validation, exact MCP registration, exact subagent registration,
unknown tools, Cedar profiles, bundle tamper/expiry/rollback/equivocation,
forced-update/kill-switch directives, and bounded offline operation.

The modeled tool corpus contains 62 schema and decision cases across file,
search, shell, web, MCP, delegation, task, schedule, agent, and emerging tool
families. Every valid case is normalized and evaluated against the current
central bundle; unknown and uncertified families remain denied.

Run only the focused policy gates while developing rules with:

```powershell
.\Test-PolicyRollout.ps1 -Runtime Docker
```

The command corpus lives at
`internal/policybundle/testdata/command-policy-corpus.json`. The rollout test
uses the real HTTPS sync handler, Edge client, signed bundle verifier, and Edge
rollback store without changing the repository's active policy source.

Current posture is visible with `Show-BapStatus.ps1`. Exact company fixtures
are captured and replayed using the procedure in
[Exact Claude fixture certification](claude-fixture-certification.md). The
strict admission form is:

```powershell
.\Test-MVP0.ps1 -Runtime Docker -RequireCompanyFixtures
```

## Administrator registries

Command, WebFetch, MCP, delegation, profile, lease, and revocation rules live in
the BAP Service control-plane source
`bap-service/policies/edge-policy-source.json`. Any content change requires a
new version and is signed before Edge consumes it. Endpoint YAML contains trust
and transport settings, not allow rules.

## What still requires real company Claude

Before admitting pilot users, capture sanitized `PreToolUse` payloads from the
exact approved Claude Code client with the approved Sonnet model.
Replay those payloads through the same corpus and record:

- exact Claude Code and model identifiers;
- every enabled built-in, MCP, plugin, and subagent tool schema;
- the expected operation decisions under Sonnet;
- company shell commands, destinations, repositories, and MCP classifications;
- negative/bypass outcomes and an approval from the policy owner.

This live step is compatibility certification. It should not change the
authorization design; a new or changed payload must fail closed until its
contract and tests are reviewed.
