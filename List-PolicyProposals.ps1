param([ValidateSet('Auto', 'Podman', 'Docker')][string]$Runtime = 'Auto')

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'
. (Join-Path $PSScriptRoot 'scripts\Runtime.ps1')
$engine = Get-BapContainerEngine -Runtime $Runtime
& $engine exec bap-service-local bap-service proposals list
if ($LASTEXITCODE -ne 0) { throw 'Could not read policy proposals.' }
