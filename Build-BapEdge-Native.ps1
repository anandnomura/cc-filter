param(
    [string]$OutputPath = '',
    [string]$Version = '',
    [ValidateSet('Windows', 'All')][string]$Targets = 'Windows'
)

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'
$go = Get-Command go -ErrorAction SilentlyContinue
if (-not $go) {
    throw 'Go is not installed or is not on PATH. Install an approved Go 1.23.12 or newer toolchain, open a new PowerShell window, and run go version.'
}
$goVersionText = (& go version)
if ($LASTEXITCODE -ne 0) { throw 'The Go compiler could not run.' }
if ($goVersionText -notmatch 'go version go(?<major>\d+)\.(?<minor>\d+)(?:\.(?<patch>\d+))?') {
    throw "Could not parse Go compiler version: $goVersionText"
}
$major = [int]$Matches.major
$minor = [int]$Matches.minor
$patch = if ($Matches.patch) { [int]$Matches.patch } else { 0 }
if ($major -lt 1 -or ($major -eq 1 -and ($minor -lt 23 -or ($minor -eq 23 -and $patch -lt 12)))) {
    throw "Go 1.23.12 or newer is required; found: $goVersionText"
}
Write-Host "Using locally installed compiler: $goVersionText"

if (-not $Version) {
    $commit = (& git rev-parse --short=12 HEAD 2>$null)
    $Version = if ($LASTEXITCODE -eq 0 -and $commit) { "company-$commit" } else { 'company-build' }
}

if ($OutputPath -and $Targets -eq 'All') {
    throw '-OutputPath can only be used with -Targets Windows.'
}
if (-not $OutputPath) {
    $OutputPath = Join-Path $PSScriptRoot 'dist\bap-edge-windows-amd64.exe'
}
$outputDirectory = Split-Path -Parent $OutputPath
if (-not $outputDirectory) { $outputDirectory = (Get-Location).Path }
New-Item -ItemType Directory -Force -Path $outputDirectory | Out-Null

Push-Location $PSScriptRoot
$previousCGO = $env:CGO_ENABLED
$previousGOOS = $env:GOOS
$previousGOARCH = $env:GOARCH
try {
    Write-Host 'Running BAP Edge tests from vendored dependencies (no public module download)...'
    & go test -mod=vendor ./cmd/bap-edge ./configs ./internal/...
    if ($LASTEXITCODE -ne 0) { throw 'BAP Edge tests failed.' }
    $targetsToBuild = @(@{ OS = 'windows'; Architecture = 'amd64'; Output = $OutputPath })
    if ($Targets -eq 'All') {
        $targetsToBuild += @(
            @{ OS = 'linux'; Architecture = 'amd64'; Output = (Join-Path $PSScriptRoot 'dist\bap-edge-linux-amd64') },
            @{ OS = 'linux'; Architecture = 'arm64'; Output = (Join-Path $PSScriptRoot 'dist\bap-edge-linux-arm64') }
        )
    }
    foreach ($target in $targetsToBuild) {
        $env:CGO_ENABLED = '0'
        $env:GOOS = $target.OS
        $env:GOARCH = $target.Architecture
        & go build -mod=vendor -trimpath -ldflags "-s -w -X main.version=$Version" -o $target.Output ./cmd/bap-edge
        if ($LASTEXITCODE -ne 0) { throw "Native BAP Edge compilation failed for $($target.OS)/$($target.Architecture)." }
        $hash = (Get-FileHash -Algorithm SHA256 -LiteralPath $target.Output).Hash
        $checksumPath = "$($target.Output).sha256"
        "$($hash.ToLowerInvariant())  $([IO.Path]::GetFileName($target.Output))" | Set-Content -LiteralPath $checksumPath -Encoding ascii
        Write-Host "BAP Edge compiled locally: $($target.Output)"
        Write-Host "SHA-256: $hash"
    }
} finally {
    $env:CGO_ENABLED = $previousCGO
    $env:GOOS = $previousGOOS
    $env:GOARCH = $previousGOARCH
    Pop-Location
}

Write-Host "Native BAP Edge build complete: $outputDirectory"
