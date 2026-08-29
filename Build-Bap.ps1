param([ValidateSet('Auto', 'Podman', 'Docker')][string]$Runtime = 'Auto')

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'
Write-Host 'Building the independently deployable BAP Edge and BAP Service components...'
& (Join-Path $PSScriptRoot 'Build-BapEdge.ps1') -Runtime $Runtime
& (Join-Path $PSScriptRoot 'Build-BapService.ps1') -Runtime $Runtime
Write-Host 'Combined development build complete.'
