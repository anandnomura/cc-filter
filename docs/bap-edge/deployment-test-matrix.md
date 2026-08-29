# Build and test matrix

This page is the authoritative map of which command runs on which machine.

| Case | Machine | Runtime/compiler needed | Command | Output/proof |
|---|---|---|---|---|
| Edge, company Windows | Windows AMD64 | Approved Go 1.23.12+ only | `.\Build-BapEdge-Native.ps1` | Locally compiled Windows EXE and SHA-256 |
| Edge, developer/CI Windows | Windows with Docker/Podman | Container runtime | `.\Build-BapEdge.ps1 -Runtime Docker` | Windows EXE |
| Edge, all cross-builds | Windows CI with Docker/Podman | Container runtime | `.\Build-BapEdge.ps1 -Runtime Docker -Targets All` | Windows AMD64, Linux AMD64/ARM64 |
| Edge, native Linux | Linux | Approved Go 1.23.12+ | `./Build-BapEdge.sh amd64` | Linux Edge binary and SHA-256 |
| Service, PowerShell dev | Windows/Linux PowerShell | Docker/Podman | `.\Build-BapService.ps1 -Runtime Podman` | Linux OCI image |
| Service, Linux | Linux | Docker/Podman | `./Build-BapService.sh podman` | Linux OCI image |
| Local combined demo | Windows | Docker or Podman | `.\Demo-Bap.ps1 -Runtime Docker -KeepRunning` | Case-by-case pass output |
| Distributed deployment | Windows Edge + Linux Service | Go on build hosts; Podman/Docker on service only | Sections below | Managed tool decisions and network audit |

All Go module dependencies are vendored. Source builds use `-mod=vendor`; no
public Go module download is needed. Container builds still need the approved
base images unless they are already present in an internal registry/cache.

## Case 1: Windows Edge build and test

Company/source-only path:

```powershell
go version
.\Build-BapEdge-Native.ps1
Get-FileHash -Algorithm SHA256 .\dist\bap-edge-windows-amd64.exe
```

This runs Edge unit tests before building. No service image or Linux binary is
created. Follow [company Windows build](company-windows-build.md) to install it.

Developer cross-build path:

```powershell
.\Build-BapEdge.ps1 -Runtime Docker -Targets All
```

Use `-Targets Windows` (the default) when Linux Edge artifacts are unnecessary.

## Case 2: Linux Edge build and test

Only needed when Claude Code and BAP Edge run on Linux:

```bash
./Build-BapEdge.sh amd64
# or
./Build-BapEdge.sh arm64
```

This is unrelated to the Linux BAP Service executable/image.

## Case 3: Linux BAP Service build and local start

On the Linux service host:

```bash
./Build-BapService.sh podman
./Start-BapService.sh podman
curl --cacert .bap/runtime/podman/dev-ca.pem https://127.0.0.1:8443/healthz
```

For Docker substitute `docker`. These Bash start commands use development TLS;
production uses company PKI and the explicit deployment in
[network-deployment.md](network-deployment.md).

## Case 4: Complete local integration

```powershell
.\Demo-Bap.ps1 -Runtime Docker -KeepRunning
.\Test-Bap.ps1 -Runtime Docker
.\View-AuditTrail.ps1 -Runtime Docker -VerifyOnly
```

Repeat with Podman after stopping the Docker container. Docker and Podman use
separate runtime directories and cannot both bind port 8443.

## Case 5: Distributed Windows Edge to Linux Service

Linux administrator:

1. Build and deploy the service image with company TLS, grant/audit keys, durable
   `/var/lib/bap`, a unique Edge API key, and its principal.
2. Allow the Windows endpoint to reach TCP 8443.
3. Deliver the CA bundle, grant public key, BAP URL, and Edge API credential
   through approved channels. Never deliver service private keys.

Windows administrator:

```powershell
.\Build-BapEdge-Native.ps1
.\Install-ManagedSettings.ps1 `
  -ServiceUrl 'https://bap.company.example:8443' `
  -EdgeBinaryPath '.\dist\bap-edge-windows-amd64.exe' `
  -GrantPublicKeyPath 'C:\approved\bap\grant-public.pem' `
  -CaBundlePath 'C:\approved\bap\company-ca.pem' `
  -ApiKey $env:PROVISIONED_BAP_EDGE_API_KEY
```

Standard Windows user:

```powershell
.\Test-ManagedSettings.ps1
curl.exe --cacert C:\approved\bap\company-ca.pem https://bap.company.example:8443/healthz
claude
```

In Claude, check `/status`, `/hooks`, and `/permissions`; test safe read, protected
read, outside-workspace read, destructive command, and unknown tool. The Linux
administrator then runs `bap-service audit verify` against the mounted service
state and confirms one correlated session/workload/tool trail, cache source when
an exact hook is retried, and post-tool success/failure events.

## Expected local automated cases

`Test-Bap.ps1` must report all of these as `PASS`:

1. unauthenticated AuthZEN request returns 401;
2. safe workspace read allows;
3. secret read denies;
4. outside-workspace read denies;
5. destructive command denies;
6. unknown tool/default deny creates a proposal;
7. exact retry consumes a cached grant only after central audit acknowledgement;
8. post-tool outcome correlates to prior allowed authorization;
9. audit covers PDP, cache, local denial, and outcome without plaintext command or
   absolute path;
10. the dedicated-key signed hash chain verifies.
