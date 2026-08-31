param(
    [ValidateSet('Auto', 'Podman', 'Docker', 'Native')][string]$Runtime = 'Auto',
    [string]$Tag = 'bap-service:local',
    [string]$Version = 'dev',
    [string]$BuildImage = 'docker.io/library/golang:1.23-bookworm',
    [string]$RuntimeImage = 'docker.io/library/debian:bookworm-slim',
    [ValidateSet('Windows', 'Linux', 'All')][string]$NativeTarget = 'Windows',
    [ValidateSet('amd64', 'arm64', 'All')][string]$Architecture = 'amd64'
)

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'
if ($Runtime -eq 'Native') {
    & (Join-Path $PSScriptRoot 'Build-BapService-Native.ps1') -Target $NativeTarget -Architecture $Architecture -Version $Version
    Write-Warning 'Native mode produced executable binaries, not an OCI image. Use Docker/Podman or the company packaging pipeline when an OCI image is required.'
    return
}
. (Join-Path $PSScriptRoot 'scripts\Runtime.ps1')
$engine = ''
try {
    $engine = Get-BapContainerEngine -Runtime $Runtime
} catch {
    if ($Runtime -ne 'Auto' -or -not (Get-Command go -ErrorAction SilentlyContinue)) { throw }
    Write-Warning 'No usable Podman/Docker runtime was found; compiling BAP Service with the installed Go toolchain.'
    & (Join-Path $PSScriptRoot 'Build-BapService-Native.ps1') -Target $NativeTarget -Architecture $Architecture -Version $Version
    return
}
Write-Host "Building the Linux BAP Service OCI image with $engine..."
& $engine build --file (Join-Path $PSScriptRoot 'Containerfile') --tag $Tag `
    --build-arg "BAP_VERSION=$Version" --build-arg "GO_BUILD_IMAGE=$BuildImage" `
    --build-arg "RUNTIME_IMAGE=$RuntimeImage" $PSScriptRoot
if ($LASTEXITCODE -ne 0) { throw 'BAP Service image build failed.' }
Write-Host "BAP Service image build complete: $Tag"
