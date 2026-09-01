<#
.SYNOPSIS
Captures one privacy-safe Claude/BAP certification fixture.

.DESCRIPTION
Scenario is a stable label for the test case. Prompt is the instruction sent
to Claude. Native runs use the installed Windows Go binaries and do not require
Docker or Podman. With -Interactive, the prompt is displayed for the operator
to paste into the company Claude UI and no arguments are passed to Claude.

.EXAMPLE
.\Capture-ClaudeFixtures.ps1 -Runtime Native -UseCompanyClaude -Interactive -ClaudeCodeVersion 'company-release-2026.08' -Scenario git-status-allow -Model sonnet -ExpectedDecision allow -Tools Bash -Prompt 'Call Bash exactly once with this exact command: git status --short'

.EXAMPLE
.\Capture-ClaudeFixtures.ps1 -Runtime Native -UseCompanyClaude -Interactive -ClaudeCodeVersion 'company-release-2026.08' -Scenario mysql-manual-only-deny -Model opus -ExpectedDecision deny -Tools Bash -Prompt 'Call Bash exactly once with this exact command: mysql -h fixture.invalid -u fixture_user'
#>
param(
    [Parameter(Mandatory)][ValidatePattern('^[A-Za-z0-9][A-Za-z0-9._-]{0,79}$')][string]$Scenario,
    [Parameter(Mandatory)][string]$Model,
    [Parameter(Mandatory)][ValidateSet('allow', 'deny')][string]$ExpectedDecision,
    [Parameter(Mandatory)][string]$Prompt,
    [ValidateSet('Auto', 'Podman', 'Docker', 'Native')][string]$Runtime = 'Auto',
    [ValidateRange(1, 65535)][int]$NativePort = 18443,
    [string]$CaptureDirectory = '',
    [string]$Tools = 'Bash',
    [switch]$UseCompanyClaude,
    [switch]$Interactive,
    [string]$ClaudeCodeVersion = '',
    [string[]]$ClaudeArguments = @()
)

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'

function Get-ClaudeExecutablePath {
    # Prefer the company wrapper when both it and an underlying executable are
    # present. Interactive company capture intentionally invokes it with no args.
    foreach ($name in @('claude.cmd', 'claude.exe', 'claude')) {
        $command = Get-Command $name -ErrorAction SilentlyContinue | Select-Object -First 1
        if ($null -eq $command) { continue }
        foreach ($propertyName in @('Path', 'Source', 'Definition')) {
            $property = $command.PSObject.Properties[$propertyName]
            if ($null -ne $property -and $property.Value -and (Test-Path -LiteralPath $property.Value -PathType Leaf)) {
                return (Resolve-Path -LiteralPath $property.Value).Path
            }
        }
    }
    $fallback = Join-Path $env:USERPROFILE '.local\bin\claude.exe'
    if (Test-Path -LiteralPath $fallback -PathType Leaf) { return (Resolve-Path -LiteralPath $fallback).Path }
    return $null
}

$claudeExecutable = Get-ClaudeExecutablePath
if (-not $claudeExecutable) { throw 'Claude Code must be installed and available on PATH.' }
if ($Interactive) {
    if (-not $UseCompanyClaude) { throw '-Interactive requires -UseCompanyClaude.' }
    if (-not $ClaudeCodeVersion) {
        $ClaudeCodeVersion = Read-Host 'Enter the Claude Code version shown by the company UI/about screen'
    }
    $claudeVersion = $ClaudeCodeVersion.Trim()
    if (-not $claudeVersion) { throw 'ClaudeCodeVersion is required for an exact interactive company capture.' }
} else {
    $claudeVersion = ((& $claudeExecutable --version 2>&1) -join ' ').Trim()
    if ($LASTEXITCODE -ne 0 -or -not $claudeVersion) { throw 'Could not determine the exact Claude Code version.' }
}

if (-not $CaptureDirectory) { $CaptureDirectory = Join-Path $PSScriptRoot '.bap\captures' }
$captureDirectory = [IO.Path]::GetFullPath($CaptureDirectory)
New-Item -ItemType Directory -Force -Path $captureDirectory | Out-Null
$beforeFixtures = @(Get-ChildItem -LiteralPath $captureDirectory -Filter '*.json' -File -ErrorAction SilentlyContinue | Where-Object Name -NotMatch 'manifest')
$beforePaths = @{}
foreach ($fixture in $beforeFixtures) { $beforePaths[$fixture.FullName] = (Get-FileHash -LiteralPath $fixture.FullName -Algorithm SHA256).Hash }
$before = $beforeFixtures.Count

