# Shadow observation and policy recommendations

Shadow mode evaluates the normal signed policy but permits ordinary policy
denials without telling Claude what BAP would have decided. It is a bounded
pilot tool for discovering real tool/intent patterns before enforcement. It is
not a production security mode and never activates suggested policy.

## Enable or disable

The control is in the administrator-owned policy source, not endpoint YAML.
To enable a time-bounded pilot, increment `version` and set:

```json
"enforcement_mode": "shadow",
"shadow_expires_at": "2026-09-16T00:00:00Z"
```

Run the policy tests, activate/sign the bundle, and deploy it normally. The
expiry must be after activation and no later than the bundle expiry. When it is
reached, every Edge automatically behaves as `enforce` without needing Service
connectivity.

To turn shadow off, increment `version`, set:

```json
"enforcement_mode": "enforce"
```

and remove `shadow_expires_at`. Production Service startup also rejects every
bundle whose signed mode is not `enforce`; setting
`BAP_DEPLOYMENT_MODE=production` is therefore the MVP go-live interlock.

The repository policy defaults to `enforce`. Do not use an endpoint flag or
environment override to weaken it.

## What remains enforced

Shadow changes only an ordinary valid policy denial into an effective allow.
The following remain fail closed:

- cc-filter secret blocking and redacted-read handling;
- protected, outside-workspace, and BAP/Claude security-control paths;
- exfiltration and obfuscation classifications;
- malformed/unknown hook input, stale/invalid policy, audit-spool failure and
  the signed kill switch;
- AgentGrant issuance, exact resource/audience binding, one-use consumption and
  API/MCP resource PEP enforcement;
- session composition/budget denials in this MVP.

Destructive or privileged operations may be observed as shadow allows only on
the explicitly approved pilot endpoints. Keep the discovery pilot on sandbox
resources unless the resource owner has accepted that risk.

Prompt rules still classify locally and store only matching signed rule IDs.
In shadow mode their advisory text is not returned to Claude, preventing the
observation from changing model behavior. Raw prompt and command text are not
written to operational logs or recommendation reports.

## Observe and verify

```powershell
.\Show-BapStatus.ps1 -Runtime Native
.\Watch-BapLogs.ps1 -Runtime Native -Component All -Tail 100
.\View-AuditTrail.ps1 -Runtime Native -Timeline -Last 100
```

For a shadow override, the audit timeline must show `Evaluated=deny`,
`Decision=allow`, `Mode=shadow`, the original evaluated reason, and an effective
`SHADOW_ALLOW`. A hard boundary must show deny for both decisions.

## Native local testing and pilot sessions

To enable developers to run realistic pilot sessions and discover missing tool rules without being blocked, the native local launcher starts in **shadow observation mode** by default:

```powershell
.\Start-BapNativeLocal.bat -UseCompanyClaude
```

When started natively:
1. **Ephemeral signed shadow bundle:** `Start-BapNativeLocal` automatically provisions an active signed bundle with `enforcement_mode: "shadow"` and a 14-day expiry for that run.
2. **Commands execute uninterrupted:** Unclassified developer commands (e.g., running build scripts, folder comparisons, Python scripts) are permitted (`SHADOW_ALLOW`) and recorded in the audit log.
3. **Hard boundaries remain fail-closed:** Accessing protected files (such as `.env` or credentials), directory traversals outside the authorized workspace root, and attempts to modify security controls remain strictly denied.
4. **Automatic snapshot on exit:** When Claude exits, `Start-BapNativeLocal` automatically captures a shadow log snapshot directly under `.bap\shadow-logs\<timestamp>-native\`.
5. **Strict enforcement override:** If you want to test with strict enforcement instead of shadow observation, pass `-Enforce`:
   ```powershell
   .\Start-BapNativeLocal.bat -Enforce -UseCompanyClaude
   ```

## Analyze a directory

Create a clearly named snapshot from the latest Native run or the currently
running Docker/Podman environment:

```powershell
.\Collect-ShadowLogs.ps1 -Runtime Native
# or: -Runtime Docker / -Runtime Podman
```

Each snapshot is stored under `.bap\shadow-logs\<timestamp>-<runtime>\` and
contains two files: `service-audit.jsonl` and `edge-observability.jsonl`.
`collection-manifest.json` records their origin. The analyzer reads **all
`*.jsonl` files recursively** beneath the directory, including all snapshots;
it deduplicates repeated events and ignores records that are not shadow
overrides. When the same operation exists in Edge and Service logs, the signed
Service event is authoritative and the Edge copy is not counted twice. You do
not select just one file. Trace matching also requires the same action and tool,
so another authorization span sharing a W3C trace remains visible.

File count is determined by collection frequency, not by how many operations
the users perform. One collection creates two JSONL files plus one manifest. A
single end-of-week collection from each of five standalone Native test
environments therefore produces 10 JSONL files and five manifests. Place all
five snapshot directories under the same `.bap\shadow-logs` parent and analyze
them together. In a centrally operated pilot, export central Service audit once
and add each endpoint's Edge JSONL; duplicate Service snapshots are safe but
waste storage.

Sessions and people are never merged: operation outcomes correlate by
`session_id`, `workload_id`, and `tool_use_id`, while recommendations group on
observed principal, action, tool, evaluated reason, and the exact privacy-safe
target key. Human reviewers—not the model—map observed principals to approved
IAM groups.

Python 3 is the only analyzer prerequisite. With the default locations, run:

```powershell
.\Analyze-ShadowLogs.ps1
```

This writes `.bap\shadow-analysis\shadow-suggestions.json`. To use explicit
locations:

```powershell
.\Analyze-ShadowLogs.ps1 `
  -InputDirectory '.\collected-bap-jsonl' `
  -OutputPath '.\shadow-suggestions.json' `
  -MinCount 2
```

