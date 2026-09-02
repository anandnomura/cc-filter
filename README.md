# BAP System

This repository keeps the upstream-compatible cc-filter source at root and adds
the BAP System components beside it: BAP Edge, BAP Service/Agent STS, a Spring
Cloud API Gateway PEP, and a Go MCP PEP. BAP Service distributes signed,
versioned rule bundles; BAP Edge verifies them, classifies traffic, evaluates
Cedar locally, and enforces each decision. Escalated protected-resource
operations use the short-lived, one-use
[AgentGrant/Agent STS path](docs/bap-edge/agent-grant-sts.md).

**Start here:** [BAP System map](docs/bap-system/README.md),
[BAP Edge operator guide](docs/bap-edge/README.md), and
[resource PEP commands](docs/bap-system/resource-peps.md). Production operators
should use the [complete deployment guide](docs/bap-system/deployment-guide.md),
and testers should use the
[protected-resource acceptance guide](docs/bap-system/protected-resource-acceptance.md).
Exact start, verify, stop and rollback procedures for every runtime are in the
[operator runbooks](docs/runbooks/README.md).

Run `Test-PolicyRollout.ps1` for the focused command/bypass and signed rollout
gate, or `Test-MVP0.ps1` for the complete certification; MVP-0 invokes the
focused gate automatically.

Use `Show-BapStatus.ps1` for a read-only control-plane/Edge posture view. Exact
Claude Code/model schema capture and replay are documented in the
[fixture certification guide](docs/bap-edge/claude-fixture-certification.md).

The operator guide includes the complete Docker/Podman build, HTTPS and key
initialization, Windows managed-settings installation, standard-user bypass
test, Cedar/AgentGrant acceptance test, audit verification, network deployment,
and fork synchronization procedures. Containerized development builds do not
need local Go; source-only company Windows builds use the documented approved
native Go compiler path.

