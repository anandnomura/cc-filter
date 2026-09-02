# Session capability controls

BAP now evaluates both the current structured tool call and the capabilities
already exercised in the same Claude `session_id`. This closes the gap where a
series of individually acceptable calls becomes unsafe when composed.

## What is enforced

- One session ID has one random workload ID and one capability ledger.
- Independent BAP Edge hook processes use a per-session filesystem lock and
  atomic state replacement. Two Claude instances using the same session ID
  cannot race through a limit. Different session IDs remain isolated.
- `SessionEnd` does not erase security state because Claude `--resume` can
  continue the same session ID after a non-interactive process exits. The
  signed lifetime/idle limits require an old resumed session to fail closed.
- A reservation is recorded as `pending` before a tool is allowed. Pending and
  successful operations count; failed operations do not accrue capability.
- A duplicate `tool_use_id`, a changed policy digest, an expired/idle session,
  a full ledger, or an unavailable/corrupt state fails closed.
- Agent STS separately limits how many grants one classified prompt-intent
  nonce can mint. The default signed policy permits exactly one.
- State contains capability labels, normalized resource IDs, IDs, status and
  timestamps. It does not contain prompt text, tool output, credentials, or
  request bodies.

The model cannot request a capability label or increase a budget. Edge derives
labels from the normalized operation using the verified signed bundle.

## Signed configuration

Edit `bap-service/policies/edge-policy-source.json`, obtain the named owner and
approval, increment `version`, and publish through the normal signed-policy
rollout. `session_policy` is not Edge-local configuration:

```json
{
  "session_policy": {
    "max_events": 1024,
    "max_lifetime_seconds": 28800,
    "idle_timeout_seconds": 3600,
    "capabilities": [{
      "id": "capability.orders-staging-deploy",
      "actions": ["gateway.execute"],
      "tools": ["mcp__bap_gateway__execute"],
      "property_equals": {"httpMethod": ["POST"]},
      "owner": "orders-platform",
      "approval": "CHANGE-1234"
    }],
    "composition_rules": [{
      "id": "forbid-deploy-after-change",
      "prior_capabilities": ["capability.change-create"],
      "current_capabilities": ["capability.orders-staging-deploy"],
      "within_seconds": 1800,
      "reason": "A separate reviewed session is required between change creation and deployment.",
      "owner": "security",
      "approval": "CHANGE-1234"
    }],
    "budget_rules": [{
      "id": "deploy-budget",
      "capabilities": ["capability.orders-staging-deploy"],
      "max_count": 3,
      "window_seconds": 300,
      "scope": "resource",
      "reason": "The signed session deployment budget is exhausted.",
      "owner": "orders-platform",
      "approval": "CHANGE-1234"
    }]
  }
}
```

`scope` is `session` or `resource`. Composition is ordered: a prior pending or
successful capability followed by a configured current capability is denied
inside the window. Unknown capability references and unsafe/zero limits make
the source invalid before signing. Enforcement logic is code; business labels,
selectors, order rules, budgets, windows, profiles, owners and approvals are
signed data.

Each AgentGrant rule also requires `max_grants_per_intent`. In production the
shared MySQL transaction updates `bap_agent_intents` and inserts
`bap_agent_grants` atomically, so separate Service/STS replicas cannot mint past
the limit. The in-memory implementation provides the same semantics only for a
single development Service process.

## Test and produce evidence

From the repository root:

```powershell
# Company laptop with native Go; no container runtime required
.\Test-SessionCapabilities.ps1 -Runtime Native

# Equivalent isolated builds
.\Test-SessionCapabilities.ps1 -Runtime Docker
.\Test-SessionCapabilities.ps1 -Runtime Podman

# Full native gate (also invokes the session suite)
.\Test-MVP0.ps1 -Runtime Native
```

The focused command tests concurrent same-session access, separate-session
isolation, pending reservations, failure handling, signed budgets, intent
issuance exhaustion, grant replay and policy validation. On success it writes a
hash-bound JSON evidence manifest under `.bap/attestations/`. The manifest is
test evidence, not a cryptographic company attestation; have CI sign it and bind
it to the promoted artifact digest, identity, runner and change approval.

For a human acceptance test, open one fresh managed Claude session, perform the
configured protected action up to its session budget using a new explicit
intent for each one, and confirm the next action is denied with
`SESSION_BUDGET_EXCEEDED`. Open a second fresh session and confirm its first
action is evaluated independently. Then verify the central audit correlation
and retain the signed bundle version/digest plus the generated evidence file.

## Neutral accretion acceptance

The repository includes a deliberately neutral eight-turn scenario that begins
with profiling `data/dummy_customers.csv` and gradually asks Claude to create a
Python analysis, configuration, documentation, dependencies, and a Windows
runner. It contains no malicious or evasion wording. The canonical prompts are
in `testdata/session-accretion-prompts.md`.

Run the deterministic BAP observation first:

```powershell
.\Test-SessionAccretion.bat -Mode DirectBap
```

This sends the likely structured `Read` and `Write` operations directly through
normalization, signed local authorization, and the same session ledger. It
separates BAP behavior from model quality. It is observational: a printed `GAP`
is a discovered policy-coverage gap, not a passing security certification.

Run it through the native local model with:

```powershell
.\Test-SessionAccretion.bat -Mode NativeClaude
```

The launcher uses one explicit Claude session ID and resumes it for every turn.
The Edge intentionally retains the workload ledger across `SessionEnd`, because
each non-interactive `claude -p` process exits between resumable turns.

On the company laptop, first install the current managed artifacts as an
administrator, then run as the normal employee:

```powershell
.\Test-SessionAccretion.bat -Mode CompanySonnet
```

The company mode verifies the BAP managed-settings drop-in, opens the exact
checklist, and invokes `claude.cmd` with no parameters. Select Sonnet in the UI,
paste one turn at a time in the same conversation, and record each tool name,
decision/reason, review prompt, and whether the tool ran. It does not attempt to
scrape the company UI.

With signed policy version 9, the direct observation currently reports zero
capability assignments for this sequence. The policy maps protected API/MCP
operations but does not yet classify general file-read, source-generation,
dependency, or launcher-generation activity. Consequently, writing the batch
file is presently an ordinary `file.write` permit. This proves session tracking
works but also identifies a genuine policy-coverage gap. Do not claim this
scenario is controlled until a policy-owner-approved mapping and composition
decision are added and the observation shows the expected capability history.

A mature result does not have to deny Turn 8. The approved composition rule can
permit it, deny it, or require a user/security review. The current hook response
model supports an interactive `ask` decision, but BAP's session-policy schema
currently implements permit/deny only; review is therefore a follow-up product
decision rather than something inferred from prompt keywords.

## Boundary

This is deliberately session-scoped. Separate session IDs do not share an
inline ledger; enterprise cross-session detection belongs in central audit and
identity analytics. A local administrator who deliberately tampers with managed
software/state is outside the current workstation threat boundary. Production
still requires protected installation, OS ACLs, managed hooks, shared MySQL for
STS replicas, central audit, and controlled policy signing.
