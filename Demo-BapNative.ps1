<#
.SYNOPSIS
Runs and displays the container-free native BAP company demonstration.
#>
param()

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'

Write-Host '=== BAP NATIVE DEMO: strict company gate ===' -ForegroundColor Cyan
& powershell.exe -NoProfile -ExecutionPolicy Bypass -File (Join-Path $PSScriptRoot 'Test-MVP0.ps1') `
    -Runtime Native -RequireCompanyFixtures
if ($LASTEXITCODE -ne 0) { throw "Native MVP-0 gate failed with exit code $LASTEXITCODE." }

$latestRunPath = Join-Path $PSScriptRoot '.bap\native-local\latest-run.txt'
if (-not (Test-Path -LiteralPath $latestRunPath -PathType Leaf)) { throw 'Native run evidence is missing.' }
$latestRun = (Get-Content -LiteralPath $latestRunPath -Raw).Trim()
$serviceState = Join-Path $latestRun 'service-state'
$serviceBinary = Join-Path $PSScriptRoot 'dist\bap-service-windows-amd64.exe'
$captureDirectory = Join-Path $PSScriptRoot '.bap\captures'
$manifestPath = Join-Path $captureDirectory 'certification-manifest.json'

Write-Host ''
Write-Host '=== BAP NATIVE DEMO: company compatibility evidence ===' -ForegroundColor Cyan
$fixtureFiles = @(Get-ChildItem -LiteralPath $captureDirectory -Filter '*.json' -File | Where-Object Name -NotMatch 'manifest')
$fixtureFiles | ForEach-Object {
    $fixture = Get-Content -LiteralPath $_.FullName -Raw | ConvertFrom-Json
    [pscustomobject]@{
        Scenario = $fixture.scenario
        Model = $fixture.model
        Decision = $fixture.actual_decision
        Reason = $fixture.reason_code
        PolicyVersion = $fixture.policy_version
    }
} | Format-Table -AutoSize

if (-not (Test-Path -LiteralPath $manifestPath -PathType Leaf)) { throw 'Certification manifest is missing.' }

Write-Host '=== BAP NATIVE DEMO: signed audit integrity ===' -ForegroundColor Cyan
$previousState = [Environment]::GetEnvironmentVariable('BAP_STATE_DIRECTORY', 'Process')
try {
    $env:BAP_STATE_DIRECTORY = $serviceState
    & $serviceBinary audit verify
    if ($LASTEXITCODE -ne 0) { throw 'Native signed audit-chain verification failed.' }
} finally {
    [Environment]::SetEnvironmentVariable('BAP_STATE_DIRECTORY', $previousState, 'Process')
}

Write-Host ''
Write-Host 'DEMO PASSED.' -ForegroundColor Green
Write-Host 'Proved: signed policy synchronization; safe allow; destructive, unknown, and manual-only deny; prompt advisory; company Sonnet hook compatibility; fixture replay; and signed audit-chain integrity.'
Write-Host "Certification manifest: $manifestPath"
Write-Host "Native run evidence: $latestRun"
