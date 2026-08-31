# Cedar policy and Claude tool coverage plan for the MVP

This document defines the policy-engine work required for a company pilot using
official Claude Code with Sonnet or Opus. The local Qwen bridge is only a
development harness. Authorization must not depend on which model generated a
tool request: the official Claude client emits the hook event, BAP Edge
normalizes it, and locally bundled Cedar evaluates the resulting operation.

Model choice still matters for testing because models can choose different
tools and compose inputs differently. Every supported Claude Code release,
Sonnet model, and Opus model therefore needs the same tool-contract and bypass
conformance suite.

## Current implementation: MVP-0A foundation

The model-independent MVP-0A foundation now:

- separates notebook writes, network search, network fetch, delegation, MCP,
  and unknown actions;
- explicitly forbids unregistered network fetch, MCP, delegation, and unknown
  tool calls while leaving ordinary web search available;
- explicitly forbids writes to Claude managed settings/hooks and Git hooks;
- classifies and forbids destructive, privileged, likely-exfiltration, and
  common encoded command forms; and
- tests each new action and risk classification;
- owns a registry entry for the current documented Claude built-in tool names,
  with higher-impact families explicitly denied until a policy slice exists;
- fails closed when required security-relevant fields are absent, empty, or
  have the wrong JSON type;
- separates `read-only` and `standard-developer` Cedar profiles;
- consumes WebFetch, MCP, delegation, profile, and shell registries only from a
  centrally signed versioned bundle;
- permits shell execution only for a deliberately small centrally configured classifier of
  inspection/build/test commands and denies shell operators, wrappers, and
  unclassified executables; and
- runs a version-controlled data fixture corpus plus end-to-end acceptance via
  `Test-MVP0.ps1`; and
- rejects signed-bundle tamper, expiry, rollback, equivocation, stale offline
  leases, forced update failure, and kill switch.

Remaining limitations include:

- the Cedar Agent entity is always constructed with `enabled: true`;
- Cedar context is empty even though AuthZEN context contains correlation data;
- MCP names are split into server and tool but do not yet have centrally owned
  read/mutate, tenant, data-classification, owner, or expiry metadata;
- WebFetch has an endpoint registry and HTTPS/DNS validation, but redirect and
  resolved-address enforcement must also exist at the network/resource layer;
- the strict command splitter and per-argument patterns are an MVP parser, not
  a complete Windows/Git-Bash/PowerShell AST and bypass corpus;
- the documented tool inventory is represented, but exact company Claude Code
  release payloads and compatibility reports are not yet captured;
- approved delegation types can be named, but parent authority, recursion,
  fan-out, and policy-ceiling propagation are not implemented;
- file rules distinguish protected/outside/destructive but not repository
  control files, generated areas, source, tests, infrastructure, or policy;
- there are no service-derived role, group, device, environment, repository,
  or risk-tier attributes;
- the policy has focused engine tests rather than a company policy corpus.

The model-independent corpus now contains 62 tool schema/decision cases and a
38-case structured shell/bypass corpus. Exact company Sonnet/Opus compatibility
still requires captured, manifested fixtures; the privacy-safe capture/replay
framework is implemented.

This is a strong certification foundation, not the final company-pilot gate.
The exact company Claude Code/Sonnet/Opus fixtures, company registry content,
shell inventory, and security review remain required.

## Supported client and model contract

The MVP support matrix must name exact approved versions rather than "latest":

| Dimension | MVP requirement |
|---|---|
| Client | Approved official Claude Code version range, managed minimum/maximum version, and release certification |
| Models | Company-approved Sonnet and Opus model identifiers |
| Platforms | Managed Windows first; add Linux/macOS only after equivalent path and endpoint tests |
| Built-in tools | Explicit inventory with captured hook schemas and normalization tests |
| MCP | Explicit server and tool allowlist; no wildcard permit for an unknown server |
| Plugins | Disabled by default or separately inventoried, signed, and policy-tested |
| Subagents | Parent identity and policy ceiling propagated; delegation cannot increase authority |

Changing Sonnet to Opus must not change authorization for the same normalized
request. Tests must prove this invariant. Model identity may be recorded as
diagnostic metadata when Claude provides it, but it must not be trusted as the
primary authorization boundary.

## Tool inventory and normalization

Before writing the expanded Cedar policies, capture real PreToolUse payloads
from the approved Claude Code version and create fixtures for every enabled
tool. The initial inventory should include at least:

