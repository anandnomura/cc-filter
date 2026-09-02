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

## Analyze a directory

Python 3 is the only analyzer prerequisite. Point it at a directory containing
retained Edge or Service JSONL files:

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

Every result is `human_review_required`. It deliberately emits neither Cedar
nor an activatable bundle. Reviewers must verify the user's authoritative IAM
group, resource owner, A2P/workflow needs, failures and counterexamples; draft a
narrow group/resource rule; add positive and bypass tests; increment the policy
version; and complete the normal approval/signing process.

This is an MVP statistical-learning model, not a production access model. It
uses only the files supplied for that run, so larger representative samples and
reviewer feedback are needed before treating its rank as meaningful. It stays
offline from activation and never infers or assigns access roles from behavior.
