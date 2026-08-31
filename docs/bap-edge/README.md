# BAP Edge operator guide

This is the starting page. Podman/Docker can run the pinned Go toolchain, or an
approved local Go 1.23.12+ installation can build the components without a
container runtime.

BAP Edge and BAP Service have independent build scripts. The combined
`Build-Bap.ps1` exists only for local development and acceptance testing.

For a completely new computer, follow [download-to-first-test](new-environment.md).
For company Windows with source-only ingress and optional/no containers, follow
[native and controlled company builds](company-windows-build.md).
For an automated case-by-case demonstration run:

```powershell
.\Demo-Bap.ps1 -Runtime Docker -KeepRunning
```

For the focused command/bypass corpus and signed v1-to-v2 rollout lifecycle:

```powershell
.\Test-PolicyRollout.ps1 -Runtime Docker
```

The complete MVP-0 gate runs this focused test automatically:

```powershell
.\Test-MVP0.ps1 -Runtime Docker
```

Exact company client/model certification uses
[`Capture-ClaudeFixtures.ps1` and `Test-ClaudeFixtures.ps1`](claude-fixture-certification.md).
View current Service, Edge, lease, kill-switch, and audit-queue posture with:

```powershell
.\Show-BapStatus.ps1 -Runtime Docker
```

## Components

| Component | Location | Runs where | Purpose |
|---|---|---|---|
| BAP Edge | repository root and `cmd/bap-edge` | Developer workstation | Endpoint data plane, local PDP/PEP, signed-bundle verification, local Cedar decisions, workload/session binding, audit retry |
| BAP Service | `bap-service/` | Local container for development; company network for production | Rule control plane, signed bundle distribution, version/revocation directives, audit ingestion, legacy AuthZEN migration API |
| Cedar policies | `bap-service/policies/` | BAP Service only | Human-readable allow and forbid rules |

## Compilation quick reference

Run these commands from the repository root. Native builds use the dependencies
already checked into `vendor/` and require Go 1.23.12 or newer.

| Artifact | Command | Output |
|---|---|---|
| BAP Edge, Windows AMD64 | `.\Build-BapEdge.ps1 -Runtime Native` | `dist\bap-edge-windows-amd64.exe` |
| BAP Edge, Windows AMD64 + Linux AMD64/ARM64 | `.\Build-BapEdge.ps1 -Runtime Native -Targets All` | `dist\bap-edge-*` |
| BAP Service, Windows AMD64 EXE | `.\Build-BapService-Native.ps1 -Target Windows` | `dist\bap-service-windows-amd64.exe` |
| BAP Service, Linux AMD64 binary | `.\Build-BapService-Native.ps1 -Target Linux -Architecture amd64` | `dist\bap-service-linux-amd64` |
| BAP Service, Linux AMD64 + ARM64 binaries | `.\Build-BapService-Native.ps1 -Target Linux -Architecture All` | `dist\bap-service-linux-*` |
| Default local Windows Edge + Service EXEs | `.\Build-Bap.ps1 -Runtime Native` | Windows Edge and Service under `dist\` |
| BAP Service OCI image | `.\Build-BapService.ps1 -Runtime Docker` | local image `bap-service:local` |
| Automatic build | `.\Build-Bap.ps1 -Runtime Auto` | OCI artifacts when a runtime works; native binaries otherwise |

Windows Go compiles the Windows EXEs directly and can cross-compile the Linux
binaries because both components use
pure Go with `CGO_ENABLED=0`. The Linux binaries do not run on Windows, and Go
alone does not package an OCI image. See the
[company build guide](company-windows-build.md) for checksums, internal
digest-pinned base images, signing, packaging, and installation.

## Five-minute local setup on Windows

Open a normal PowerShell window in the repository:

```powershell
.\Build-Bap.ps1 -Runtime Docker
.\Initialize-Certificates.bat -Runtime Docker
.\Start-Bap.ps1 -Runtime Docker
.\Test-Bap.ps1 -Runtime Docker
.\View-AuditTrail.ps1 -Runtime Docker -VerifyOnly
```

Use `-Runtime Podman` instead when using Podman. `Auto` prefers a running Podman
engine and otherwise uses Docker.

`Initialize-Certificates` creates development TLS, separate grant/audit signing
keys, and a dedicated local BAP API key. The development service listens at
`https://127.0.0.1:8443`. HTTPS is mandatory
even locally. The local CA is used only by this project and is not installed as a
machine-wide trusted CA.

