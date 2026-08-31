# Cedar policies and safe learning

The control-plane source is
`bap-service/policies/edge-policy-source.json`; Cedar is in
`bap-service/policies/agent-tools.cedar`. BAP Service validates, combines, and
signs both. BAP Edge consumes the bundle and evaluates locally.

Cedar is default deny: a request needs a matching `permit`, and any matching
`forbid` overrides permits. The current hardening baseline forbids protected
paths, outside-workspace paths, security-control writes, destructive,
privileged, likely-exfiltration, and common obfuscated commands. It also
explicitly denies arbitrary network fetch, MCP, delegation, and unknown tools
until governed registries or profiles authorize those families.

Structured command rules have three effects:

- `eligible-for-permit` makes a precisely matched command eligible for Cedar;
- `manual-only` denies Claude execution and returns a safe manual handoff; and
- `forbid` denies and overrides both other effects.

The initial manual-only registry covers MySQL, PostgreSQL, SQL Server, Oracle,
SSH, and kubectl client executable names. It is an executable boundary, not a
database proxy or natural-language intent classifier. See [Manual execution
boundary for privileged access](manual-execution-boundary.md).

## Changing or adding a rule

Add the structured command/network/MCP/delegation entry, include owner and
approval, increment top-level `version`, add positive and negative/bypass cases
to `internal/policybundle/testdata/command-policy-corpus.json`, then run
`Test-PolicyRollout.ps1` followed by `Test-MVP0.ps1`. Changing content without a version increment
causes service activation or Edge equivocation checks to fail closed.

## Missing-rule proposals

The current proposal collector belongs to the legacy central AuthZEN path. The
local Edge path records `LOCAL_NO_MATCHING_POLICY` in audit but does not yet
create a governed proposal. Future asynchronous proposal ingestion must store
only classification metadata
such as action and tool name; it does not store prompts, paths, command strings,
subject IDs, or secrets.

An unknown or malformed tool is an explicit security forbid, not a learning
candidate. This prevents a new tool schema from teaching the system to weaken
its own boundary. Proposals are reserved for recognized operations that are not
covered by a permit or forbid.

List aggregated proposals:

```bat
List-PolicyProposals.bat -Runtime Docker
```

or inside a network container:

```sh
podman exec bap-service bap-service proposals list
```

An administrator reviews frequency and intent, edits the central source/Cedar,
increments the version, adds allow/deny/bypass tests, runs `Test-Bap.ps1`, and
deploys the new control-plane image. Nothing turns a proposal directly into
live authority.