$names = @('BAP_FIXTURE_CAPTURE_DIRECTORY','BAP_FIXTURE_SCENARIO','BAP_FIXTURE_MODEL','BAP_FIXTURE_CLAUDE_VERSION','BAP_FIXTURE_EXPECTED_DECISION')
$previous = @{}
foreach ($name in $names) { $previous[$name] = [Environment]::GetEnvironmentVariable($name, 'Process') }
try {
    $env:BAP_FIXTURE_CAPTURE_DIRECTORY = $captureDirectory
    $env:BAP_FIXTURE_SCENARIO = $Scenario
    $env:BAP_FIXTURE_MODEL = $Model
    $env:BAP_FIXTURE_CLAUDE_VERSION = $claudeVersion
    $env:BAP_FIXTURE_EXPECTED_DECISION = $ExpectedDecision

    if ($Interactive) {
        Write-Host ''
        Write-Host "Select this company model/profile in the Claude UI: $Model"
        Write-Host "Scenario: $Scenario"
        Write-Host "Expected BAP decision: $ExpectedDecision"
        Write-Host 'Paste this exact prompt into Claude:'
        Write-Host $Prompt
        Write-Host 'After the tool result or BAP denial appears, exit Claude to finish capture.'
        Write-Host ''
    }

    $launcherPrompt = ''
    if (-not $Interactive) { $launcherPrompt = $Prompt }
    $launcherParameters = @{
        UseCompanyClaude = $UseCompanyClaude
        InteractiveClaude = $Interactive
        CompanyCliArguments = $UseCompanyClaude -and (-not $Interactive)
        Model = $Model
        Tools = $Tools
        Print = -not $Interactive
        Prompt = $launcherPrompt
    }
    if ($Runtime -eq 'Native') {
        $launcherParameters['Port'] = $NativePort
        & (Join-Path $PSScriptRoot 'Start-BapNativeLocal.ps1') @launcherParameters @ClaudeArguments
    } else {
        $launcherParameters['Runtime'] = $Runtime
        & (Join-Path $PSScriptRoot 'Start-LocalClaude.ps1') @launcherParameters @ClaudeArguments
    }
    if ($LASTEXITCODE -ne 0) { throw "Claude fixture scenario $Scenario exited with code $LASTEXITCODE." }
} finally {
    foreach ($name in $names) { [Environment]::SetEnvironmentVariable($name, $previous[$name], 'Process') }
}

$fixtures = @(Get-ChildItem -LiteralPath $captureDirectory -Filter '*.json' -File | Where-Object Name -NotMatch 'manifest')
$changedFixtures = @($fixtures | Where-Object {
    $currentHash = (Get-FileHash -LiteralPath $_.FullName -Algorithm SHA256).Hash
    -not $beforePaths.ContainsKey($_.FullName) -or $beforePaths[$_.FullName] -ne $currentHash
})
$match = @($changedFixtures | ForEach-Object { try { Get-Content -LiteralPath $_.FullName -Raw | ConvertFrom-Json } catch {} } | Where-Object { $_.scenario -eq $Scenario -and $_.model -eq $Model })
if ($match.Count -eq 0) {
    throw 'No new fixture was captured in this session. Confirm Claude requested the displayed tool and that the current BAP Edge hook ran.'
}
$latest = $match | Sort-Object captured_at | Select-Object -Last 1
if ($latest.actual_decision -ne $ExpectedDecision) {
    throw "Scenario $Scenario produced $($latest.actual_decision), expected $ExpectedDecision. The fixture is not certifiable."
}
Write-Host "PASS: captured privacy-safe fixture $Scenario for $Model using Claude Code $claudeVersion."
Write-Host "Capture directory: $captureDirectory"
Write-Host "Captured fixture count: $($fixtures.Count) (previously $before)"
Write-Host "After all required captures, run: .\Test-ClaudeFixtures.ps1 -Runtime $Runtime -UpdateManifest -RequiredModels @('$Model')"
