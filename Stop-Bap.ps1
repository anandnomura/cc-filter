param([ValidateSet('Auto', 'Podman', 'Docker')][string]$Runtime = 'Auto')

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'
. (Join-Path $PSScriptRoot 'scripts\Runtime.ps1')
$engine = Get-BapContainerEngine -Runtime $Runtime
$stopTimeoutFlag = if ($engine -eq 'docker') { '--timeout' } else { '--time' }
$existingContainers = @(& $engine ps --all --filter 'name=^/bap-' --format '{{.Names}}')
if ('bap-service-local' -in $existingContainers) {
    & $engine stop $stopTimeoutFlag 15 bap-service-local | Out-Host
    & $engine rm bap-service-local | Out-Host
    if ($LASTEXITCODE -ne 0) { throw 'Could not stop the BAP Service container.' }
}
if ('bap-mysql-local' -in $existingContainers) {
    & $engine stop $stopTimeoutFlag 30 bap-mysql-local | Out-Host
    & $engine rm bap-mysql-local | Out-Host
}
Write-Host 'BAP Service and local MySQL stopped. Tool calls will now fail closed.'
