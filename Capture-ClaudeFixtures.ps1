param(
    [Parameter(Mandatory)][ValidatePattern('^[A-Za-z0-9][A-Za-z0-9._-]{0,79}$')][string]$Scenario,
    [Parameter(Mandatory)][string]$Model,
    [Parameter(Mandatory)][ValidateSet('allow', 'deny')][string]$ExpectedDecision,
    [Parameter(Mandatory)][string]$Prompt,
    [ValidateSet('Auto', 'Podman', 'Docker')][string]$Runtime = 'Auto',
    [string]$CaptureDirectory = '',
    [string]$Tools = 'Bash',
    [switch]$UseCompanyClaude,
    [string[]]$ClaudeArguments = @()
)

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'

$claude = Get-Command claude -ErrorAction SilentlyContinue
if (-not $claude) { throw 'Claude Code must be installed and available on PATH.' }
$claudeVersion = ((& $claude.Source --version 2>&1) -join ' ').Trim()
if ($LASTEXITCODE -ne 0 -or -not $claudeVersion) { throw 'Could not determine the exact Claude Code version.' }

if (-not $CaptureDirectory) { $CaptureDirectory = Join-Path $PSScriptRoot '.bap\captures' }
$captureDirectory = [IO.Path]::GetFullPath($CaptureDirectory)
New-Item -ItemType Directory -Force -Path $captureDirectory | Out-Null
$before = @(Get-ChildItem -LiteralPath $captureDirectory -Filter '*.json' -File -ErrorAction SilentlyContinue | Where-Object Name -NotMatch 'manifest').Count

$names = @('BAP_FIXTURE_CAPTURE_DIRECTORY','BAP_FIXTURE_SCENARIO','BAP_FIXTURE_MODEL','BAP_FIXTURE_CLAUDE_VERSION','BAP_FIXTURE_EXPECTED_DECISION')
$previous = @{}
foreach ($name in $names) { $previous[$name] = [Environment]::GetEnvironmentVariable($name, 'Process') }
try {
    $env:BAP_FIXTURE_CAPTURE_DIRECTORY = $captureDirectory
    $env:BAP_FIXTURE_SCENARIO = $Scenario
    $env:BAP_FIXTURE_MODEL = $Model
    $env:BAP_FIXTURE_CLAUDE_VERSION = $claudeVersion
    $env:BAP_FIXTURE_EXPECTED_DECISION = $ExpectedDecision

    & (Join-Path $PSScriptRoot 'Start-LocalClaude.ps1') `
        -Runtime $Runtime -UseCompanyClaude:$UseCompanyClaude -Model $Model -Tools $Tools `
        -Print -Prompt $Prompt @ClaudeArguments
    if ($LASTEXITCODE -ne 0) { throw "Claude fixture scenario $Scenario exited with code $LASTEXITCODE." }
} finally {
    foreach ($name in $names) { [Environment]::SetEnvironmentVariable($name, $previous[$name], 'Process') }
}

$fixtures = @(Get-ChildItem -LiteralPath $captureDirectory -Filter '*.json' -File | Where-Object Name -NotMatch 'manifest')
$match = @($fixtures | ForEach-Object { try { Get-Content -LiteralPath $_.FullName -Raw | ConvertFrom-Json } catch {} } | Where-Object { $_.scenario -eq $Scenario -and $_.model -eq $Model })
if ($match.Count -eq 0) {
    throw 'No fixture was captured. If managed hooks are installed, reinstall the current BAP Edge binary and retry.'
}
$latest = $match | Sort-Object captured_at | Select-Object -Last 1
if ($latest.actual_decision -ne $ExpectedDecision) {
    throw "Scenario $Scenario produced $($latest.actual_decision), expected $ExpectedDecision. The fixture is not certifiable."
}
Write-Host "PASS: captured privacy-safe fixture $Scenario for $Model using Claude Code $claudeVersion."
Write-Host "Capture directory: $captureDirectory"
Write-Host "Captured fixture count: $($fixtures.Count) (previously $before)"
Write-Host 'After capturing the same scenario on every required model, create and verify the manifest with Test-ClaudeFixtures.ps1 -UpdateManifest -RequiredModels sonnet,opus.'
