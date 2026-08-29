param([ValidateSet('Auto', 'Podman', 'Docker')][string]$Runtime = 'Auto')

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'
. (Join-Path $PSScriptRoot 'scripts\Runtime.ps1')
$engine = Get-BapContainerEngine -Runtime $Runtime
$runtimeDirectory = Get-BapRuntimeDirectory -Engine $engine
if (-not (Test-Path -LiteralPath $runtimeDirectory)) { throw 'Run Start-Bap.ps1 first.' }
$mount = "$($runtimeDirectory):/var/lib/bap"
& $engine run --rm --volume $mount bap-service:local proposals list
if ($LASTEXITCODE -ne 0) { throw 'Could not read policy proposals.' }
