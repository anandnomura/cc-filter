# Build and acceptance testing

## Prerequisites

Install either Docker Desktop or Podman Desktop/Podman machine. You do **not**
need Go: builds and tests use `golang:1.23-bookworm` inside the selected runtime.

## Docker acceptance test

From a normal PowerShell window:

```powershell
cd C:\Users\User\pyprj\bap-edge
.\Build-Bap.ps1 -Runtime Docker
.\Start-Bap.ps1 -Runtime Docker
.\Test-Bap.ps1 -Runtime Docker
.\Test-DatabaseFailure.ps1 -Runtime Docker
.\View-AuditTrail.ps1 -Runtime Docker -VerifyOnly
```

The test proves unit/integration tests, HTTPS, AuthZEN discovery, missing API-key
rejection, Cedar allow/deny, local denial auditing, per-session workload IDs,
signed grants, centrally acknowledged cache reuse, post-tool outcomes, W3C trace
correlation, privacy-safe Edge JSONL, bounded Prometheus metrics, audit privacy,
and signed-chain verification.

The database-failure test stops local MySQL, proves `/readyz` returns 503 and a
fresh evaluation cannot be authorized, restores MySQL, and requires readiness
to recover.

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
cd C:\Users\User\pyprj\bap-edge
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

## Fail-closed test

```powershell
.\Stop-Bap.ps1 -Runtime Docker
```

Start Claude and request a tool operation. It must be denied because the service
cannot provide or audit authority. Start the service again before normal use.

## Standard-user tamper test

From a non-administrator shell, attempts to edit these files must fail:

- `C:\Program Files\BAP Edge\bap-edge.exe`
- `C:\Program Files\BAP Edge\bap-edge.yaml`
- `C:\Program Files\ClaudeCode\managed-settings.d\50-bap-edge.json`

`Test-ManagedSettings.ps1` checks their ACLs and directly attempts to open the
managed file for writing. Local/project/user Claude settings cannot add hooks or
permission rules because the managed-only controls are enabled.
