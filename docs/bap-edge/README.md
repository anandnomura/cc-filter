# BAP Edge operator guide

This is the starting page. You do not need Go installed. Podman or Docker runs a
pinned Go toolchain for builds and tests.

BAP Edge and BAP Service have independent build scripts. The combined
`Build-Bap.ps1` exists only for local development and acceptance testing.

For a completely new computer, follow [download-to-first-test](new-environment.md).
For company Windows with source-only ingress and no containers, follow
[native Windows Edge build](company-windows-build.md).
For an automated case-by-case demonstration run:

```powershell
.\Demo-Bap.ps1 -Runtime Docker -KeepRunning
```

## Components

| Component | Location | Runs where | Purpose |
|---|---|---|---|
| BAP Edge | repository root and `cmd/bap-edge` | Developer workstation | Managed hook, local cc-filter, workload/session binding, AuthZEN client, grant cache, outcome retry |
| BAP Service | `bap-service/` | Local container for development; company network for production | Authenticated AuthZEN PDP, Cedar, grants, audit trail, proposals |
| Cedar policies | `bap-service/policies/` | BAP Service only | Human-readable allow and forbid rules |

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

Stopping BAP Service does not make tools permissive. BAP Edge fails closed, so
Claude can continue reasoning but tool operations are denied.

## Read next

- [MVP roadmap, readiness ledger, release gates, and next iteration](mvp-roadmap.md)
- [Local laptop MVP acceptance test](local-laptop-mvp-test.md)
- [Company internal-pilot MVP acceptance and go/no-go test](company-pilot-mvp-test.md)
- [MVP technical architecture, trust boundaries, identity, APIs, and technologies](mvp-technical-architecture.md)
- [Cedar MVP policy and Sonnet/Opus tool coverage plan](cedar-mvp-policy-plan.md)
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
