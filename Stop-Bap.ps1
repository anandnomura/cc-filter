param([ValidateSet('Auto', 'Podman', 'Docker')][string]$Runtime = 'Auto')

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'
. (Join-Path $PSScriptRoot 'scripts\Runtime.ps1')
$engine = Get-BapContainerEngine -Runtime $Runtime
& $engine rm --force bap-service-local
if ($LASTEXITCODE -ne 0) { throw 'Could not stop the BAP Service container.' }
Write-Host 'BAP Service stopped. Tool calls will now fail closed.'
