All three points are now confirmed and documented.

### 1. Shadow files and directory

Run:

```powershell
.\Collect-ShadowLogs.ps1 -Runtime Native
```

Use `Docker` or `Podman` as needed.

It creates:

```text
.bap\shadow-logs\<timestamp>-<runtime>\
    service-audit.jsonl
    edge-observability.jsonl
    collection-manifest.json
```

Then run:

```powershell
.\Analyze-ShadowLogs.ps1
```

The analyzer reads all `*.jsonl` files recursively under `.bap\shadow-logs`, across every collected snapshot. It deduplicates events and ignores non-shadow decisions.

The report is written to:

```text
.bap\shadow-analysis\shadow-suggestions.json
```

See [shadow collection documentation](C:/Users/User/pyprj/bap-edge/docs/bap-edge/shadow-mode.md:81).

### 2. MVP and security readiness

The shadow-learning feature is ready for a controlled internal MVP pilot. The tested scope passes Native collection, Docker collection, Docker security tests, cc-filter redaction, signed policy handling, automatic expiry, production rejection, session controls, and ML analysis.

I cannot honestly say “there are no security risks.” Important boundaries remain:

- Shadow mode intentionally allows ordinary policy denials. Use it only on approved pilot/sandbox resources.
- Production mode rejects shadow bundles and requires enforcement mode.
- Secrets, protected paths, session controls, malformed input, expired policies, kill switch, AgentGrant and resource PEP controls remain enforced.
- ML cannot authorize anything.
- Suggestions may be inaccurate with small or biased datasets.
- Enterprise production still requires company identity lifecycle, signed releases, HA/DR, SIEM retention, representative Sonnet/tool certification, and independent security testing.

The repository is therefore internal-MVP ready, but not yet an enterprise production control plane. This distinction is recorded in [production readiness](C:/Users/User/pyprj/bap-edge/docs/bap-edge/production-readiness.md:3).

### 3. ML works and produces recommendations

The tested sample produced:

```text
Status:             human_review_required
Recommendation:     Consider a narrowly scoped permit candidate
Effect:             permit_candidate
PrincipalToVerify:  pilot-developer
Action:             command.execute
Tool:               Bash
ResourceClass:      command-sha256
Model:              categorical_density_v1
Priority:           4.1699
AutoActivation:     False
```

The full evidence report is currently at:

```text
.bap\attestations\shadow-ml-sample-20260902T223919Z.json
```

Run the complete test yourself:

```powershell
.\Test-ShadowMode.ps1 -Runtime Native
```

Or:

```powershell
.\Test-ShadowMode.ps1 -Runtime Docker
.\Test-ShadowMode.ps1 -Runtime Podman
```

It generates a fresh evidence file under `.bap\attestations`.

To inspect a recommendation without waiting for real pilot traffic:

```powershell
.\Analyze-ShadowLogs.ps1 `
  -InputDirectory '.\examples\shadow-analysis' `
  -OutputPath '.\.bap\shadow-analysis\sample-suggestions.json'

Get-Content '.\.bap\shadow-analysis\sample-suggestions.json'
```

The scripts and current commands are in [Build/test commands](C:/Users/User/pyprj/bap-edge/README.md:169) and [shadow testing](C:/Users/User/pyprj/bap-edge/docs/bap-edge/testing.md:97).

You should analyze all users and all files together. Do not process them one by one.

### File counts

File count depends on how often you collect, not how long people work.

One collection creates:

```text
.bap\shadow-logs\<timestamp>-<runtime>\
    service-audit.jsonl
    edge-observability.jsonl
    collection-manifest.json
```

Examples:

- One developer, one collection after a week: 2 JSONL files and 1 manifest.
- Five developers, each collecting once: 10 JSONL files and 5 manifests.
- Centralized pilot: ideally 1 central Service audit export plus 5 Edge logs: 6 JSONL files.

Put every snapshot beneath:

```text
.bap\shadow-logs\
```

Then run once:

```powershell
.\Analyze-ShadowLogs.ps1
```

The analyzer:

- Recursively reads all `*.jsonl` files.
- Correlates outcomes using `session_id + workload_id + tool_use_id`.
- Keeps different Claude sessions and employees separate.
- Deduplicates repeated snapshots.
- Avoids double-counting the same decision from Edge and Service.
- Prefers the signed Service event when both copies exist.
- Groups only the same principal, action, tool, reason and exact privacy-safe target hash.

Five different users are not automatically treated as one authorization group. If several users show the same need, a reviewer must verify their authoritative IAM groups before creating a shared Cedar/profile rule.

Documentation: [multi-user shadow processing](C:/Users/User/pyprj/bap-edge/docs/bap-edge/shadow-mode.md:90).

### Turning an ML recommendation into policy

The ML result is evidence, not a policy. There is intentionally no automatic promotion.

1. Open the report:

```powershell
Get-Content '.\.bap\shadow-analysis\shadow-suggestions.json'
```

2. For a candidate, verify:

- What actual operation produced the hash.
- The employee/device identity.
- The employee’s authoritative IAM group.
- Whether the operation succeeded.
- Resource-owner approval.
- Whether it should be allowed, manual-only, or forbidden.

3. Update the structured policy source:

```text
bap-service\policies\edge-policy-source.json
```

For a normal shell command, add a precise `command_rules` entry, for example:

```json
{
  "id": "command.example.inspect",
  "executable": "example",
  "subcommand": "inspect",
  "effect": "eligible-for-permit",
  "argument_patterns": ["--read-only"],
  "profiles": ["standard-developer"],
  "owner": "developer-platform",
  "approval": "CHANGE-12345"
}
```

Do not use `.*` for an allowed command. Bound the executable, subcommand and arguments.

4. Usually, an ordinary command does not require a Cedar change. Existing Cedar permits it only when the registry sets `resource.shellApproved`.

Change this only when introducing a new authorization behavior or action family:

```text
bap-service\policies\agent-tools.cedar
```

5. Increment the top-level policy `version`.

6. Add positive, negative and bypass tests, then run:

```powershell
.\Test-PolicyRollout.ps1 -Runtime Native
.\Test-ShadowMode.ps1 -Runtime Native
.\Test-MVP0.ps1 -Runtime Native
```

Replace `Native` with `Docker` or `Podman` when appropriate.

7. Sign the reviewed policy in the controlled signing environment:

```powershell
$env:BAP_POLICY_PATH = 'C:\bap-policy\agent-tools.cedar'
$env:BAP_BUNDLE_SOURCE_PATH = 'C:\bap-policy\edge-policy-source.json'
$env:BAP_BUNDLE_PRIVATE_KEY_PATH = 'C:\signer-secrets\bundle-private.pem'
$env:BAP_ACTIVE_POLICY_BUNDLE_PATH = 'C:\bap-release\active-policy-bundle.json'

dist\bap-service-windows-amd64.exe policy activate
```

8. Deploy to BAP Service:

- `active-policy-bundle.json`
- Bundle public verification key

Do not deploy the private signing key to the runtime Service.

9. Canary the new higher version, inspect audit results, and then promote it. The go-live bundle must explicitly contain:

```json
"enforcement_mode": "enforce"
```

The complete checklist is in [Promote a recommendation to policy](C:/Users/User/pyprj/bap-edge/docs/bap-edge/shadow-mode.md:158) and the signing command is in [Policy activation and runtime separation](C:/Users/User/pyprj/bap-edge/docs/bap-system/deployment-guide.md:131).