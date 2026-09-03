# Command and flag reference

Run commands from the repository root. Prefer an explicit `-Runtime` in CI and
company evidence. Supported values are `Native`, `Docker`, and `Podman` where
listed. `Auto` resolves one runtime at the start of the complete MVP workflow.
Container lifecycle scripts do not accept `Native`; use
`Start-BapNativeLocal.ps1` for native operation.

The canonical build selector is `-Target`. Older `-Targets` on Edge builds and
`-NativeTarget` on the Service wrapper remain compatibility aliases. New
commands and documentation use `-Target`.

## Build, start, and test

| Goal | Command | Optional flags |
|---|---|---|
| Complete build | `Build-Bap.ps1` | `-Runtime`, `-SeparateAgentSTS` |
| Edge build | `Build-BapEdge.ps1` | `-Runtime`, `-Target Windows|All`, `-Version` |
| Native Edge build | `Build-BapEdge-Native.ps1` | `-Target Windows|All`, `-OutputPath`, `-Version` |
| Service build/image | `Build-BapService.ps1` | `-Runtime`, `-Target Windows|Linux|All`, `-Architecture amd64|arm64|All`, `-Tag`, `-Version`, `-BuildImage`, `-RuntimeImage`, `-SeparateAgentSTS`, `-AgentSTSTag` |
| Native Service build | `Build-BapService-Native.ps1` | `-Target`, `-Architecture`, `-Version`, `-SeparateAgentSTS` |
| Start container Service | `Start-Bap.ps1` | `-Runtime Auto|Docker|Podman`, database DSN/TLS flags |
| Start native local stack | `Start-BapNativeLocal.ps1` | `-Rebuild`, `-VerifyOnly`, `-Port`, `-UseCompanyClaude` |
| Stop container Service | `Stop-Bap.ps1` | `-Runtime Auto|Docker|Podman` |
| Complete MVP gate | `Test-MVP0.ps1` | `-Runtime`, `-NativePort`, `-RequireCompanyFixtures`, `-RequiredModels` |
| Policy rollout gate | `Test-PolicyRollout.ps1` | `-Runtime Auto|Native|Docker|Podman` |
| Shadow security/ML gate | `Test-ShadowMode.ps1` | `-Runtime Auto|Native|Docker|Podman` |
| Session gate | `Test-SessionCapabilities.ps1` | `-Runtime`, `-AttestationPath` |
| AgentGrant gate | `Test-AgentGrant.ps1` | `-Runtime` |
| Resource PEP build | `Build-ResourcePEPs.ps1` | `-Runtime`, `-Target`, `-Architecture`, `-MCPTag`, `-APITag` |
| Resource PEP test | `Test-ResourcePEPs.ps1` | `-Runtime` |
| Protected-resource demo | `Demo-ResourcePEPs.ps1` | `-Runtime`, `-Rebuild`, Service/API/MCP/mock port flags |

`Demo-Bap.ps1 -SkipBuild` and `-KeepRunning` apply only to Docker/Podman.
Native demo mode rejects those flags instead of silently ignoring them.

## Shadow and operations

| Goal | Command | Flags |
|---|---|---|
| Collect one snapshot | `Collect-ShadowLogs.ps1` | `-Runtime`, `-OutputDirectory` |
| Analyze every snapshot | `Analyze-ShadowLogs.ps1` | `-InputDirectory`, `-OutputPath`, `-MinCount`, `-DisableML` |
| Verify a candidate target hash | `Find-ShadowCandidateHash.ps1` | exactly one of `-Command` or `-OutsideWorkspacePath`; optional `-TargetHash` |
| View signed audit | `View-AuditTrail.ps1` | `-Runtime`, `-VerifyOnly`, `-Timeline`, `-Details`, `-SessionID`, `-Last` |
| Watch logs | `Watch-BapLogs.ps1` | `-Runtime`, `-Component All|Edge|Service`, `-Tail`, `-NoFollow` |
| Show status | `Show-BapStatus.ps1` | `-Runtime`, `-StateDirectory` |

Default shadow locations are `.bap\shadow-logs` and
`.bap\shadow-analysis\shadow-suggestions.json`. Process the full directory in
one analyzer run, not one JSONL file at a time.

## Company Claude

All three entry points (`Start-BapNativeLocal.ps1`, `Start-LocalClaude.ps1`, and
`Capture-ClaudeFixtures.ps1`) use the same company flags:

- `-UseCompanyClaude` selects company authentication and defaults to an
  interactive launch with no CLI arguments;
- `-CompanyCliArguments` explicitly opts into CLI arguments, only when the
  company wrapper supports them;
- `-InteractiveClaude` explicitly selects interactive behavior but is normally
  unnecessary because it is already the company default.

The last two flags are mutually exclusive and both require
`-UseCompanyClaude`. The older capture-only `-Interactive` spelling remains an
alias for `-InteractiveClaude`.

Automated local-model input also supports `-Model`, `-Tools`, `-SystemPrompt`,
`-Prompt`, `-Print`, `-InputFile`, `-SequentialPrompts`, and
`-SequentialSessionID`. Fixture commands and their required values are covered
by the [fixture guide](claude-fixture-certification.md).

## Prevent command drift

Run after changing a script or documented command:

```powershell
.\Test-ScriptContracts.ps1
```

It parses every root PowerShell script, checks documented same-line flags,
verifies build compatibility aliases, and confirms batch wrappers forward all
arguments and return exit codes. `Test-MVP0.ps1` runs it automatically.
