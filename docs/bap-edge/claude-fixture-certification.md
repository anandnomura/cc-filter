# Exact Claude fixture certification

The modeled MVP-0 corpus is necessary but does not certify a particular Claude
Code or model release. Exact certification captures the schema and enforcement
result produced by the company-supported Claude Code and approved Sonnet
combination and binds it to one signed BAP policy version and digest. Additional
models can be added later but are not required by the current company gate.

## Privacy boundary

Capture is opt-in and occurs inside BAP Edge after normalization and local
policy evaluation. A fixture contains only:

- scenario, Claude Code version, model label, OS, hook event, and tool name;
- the JSON field/type shape and its SHA-256 digest;
- normalized action and boolean risk classifications;
- expected/actual allow or deny, reason code, and matched rule IDs; and
- signed policy version and rules digest.

It never stores session/workload/tool-use IDs, working directories, paths,
commands, prompts, queries, URLs, MCP argument values, content, credentials, or
model output. Unit tests inject distinctive secrets into every one of these
locations and require their absence from captured JSON.

Replay regenerates safe representative values from the recorded schema and
runs the current strict normalizer and signed local Cedar policy. Command
representatives come from matched central rule IDs. This verifies schema,
action, decision, and reason stability without retaining the original value.

## Capture a scenario

Use a dedicated test workspace. An allowed tool call can execute; only request
known-safe operations. For company Claude, use `-UseCompanyClaude` so the local
ccbridge variables are not installed:

```powershell
.\Capture-ClaudeFixtures.ps1 `
  -Runtime Docker `
  -UseCompanyClaude `
  -Scenario git-status-allow `
  -Model company-sonnet-ID `
  -ExpectedDecision allow `
  -Tools Bash `
  -Prompt 'Call Bash exactly once with this exact command: git status --short'
```

Additional models or safe scenarios may be captured later with their approved
labels. They are optional extensions to the current Sonnet compatibility gate.
Do not require a live denial fixture when the model can refuse before issuing a
tool call; deterministic Edge and policy tests cover denial enforcement.

If managed hooks are installed, reinstall the current Edge binary before
capture. The capture script fails when no updated Edge hook writes a fixture.

### Exact container-free company baseline

On a company test laptop using approved Go and no Docker/Podman, managed-only
hooks must not be active because the native capture launcher uses temporary
project-local hooks. If this is an administrator-owned test endpoint, run
`Install-ManagedSettings.ps1 -Undo` elevated and close every existing Claude
session first. Then build the current native binaries:

```powershell
.\Build-Bap.ps1 -Runtime Native
```

For a company launcher that supports only interactive use, run this one command
from normal PowerShell:

```powershell
.\Capture-CompanyClaudeFixtures.ps1 -Runtime Native
```

The helper asks for the Claude Code version once and launches the normal
company UI without arguments for one Sonnet compatibility case. Keep Sonnet
selected, paste the displayed `git status --short` prompt, wait for the tool
result, and exit Claude. Edge writes the fixture JSON automatically; do not
copy or convert the model's prose response.

Do not change models inside one capture session: the hook payload does not
expose that change.

The live gate does not require destructive or privileged-client fixtures.
Sonnet can refuse such a prompt before `PreToolUse`, leaving no hook payload to
capture. Deterministic normalizer, Cedar, and native Edge tests remain
authoritative for `git reset --hard` and manual-only database-client denial.

If the company requires an immutable model ID rather than the `sonnet` label,
pass it with `-Model` and use the same value in `-RequiredModels`. Do not invent
an identifier.

Review the privacy-safe JSON files under `.bap\captures`, then create and verify
the native manifest:

```powershell
.\Test-ClaudeFixtures.ps1 -Runtime Native -UpdateManifest
.\Test-ClaudeFixtures.ps1 -Runtime Native
.\Test-MVP0.ps1 -Runtime Native -RequireCompanyFixtures
```

## Review and create the manifest

Captures default to `.bap/captures`, which is ignored by Git. Review the JSON,
then create a hash manifest only after every required model and scenario is
present:

```powershell
.\Test-ClaudeFixtures.ps1 `
  -Runtime Docker `
  -UpdateManifest `
  -RequiredModels sonnet

.\Test-ClaudeFixtures.ps1 `
  -Runtime Docker `
  -RequiredModels sonnet
```

For a reviewed release artifact, use `-CaptureDirectory` with a repository
subdirectory such as `certifications/claude/release-2026-08`; the manifest and
fixtures can then be reviewed and versioned together. Do not promote captures
until privacy review passes.

Manifest verification fails on:

- a changed fixture hash or unlisted fixture;
- stale policy version/digest;
- expected/actual decision mismatch;
- unknown or malformed tool schema;
- normalization or local-policy replay drift;
- a missing required model family for any captured required scenario; or
- different tool/schema/action/decision/reason results when multiple models
  are explicitly required.

## Complete MVP-0 gate

The standard test reports company fixtures as pending when none exist. The
company admission gate requires them:

```powershell
.\Test-MVP0.ps1 -Runtime Docker -RequireCompanyFixtures
```

Without `-RequireCompanyFixtures`, all model-independent gates still run and
the script prints an explicit pending result. This prevents lab development
from pretending to be company model certification.
