param(
    [ValidateSet('Auto', 'Podman', 'Docker')][string]$Runtime = 'Auto',
    [switch]$VerifyOnly
)

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'
. (Join-Path $PSScriptRoot 'scripts\Runtime.ps1')
$engine = Get-BapContainerEngine -Runtime $Runtime
$runtimeDirectory = Get-BapRuntimeDirectory -Engine $engine
$mount = "${runtimeDirectory}:/var/lib/bap"

& $engine run --rm --volume $mount bap-service:local audit verify
if ($LASTEXITCODE -ne 0) { throw 'Audit verification failed. Treat the log as potentially tampered.' }
if (-not $VerifyOnly) {
    & $engine run --rm --volume $mount bap-service:local audit list
    if ($LASTEXITCODE -ne 0) { throw 'Could not read the verified audit trail.' }
}
