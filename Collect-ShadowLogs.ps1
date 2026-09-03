param(
    [ValidateSet('Auto', 'Podman', 'Docker', 'Native')][string]$Runtime = 'Auto',
    [string]$OutputDirectory = ''
)

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'
. (Join-Path $PSScriptRoot 'scripts\Runtime.ps1')

function Write-JsonLines {
    param([Parameter(Mandatory)]$Values, [Parameter(Mandatory)][string]$Path)
    $writer = [IO.StreamWriter]::new($Path, $false, [Text.UTF8Encoding]::new($false))
    try {
        foreach ($value in @($Values)) {
            $writer.WriteLine(($value | ConvertTo-Json -Compress -Depth 30))
        }
    } finally {
        $writer.Dispose()
    }
}

if ($Runtime -eq 'Auto') {
    try { $Runtime = Get-BapContainerEngine -Runtime Auto } catch { $Runtime = 'Native' }
}
$runtimeName = $Runtime.ToLowerInvariant()
$collectionRoot = Join-Path $PSScriptRoot '.bap\shadow-logs'
if (-not $OutputDirectory) {
    $stamp = (Get-Date).ToUniversalTime().ToString('yyyyMMddTHHmmssfffZ')
    $OutputDirectory = Join-Path $collectionRoot "$stamp-$runtimeName"
}
[IO.Directory]::CreateDirectory($OutputDirectory) | Out-Null
$resolvedOutput = (Resolve-Path -LiteralPath $OutputDirectory).Path

$auditJSON = ''
$edgeSource = ''
if ($Runtime -eq 'Native') {
    $nativeRun = Get-BapNativeLatestRunDirectory
    $serviceState = Join-Path $nativeRun 'service-state'
    $serviceBinary = Join-Path $PSScriptRoot 'dist\bap-service-windows-amd64.exe'
    if (-not (Test-Path -LiteralPath $serviceBinary -PathType Leaf)) {
        throw "Native BAP Service executable is missing: $serviceBinary"
    }
    $previousState = [Environment]::GetEnvironmentVariable('BAP_STATE_DIRECTORY', 'Process')
    try {
        $env:BAP_STATE_DIRECTORY = $serviceState
        & $serviceBinary audit verify
        if ($LASTEXITCODE -ne 0) { throw 'Audit verification failed; shadow collection stopped.' }
        $auditJSON = (& $serviceBinary audit list | Out-String)
        if ($LASTEXITCODE -ne 0) { throw 'Could not export the native audit trail.' }
    } finally {
        [Environment]::SetEnvironmentVariable('BAP_STATE_DIRECTORY', $previousState, 'Process')
    }
    $edgeSource = Join-Path $nativeRun 'edge-state\observability\edge.jsonl'
} else {
    $engine = Get-BapContainerEngine -Runtime $Runtime
    & $engine exec bap-service-local bap-service audit verify
    if ($LASTEXITCODE -ne 0) { throw 'Audit verification failed; shadow collection stopped.' }
    $auditJSON = (& $engine exec bap-service-local bap-service audit list | Out-String)
    if ($LASTEXITCODE -ne 0) { throw 'Could not export the container audit trail.' }
    $runtimeDirectory = Get-BapRuntimeDirectory -Engine $engine
    $edgeSource = Join-Path $runtimeDirectory 'test-edge-state\observability\edge.jsonl'
}

$auditEvents = @($auditJSON | ConvertFrom-Json)
$auditOutput = Join-Path $resolvedOutput 'service-audit.jsonl'
Write-JsonLines -Values $auditEvents -Path $auditOutput

$edgeOutput = Join-Path $resolvedOutput 'edge-observability.jsonl'
if (Test-Path -LiteralPath $edgeSource -PathType Leaf) {
    [IO.File]::Copy((Resolve-Path -LiteralPath $edgeSource).Path, $edgeOutput, $true)
} else {
    [IO.File]::WriteAllText($edgeOutput, '', [Text.UTF8Encoding]::new($false))
    Write-Warning "No Edge observability file was found at $edgeSource; Service audit was still collected."
}

$manifest = [ordered]@{
    schema_version = 1
    collected_at = (Get-Date).ToUniversalTime().ToString('o')
    runtime = $runtimeName
    source_edge_log = $edgeSource
    files = @('service-audit.jsonl', 'edge-observability.jsonl')
    analyzer_scope = 'All *.jsonl files below .bap\shadow-logs are read recursively; non-shadow records are ignored.'
}
$manifest | ConvertTo-Json -Depth 8 | Set-Content -LiteralPath (Join-Path $resolvedOutput 'collection-manifest.json') -Encoding utf8
Write-Host "Shadow log snapshot: $resolvedOutput"
Write-Host 'Analyze all snapshots with: .\Analyze-ShadowLogs.ps1'
