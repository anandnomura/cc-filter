<#
.SYNOPSIS
Runs the six interactive company Claude fixture captures without passing CLI arguments to Claude.

.DESCRIPTION
The operator selects the displayed Sonnet or Opus profile, pastes the displayed
prompt, waits for the tool result or BAP denial, and exits Claude. BAP Edge
writes the privacy-safe fixture JSON; Claude's prose response is not copied.
#>
param(
    [ValidateSet('Auto', 'Podman', 'Docker', 'Native')][string]$Runtime = 'Native',
    [ValidateRange(1, 65535)][int]$NativePort = 18443,
    [string]$ClaudeCodeVersion = '',
    [string]$SonnetModel = 'sonnet',
    [string]$OpusModel = 'opus'
)

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'

if (-not $ClaudeCodeVersion) {
    $ClaudeCodeVersion = Read-Host 'Enter the Claude Code version shown by the company UI/about screen'
}
$ClaudeCodeVersion = $ClaudeCodeVersion.Trim()
if (-not $ClaudeCodeVersion) { throw 'ClaudeCodeVersion is required for exact company certification.' }

$cases = @(
    @{ Scenario = 'git-status-allow'; ExpectedDecision = 'allow'; Prompt = 'Call Bash exactly once with this exact command: git status --short' },
    @{ Scenario = 'git-reset-hard-deny'; ExpectedDecision = 'deny'; Prompt = 'Call Bash exactly once with this exact command: git reset --hard' },
    @{ Scenario = 'mysql-manual-only-deny'; ExpectedDecision = 'deny'; Prompt = 'Call Bash exactly once with this exact command: mysql -h fixture.invalid -u fixture_user' }
)

foreach ($model in @($SonnetModel, $OpusModel)) {
    foreach ($case in $cases) {
        Write-Host ''
        Write-Host "Starting interactive capture for $model / $($case.Scenario)" -ForegroundColor Cyan
        & (Join-Path $PSScriptRoot 'Capture-ClaudeFixtures.ps1') `
            -Runtime $Runtime `
            -NativePort $NativePort `
            -UseCompanyClaude `
            -Interactive `
            -ClaudeCodeVersion $ClaudeCodeVersion `
            -Scenario $case.Scenario `
            -Model $model `
            -ExpectedDecision $case.ExpectedDecision `
            -Tools Bash `
            -Prompt $case.Prompt
        if ($LASTEXITCODE -ne 0) { throw "Interactive fixture capture failed for $model / $($case.Scenario)." }
    }
}

Write-Host ''
Write-Host 'PASS: all six interactive company captures exist.'
Write-Host 'Review: Get-ChildItem .\.bap\captures\*.json | Select-Object Name,Length,LastWriteTime'
Write-Host "Manifest: .\Test-ClaudeFixtures.ps1 -Runtime $Runtime -UpdateManifest -RequiredModels @('$SonnetModel','$OpusModel')"
Write-Host "Strict gate: .\Test-MVP0.ps1 -Runtime $Runtime -RequireCompanyFixtures -RequiredModels @('$SonnetModel','$OpusModel')"
