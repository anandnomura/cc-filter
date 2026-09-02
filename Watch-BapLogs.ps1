param(
    [ValidateSet('Auto', 'Native', 'Docker', 'Podman')][string]$Runtime = 'Auto',
    [ValidateSet('All', 'Edge', 'Service')][string]$Component = 'All',
    [ValidateRange(0, 10000)][int]$Tail = 50,
    [switch]$NoFollow
)

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'
. (Join-Path $PSScriptRoot 'scripts\Runtime.ps1')

if ($Runtime -eq 'Auto') {
    try { $Runtime = Get-BapContainerEngine -Runtime Auto } catch { $Runtime = 'Native' }
}

$fileSources = @()
$engine = ''
if ($Runtime -eq 'Native') {
    $run = Get-BapNativeLatestRunDirectory
    if ($Component -in @('All', 'Service')) {
        $fileSources += [pscustomobject]@{ Label = 'SERVICE'; Path = (Join-Path $run 'bap-service.stderr.log') }
        $fileSources += [pscustomobject]@{ Label = 'SERVICE-OUT'; Path = (Join-Path $run 'bap-service.stdout.log') }
    }
    if ($Component -in @('All', 'Edge')) {
        $fileSources += [pscustomobject]@{ Label = 'EDGE'; Path = (Join-Path $run 'edge-state\observability\edge.jsonl') }
    }
    Write-Host "Native run: $run" -ForegroundColor Cyan
} else {
    $engine = Get-BapContainerEngine -Runtime $Runtime
    if ($Component -in @('All', 'Edge')) {
        $edgePath = Join-Path (Get-BapRuntimeDirectory -Engine $engine) 'test-edge-state\observability\edge.jsonl'
        $fileSources += [pscustomobject]@{ Label = 'EDGE'; Path = $edgePath }
    }
}

$fileSources = @($fileSources | Where-Object { Test-Path -LiteralPath $_.Path -PathType Leaf })
if ($NoFollow) {
    foreach ($source in $fileSources) {
        Get-Content -LiteralPath $source.Path -Tail $Tail | ForEach-Object { "[$($source.Label)] $_" }
    }
    if ($engine -and $Component -in @('All', 'Service')) {
        & $engine logs --tail $Tail bap-service-local 2>&1 | ForEach-Object { "[SERVICE] $_" }
        if ($LASTEXITCODE -ne 0) { throw 'Could not read BAP Service container logs.' }
    }
    exit 0
}

if ($fileSources.Count -eq 0 -and (-not $engine -or $Component -eq 'Edge')) {
    throw 'No matching BAP log files were found. Start BAP first or choose another component/runtime.'
}

Write-Host 'Watching BAP logs. Press Ctrl+C to stop.' -ForegroundColor Cyan
$jobs = @()
try {
    foreach ($source in $fileSources) {
        $jobs += Start-Job -ArgumentList $source.Path, $source.Label, $Tail -ScriptBlock {
            param($Path, $Label, $TailCount)
            Get-Content -LiteralPath $Path -Tail $TailCount -Wait | ForEach-Object { "[$Label] $_" }
        }
    }
    if ($engine -and $Component -in @('All', 'Service')) {
        $jobs += Start-Job -ArgumentList $engine, $Tail -ScriptBlock {
            param($ContainerEngine, $TailCount)
            & $ContainerEngine logs --follow --tail $TailCount bap-service-local 2>&1 | ForEach-Object { "[SERVICE] $_" }
        }
    }
    while ($true) {
        foreach ($job in $jobs) { Receive-Job -Job $job }
        Start-Sleep -Milliseconds 250
    }
} finally {
    $jobs | Stop-Job -ErrorAction SilentlyContinue
    $jobs | Remove-Job -Force -ErrorAction SilentlyContinue
}