The analyzer recursively correlates shadow decisions and tool outcomes,
deduplicates records, groups repeated action/tool/reason/identity/resource
classes, counts prompt-rule signals, and hashes every input file into the
report manifest. Command values represented by hashes remain hashes. By
default, its dependency-free `categorical_density_v1` model learns categorical
frequencies from the supplied corpus and ranks candidates using explainable
novelty, recurrence, cross-session evidence, and observed outcomes. Use
`-DisableML` to emit deterministic counts without learned ranking.

Each recommendation includes a `proposed_review_scope` with the observed
action, tool and resource class, the identity that must be checked against
authoritative IAM, and the required approval/test controls. It is a review
candidate—not generated Cedar and not permission to execute.

Hashed command and outside-workspace targets retain their complete stable hash
as `target_key`. Consequently, two unrelated commands are never combined merely
because both are shell commands. The plaintext value is still not disclosed.

Every result is `human_review_required`. It deliberately emits neither Cedar
nor an activatable bundle. Reviewers must verify the user's authoritative IAM
group, resource owner, A2P/workflow needs, failures and counterexamples; draft a
narrow group/resource rule; add positive and bypass tests; increment the policy
version; and complete the normal approval/signing process.

This is an MVP statistical-learning model, not a production access model. It
uses only the files supplied for that run, so larger representative samples and
reviewer feedback are needed before treating its rank as meaningful. It stays
offline from activation and never infers or assigns access roles from behavior.

To see the output without running a shadow pilot, analyze the sanitized sample:

```powershell
.\Analyze-ShadowLogs.ps1 `
  -InputDirectory '.\examples\shadow-analysis' `
  -OutputPath '.\.bap\shadow-analysis\sample-suggestions.json'
```

## Promote a recommendation to policy

There is deliberately no automatic conversion button. For each candidate:

1. Recover the intended operation from the pilot owner and verify the observed
   principal against authoritative IAM. A target hash alone is not approval.
   Reviewers can verify suspected commands against the observed `target_key`
   using:
   ```powershell
   .\Find-ShadowCandidateHash.ps1 -Command "git status" -TargetHash "command-sha256:..."
   ```
   Specify exactly one of `-Command` or `-OutsideWorkspacePath`. A match exits
   successfully; a mismatch prints `Matches: False` and exits nonzero so review
   automation cannot silently accept it.
2. Have the resource owner and security reviewer decide whether it should be a
   narrow permit, remain manual-only, or become an explicit forbid.
3. Edit `bap-service/policies/edge-policy-source.json`. For a normal shell
   candidate, add a precise `command_rules` entry with executable, subcommand,
   bounded argument patterns, profiles, owner, approval, and
   `effect: eligible-for-permit`. Network, MCP, prompt-intent, AgentGrant and
   session candidates belong in their corresponding structured registries.
4. Edit `bap-service/policies/agent-tools.cedar` only when the authorization
   semantics or action family changes. A normal approved command already flows
   through `resource.shellApproved`, so registry-only changes are preferred.
5. Increment the source's top-level `version`; add positive, negative, and
   bypass cases; run `Test-PolicyRollout.ps1`, `Test-ShadowMode.ps1`, and
   `Test-MVP0.ps1`.
6. Run `bap-service policy activate` in the controlled signing job. Deploy the
   resulting `active-policy-bundle.json` and public verification key to BAP
   Service; never deploy the bundle private key to runtime instances.
7. Canary the new higher version, inspect audit, then promote it. For go-live,
   the signed source must explicitly use `enforcement_mode: enforce`.

Exact activation environment variables and the native command are in the
[deployment guide](../bap-system/deployment-guide.md#policy-activation-and-runtime-separation).
