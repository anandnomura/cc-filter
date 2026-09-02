param(
    [ValidateSet('Auto', 'Podman', 'Docker', 'Native')][string]$Runtime = 'Auto',
    [switch]$VerifyOnly,
    [switch]$Timeline,
    [switch]$Details,
    [string]$SessionID = '',
    [ValidateRange(1, 10000)][int]$Last = 50
)

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'
. (Join-Path $PSScriptRoot 'scripts\Runtime.ps1')

if ($Runtime -eq 'Auto') {
    try { $Runtime = Get-BapContainerEngine -Runtime Auto } catch { $Runtime = 'Native' }
}

$auditJSON = ''
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
        if ($LASTEXITCODE -ne 0) { throw 'Audit verification failed. Treat the log as potentially tampered.' }
        if (-not $VerifyOnly) {
            $auditJSON = (& $serviceBinary audit list | Out-String)
            if ($LASTEXITCODE -ne 0) { throw 'Could not read the verified native audit trail.' }
        }
    } finally {
        [Environment]::SetEnvironmentVariable('BAP_STATE_DIRECTORY', $previousState, 'Process')
    }
    Write-Host "Verified native audit: $serviceState"
} else {
    $engine = Get-BapContainerEngine -Runtime $Runtime
    & $engine exec bap-service-local bap-service audit verify
    if ($LASTEXITCODE -ne 0) { throw 'Audit verification failed. Treat the log as potentially tampered.' }
    if (-not $VerifyOnly) {
        $auditJSON = (& $engine exec bap-service-local bap-service audit list | Out-String)
        if ($LASTEXITCODE -ne 0) { throw 'Could not read the verified audit trail.' }
    }
}

if ($VerifyOnly) { exit 0 }
$parsedEvents = $auditJSON | ConvertFrom-Json
$events = @()
foreach ($event in $parsedEvents) { $events += $event }
if ($SessionID) { $events = @($events | Where-Object { $_.session_id -eq $SessionID }) }
$events = @($events | Sort-Object { [DateTime]$_.timestamp } | Select-Object -Last $Last)

if (-not $Timeline -and -not $SessionID) {
    $events | ConvertTo-Json -Depth 20
    exit 0
}

if ($events.Count -eq 0) {
    Write-Host 'No matching audit events were found.'
    exit 0
}

$timelineRows = @($events | ForEach-Object {
    $allowedProperty = $_.PSObject.Properties['allowed']
    $decision = if ($null -ne $allowedProperty) { if ($allowedProperty.Value) { 'allow' } else { 'deny' } } elseif ($_.outcome) { $_.outcome } else { '' }
    [pscustomobject]@{
        Timestamp = ([DateTime]$_.timestamp).ToUniversalTime().ToString('yyyy-MM-dd HH:mm:ss.fffZ')
        Session = $_.session_id
        Tool = $_.tool
        Action = $_.action
        Decision = $decision
        Reason = $_.reason_code
        Source = $_.source
        ToolUseID = $_.tool_use_id
        Target = $_.target_summary
    }
})
if ($Details) {
    $timelineRows | Format-List Timestamp,Session,Tool,Action,Decision,Reason,Source,ToolUseID,Target
} else {
    $timelineRows | Format-Table Timestamp,Tool,Action,Decision,Reason -AutoSize -Wrap
}

Write-Host 'Session IDs in this result:' -ForegroundColor Cyan
$events | Where-Object session_id | Group-Object session_id | ForEach-Object {
    [pscustomobject]@{ SessionID = $_.Name; Events = $_.Count }
} | Format-Table -AutoSize