| Tool family | Known examples | Required normalized action |
|---|---|---|
| File read | `Read` | `file.read` |
| File discovery/search | `Glob`, `Grep`, `Search` | `file.search` |
| File mutation | `Write`, `Edit`, `MultiEdit` | `file.write` |
| Notebook mutation | `NotebookEdit` | `notebook.write` |
| Shell | `Bash` and any supported PowerShell tool | `command.execute` |
| Network | `WebFetch`, `WebSearch` | `network.fetch`, `network.search` |
| MCP | `mcp__<server>__<tool>` | `mcp.read` or `mcp.mutate` after registry lookup |
| Delegation | `Task`, `Agent`, or the approved client equivalent | `agent.delegate` |
| User interaction | approved question/communication tools if hook-visible | `interaction.request` |
| Unknown | any new or malformed tool | `tool.unknown` and default deny |

Each normalizer needs positive, negative, malformed-input, path-traversal,
symlink, quoting, case, Unicode, alternate-separator, and version-fixture tests.
An unknown tool or missing security-relevant input must never fall into a broad
permit.

## Required authorization attributes

The Cedar schema and adapter should evolve from a single generic invocation to
explicit, typed policy inputs.

### Agent/device attributes

- registered principal and Edge/device ID;
- enabled, expired, and revoked state;
- enterprise user identity and approved groups/roles;
- managed-device and compliance state;
- environment such as development, test, staging, or production;
- maximum risk tier and approved policy profile.

The service must obtain these from its authenticated registry or enterprise
identity, not blindly trust values asserted by the Edge.

### Operation/resource attributes

- canonical tool family and original tool name;
- read-only versus mutating operation;
- canonical workspace and workspace-relative path;
- path class: source, test, documentation, generated, build, infrastructure,
  policy, repository-control, credential, or unknown;
- protected, outside-workspace, symlink-escape, and sensitive flags;
- parsed executable, argument classes, shell operators, and command risk;
- network scheme, normalized host, port, and managed destination class;
- MCP server ID, tool ID, read/mutate class, and resource scope;
- delegation target/type, parent workload, and requested authority ceiling;
- risk tier and whether explicit approval is required.

### Context attributes

- session, workload, tool-use, trace, and parent-span IDs;
- endpoint and authenticated principal;
- repository/workspace identity;
- policy profile and environment;
- approved change/ticket identifier when required;
- client and tool-contract version;
- optional model metadata for investigation, not authority.

Sensitive raw values can be evaluated transiently, but audit storage must retain
only the privacy-safe summaries documented in the audit specification.

## Baseline policy profiles

The MVP should ship profiles rather than one universal permit.

### Read-only developer

Permit:

- reads and benign searches inside approved workspaces;
- approved documentation/source discovery;
- explicitly approved read-only MCP tools;
- approved read-only network destinations when required.

Deny writes, shell mutation, mutating MCP calls, delegation, protected data,
outside-workspace access, and unknown tools.

### Standard developer

Permit:

- the read-only profile;
- source/test/documentation writes within approved repositories;
- a defined set of non-destructive build, test, formatting, and version-control
  inspection commands;
- approved package operations against company registries according to policy;
- explicitly approved MCP tools and destinations.

Deny protected/repository-control/policy/credential writes, destructive version
control, privilege changes, persistence, secret discovery, unapproved network
destinations, unknown interpreters, and authority-expanding delegation.

### Elevated change workflow

Do not implement this as a permanent broad role. A future workflow may issue a
short-lived approval bound to a specific principal, endpoint, repository,
action/resource, ticket, and expiry. Cedar must still apply explicit forbids and
the grant must not be reusable for a different operation.

## Explicit forbid families

The policy corpus must cover at least these families. Exact company rules must
be supplied by security, platform, developer-experience, and data owners.

1. Credentials and secrets: `.env`, SSH, cloud credentials, tokens, private
   keys, password stores, secret directories, and credential commands.
2. Workspace escape: traversal, absolute outside paths, symlink/junction escape,
   alternate path syntax, environment expansion, UNC/device paths, and case
   variations.
3. Destructive filesystem: recursive deletion, formatting, destructive moves,
   mass overwrite, permission/ownership changes, and protected repository
   metadata.
4. Version control: hard reset, destructive clean, forced push, hook/config
   manipulation, credential helpers, and unsafe remote changes.
5. Privilege/persistence: elevation, service/task creation, startup changes,
   account/group changes, security-control modification, and executable policy
   bypass.
6. Process/code execution: unapproved interpreters, encoded/obfuscated commands,
   download-and-execute, dynamic evaluation, shell chaining that changes risk,
   and executable writes to protected locations.
