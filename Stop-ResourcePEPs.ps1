Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'
$statePath = Join-Path $PSScriptRoot '.bap\resource-peps\running.json'
if (-not (Test-Path -LiteralPath $statePath)) { Write-Host 'Resource PEPs are not recorded as running.'; return }
$state = Get-Content -LiteralPath $statePath -Raw | ConvertFrom-Json
if ($state.runtime -eq 'Native') {
    foreach ($processID in @($state.api_pid, $state.mcp_pid)) {
        $process = Get-Process -Id $processID -ErrorAction SilentlyContinue
        if ($process) { Stop-Process -Id $processID -Force }
    }
} else {
    foreach ($container in @($state.containers)) { & $state.engine rm --force $container 2>$null | Out-Null }
}
[IO.File]::Delete($statePath)
Write-Host 'PASS: resource PEPs stopped.'
