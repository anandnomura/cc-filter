param([string]$OutputPath = '')

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'
$go = Get-Command go -ErrorAction SilentlyContinue
if (-not $go) {
    throw 'Go is not installed or is not on PATH. Install an approved Go 1.23.12 or newer toolchain, open a new PowerShell window, and run go version.'
}
$version = (& go version)
if ($LASTEXITCODE -ne 0) { throw 'The Go compiler could not run.' }
Write-Host "Using locally installed compiler: $version"

if (-not $OutputPath) {
    $OutputPath = Join-Path $PSScriptRoot 'dist\bap-edge-windows-amd64.exe'
}
$outputDirectory = Split-Path -Parent $OutputPath
New-Item -ItemType Directory -Force -Path $outputDirectory | Out-Null

Push-Location $PSScriptRoot
$previousCGO = $env:CGO_ENABLED
$previousGOOS = $env:GOOS
$previousGOARCH = $env:GOARCH
try {
    Write-Host 'Running BAP Edge tests from vendored dependencies (no public module download)...'
    & go test -mod=vendor ./cmd/bap-edge ./configs ./internal/...
    if ($LASTEXITCODE -ne 0) { throw 'BAP Edge tests failed.' }
    $env:CGO_ENABLED = '0'
    $env:GOOS = 'windows'
    $env:GOARCH = 'amd64'
    & go build -mod=vendor -trimpath -o $OutputPath ./cmd/bap-edge
    if ($LASTEXITCODE -ne 0) { throw 'Native Windows BAP Edge compilation failed.' }
} finally {
    $env:CGO_ENABLED = $previousCGO
    $env:GOOS = $previousGOOS
    $env:GOARCH = $previousGOARCH
    Pop-Location
}

$hash = (Get-FileHash -Algorithm SHA256 -LiteralPath $OutputPath).Hash
Write-Host "BAP Edge compiled locally: $OutputPath"
Write-Host "SHA-256: $hash"
