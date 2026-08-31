param(
    [ValidateSet('Auto', 'Podman', 'Docker')][string]$Runtime = 'Auto',
    [switch]$SkipBuild,
    [switch]$KeepRunning
)

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'

function Show-Step([int]$Number, [string]$Message) {
    Write-Host ""
    Write-Host "=== DEMO STEP ${Number}: $Message ===" -ForegroundColor Cyan
}

Show-Step 1 'Build and run all Go tests inside a container'
if (-not $SkipBuild) {
    & (Join-Path $PSScriptRoot 'Build-Bap.ps1') -Runtime $Runtime
} else {
    Write-Host 'Skipped by -SkipBuild.'
}

Show-Step 2 'Initialize HTTPS, grant signing, audit signing, and local API credentials'
& (Join-Path $PSScriptRoot 'Initialize-Certificates.ps1') -Runtime $Runtime

Show-Step 3 'Start BAP Service over HTTPS'
& (Join-Path $PSScriptRoot 'Start-Bap.ps1') -Runtime $Runtime

Show-Step 4 'Run case-by-case authorization and audit acceptance tests'
& (Join-Path $PSScriptRoot 'Test-PolicyRollout.ps1') -Runtime $Runtime
& (Join-Path $PSScriptRoot 'Test-Bap.ps1') -Runtime $Runtime
& (Join-Path $PSScriptRoot 'Test-ClaudeFixtures.ps1') -Runtime $Runtime
& (Join-Path $PSScriptRoot 'Show-BapStatus.ps1') -Runtime $Runtime

Show-Step 5 'Verify the signed audit hash chain'
& (Join-Path $PSScriptRoot 'View-AuditTrail.ps1') -Runtime $Runtime -VerifyOnly

Show-Step 6 'Show pending missing-policy proposals'
& (Join-Path $PSScriptRoot 'List-PolicyProposals.ps1') -Runtime $Runtime

Write-Host ""
Write-Host 'DEMO PASSED.' -ForegroundColor Green
Write-Host 'The automated cases proved: signed policy rollout, command bypass resistance, API authentication, workload/session correlation, safe allow, secret/outside/destructive/unknown deny, local Edge decision audit, outcome audit, HTTPS, Cedar, and audit integrity.'
Write-Host 'Managed-settings installation is intentionally separate because it requires an elevated administrator shell.'

if ($KeepRunning) {
    Write-Host 'BAP Service remains running because -KeepRunning was selected.'
} else {
    Show-Step 7 'Stop the local service'
    & (Join-Path $PSScriptRoot 'Stop-Bap.ps1') -Runtime $Runtime
}
