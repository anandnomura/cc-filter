# Cedar policies and safe learning

Policies are in `bap-service/policies/agent-tools.cedar`. The schema beside the
policy documents the available entity attributes and actions.

Cedar is default deny: a request needs a matching `permit`, and any matching
`forbid` overrides permits. The current hardening baseline forbids protected
paths, outside-workspace paths, security-control writes, destructive,
privileged, likely-exfiltration, and common obfuscated commands. It also
explicitly denies arbitrary network fetch, MCP, delegation, and unknown tools
until governed registries or profiles authorize those families.

## Missing-rule proposals

Only a denial with reason code `NO_MATCHING_POLICY` creates a proposal. Explicit
forbids never become proposals. The proposal log stores classification metadata
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

An administrator should review frequency and intent, edit the Cedar policy,
add allow and deny tests, run `Test-Bap.ps1`, review the Git change, and deploy a
new service image. The service never turns a proposal into live authority by
itself. This prevents an agent from training the authorization layer to approve
its own escalation attempts.
