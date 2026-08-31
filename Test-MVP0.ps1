param(
    [ValidateSet('Auto', 'Podman', 'Docker')][string]$Runtime = 'Auto',
    [switch]$RequireCompanyFixtures,
    [string[]]$RequiredModels = @()
)

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'

# Docker and Podman use the same intentional loopback port. When the caller
# selects one explicitly, stop only the other runtime's local BAP service.
if ($Runtime -eq 'Docker' -and (Get-Command podman -ErrorAction SilentlyContinue)) {
    $other = @(& podman ps --filter 'name=^bap-service-local$' --format '{{.Names}}' 2>$null)
    if ('bap-service-local' -in $other) { Write-Host 'Stopping the Podman BAP service that owns port 8443...'; & podman stop bap-service-local | Out-Host }
}
if ($Runtime -eq 'Podman' -and (Get-Command docker -ErrorAction SilentlyContinue)) {
    $other = @(& docker ps --filter 'name=^/bap-service-local$' --format '{{.Names}}' 2>$null)
    if ('bap-service-local' -in $other) { Write-Host 'Stopping the Docker BAP service that owns port 8443...'; & docker stop bap-service-local | Out-Host }
}

Write-Host 'Building the current BAP Edge and BAP Service sources...'
& (Join-Path $PSScriptRoot 'Build-Bap.ps1') -Runtime $Runtime

Write-Host 'Running focused policy corpus and signed rollout lifecycle gates...'
& (Join-Path $PSScriptRoot 'Test-PolicyRollout.ps1') -Runtime $Runtime

Write-Host 'Starting the rebuilt service and durable MySQL store...'
& (Join-Path $PSScriptRoot 'Start-Bap.ps1') -Runtime $Runtime

Write-Host 'Running the MVP-0 policy, fail-closed, audit, tracing, and storage suite...'
& (Join-Path $PSScriptRoot 'Test-Bap.ps1') -Runtime $Runtime

Write-Host 'Checking exact Claude Code/model fixture certification...'
& (Join-Path $PSScriptRoot 'Test-ClaudeFixtures.ps1') -Runtime $Runtime -RequiredModels $RequiredModels -RequireCompanyFixtures:$RequireCompanyFixtures

Write-Host 'Showing current control-plane, Edge lease, and audit-queue status...'
& (Join-Path $PSScriptRoot 'Show-BapStatus.ps1') -Runtime $Runtime

Write-Host 'PASS: model-independent MVP-0A certification foundation.'
Write-Host 'PASS: control-plane/data-plane contract: signed central rules, local Cedar decisions, bounded offline lease, rollback/tamper/expiry default deny.'
Write-Host 'PASS: exact Edge hook contract: git status --short and centrally configured ls -al allowed; git reset --hard denied.'
Write-Host 'PASS: data-driven bypass corpus and live signed policy rollout lifecycle.'
if ($RequireCompanyFixtures) {
    Write-Host 'PASS: required exact company Claude Code/model fixtures are certified.'
} else {
    Write-Host 'PENDING: run with -RequireCompanyFixtures after capturing the exact company Claude Code, Sonnet, and Opus versions.'
}
