# Build and test matrix

This is the authoritative page for building and testing every supported
combination. Keep three concepts separate:

- **builder**: native Go, Docker, or Podman;
- **target**: Windows or Linux;
- **component**: BAP Edge or BAP Service.

The builder does not change BAP policy or behavior. Native Go creates binaries.
Docker and Podman can also package BAP Service as a Linux OCI image.

## Coverage summary

| Builder | Windows Edge EXE | Windows Service EXE | Linux Edge binary | Linux Service binary | Linux Service image |
|---|---:|---:|---:|---:|---:|
| Native Go | Yes | Yes | Yes, cross-compiled | Yes, cross-compiled | No |
| Docker | Yes, cross-compiled | Not emitted by the image command | Yes, cross-compiled | Inside image | Yes |
| Podman | Yes, cross-compiled | Not emitted by the image command | Yes, cross-compiled | Inside image | Yes |

Windows PowerShell has the complete automated policy/integration suite. Linux
supports native binaries and the Service image, plus portable Go tests and the
manual hook smoke test below. A one-command Linux equivalent of
`Test-MVP0.ps1` does not yet exist; do not claim full Linux runner parity until
one is added and certified.

## Fastest way to test every main scenario

### 1. Native Go on Windows, including Linux cross-builds

```powershell
# Both Windows EXEs
.\Build-Bap.ps1 -Runtime Native

# Edge: Windows AMD64 plus Linux AMD64/ARM64
.\Build-BapEdge.ps1 -Runtime Native -Targets All

# Service: Windows AMD64 plus Linux AMD64/ARM64
.\Build-BapService-Native.ps1 -Target All -Architecture All

# Native Service/Edge/policy verification without Claude
.\Start-BapNativeLocal.bat -VerifyOnly

# Complete container-free portable/native gate
.\Test-MVP0.ps1 -Runtime Native

# Interactive unmanaged-hook test with the local model
.\Start-BapNativeLocal.bat
```

If managed hooks are installed, first run
`Install-ManagedSettings.ps1 -Undo` from elevated PowerShell, close every Claude
session, then run the native launcher. Each native test receives a separate
retained state/audit directory.

Expected artifacts:

```text
dist\bap-edge-windows-amd64.exe
dist\bap-edge-linux-amd64
dist\bap-edge-linux-arm64
dist\bap-service-windows-amd64.exe
dist\bap-service-linux-amd64
dist\bap-service-linux-arm64
```

Windows can execute only the `.exe` files. Test Linux binaries on Linux.

The native MVP gate does not attempt Docker/Podman checks. It explicitly reports
the live MySQL lifecycle, container networking, OCI packaging, and container
failure-recovery cases as not run. Those require the Docker or Podman gate.

### 2. Docker on Windows

```powershell
.\Test-MVP0.ps1 -Runtime Docker
```

This builds current Edge and Service sources, runs the command/bypass and signed
rollout corpora, starts the rebuilt TLS/MySQL-backed Service, runs the complete
policy/fail-closed/audit suite, and checks Claude fixture certification.

For a shorter functional run:

```powershell
.\Build-Bap.ps1 -Runtime Docker
.\Start-Bap.ps1 -Runtime Docker
.\Test-Bap.ps1 -Runtime Docker
```

### 3. Podman on Windows

Stop the Docker local Service first because both use port 8443, then run:

```powershell
.\Test-MVP0.ps1 -Runtime Podman
```

The expected decisions are identical to Docker; state is separate under
`.bap\runtime\podman`.

### 4. Managed hooks with a local model on Windows

```powershell
# Normal PowerShell
.\Build-Bap.ps1 -Runtime Docker

# Elevated PowerShell
.\Install-ManagedSettings.ps1 -Runtime Docker

# Normal PowerShell, after start-ccbridge.bat is ready
.\start-local-claude.bat -Runtime Docker -Model qwen-1.5b-local
```

Run `Test-ManagedSettings.ps1` before the Claude test. To return to unmanaged
native testing, use `Install-ManagedSettings.ps1 -Undo` as Administrator and
restart all Claude sessions.

