# Company Windows build without Docker or Podman

Use this path when company policy allows source code but prohibits importing an
externally compiled executable. The executable is compiled inside the company
boundary.

## Recommended company release build

For an auditable release, build both deployable artifacts from one clean,
reviewed Git commit on a controlled build workstation. The workstation needs an
approved Go toolchain plus Docker or Podman connected only to your internal
registry. Mirror and approve these two base images first, pinned by digest:

- `golang:1.23-bookworm` for compilation;
- `debian:bookworm-slim` for the BAP Service runtime.

Then run:

```powershell
git status --short
git rev-parse HEAD

.\Build-CompanyArtifacts.ps1 `
  -Runtime Docker `
  -Version '1.0.0-company.1' `
  -Registry 'registry.company.example/security' `
  -BuildImage 'registry.company.example/base/golang@sha256:<approved-digest>' `
  -RuntimeImage 'registry.company.example/base/debian@sha256:<approved-digest>'
```

The command refuses a dirty Git checkout, runs the vendored Edge tests, builds
the versioned Windows Edge executable, builds the versioned Linux BAP Service
image, exports the service image, and writes hashes plus source/build metadata:

```text
dist\bap-edge-windows-amd64.exe
dist\bap-edge-windows-amd64.exe.sha256
dist\bap-service-1.0.0-company.1.oci.tar
dist\company-build-1.0.0-company.1.json
```

Import the OCI archive into the internal registry, scan both artifacts, produce
an SBOM, sign the executable and image with company identities, and retain the
manifest with the release approval. Deploy the service image by immutable
digest, not by a mutable tag.

If the Windows compiler and Linux container builder must be separated, use the
native Edge procedure below on Windows and build the service on Linux with:

```bash
export BAP_BUILD_VERSION=1.0.0-company.1
export BAP_IMAGE_TAG=registry.company.example/security/bap-service:1.0.0-company.1
export BAP_GO_BUILD_IMAGE=registry.company.example/base/golang@sha256:<approved-digest>
export BAP_RUNTIME_IMAGE=registry.company.example/base/debian@sha256:<approved-digest>
./Build-BapService.sh podman
```

## Inputs your company must provide

The source build is self-contained, but a company deployment is not. Before
admitting users, the company must supply:

- the approved source commit and release version;
- internal registry image digests and signing/scanning services;
- a company TLS certificate and CA chain for BAP Service;
- a unique client certificate and private key for each managed endpoint;
- the policy-bundle Ed25519 public key distributed to endpoints;
- managed MySQL, secret injection, backup, monitoring, and retention settings;
- approved Claude Code/Sonnet version and sanitized certification fixtures;
- endpoint-management deployment, application allowlisting, and removal of
  local administrator rights for pilot users.

Do not reuse the development bearer credential across company endpoints. The
installer supports per-device mTLS now; enrollment, rotation, and revocation
still need to be supplied by the company identity/PKI workflow.

## What the workstation needs

- the BAP Edge source branch, including `vendor/`;
- an internally approved Go 1.23.12 or newer Windows compiler;
- PowerShell;
- the company BAP Service URL, CA bundle, policy-bundle public key, and
  provisioned per-device mTLS identity;
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
git clone --branch bap-edge https://github.com/anandnomura/cc-filter.git bap-system
cd bap-edge
```

Otherwise transfer the approved source archive through the normal source-code
review process. Do not omit `vendor/`; it contains the pinned Cedar/YAML source
dependencies and permits `-mod=vendor` builds without a public Go module proxy.

## 3. Compile and test BAP Edge natively

### Individual component commands

Build only the Windows AMD64 BAP Edge executable:

```powershell
.\Build-BapEdge.ps1 -Runtime Native
```

This invokes `Build-BapEdge-Native.ps1`, runs Edge tests from `vendor/`, compiles
Windows AMD64, and writes a SHA-256 checksum. You can also keep using the normal
automatic command:

```powershell
.\Build-BapEdge.ps1 -Runtime Auto
```

`Auto` uses a working Podman/Docker runtime when one exists and falls back to
the installed Go toolchain when neither runtime is usable. To cross-compile the
Windows AMD64 and Linux AMD64/ARM64 Edge binaries with local Go, use:

```powershell
.\Build-BapEdge.ps1 -Runtime Native -Target All
```

Build the Windows AMD64 BAP Service executable for container-free local testing:

```powershell
.\Build-BapService-Native.ps1 -Target Windows
```