7. Network/exfiltration: unapproved hosts, raw IP destinations where disallowed,
   insecure schemes, tunnels/proxies, arbitrary upload, DNS/HTTP exfiltration,
   and package registries outside the company allowlist.
8. Infrastructure and data: destructive cloud/Kubernetes/Terraform/database
   operations, production mutations, schema drops, bulk export, and unapproved
   environment selection.
9. MCP: unknown servers/tools, mutating tools under a read-only profile,
   cross-tenant resources, and tools without a maintained classification.
10. Delegation: subagent creation without inherited identity/context, increased
    authority, unsupported agent types, and unbounded recursion/fan-out.
11. BAP self-protection: changes to managed settings, Edge binaries/config,
    Cedar bundles, trust roots, credentials, audit data, or BAP operational
    controls from the protected Claude session.
12. Unknown/malformed: absent required fields, new tool schemas, inconsistent
    classification, parse uncertainty, and unsupported client versions.

Explicit forbids override permits. Unknown and uncertain inputs default deny and
create sanitized review signals only when appropriate; they never auto-learn an
allow.

## Shell policy strategy

A production policy cannot safely classify arbitrary shell text with one regular
expression. The MVP should:

1. parse the approved Windows shell syntax into executable/arguments/operators;
2. reject parse ambiguity, encoded commands, unsupported interpreters, and
   dangerous chaining;
3. classify every command segment, not just the first executable;
4. normalize aliases, wrappers, paths, quoting, case, and environment expansion;
5. distinguish inspection, build/test, mutation, network, privilege, and
   destructive operations;
6. make allowlists profile- and environment-specific;
7. keep a bypass corpus covering PowerShell, Git Bash, `cmd.exe`, scripting
   runtimes, version control, package managers, cloud CLIs, containers, and
   infrastructure tools actually enabled by the company.

Until this parser and corpus exist, broad `command.execute` permission is an MVP
blocker.

## MCP and network policy strategy

Maintain an administrator-owned registry:

```text
server/tool or destination
  -> owner
  -> read/mutate classification
  -> data sensitivity
  -> allowed environments/groups
  -> resource scope
  -> required approval
  -> expiry/review date
```

Cedar consumes the registry-derived classification. It must not infer safety
from an `mcp__` prefix or permit every HTTPS destination. Unknown entries default
deny and generate a sanitized proposal for administrator review.

## Policy test corpus and release gate

The policy suite should be data-driven and contain:

- one allow and multiple deny cases for every supported tool/action/profile;
- equivalent Sonnet and Opus tool payload fixtures where they differ;
- compound and adversarial shell cases;
- Windows path traversal, junction/symlink, UNC, alternate stream, casing, and
  environment-expansion cases;
- read versus mutation cases for every approved MCP tool;
- domain, redirect, scheme, IP-literal, and upload cases for network tools;
- subagent inheritance and no-authority-escalation cases;
- policy change regression tests against all prior explicit forbids;
- unknown client version, tool name, and malformed schema fail-closed cases;
- audit privacy assertions proving that prompts, plaintext commands, outputs,
  secrets, and unnecessary absolute paths are absent.

MVP exit criteria:

- every enabled Claude tool has an owned normalization contract;
- every approved Sonnet/Opus and Claude Code combination passes the same policy
  outcomes for equivalent operations;
- no wildcard MCP/network/unknown-tool permit remains;
- shell mutation is permitted only through tested classifications;
- profiles and company resource boundaries are represented in Cedar;
- policy coverage, mutation testing, and bypass results are release artifacts;
- a new or changed tool schema fails closed until certified.

## Recommended delivery sequence

1. Inventory company-enabled built-in, MCP, plugin, shell, and delegation tools.
2. Capture sanitized hook fixtures using the approved Claude Code version with
   both Sonnet and Opus.
3. Define company roles, environments, repositories, destinations, MCP tools,
   protected assets, and approval boundaries.
4. Expand the normalized AuthZEN contract and Cedar schema.
5. Implement robust path, shell, network, MCP, and delegation classifiers.
6. Split the universal policy into explicit profiles and forbid modules.
7. Build the data-driven policy/bypass corpus and mutation coverage.
8. Add versioned policy bundles, staging, approval, and rollback from MVP-3.
9. Pilot in observe/proposal mode for missing classifications while explicit
   forbids remain enforced; never silently allow a no-match.
10. Certify the exact Claude Code, Sonnet, and Opus versions before rollout.
