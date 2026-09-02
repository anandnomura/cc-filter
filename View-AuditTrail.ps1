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

function Get-AuditField {
    param(
        [Parameter(Mandatory)]$Event,
        [Parameter(Mandatory)][string]$Name,
        $Default = ''
    )
    $property = $Event.PSObject.Properties[$Name]
    if ($null -eq $property -or $null -eq $property.Value) { return $Default }
    return $property.Value
}

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
if ($SessionID) { $events = @($events | Where-Object { (Get-AuditField -Event $_ -Name 'session_id') -eq $SessionID }) }
$events = @($events | Sort-Object { [DateTime](Get-AuditField -Event $_ -Name 'timestamp' -Default ([DateTime]::MinValue)) } | Select-Object -Last $Last)

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
    $outcome = Get-AuditField -Event $_ -Name 'outcome'
    $decision = if ($null -ne $allowedProperty) { if ($allowedProperty.Value) { 'allow' } else { 'deny' } } elseif ($outcome) { $outcome } else { '' }
    $timestamp = [DateTime](Get-AuditField -Event $_ -Name 'timestamp' -Default ([DateTime]::MinValue))
    [pscustomobject]@{
        Timestamp = $timestamp.ToUniversalTime().ToString('yyyy-MM-dd HH:mm:ss.fffZ')
        Session = Get-AuditField -Event $_ -Name 'session_id'
        Tool = Get-AuditField -Event $_ -Name 'tool'
        Action = Get-AuditField -Event $_ -Name 'action'
        Decision = $decision
        Reason = Get-AuditField -Event $_ -Name 'reason_code'
        Source = Get-AuditField -Event $_ -Name 'source'
        ToolUseID = Get-AuditField -Event $_ -Name 'tool_use_id'
        Target = Get-AuditField -Event $_ -Name 'target_summary'
    }
})
if ($Details) {
    $timelineRows | Format-List Timestamp,Session,Tool,Action,Decision,Reason,Source,ToolUseID,Target
} else {
    $timelineRows | Format-Table Timestamp,Tool,Action,Decision,Reason -AutoSize -Wrap
}

Write-Host 'Session IDs in this result:' -ForegroundColor Cyan
$timelineRows | Where-Object { $_.Session } | Group-Object Session | ForEach-Object {
    [pscustomobject]@{ SessionID = $_.Name; Events = $_.Count }
} | Format-Table -AutoSize
