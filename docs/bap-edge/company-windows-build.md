# Company Windows build without Docker or Podman

Use this path when company policy allows source code but prohibits importing an
externally compiled executable. The executable is compiled inside the company
boundary.

## What the workstation needs

- the BAP Edge source branch, including `vendor/`;
- an internally approved Go 1.23.12 or newer Windows compiler;
- PowerShell;
- the company BAP Service URL, CA bundle, grant public key, and provisioned BAP
  Edge API credential;
- administrator access for the managed-settings installation.

It does **not** need Docker, Podman, a Linux binary, Cedar tooling, or a Go runtime
after compilation. Go executables are self-contained here because CGO is disabled.

## 1. Install/verify the approved Go compiler

Use your company software portal or internal package repository. Open a new
PowerShell and verify:

```powershell
go version
```

The result must be Go 1.23.12 or newer. If `go` is not recognized, the Go `bin`
directory is not on PATH or the shell was opened before installation.

## 2. Obtain the source

If company Git can reach GitHub:

```powershell
git clone --branch bap-edge https://github.com/anandnomura/cc-filter.git bap-edge
cd bap-edge
```

Otherwise transfer the approved source archive through the normal source-code
review process. Do not omit `vendor/`; it contains the pinned Cedar/YAML source
dependencies and permits `-mod=vendor` builds without a public Go module proxy.

## 3. Compile and test BAP Edge natively

```powershell
.\Build-BapEdge-Native.ps1
```

The script runs Edge tests, compiles only Windows AMD64, and prints a SHA-256.
The locally created file is:

```text
dist\bap-edge-windows-amd64.exe
```

No BAP Service or Linux artifact is compiled by this script.

## 4. Install the locally compiled Edge

Open PowerShell as Administrator:

```powershell
.\Install-ManagedSettings.ps1 `
  -ServiceUrl 'https://bap.company.example' `
  -EdgeBinaryPath '.\dist\bap-edge-windows-amd64.exe' `
  -GrantPublicKeyPath 'C:\approved\bap\grant-public.pem' `
  -CaBundlePath 'C:\approved\bap\company-ca.pem' `
  -ApiKey $env:PROVISIONED_BAP_EDGE_API_KEY
```

This network installation path does not look for a container runtime. It copies
the executable and public trust files into Program Files, writes the managed
Claude hooks, provisions the dedicated credential as a machine environment
variable, and removes standard-user write permissions.

Close every Claude process and open a non-administrator PowerShell.

## 5. Validate

```powershell
.\Test-ManagedSettings.ps1
claude
```

Inside Claude, validate `/status`, `/hooks`, and `/permissions`, then run the safe
read, `.env` denial, and destructive-command denial cases documented in
[testing.md](testing.md). The network BAP Service administrator verifies the
correlated signed audit events.

## Troubleshooting an offline build

The build command explicitly uses `-mod=vendor`. If Go still tries the public
internet, verify `vendor/modules.txt` is present and that the complete source tree
was transferred. The Go compiler itself must already satisfy the `toolchain`
version in `go.mod`; an older compiler may attempt to download a newer toolchain.
