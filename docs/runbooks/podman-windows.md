# Podman Windows runbook

This is the Docker-independent OCI path for a Windows laptop. Podman Desktop
runs Linux containers in a WSL machine; BAP Edge still runs on Windows beside
Claude Code.

## Prerequisites and one-time networking

```powershell
podman machine list
podman machine inspect --format '{{.UserModeNetworking}}'
```

The second command must return `true` so Windows can reach published PEP ports
and containers can reach the temporary Windows Service. If it returns `false`,
stop other Podman workloads and run:

```powershell
podman machine stop
podman machine set --user-mode-networking=true
podman machine start
podman info
```

`Start-ResourcePEPs.ps1` checks this prerequisite and fails immediately with
the same remediation rather than timing out.

## Build

```powershell
cd C:\path\to\bap-system
.\Build-Bap.ps1 -Runtime Podman
.\Build-ResourcePEPs.ps1 -Runtime Podman
```

Verify:

```powershell
podman image inspect bap-service:local | Out-Null
podman image inspect bap-api-gateway-springcloud:local | Out-Null
podman image inspect bap-mcp-pep:local | Out-Null
```

## Service lifecycle and acceptance

```powershell
.\Start-Bap.ps1 -Runtime Podman
.\Show-BapStatus.ps1 -Runtime Podman
.\Test-Bap.ps1 -Runtime Podman
.\Stop-Bap.ps1 -Runtime Podman

.\Test-AgentGrant.ps1 -Runtime Podman
.\Test-ResourcePEPs.ps1 -Runtime Podman
.\Demo-ResourcePEPs.ps1 -Runtime Podman -Rebuild
```

Podman MySQL uses the named volume `bap-mysql-local-data` because a Windows
bind mount does not provide the Linux ownership/mode semantics required by
MySQL. Do not delete that volume during ordinary stop/start.

## Diagnosis

```powershell
podman ps --all --filter 'name=bap-'
podman port bap-mcp-pep
podman logs bap-api-gateway-springcloud
podman logs bap-mcp-pep
podman machine ssh 'curl -s http://127.0.0.1:18765/healthz'
```

If the VM can reach a published port but Windows cannot, recheck
`UserModeNetworking`. If a PEP cannot resolve `host.containers.internal`, make
sure it was launched by the repository script, which adds the explicit
`host-gateway` mapping.

## Shutdown and rollback

```powershell
.\Stop-ResourcePEPs.ps1
.\Stop-Bap.ps1 -Runtime Podman
```

Rollback uses an immutable previously approved image digest plus its matching
signed policy and public keys. Restart the canary and repeat the complete test
sequence before widening deployment.
