<#
.SYNOPSIS
Verifies that the reviewer hash helper matches BAP Service hashing semantics.
#>
param()

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'
$helper = Join-Path $PSScriptRoot 'Find-ShadowCandidateHash.ps1'

$commandResult = & $helper -Command 'git status'
if ($commandResult.TargetSummary -ne 'command-sha256:e62b04aadf39df1a47b771265e4ae5c452df3f1903d5c263ab00f088e86102f6') {
    throw 'ASCII command hashing does not match BAP Service.'
}

$unicodeCommand = 'caf' + [char]0x00e9 + ' ' + [char]::ConvertFromUtf32(0x1F680)
$unicodeResult = & $helper -Command $unicodeCommand
if ($unicodeResult.TargetSummary -ne 'command-sha256:22b8c9581c75829df9ef018ebff80267c35f980ae1501f77d8a93ee193bede78') {
    throw 'UTF-8 command hashing does not match BAP Service.'
}

$unicodePath = 'C:\Temp\R' + [char]0x00e9 + 'sum' + [char]0x00e9 + '.txt'
$pathResult = & $helper -OutsideWorkspacePath $unicodePath
if ($pathResult.TargetSummary -ne 'outside-workspace-sha256:6ae51dc88e77d3c176421e9bc807c3f55ae2a008ccdedcfd9cd18a5f214c224e') {
    throw 'Outside-workspace slash normalization or UTF-8 hashing does not match BAP Service.'
}

$matchResult = & $helper -Command 'git status' -TargetHash $commandResult.TargetSummary
if ($matchResult.Matches -ne $true) { throw 'A matching candidate hash was rejected.' }

$rejectedBoth = $false
try {
    & $helper -Command 'git status' -OutsideWorkspacePath 'C:\Temp\file.txt' *> $null
} catch {
    $rejectedBoth = $_.Exception.Message -like '*exactly one*'
}
if (-not $rejectedBoth) { throw 'The helper did not reject two candidate inputs.' }

$previousErrorPreference = $ErrorActionPreference
$ErrorActionPreference = 'Continue'
try {
    $mismatchOutput = & powershell.exe -NoProfile -File $helper -Command 'git status' -TargetHash 'command-sha256:deadbeef' 2>&1 | Out-String
    $mismatchExitCode = $LASTEXITCODE
} finally {
    $ErrorActionPreference = $previousErrorPreference
}
if ($mismatchExitCode -eq 0 -or $mismatchOutput -notlike '*NO MATCH*') {
    throw 'A target-hash mismatch did not produce a clear nonzero process result.'
}

Write-Host 'PASS: reviewer candidate hashes match BAP Service for ASCII, UTF-8, and normalized paths; invalid and mismatched inputs fail.'
