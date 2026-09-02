# Docker Windows runbook

This runbook builds Linux Service/PEP images while retaining BAP Edge as the
Windows host hook executable used by Claude Code.

## Prerequisites

```powershell
docker version
docker info
```

Docker Desktop must be running in Linux-container mode. Ports `8443`, `9443`,
`8765`, `18443`, `19443`, `18765`, and `19090` must not be occupied by an
unrelated process when their corresponding command runs.

## Build

```powershell
cd C:\path\to\bap-system
.\Build-Bap.ps1 -Runtime Docker
.\Build-ResourcePEPs.ps1 -Runtime Docker
```

Verify images and the host Edge executable:

```powershell
docker image inspect bap-service:local | Out-Null
docker image inspect bap-api-gateway-springcloud:local | Out-Null
docker image inspect bap-mcp-pep:local | Out-Null
Get-FileHash .\dist\bap-edge-windows-amd64.exe -Algorithm SHA256
```

## Service development lifecycle

This path starts BAP Service plus local MySQL for ordinary Edge/policy testing:

```powershell
.\Start-Bap.ps1 -Runtime Docker
.\Show-BapStatus.ps1 -Runtime Docker
.\Test-Bap.ps1 -Runtime Docker
```

State, generated development certificates and local credentials are under
`.bap\runtime\docker\`. They are development artifacts and must not be copied
to production.

Stop:

```powershell
.\Stop-Bap.ps1 -Runtime Docker
```

## Complete protected-resource proof

The resource proof creates its own isolated Service configuration and distinct
Edge/API-PEP/MCP-PEP identities:

```powershell
.\Test-AgentGrant.ps1 -Runtime Docker
.\Test-SessionCapabilities.ps1 -Runtime Docker
.\Test-ResourcePEPs.ps1 -Runtime Docker
.\Demo-ResourcePEPs.ps1 -Runtime Docker -Rebuild
```

Do not run `Start-Bap.ps1` on port 8443 as a substitute for the demo's isolated
Agent STS consumer configuration. For long-running development resources,
configure `BAP_AGENT_STS_CONSUMERS_JSON` and distinct PEP credentials exactly
as described in the deployment guide.

Container logs captured during the PEP lifecycle are retained at:

```text
.bap\resource-peps\bap-api-gateway-springcloud.container.log
.bap\resource-peps\bap-mcp-pep.container.err.log
```

## Failure recovery

```powershell
.\Stop-ResourcePEPs.ps1
.\Stop-Bap.ps1 -Runtime Docker
docker ps --all --filter 'name=bap-'
```

If a script was interrupted, inspect container logs before removing only the
named local test containers. Preserve `.bap` evidence when investigating a
security or audit failure.

## Rollback

Retag the previously approved immutable image digest, restore the matching
signed policy/key set, restart the canary, and rerun `Test-Bap.ps1` plus the
protected-resource demo. Never roll policy back to a lower signed version on an
Edge state directory that has already observed a newer version; use the
documented emergency policy/version process.
