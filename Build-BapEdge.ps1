param(
    [ValidateSet('Auto', 'Podman', 'Docker', 'Native')][string]$Runtime = 'Auto',
    [Alias('Targets')][ValidateSet('Windows', 'All')][string]$Target = 'Windows',
    [string]$Version = ''
)

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'
. (Join-Path $PSScriptRoot 'scripts\Runtime.ps1')
. (Join-Path $PSScriptRoot 'scripts\GoToolchain.ps1')
$engine = ''
if ($Runtime -ne 'Native') {
    try {
        $engine = Get-BapContainerEngine -Runtime $Runtime
    } catch {
        if ($Runtime -ne 'Auto' -or -not (Get-BapGoCommand)) { throw }
        Write-Warning 'No usable Podman/Docker runtime was found; falling back to the installed Go toolchain.'
    }
}
if (-not $engine) {
    & (Join-Path $PSScriptRoot 'Build-BapEdge-Native.ps1') -Target $Target -Version $Version
    return
}
$dist = Join-Path $PSScriptRoot 'dist'
New-Item -ItemType Directory -Force -Path $dist | Out-Null
$mount = "$($PSScriptRoot):/src"

Write-Host "Testing and compiling BAP Edge with $engine..."
& $engine run --rm --volume $mount --workdir /src docker.io/library/golang:1.23-bookworm go test -mod=vendor ./bap-edge/... ./configs ./internal/...
if ($LASTEXITCODE -ne 0) { throw 'BAP Edge tests failed.' }

$targetsToBuild = @(@{ OS = 'windows'; Architecture = 'amd64'; Output = 'dist/bap-edge-windows-amd64.exe' })
if ($Target -eq 'All') {
    $targetsToBuild += @(
        @{ OS = 'linux'; Architecture = 'amd64'; Output = 'dist/bap-edge-linux-amd64' },
        @{ OS = 'linux'; Architecture = 'arm64'; Output = 'dist/bap-edge-linux-arm64' }
    )
}
foreach ($buildTarget in $targetsToBuild) {
    $buildVersion = if ($Version) { $Version } else { 'dev' }
    & $engine run --rm --volume $mount --workdir /src --env CGO_ENABLED=0 --env "GOOS=$($buildTarget.OS)" --env "GOARCH=$($buildTarget.Architecture)" docker.io/library/golang:1.23-bookworm go build -mod=vendor -trimpath -ldflags "-s -w -X main.version=$buildVersion" -o $buildTarget.Output ./bap-edge/cmd
    if ($LASTEXITCODE -ne 0) { throw "BAP Edge compilation failed for $($buildTarget.OS)/$($buildTarget.Architecture)." }
}
Write-Host "BAP Edge build complete: $dist"