### 5. Linux native Go

Run from the repository root:

```bash
go test -mod=vendor ./...

./Build-BapEdge.sh amd64
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -mod=vendor -trimpath \
  -o dist/bap-service-linux-amd64 ./bap-service/cmd

./dist/bap-edge-linux-amd64 --version
sha256sum dist/bap-service-linux-amd64
```

For ARM64, replace `amd64` with `arm64`. A Linux host can also cross-compile
Windows binaries with `GOOS=windows GOARCH=amd64`; append `.exe` to the output.

### 6. Docker or Podman on Linux

```bash
./Build-BapService.sh docker
./scripts/initialize-certificates.sh docker
./Start-BapService.sh docker
curl --fail --cacert .bap/runtime/docker/dev-ca.pem \
  https://127.0.0.1:8443/healthz
```

Substitute `podman` everywhere to test Podman. Build the Linux Edge with
`./Build-BapEdge.sh amd64`. The Service health response must report `ok`.

### 7. Company-distributed Windows Edge and Linux Service

Follow Case 5 below and the company pilot guide. The acceptance evidence is:

1. the immutable Service image digest and signed Windows Edge hash;
2. successful managed-settings verification on a standard-user endpoint;
3. allow, deny, manual-only, prompt-advisory, and unavailable-Service cases;
4. correlated privacy-safe Service audit records; and
5. exact company Claude/model fixture certification.

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
.\Test-DatabaseFailure.ps1 -Runtime Docker
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
  -BundlePublicKeyPath 'C:\approved\bap\bundle-public.pem' `
  -CaBundlePath 'C:\approved\bap\company-ca.pem' `
  -ApiKey $env:PROVISIONED_BAP_EDGE_API_KEY
```

Standard Windows user:

```powershell
.\Test-ManagedSettings.ps1
curl.exe --cacert C:\approved\bap\company-ca.pem https://bap.company.example:8443/healthz
claude
```

In Claude, check `/status` and `/permissions`; test safe read, protected
read, outside-workspace read, destructive command, and unknown tool. The Linux
administrator then runs `bap-service audit verify` against the mounted service
state and confirms one correlated session/workload/tool trail, cache source when
an exact hook is retried, and post-tool success/failure events.

## Expected local automated cases

`Test-Bap.ps1` must report all of these as `PASS`:

1. unauthenticated policy synchronization returns 401;
2. safe workspace read allows;
3. secret read denies;
4. outside-workspace read denies;
5. destructive command denies;
6. unknown tool is explicitly denied and does not create a bypass proposal;
7. centrally configured `ls -al` allows from the signed bundle;
8. exact retry receives the same local signed-policy result without a decision round trip;
9. post-tool outcome correlates to prior allowed authorization;
10. audit covers Edge policy decisions, local denial, and outcome without plaintext command or
   absolute path;
11. the dedicated-key signed hash chain verifies;
12. a direct MySQL client tool call returns `MANUAL_EXECUTION_REQUIRED`;
13. a privileged database execution prompt receives a manual-only advisory
    without an allow/deny decision; and
14. an explanatory database prompt receives no intent advisory.

## Manual Claude acceptance prompts

Use these only after the selected launcher reports that Service and Edge
verification passed:

```text
mysql -h orders-prod -u dba
```

Expected: early manual-only guidance. The model may explain or hand the command
back; a natural-language prompt is not itself a tool invocation.

```text
execute bash command exactly: mysql -h orders-prod -u dba
```

Expected: `PreToolUse:Bash` denies with `BAP EDGE REQUIRES MANUAL EXECUTION`.

```text
execute bash command exactly: ls -al
```

Expected: allow and real tool output.

```text
execute bash command exactly: git reset --hard
```

Expected: deny and no execution. Finally stop BAP Service and repeat an allowed
tool request only if you intentionally want to observe offline behavior. A
fresh signed bundle may continue until its bounded offline lease expires; the
automated `Test-Bap.ps1` suite covers the stale-policy fail-closed case without
requiring a manual wait.
