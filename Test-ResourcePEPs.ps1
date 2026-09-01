param([ValidateSet('Auto', 'Native', 'Docker', 'Podman')][string]$Runtime = 'Auto')

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'
. (Join-Path $PSScriptRoot 'scripts\GoToolchain.ps1')
. (Join-Path $PSScriptRoot 'scripts\Runtime.ps1')
$goCommand = Get-BapGoCommand
$mavenCommand = Get-Command mvn -ErrorAction SilentlyContinue | Select-Object -First 1
if ($Runtime -eq 'Auto') {
    if ($goCommand -and $mavenCommand) { $Runtime = 'Native' }
    else { $Runtime = Get-BapContainerEngine -Runtime Auto }
}
if ($Runtime -eq 'Native') {
    if (-not $goCommand) { $goCommand = Get-BapGoCommand -Required }
    if (-not $mavenCommand) { throw 'Native resource PEP tests require Maven on PATH and Java 21.' }
    Push-Location $PSScriptRoot
    try {
        & $goCommand test -mod=vendor -v ./bap-mcp-pep/internal/mcppep ./internal/policybundle ./bap-edge/internal/bapedge ./bap-service/internal/agentsts ./bap-service/internal/agentgateway ./bap-service/internal/server
        if ($LASTEXITCODE -ne 0) { throw 'Go resource PEP/AgentGrant tests failed.' }
        & $mavenCommand.Source --batch-mode -f (Join-Path $PSScriptRoot 'bap-api-gateway-springcloud\pom.xml') test
        if ($LASTEXITCODE -ne 0) { throw 'Spring Cloud API Gateway PEP tests failed.' }
    } finally { Pop-Location }
    Write-Host 'PASS: API and MCP PEPs consume exact grants, reject tampering/replay, strip BAP fields, and use PEP-owned identities.'
    return
}
& (Join-Path $PSScriptRoot 'Build-ResourcePEPs.ps1') -Runtime $Runtime
if ($LASTEXITCODE -ne 0) { throw 'Container resource PEP verification failed.' }
Write-Host "PASS: resource PEP container tests completed during both image builds with $Runtime."
