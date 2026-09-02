# Build and acceptance testing

## Neutral session-accretion observation

Use `Test-SessionAccretion.bat -Mode DirectBap` for a deterministic observation
against normalization, the signed policy, and one session ledger. Use
`-Mode NativeClaude` for the local model, or `-Mode CompanySonnet` to verify the
managed-hook installation and open the exact eight-turn checklist before the
zero-argument company Claude launcher starts. A reported `GAP` is not a passing
security certification; see
[session capability controls](session-capability-controls.md#neutral-accretion-acceptance).

## Prerequisites

Install either Docker Desktop or Podman Desktop/Podman machine. You do **not**
need Go: builds and tests use `golang:1.23-bookworm` inside the selected runtime.

For a container-free company workstation, install approved Go 1.23.12+ and run:

```powershell
.\Test-MVP0.ps1 -Runtime Native
```

This runs the portable/native MVP-0A gate. It does not pretend to cover the live
MySQL or OCI/container lifecycle checks described below.

## One-command demonstrations

Once the company Sonnet fixture manifest has been created, use:

```powershell
.\Demo-Bap.ps1 -Runtime Native
.\Demo-Bap.ps1 -Runtime Docker
.\Demo-Bap.ps1 -Runtime Podman
.\Demo-Bap.ps1 -Runtime Auto
```

Every mode requires the company fixture certification. `Auto` selects
Podman/Docker when available and otherwise uses native Go. Native mode prints
the retained evidence paths and verifies its signed JSONL audit chain; the
container modes include their MySQL-backed integration and audit checks.

## Docker acceptance test

From a normal PowerShell window:

```powershell
cd C:\Users\User\pyprj\bap-system
.\Build-Bap.ps1 -Runtime Docker
.\Test-PolicyRollout.ps1 -Runtime Docker
.\Start-Bap.ps1 -Runtime Docker
.\Test-Bap.ps1 -Runtime Docker
.\Test-DatabaseFailure.ps1 -Runtime Docker
.\View-AuditTrail.ps1 -Runtime Docker -VerifyOnly
.\Show-BapStatus.ps1 -Runtime Docker
```

The test proves unit/integration tests, HTTPS, authenticated policy sync, signed
bundle verification, rollback/expiry/tamper rejection, locally evaluated Cedar
allow/deny, centrally configured `ls -al`, bounded offline decisions, local
denial auditing, workload IDs, post-tool outcomes, trace correlation,
privacy-safe logs, metrics, audit privacy, and signed-chain verification.

`Test-PolicyRollout.ps1` is the focused policy gate. It runs the reviewed,
data-driven command/bypass corpus and a real HTTPS Service-to-Edge lifecycle:
v1 permits `ls -al`, v2 removes that rule and denies it, and rollback,
same-version equivocation, payload tamper, kill switch, and expired offline
lease all fail closed. `Test-MVP0.ps1` invokes this gate automatically.

Exact company Claude Code/model captures are optional for model-independent lab
work and mandatory for admission. See
[fixture certification](claude-fixture-certification.md), then run:

```powershell
.\Test-MVP0.ps1 -Runtime Docker -RequireCompanyFixtures
```

The database-failure test stops local MySQL, proves `/readyz` and policy sync
return 503, restores MySQL, and requires control-plane readiness to recover.

For component-only builds use `Build-BapEdge.ps1` on the Windows side and
`Build-BapService.sh docker` or `Build-BapService.sh podman` on the Linux side.

## Podman acceptance test

Only one runtime can bind local port 8443 at a time:

```powershell
.\Stop-Bap.ps1 -Runtime Docker
podman machine start
.\Build-Bap.ps1 -Runtime Podman
.\Start-Bap.ps1 -Runtime Podman
.\Test-Bap.ps1 -Runtime Podman
.\View-AuditTrail.ps1 -Runtime Podman -VerifyOnly
```

## Managed-settings test

Open PowerShell as Administrator:

```powershell
cd C:\Users\User\pyprj\bap-system
.\Install-ManagedSettings.ps1 -Runtime Docker
```

Restart Claude Code. Open a **non-administrator** PowerShell and run:

```powershell
.\Test-ManagedSettings.ps1
claude
```

Inside Claude Code verify `/status` and `/permissions`. `/hooks` may show zero
because it lists editable hooks rather than managed policy hooks. The test
script exercises the installed Program Files Edge binary directly. Ask Claude
to read `README.md` (allow), read `.env` (deny), and run `git reset --hard`
(deny). Then inspect the audit trail.

## Bounded offline and fail-closed test

```powershell
.\Stop-Bap.ps1 -Runtime Docker
```

A fresh verified bundle may continue local traffic decisions until its
`max_offline_seconds` lease expires. An Edge with no bundle, an invalid bundle,
or an expired offline lease must deny. Restart the service before lease expiry
for normal operation.

## Standard-user tamper test

From a non-administrator shell, attempts to edit these files must fail:

- `C:\Program Files\BAP Edge\bap-edge.exe`
- `C:\Program Files\BAP Edge\bap-edge.yaml`
- `C:\Program Files\ClaudeCode\managed-settings.d\50-bap-edge.json`

`Test-ManagedSettings.ps1` checks their ACLs and directly attempts to open the
managed file for writing. Local/project/user Claude settings cannot add hooks or
permission rules because the managed-only controls are enabled.