For a controlled company build using internal, digest-pinned base images and a
clean source commit, use `Build-CompanyArtifacts.ps1` and the
[company build guide](docs/bap-edge/company-windows-build.md).
If containers are unavailable, `Build-BapEdge.ps1 -Runtime Native` builds the
Windows Edge and `Build-BapService-Native.ps1 -Target Windows` builds the
Windows Service EXE using an installed Go 1.23.12+ toolchain. Use `-Target Linux`
to cross-compile the separately deployable Linux Service binary.
The complete per-component command/output table is in the
[operator compilation quick reference](docs/bap-edge/README.md#compilation-quick-reference).
For a container-free local test with both Windows EXEs and non-managed Claude
hooks, run `Start-BapNativeLocal.bat` and follow the
[native local testing guide](docs/bap-edge/native-local-testing.md).

## Build/test commands

For the complete Go/Docker/Podman × Windows/Linux scenario matrix, prerequisites,
expected artifacts, and acceptance checks, use the
[authoritative build and test matrix](docs/bap-edge/deployment-test-matrix.md).

Run these from the repository root.

### Build

```powershell
# Build both Windows EXEs with installed Go (no Docker/Podman)
.\Build-Bap.ps1 -Runtime Native

# Build only BAP Edge for Windows AMD64
.\Build-BapEdge.ps1 -Runtime Native

# Build BAP Edge for Windows AMD64 plus Linux AMD64 and ARM64
.\Build-BapEdge.ps1 -Runtime Native -Targets All

# Build only BAP Service as a Windows EXE
.\Build-BapService-Native.ps1 -Target Windows

# Opt in to a second STS-only EXE; combined BAP Service remains the default
.\Build-BapService-Native.ps1 -Target Windows -SeparateAgentSTS

# Cross-compile BAP Service for Linux AMD64 from Windows Go
.\Build-BapService-Native.ps1 -Target Linux -Architecture amd64

# Cross-compile BAP Service for Linux AMD64 and ARM64
.\Build-BapService-Native.ps1 -Target Linux -Architecture All

# Build BAP Service for Windows AMD64 plus Linux AMD64 and ARM64
.\Build-BapService-Native.ps1 -Target All -Architecture All

# Automatically use Podman/Docker, or fall back to native Go
.\Build-Bap.ps1 -Runtime Auto

# Build the development artifacts with Docker
.\Build-Bap.ps1 -Runtime Docker

# Build combined Service plus a separate STS-only OCI image
.\Build-Bap.ps1 -Runtime Docker -SeparateAgentSTS

# Build the development artifacts with Podman
.\Build-Bap.ps1 -Runtime Podman

# Build only the BAP Service OCI image
.\Build-BapService.ps1 -Runtime Docker -Tag bap-service:local

# Build only BAP Edge with Docker, including Windows/Linux targets
.\Build-BapEdge.ps1 -Runtime Docker -Targets All

# Build the Spring API PEP and Go MCP PEP natively
.\Build-ResourcePEPs.ps1 -Runtime Native

# Build both resource PEP OCI images
.\Build-ResourcePEPs.ps1 -Runtime Docker
.\Build-ResourcePEPs.ps1 -Runtime Podman

# Controlled company release: clean Git tree and approved digest-pinned images
.\Build-CompanyArtifacts.ps1 -Runtime Docker -Version '1.0.0' -Registry 'registry.company.example/security' -BuildImage 'registry.company.example/build/golang@sha256:REPLACE_WITH_APPROVED_DIGEST' -RuntimeImage 'registry.company.example/runtime/debian@sha256:REPLACE_WITH_APPROVED_DIGEST'
```

Native outputs are written under `dist\`. Linux binaries have no `.exe` suffix
and cannot run on Windows. Native Go compilation does not create an OCI image.
The complete artifact/output matrix is in the
[operator compilation quick reference](docs/bap-edge/README.md#compilation-quick-reference).

### Test and launch

Choose the launcher that matches the hook installation. Do not replace either
launcher with a bare `claude` command.

```powershell
# Unmanaged native hooks + local model; only when managed hooks are NOT installed
.\Start-BapNativeLocal.bat -Rebuild

# Local model without rebuilding existing EXEs
.\Start-BapNativeLocal.bat

# Unmanaged native hooks + company-authenticated interactive Claude Code;
# the company launcher receives no command-line arguments
.\Start-BapNativeLocal.bat -UseCompanyClaude

# Managed hooks + local model: build current Docker artifacts
.\Build-Bap.ps1 -Runtime Docker

# Managed hooks + local model: run once as Administrator after Edge changes
.\Install-ManagedSettings.ps1 -Runtime Docker

# Administrator undo: remove only the BAP managed-settings drop-in
.\Install-ManagedSettings.ps1 -Undo

# Preview the undo target without changing anything
.\Install-ManagedSettings.ps1 -Undo -WhatIf

# Managed hooks + local model: normal non-admin launch (start ccbridge first)
.\start-local-claude.bat -Runtime Docker -Model qwen-1.5b-local

# Verify Service, Edge, policy, commands, and prompt classifier without Claude
.\Start-BapNativeLocal.bat -VerifyOnly

# Verify and view the latest native signed audit as a readable decision timeline
.\View-AuditTrail.ps1 -Runtime Native -VerifyOnly
.\View-AuditTrail.ps1 -Runtime Native -Timeline -Last 30

# Show exact IDs/targets for one session copied from the timeline
.\View-AuditTrail.ps1 -Runtime Native -Timeline -SessionID 'COPY-SESSION-ID' -Details

# Native control-plane, policy lease, Edge state, and audit queue status
.\Show-BapStatus.ps1 -Runtime Native

# Live combined Edge + Service logs; Ctrl+C stops watching
.\Watch-BapLogs.bat -Runtime Native
.\Watch-BapLogs.ps1 -Runtime Docker -Component All -Tail 100
.\Watch-BapLogs.ps1 -Runtime Podman -Component Service -Tail 100

# Print recent logs once (useful in CI or when reporting an error)
.\Watch-BapLogs.ps1 -Runtime Native -NoFollow -Tail 100

# Company Windows/native AgentGrant acceptance: rebuild, issue, consume once,
# reject replay, and verify signed audit (no Docker, Podman, or Claude needed)
.\Start-BapNativeLocal.ps1 -VerifyOnly -Rebuild -Port 18443

# Container-free company/native MVP-0A gate
.\Test-MVP0.ps1 -Runtime Native

# Use another Service port if 8443 is occupied
.\Start-BapNativeLocal.bat -Port 18443

# Override the local model identifier when necessary
.\Start-BapNativeLocal.bat -Model claude-3-5-sonnet-20241022

# Non-interactive local classifier smoke test
.\Start-BapNativeLocal.bat -Print -Prompt "Please connect to the MySQL orders database and reindex it"

# Container-backed functional test
.\Test-Bap.ps1 -Runtime Docker

# Focused signed-policy rollout and bypass corpus
.\Test-PolicyRollout.ps1 -Runtime Docker

# Complete MVP-0 certification gate
.\Test-MVP0.ps1 -Runtime Docker

# Focused AgentGrant/STS + gateway security transaction
.\Test-AgentGrant.ps1 -Runtime Native
.\Test-AgentGrant.ps1 -Runtime Docker
.\Test-AgentGrant.ps1 -Runtime Podman

# Concurrent session-capability, composition, budget, and intent-accretion gate;
# writes a hash-bound evidence manifest under .bap\attestations
.\Test-SessionCapabilities.ps1 -Runtime Native
.\Test-SessionCapabilities.ps1 -Runtime Docker
.\Test-SessionCapabilities.ps1 -Runtime Podman

# Neutral eight-turn accretion observation directly against BAP (deterministic)
.\Test-SessionAccretion.bat -Mode DirectBap

# Run the same turns through the local Claude model and native BAP processes
.\Test-SessionAccretion.bat -Mode NativeClaude

# Company Sonnet: verifies managed hooks, opens the exact prompt checklist,
# and starts the company's interactive claude.cmd with no CLI arguments
.\Test-SessionAccretion.bat -Mode CompanySonnet

# Resource-specific API and MCP PEP tests
.\Test-ResourcePEPs.ps1 -Runtime Native
.\Test-ResourcePEPs.ps1 -Runtime Docker
.\Test-ResourcePEPs.ps1 -Runtime Podman

# Complete AgentGrant -> PEP -> protected-resource proof
.\Demo-ResourcePEPs.ps1 -Runtime Native -Rebuild
.\Demo-ResourcePEPs.ps1 -Runtime Docker -Rebuild
.\Demo-ResourcePEPs.ps1 -Runtime Podman -Rebuild
```

See [AgentGrant STS: prove it works on a company Windows laptop](docs/bap-edge/agent-grant-sts.md#prove-agentgrant-works-on-a-company-windows-laptop)
for the exact Agent STS assertions. The runnable API and MCP boundaries are in
the [resource PEP guide](docs/bap-system/resource-peps.md).
Session-aware rule configuration, multi-Claude concurrency behavior, human
acceptance, and evidence handling are in the
[session capability controls guide](docs/bap-edge/session-capability-controls.md).
The neutral CSV-to-Python-to-batch scenario, expected observations, and current
policy boundary are documented in the same guide under
[neutral accretion acceptance](docs/bap-edge/session-capability-controls.md#neutral-accretion-acceptance).

### Capture and certify company Claude fixtures without containers

Use this sequence on the administrator-owned company test laptop when Docker
and Podman are unavailable. `Scenario` is the stable test-case name; `Prompt`
is the instruction that should cause Claude to request one Bash tool call.

First, run this once in an elevated PowerShell window if BAP managed settings
are installed, and then close every existing Claude session:

```powershell
cd C:\Users\User\pyprj\bap-system
.\Install-ManagedSettings.ps1 -Undo
```

Run everything below in a normal, non-administrator PowerShell window:

```powershell
cd C:\Users\User\pyprj\bap-system

# Build the latest Windows EXEs
.\Build-Bap.ps1 -Runtime Native

# Recommended company workflow: one command, normal interactive Claude UI
.\Capture-CompanyClaudeFixtures.ps1 -Runtime Native

# Review the privacy-safe captures before creating the certification manifest
Get-ChildItem .\.bap\captures\*.json | Select-Object Name,Length,LastWriteTime

# Create the manifest, independently verify it, then run the strict native gate
.\Test-ClaudeFixtures.ps1 -Runtime Native -UpdateManifest
.\Test-ClaudeFixtures.ps1 -Runtime Native
.\Test-MVP0.ps1 -Runtime Native -RequireCompanyFixtures
```

The interactive helper asks for the Claude Code version once. It then opens the
company Claude UI once for Sonnet and passes **no command-line arguments to Claude**.
Keep Sonnet selected, paste the displayed prompt, wait for the requested tool result, and exit
Claude. Do not copy Claude's prose response into a file: BAP Edge automatically
writes the actual hook schema and decision to `.bap\captures`.

The required compatibility prompt is:

```text
Call Bash exactly once with this exact command: git status --short
```

Read the Claude Code version from the company
UI/about screen when the helper asks for it; the
company launcher does not need to support `claude --version`.

Live destructive/manual-only fixture capture is not required because Sonnet
may refuse those requests before issuing a tool call. The deterministic Go and
native Edge suites still require `git reset --hard` and MySQL client requests
to be denied, so removing them from live capture does not remove policy tests.

To use company-specific model labels, run:

```powershell
.\Capture-CompanyClaudeFixtures.ps1 -Runtime Native -Model 'COMPANY_SONNET_LABEL'
```

Changing models inside an already-open capture session does not change its
fixture label because Claude's hook payload does not expose the selected model.
Keep Sonnet selected and exit each session when instructed.

`Capture-CompanyClaudeFixtures.ps1`, `Capture-ClaudeFixtures.ps1`,
`Test-ClaudeFixtures.ps1`, and `Test-MVP0.ps1`
all accept `-Runtime Native`. If PowerShell reports that `Runtime` is unknown,
the laptop has an older checkout: run `Get-Command
.\Capture-ClaudeFixtures.ps1 -Syntax`. Its output must contain
`[-Runtime <string>]` and `[-NativePort <int>]` before capture. Do not use
`Runtime Native` without the leading hyphen.

For automated CLI capture, the helper records `claude --version`. For the
interactive company workflow, enter the version or approved company release
label displayed by the company UI. The `sonnet` value is a certification label;
replace it if the company mandates an immutable full model identifier.
The detailed privacy and replay rules are in the
[exact fixture certification guide](docs/bap-edge/claude-fixture-certification.md#exact-container-free-company-baseline).

### Show that BAP is working

After the Sonnet fixture manifest exists, use the same demo entry point for
every supported development runtime:

```powershell
# Installed Go; Windows EXEs; no Docker/Podman
.\Demo-Bap.ps1 -Runtime Native

# Docker-backed Service, MySQL, audit, and policy lifecycle
.\Demo-Bap.ps1 -Runtime Docker

# Podman-backed Service, MySQL, audit, and policy lifecycle
.\Demo-Bap.ps1 -Runtime Podman

# Prefer Podman/Docker; fall back to native Go when neither is usable
.\Demo-Bap.ps1 -Runtime Auto
```

Native mode reruns the strict native MVP-0 gate, displays the privacy-safe company
fixture result, verifies the signed JSONL audit chain, and prints the manifest
and retained native-run evidence paths. A successful demonstration ends with
`DEMO PASSED`. Docker and Podman run the container-backed build, policy,
database, status, fixture, and audit demonstration. The native gate directly proves safe allow, destructive/default/manual-
only deny, signed policy synchronization, prompt advisory, fixture replay, and
audit integrity; it does not depend on Sonnet agreeing to issue a dangerous
tool call.

For an optional visible Claude UI demonstration, run:

```powershell
.\Start-BapNativeLocal.bat -UseCompanyClaude
```

Then ask Claude to call `git status --short` (allowed) and
`python -c "print(1)"` (harmless if attempted, but default-denied by BAP). The
automated native demo remains the authoritative evidence if the model refuses
to issue a requested tool call.

Both local-model launchers select `http://127.0.0.1:8080`, supply the local demo
credential expected by ccbridge, select the requested local model, and limit
the initial Claude tool contract to `Bash`. The smaller tool contract matters
for local models with an 8K context window. When managed hooks are installed,
`allowManagedHooksOnly` intentionally makes `Start-BapNativeLocal.bat`
inapplicable; use `start-local-claude.bat` so the managed Edge and its matching
port-8443 Service are used.

Each unmanaged native launch uses a separate retained directory under
`.bap\native-local\runs`; `.bap\native-local\latest-run.txt` points to the most
recent one. Native test Services therefore never share a JSONL audit chain.

`Install-ManagedSettings.ps1 -Undo` verifies and removes only
`50-bap-edge.json`. It retains the installed Edge files, trust material, and
machine credential so an administrator can restore enforcement by rerunning the
normal install command. It is the supported transition to unmanaged native
testing: restart all Claude sessions, then run `Start-BapNativeLocal.bat`, which
creates a new isolated run instead of reusing an older audit chain.

The original cc-filter behavior and documentation remain below so this fork can
continue to synchronize with `wissem/cc-filter`.

---

# cc-filter: Claude Code Sensitive Information Filter

```
 ██████╗ ██████╗     ███████╗██╗██╗  ████████╗███████╗██████╗
██╔════╝██╔════╝     ██╔════╝██║██║  ╚══██╔══╝██╔════╝██╔══██╗
██║     ██║          █████╗  ██║██║     ██║   █████╗  ██████╔╝
██║     ██║          ██╔══╝  ██║██║     ██║   ██╔══╝  ██╔══██╗
╚██████╗╚██████╔╝    ██║     ██║███████╗██║   ███████╗██║  ██║
 ╚═════╝ ╚═════╝     ╚═╝     ╚═╝╚══════╝╚═╝   ╚══════╝╚═╝  ╚═╝
```

>Claude: You are absolutely right, I can read everything from your `.env` file

>Claude: read `.env`

>Me: WTF! `.env` is on my denied list!

>Claude: Ah, I see the problem! I shouldn't have access to this file!

Claude Code, somewhere based on a true story.

## Overview

cc-filter adds a hard security layer in front of Claude Code hooks. It blocks sensitive file access, blocks risky shell/search commands, and redacts secrets from text.

It is designed to protect against bypasses (alternate paths, command tricks, indirect reads) that can slip past normal allow/deny patterns.

## What it protects

1. **Hard file blocks** (`.env`, key/cert files, secrets files)
2. **Command blocks** (e.g. commands trying to print secrets)
3. **Search blocks** (e.g. grep/find patterns targeting secrets)
4. **Prompt blocks** for `UserPromptSubmit` (exit code `2`, prompt never reaches Claude)
5. **Optional redaction** for selected source/config files

## Install

Download the latest release for your platform.

**macOS (Intel)**
```bash
curl -L -o cc-filter https://github.com/wissem/cc-filter/releases/latest/download/cc-filter-darwin-amd64
chmod +x cc-filter
sudo mv cc-filter /usr/local/bin/
```

**macOS (Apple Silicon)**
```bash
curl -L -o cc-filter https://github.com/wissem/cc-filter/releases/latest/download/cc-filter-darwin-arm64
chmod +x cc-filter
sudo mv cc-filter /usr/local/bin/
```

**Linux (x86_64)**
```bash
curl -L -o cc-filter https://github.com/wissem/cc-filter/releases/latest/download/cc-filter-linux-amd64
chmod +x cc-filter
sudo mv cc-filter /usr/local/bin/
```

**Windows (PowerShell)**
```powershell
Invoke-WebRequest -Uri "https://github.com/wissem/cc-filter/releases/latest/download/cc-filter-windows-amd64.exe" -OutFile "cc-filter.exe"
Move-Item cc-filter.exe C:\Windows\System32\
```

## Quick setup (Claude Code hooks)

Add this to Claude settings:
- global: `~/.claude/settings.json`
- project-specific: `.claude/settings.json`

```json
{
  "hooks": {
    "PreToolUse": [{
      "matcher": "*",
      "hooks": [{
        "type": "command",
        "command": "cc-filter"
      }]
    }],
    "UserPromptSubmit": [{
      "hooks": [{
        "type": "command",
        "command": "cc-filter"
      }]
    }],
    "SessionEnd": [{
      "hooks": [{
        "type": "command",
        "command": "cc-filter"
      }]
    }]
  }
}
```

## Standalone usage

```bash
echo "API_KEY=sk-1234567890abcdef" | cc-filter
# Output: API_KEY=***FILTERED***

# Filter API keys from files
cat config.txt | cc-filter

# Filter OpenAI keys
echo "My key is sk-1234567890abcdefghijklmnopqrstuvwxyz123456789012" | cc-filter
# Output: My key is ***************************************************
```

## Configuration basics

Configuration is layered (later overrides earlier):
1. `configs/default-rules.yaml` (built-in defaults)
2. `~/.cc-filter/config.yaml` (user-wide)
3. `./config.yaml` (project)

Merge behavior:
- `patterns`: add new names, or override defaults by reusing a name
- `file_blocks`, `search_blocks`, `command_blocks`: merged + deduplicated

Minimal user config example:

```yaml
patterns:
  - name: "company_api_key"
    regex: 'COMPANY_API_KEY=([a-zA-Z0-9]{32})'
    replacement: "***FILTERED***"

file_blocks:
  - "*.private"

search_blocks:
  - "internal_token"

command_blocks:
  - "cat.*company"
```

For a complete example, see `configs/example-config.yaml`.

## Docs

- [Configuration reference](docs/configuration.md)
- [Smart file redaction](docs/redaction.md)
- [Hook behavior and limitations](docs/hooks-and-limitations.md)
- [Default protected patterns/files](docs/default-rules.md)
- [Standalone + integrations](docs/integrations.md)
- [Logging + troubleshooting](docs/troubleshooting.md)

## License

MIT
