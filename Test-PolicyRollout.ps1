param([ValidateSet('Auto', 'Native', 'Docker', 'Podman')][string]$Runtime = 'Auto')

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'
. (Join-Path $PSScriptRoot 'scripts\GoToolchain.ps1')
$goCommand = Get-BapGoCommand
$engine = ''
if ($Runtime -eq 'Auto') {
    . (Join-Path $PSScriptRoot 'scripts\Runtime.ps1')
    try {
        $engine = Get-BapContainerEngine -Runtime Auto
        $Runtime = if ((Split-Path $engine -Leaf) -match 'podman') { 'Podman' } else { 'Docker' }
    } catch {
        if (-not $goCommand) { throw }
        $Runtime = 'Native'
    }
}

function Invoke-PolicyTest {
    param([Parameter(Mandatory)][string]$Package, [Parameter(Mandatory)][string]$Pattern)
    if ($Runtime -eq 'Native') {
        if (-not $script:goCommand) { $script:goCommand = Get-BapGoCommand -Required }
        & $script:goCommand test -mod=vendor -v $Package -run $Pattern
    } else {
        if (-not $script:engine) {
            . (Join-Path $PSScriptRoot 'scripts\Runtime.ps1')
            $script:engine = Get-BapContainerEngine -Runtime $Runtime
        }
        $mount = "$($PSScriptRoot):/src"
        & $script:engine run --rm --volume $mount --workdir /src docker.io/library/golang:1.23-bookworm `
            go test -mod=vendor -v $Package -run $Pattern
    }
}

Write-Host 'Running the data-driven command and bypass policy corpus...'
Invoke-PolicyTest -Package './internal/policybundle' -Pattern '^TestCommandPolicyCorpus$'
if ($LASTEXITCODE -ne 0) { throw 'The command and bypass policy corpus failed.' }

Write-Host 'Running the modeled Claude tool-schema and local-decision corpus...'
Invoke-PolicyTest -Package './bap-edge/internal/bapedge' -Pattern '^TestMVP0ToolContractCorpus$'
if ($LASTEXITCODE -ne 0) { throw 'The modeled Claude tool contract corpus failed.' }

Write-Host 'Running the signed Service-to-Edge policy rollout lifecycle...'
Invoke-PolicyTest -Package './bap-service/internal/server' -Pattern '^TestSignedPolicyRolloutEndToEnd$'
if ($LASTEXITCODE -ne 0) { throw 'The signed policy rollout lifecycle failed.' }

Write-Host 'PASS: command/bypass corpus.'
Write-Host 'PASS: modeled Claude tool schemas and local policy decisions.'
Write-Host 'PASS: v1 allow -> v2 rule removal deny.'
Write-Host 'PASS: CURRENT, forced update, rollback, equivocation, tamper, kill switch, and offline-expiry controls.'