Next, open **PowerShell as Administrator** and run:

```powershell
cd C:\Users\User\pyprj\bap-edge
.\Install-ManagedSettings.ps1 -Runtime Docker
```

Close the administrator window. From a normal PowerShell window run:

```powershell
.\Test-ManagedSettings.ps1
claude
```

Inside Claude Code check:

1. `/status` lists an active managed settings source.
2. `/hooks` may show `0 hooks configured` because it lists editable hooks, not
   administrator-managed policy hooks. Use `Test-ManagedSettings.ps1` for the
   authoritative managed-hook test.
3. `/permissions` shows that bypass mode is disabled.
4. Ask Claude to read `README.md`; BAP should allow it.
5. Ask Claude to read `.env` or run `git reset --hard`; BAP should deny it.
6. Run `.\View-AuditTrail.ps1 -Runtime Docker` and correlate the session,
   workload, tool-use, authorization source, and outcome.

## Normal operation

```powershell
.\Start-Bap.ps1 -Runtime Docker
.\Test-Bap.ps1 -Runtime Docker
.\List-PolicyProposals.bat -Runtime Docker
.\View-AuditTrail.bat -Runtime Docker
.\Stop-Bap.ps1 -Runtime Docker
```

Stopping BAP Service does not make policy unbounded. A fresh verified bundle may
continue local decisions only through its signed maximum-offline lease; after
that lease BAP Edge fails closed. Claude can continue reasoning without tools.

## Read next

- [MVP roadmap, readiness ledger, release gates, and next iteration](mvp-roadmap.md)
- [Local laptop MVP acceptance test](local-laptop-mvp-test.md)
- [Company internal-pilot MVP acceptance and go/no-go test](company-pilot-mvp-test.md)
- [MVP technical architecture, trust boundaries, identity, APIs, and technologies](mvp-technical-architecture.md)
- [Cedar MVP policy and Sonnet/Opus tool coverage plan](cedar-mvp-policy-plan.md)
- [Exact Claude Code/Sonnet/Opus fixture capture and certification](claude-fixture-certification.md)
- [Central policy authority and signed Edge distribution proposal](central-policy-distribution-proposal.md)
- [MVP-0A local certification and remaining live-model gate](mvp0-certification.md)
- [Architecture and request flow](architecture.md)
- [Edge YAML and service environment reference](configuration.md)
- [MySQL storage, local operation, and enterprise database switch](storage.md)
- [Audit trail, integrity, privacy, and operations](audit-trail.md)
- [End-to-end tracing, structured logs, metrics, and privacy boundary](observability.md)
- [Docker, Podman, managed-settings, and fail-closed tests](testing.md)
- [New computer: download, build, initialize, install, and demonstrate](new-environment.md)
- [Company Windows: compile Edge locally without Docker or Podman](company-windows-build.md)
- [Every Windows/Linux/local/distributed build and test case](deployment-test-matrix.md)
- [Performance test commands and measured baseline](performance.md)
- [What is still required for enterprise production](production-readiness.md)
- [Certificates and rotation](certificates.md)
- [Windows managed settings and bypass tests](managed-settings-windows.md)
- [Network and Podman deployment](network-deployment.md)
- [Cedar policies and safe learning](policies-and-proposals.md)
- [Keeping the fork synchronized](fork-sync.md)
- [Troubleshooting](troubleshooting.md)
