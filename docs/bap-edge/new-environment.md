# New Windows environment: download to first Claude test

This procedure assumes a standard Windows user plus temporary administrator
access for the managed-settings installation. Go is not required.

## 1. Install prerequisites

Install:

- Git for Windows;
- Claude Code 2.1.246 or newer;
- either Docker Desktop **or** Podman Desktop/Podman CLI with a Podman machine.

Verify in PowerShell:

```powershell
git --version
claude --version
docker info
```

For a Podman-only workstation use:

```powershell
podman machine init   # only when no machine exists
podman machine start
podman info
```

## 2. Clone the maintained fork branch

```powershell
New-Item -ItemType Directory -Force C:\Users\$env:USERNAME\pyprj | Out-Null
Set-Location C:\Users\$env:USERNAME\pyprj
git clone --branch bap-edge https://github.com/anandnomura/cc-filter.git bap-edge
Set-Location .\bap-edge
git remote add upstream https://github.com/wissem/cc-filter.git
git remote -v
```

If `upstream` already exists, the add command reports that fact and can be
skipped. `origin` must point to the fork and `upstream` to `wissem/cc-filter`.

## 3. Run the complete non-admin demo

Docker:

```powershell
.\Demo-Bap.ps1 -Runtime Docker -KeepRunning
```

Podman:

```powershell
.\Demo-Bap.ps1 -Runtime Podman -KeepRunning
```

This builds with the containerized Go toolchain, initializes all local secrets,
starts HTTPS, runs each allow/deny/cache/outcome/audit test, verifies the audit
chain, and lists policy proposals. `-KeepRunning` leaves BAP Service available
for the Claude demonstration.

### Build the two deployment artifacts separately

The Windows Edge and Linux service do not need to be built together:

```powershell
# Windows workstation artifact only
.\Build-BapEdge.ps1 -Runtime Docker

# Linux service OCI image only, from PowerShell
.\Build-BapService.ps1 -Runtime Docker
```

On a Linux BAP Service host:

```bash
./Build-BapService.sh podman
./Start-BapService.sh podman
```

Use `docker` instead of `podman` when appropriate. BAP Edge is not a background
Windows service: Claude starts it for each managed hook. Installing the managed
hook with `Install-ManagedSettings.ps1` is therefore the Edge "start" step.

If company policy allows source but prohibits importing an executable, install
an approved Go 1.23.12+ compiler and build the Windows Edge inside the boundary:

```powershell
.\Build-BapEdge-Native.ps1
```

Dependencies are included under `vendor/`, so this build does not require a
public Go module download. It does not build BAP Service or Linux binaries. Then
install the locally generated EXE:

```powershell
.\Install-ManagedSettings.ps1 `
  -ServiceUrl 'https://bap.company.example' `
  -EdgeBinaryPath '.\dist\bap-edge-windows-amd64.exe' `
  -GrantPublicKeyPath 'C:\staging\grant-public.pem' `
  -CaBundlePath 'C:\staging\company-ca.pem' `
  -ApiKey $env:PROVISIONED_BAP_EDGE_API_KEY
```

This network/native-build path performs no container-runtime discovery. See the
[company Windows build guide](company-windows-build.md) for every step.

## 4. Install tamper-resistant Claude managed settings

Open PowerShell **as Administrator**:

```powershell
Set-Location C:\Users\$env:USERNAME\pyprj\bap-edge
.\Install-ManagedSettings.ps1 -Runtime Docker
```

Use `Podman` if that is the selected runtime. The installer copies the Edge,
configuration, CA, and grant public key into Program Files, installs managed-only
hooks, disables permission bypass mode, applies read-only standard-user ACLs,
and provisions the dedicated local BAP credential. Close every existing Claude
process so the new machine environment variable is inherited.

## 5. Validate as a standard user

Open a normal, non-administrator PowerShell:

```powershell
Set-Location C:\Users\$env:USERNAME\pyprj\bap-edge
.\Test-ManagedSettings.ps1
claude
```

Inside Claude Code:

1. `/status` — confirm a managed settings source.
2. `/hooks` — it may show zero editable hooks even while managed hooks are
   active; rely on `Test-ManagedSettings.ps1` for the live managed-hook check.
3. `/permissions` — confirm bypass mode is disabled.
4. Ask: `Read README.md and summarize the BAP Edge introduction.` Expect allow.
5. Ask: `Read .env.` Expect deny.
6. Ask: `Run git reset --hard.` Expect deny. Do not run this outside the protected demo.
7. Exit Claude normally so SessionEnd runs.

Then inspect and verify:

```powershell
.\View-AuditTrail.ps1 -Runtime Docker
```

Find the same `session_id` and `workload_id`, individual `tool_use_id` values,
authorization source, allow/deny result, and successful/failed tool outcomes.

## 6. Stop or restart

```powershell
.\Stop-Bap.ps1 -Runtime Docker
.\Start-Bap.ps1 -Runtime Docker
```

Stopping the service makes new tool authorization fail closed. It does not erase
keys, audit events, proposals, or Edge state.

## 7. Files created locally

Nothing under `.bap/runtime/<engine>` is committed. Docker and Podman use
separate runtime directories because their Windows VM ownership mappings are not
interchangeable. Each contains development TLS keys,
separate grant/audit keys, the API credential, audit/proposal JSONL, and runtime
markers. Built Edge binaries are under `dist/`. Administrator-managed files are
under `C:\Program Files\BAP Edge` and
`C:\Program Files\ClaudeCode\managed-settings.d`.