Output:

```text
dist\bap-service-windows-amd64.exe
```

`bap-service-linux-amd64` is a separate Linux executable without an `.exe`
extension; it cannot run on Windows. Cross-compile it explicitly with:

```powershell
.\Build-BapService-Native.ps1 -Target Linux -Architecture amd64
```

Build Linux AMD64 and ARM64 BAP Service executables with:

```powershell
.\Build-BapService-Native.ps1 -Target Linux -Architecture All
```

The ordinary wrapper also supports both explicit and automatic fallback:

```powershell
.\Build-BapService.ps1 -Runtime Native -Target Windows
.\Build-BapService.ps1 -Runtime Auto -Target Windows
```

With `Auto`, a usable container runtime produces the OCI image; without one, an
installed Go toolchain produces the Linux executable and prints that distinction.

Or build the default Windows Edge and Windows Service executables together:

```powershell
.\Build-Bap.ps1 -Runtime Native
```

These builds work because both programs use pure Go and set `CGO_ENABLED=0`.
A Windows Go compiler emits the Windows Service EXE directly and can emit Linux
binaries by setting `GOOS=linux`; Linux output does not run on Windows.

### Start the Windows Service EXE for a local smoke test

From the repository root, configure an isolated development state directory and
initialize its TLS/signing keys once:

```powershell
$env:BAP_STATE_DIRECTORY = "$PWD\.bap\runtime\native"
$env:BAP_POLICY_PATH = "$PWD\bap-service\policies\agent-tools.cedar"
$env:BAP_BUNDLE_SOURCE_PATH = "$PWD\bap-service\policies\edge-policy-source.json"
$env:BAP_LISTEN_ADDRESS = '127.0.0.1:8443'
$env:BAP_DEVELOPMENT_TLS = 'true'
$env:BAP_EDGE_API_KEY = 'replace-with-a-dedicated-local-test-secret'

.\dist\bap-service-windows-amd64.exe initialize-certificates
.\dist\bap-service-windows-amd64.exe
```

This foreground process uses signed JSONL audit/proposal storage under
`.bap\runtime\native` when `BAP_DATABASE_DSN` is unset. That is suitable for a
local smoke test, not a company pilot. Set `BAP_DATABASE_DSN` and the documented
TLS settings when using company MySQL. The generated Edge trust inputs are
`.bap\runtime\native\dev-ca.pem` and
`.bap\runtime\native\bundle-public.pem`.

The Linux BAP Service binary still needs the Cedar policy files under
`bap-service/policies/`, company certificates/secrets, and a Linux runtime host.
Go alone does not create an OCI image. Use `Build-BapService.ps1 -Runtime Docker`
or `-Runtime Podman` when an OCI image is required, or hand the Linux binary to
the company container packaging pipeline.

The locally created file is:

```text
dist\bap-edge-windows-amd64.exe
```

The default Edge-only command does not compile BAP Service. `-Target All`
additionally cross-compiles the Linux Edge binaries.

`Build-Bap.ps1 -Runtime Native` builds the Windows Edge and Windows Service
executables for container-free local testing. Use the explicit `-Target Linux`
command when a Linux Service binary is needed for packaging. The final BAP
Service OCI image must still be created in a Docker/Podman or separate Linux
packaging pipeline.

## 4. Install the locally compiled Edge

Open PowerShell as Administrator:

```powershell
.\Install-ManagedSettings.ps1 `
  -ServiceUrl 'https://bap.company.example' `
  -EdgeBinaryPath '.\dist\bap-edge-windows-amd64.exe' `
  -BundlePublicKeyPath 'C:\approved\bap\bundle-public.pem' `
  -CaBundlePath 'C:\approved\bap\company-ca.pem' `
  -ClientCertificatePath 'C:\approved\bap\device-certificate.pem' `
  -ClientKeyPath 'C:\approved\bap\device-private-key.pem'
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

Inside Claude, validate `/status` and `/permissions`, then run the safe
read, `.env` denial, and destructive-command denial cases documented in
[testing.md](testing.md). The network BAP Service administrator verifies the
correlated signed audit events.

## Troubleshooting an offline build

The build command explicitly uses `-mod=vendor`. If Go still tries the public
internet, verify `vendor/modules.txt` is present and that the complete source tree
was transferred. The Go compiler itself must already satisfy the `toolchain`
version in `go.mod`; an older compiler may attempt to download a newer toolchain.
