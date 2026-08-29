param(
    [ValidateSet('Auto', 'Podman', 'Docker')][string]$Runtime = 'Auto',
    [ValidateSet('Windows', 'All')][string]$Targets = 'Windows'
)

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'
. (Join-Path $PSScriptRoot 'scripts\Runtime.ps1')
$engine = Get-BapContainerEngine -Runtime $Runtime
$dist = Join-Path $PSScriptRoot 'dist'
New-Item -ItemType Directory -Force -Path $dist | Out-Null
$mount = "$($PSScriptRoot):/src"

Write-Host "Testing and compiling BAP Edge with $engine..."
& $engine run --rm --volume $mount --workdir /src docker.io/library/golang:1.23-bookworm go test ./cmd/bap-edge ./configs ./internal/...
if ($LASTEXITCODE -ne 0) { throw 'BAP Edge tests failed.' }

$targetsToBuild = @(@{ OS = 'windows'; Architecture = 'amd64'; Output = 'dist/bap-edge-windows-amd64.exe' })
if ($Targets -eq 'All') {
    $targetsToBuild += @(
        @{ OS = 'linux'; Architecture = 'amd64'; Output = 'dist/bap-edge-linux-amd64' },
        @{ OS = 'linux'; Architecture = 'arm64'; Output = 'dist/bap-edge-linux-arm64' }
    )
}
foreach ($target in $targetsToBuild) {
    & $engine run --rm --volume $mount --workdir /src --env CGO_ENABLED=0 --env "GOOS=$($target.OS)" --env "GOARCH=$($target.Architecture)" docker.io/library/golang:1.23-bookworm go build -trimpath -o $target.Output ./cmd/bap-edge
    if ($LASTEXITCODE -ne 0) { throw "BAP Edge compilation failed for $($target.OS)/$($target.Architecture)." }
}
Write-Host "BAP Edge build complete: $dist"
