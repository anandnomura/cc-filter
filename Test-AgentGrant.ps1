param([ValidateSet('Auto', 'Podman', 'Docker', 'Native')][string]$Runtime = 'Auto')

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'
. (Join-Path $PSScriptRoot 'scripts\GoToolchain.ps1')

$packages = @(
    './internal/agentgrant',
    './bap-service/internal/agentsts',
    './bap-service/internal/agentgateway',
    './internal/bapedge'
)

$goCommand = Get-BapGoCommand
if ($Runtime -eq 'Native' -or ($Runtime -eq 'Auto' -and $goCommand)) {
    if (-not $goCommand) { $goCommand = Get-BapGoCommand -Required }
    Push-Location $PSScriptRoot
    try {
        & $goCommand test -mod=vendor -v @packages
        if ($LASTEXITCODE -ne 0) { throw 'AgentGrant test suite failed.' }
    } finally {
        Pop-Location
    }
} else {
    . (Join-Path $PSScriptRoot 'scripts\Runtime.ps1')
    $engine = Get-BapContainerEngine -Runtime $Runtime
    $mount = "$($PSScriptRoot):/src"
    & $engine run --rm --volume $mount --workdir /src docker.io/library/golang:1.23-bookworm `
        go test -mod=vendor -v @packages
    if ($LASTEXITCODE -ne 0) { throw 'AgentGrant container test suite failed.' }
}

Write-Host 'PASS: intent, exact request, audience, policy digest, epoch, expiry, and one-use bindings.'
Write-Host 'PASS: gateway rejects tampering/replay and invokes the backend only after atomic consumption.'
