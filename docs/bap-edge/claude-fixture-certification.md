# Exact Claude fixture certification

The modeled MVP-0 corpus is necessary but does not certify a particular Claude
Code or model release. Exact certification captures the schema and enforcement
result produced by the company-supported Claude Code, Sonnet, and Opus
combinations and binds them to one signed BAP policy version and digest.

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

Repeat the identical scenario with the approved Opus identifier:

```powershell
.\Capture-ClaudeFixtures.ps1 `
  -Runtime Docker `
  -UseCompanyClaude `
  -Scenario git-status-allow `
  -Model company-opus-ID `
  -ExpectedDecision allow `
  -Tools Bash `
  -Prompt 'Call Bash exactly once with this exact command: git status --short'
```

Capture both models for every reviewed scenario. Use `-Tools Read`, `Write`,
`Edit`, `Glob`, `Grep`, `WebSearch`, or the exact company-enabled tool list as
appropriate. Denial scenarios are captured the same way with
`-ExpectedDecision deny`.

If managed hooks are installed, reinstall the current Edge binary before
capture. The capture script fails when no updated Edge hook writes a fixture.

## Review and create the manifest

Captures default to `.bap/captures`, which is ignored by Git. Review the JSON,
then create a hash manifest only after every required model and scenario is
present:

```powershell
.\Test-ClaudeFixtures.ps1 `
  -Runtime Docker `
  -UpdateManifest `
  -RequiredModels sonnet,opus

.\Test-ClaudeFixtures.ps1 `
  -Runtime Docker `
  -RequiredModels sonnet,opus
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
- a missing required model family for any scenario; or
- different tool/schema/action/decision/reason results across models.

## Complete MVP-0 gate

The standard test reports company fixtures as pending when none exist. The
company admission gate requires them:

```powershell
.\Test-MVP0.ps1 -Runtime Docker -RequireCompanyFixtures
```

Without `-RequireCompanyFixtures`, all model-independent gates still run and
the script prints an explicit pending result. This prevents lab development
from pretending to be company model certification.
