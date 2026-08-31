param(
    [ValidateSet('Auto', 'Podman', 'Docker')][string]$Runtime = 'Auto',
    [string]$StateDirectory = ''
)

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'
. (Join-Path $PSScriptRoot 'scripts\Runtime.ps1')

$engine = Get-BapContainerEngine -Runtime $Runtime
$runtimeDirectory = Get-BapRuntimeDirectory -Engine $engine
$caBundle = Join-Path $runtimeDirectory 'dev-ca.pem'
$serviceEnvelopePath = Join-Path $runtimeDirectory 'active-policy-bundle.json'
$serviceBundle = $null
if (Test-Path -LiteralPath $serviceEnvelopePath) {
    $serviceBundle = (Get-Content -LiteralPath $serviceEnvelopePath -Raw | ConvertFrom-Json).payload
}

$ready = $false
if (Test-Path -LiteralPath $caBundle) {
    try {
        $response = & curl.exe --silent --show-error --fail --max-time 3 --ssl-no-revoke --cacert $caBundle 'https://127.0.0.1:8443/readyz' 2>$null | ConvertFrom-Json
        $ready = $response.status -eq 'ready'
    } catch { $ready = $false }
}
$mysql = 'not-found'
$oldPreference = $ErrorActionPreference
try {
    $ErrorActionPreference = 'Continue'
    $mysqlResult = & $engine inspect --format '{{if .State.Health}}{{.State.Health.Status}}{{else}}{{.State.Status}}{{end}}' bap-mysql-local 2>$null
    if ($LASTEXITCODE -eq 0) { $mysql = ($mysqlResult -join '').Trim() }
} finally { $ErrorActionPreference = $oldPreference }

Write-Host 'BAP Service control plane' -ForegroundColor Cyan
[pscustomobject]@{
    Runtime = $engine
    Ready = $ready
    MySQL = $mysql
    PolicyVersion = if ($serviceBundle) { $serviceBundle.version } else { 'missing' }
    RulesDigest = if ($serviceBundle) { $serviceBundle.rules_digest } else { 'missing' }
    BundleExpires = if ($serviceBundle) { $serviceBundle.expires_at } else { 'missing' }
    RefreshSeconds = if ($serviceBundle) { $serviceBundle.refresh_after_seconds } else { 'missing' }
    MaxOfflineSeconds = if ($serviceBundle) { $serviceBundle.max_offline_seconds } else { 'missing' }
    ForceUpdate = if ($serviceBundle) { $serviceBundle.force_update } else { 'unknown' }
    KillSwitch = if ($serviceBundle) { $serviceBundle.kill_switch } else { 'unknown' }
} | Format-List

$candidates = [ordered]@{}
if ($StateDirectory) { $candidates['Explicit'] = $StateDirectory }
$candidates['Managed'] = Join-Path $env:LOCALAPPDATA 'BAP Edge'
$candidates['LocalClaude'] = Join-Path $PSScriptRoot '.bap\local-claude\edge-state'
$candidates['Acceptance'] = Join-Path $runtimeDirectory 'test-edge-state'
$rows = @()
$now = [DateTime]::UtcNow
foreach ($entry in $candidates.GetEnumerator()) {
    $policyStatePath = Join-Path $entry.Value 'policy\policy-state.json'
    $edgeEnvelopePath = Join-Path $entry.Value 'policy\active-bundle.json'
    if (-not (Test-Path -LiteralPath $policyStatePath) -or -not (Test-Path -LiteralPath $edgeEnvelopePath)) { continue }
    try {
        $policyState = Get-Content -LiteralPath $policyStatePath -Raw | ConvertFrom-Json
        $edgeBundle = (Get-Content -LiteralPath $edgeEnvelopePath -Raw | ConvertFrom-Json).payload
        $lastSync = ([DateTime]$policyState.last_sync).ToUniversalTime()
        $offlineRemaining = [Math]::Floor([double]$edgeBundle.max_offline_seconds - ($now - $lastSync).TotalSeconds)
        $refreshRemaining = [Math]::Floor([double]$edgeBundle.refresh_after_seconds - ($now - $lastSync).TotalSeconds)
        $instancePath = Join-Path $entry.Value 'edge-instance.json'
        $instanceID = if (Test-Path -LiteralPath $instancePath) { (Get-Content -LiteralPath $instancePath -Raw | ConvertFrom-Json).id } else { 'missing' }
        $spoolPath = Join-Path $entry.Value 'audit-spool'
        $spoolFiles = @(if (Test-Path -LiteralPath $spoolPath) { Get-ChildItem -LiteralPath $spoolPath -File | Where-Object Extension -In @('.json', '.sending') })
        $queued = $spoolFiles.Count
        $spoolBytes = if ($queued -gt 0) { ($spoolFiles | Measure-Object -Property Length -Sum).Sum } else { 0 }
        $oldestSpoolSeconds = if ($queued -gt 0) {
            [Math]::Max(0, [Math]::Floor(($now - (($spoolFiles | Sort-Object LastWriteTimeUtc | Select-Object -First 1).LastWriteTimeUtc)).TotalSeconds))
        } else { 0 }
        $rows += [pscustomobject]@{
            Edge = $entry.Key
            Version = $edgeBundle.version
            DigestMatchesService = [bool]($serviceBundle -and $edgeBundle.rules_digest -eq $serviceBundle.rules_digest)
            LastSyncUTC = $lastSync.ToString('u')
            RefreshInSeconds = [Math]::Max(0, $refreshRemaining)
            OfflineLeaseSeconds = [Math]::Max(0, $offlineRemaining)
            LeaseValid = $offlineRemaining -ge 0 -and $now -lt ([DateTime]$edgeBundle.expires_at).ToUniversalTime()
            KillSwitch = $edgeBundle.kill_switch
            AuditQueued = $queued
            AuditBytes = $spoolBytes
            AuditOldestSeconds = $oldestSpoolSeconds
            InstanceID = $instanceID
            StateDirectory = $entry.Value
        }
    } catch {
        Write-Warning "Could not parse Edge state $($entry.Value): $($_.Exception.Message)"
    }
}

Write-Host 'BAP Edge data planes' -ForegroundColor Cyan
if ($rows.Count -eq 0) {
    Write-Host 'No initialized Edge policy state was found.'
} else {
    $rows | Format-Table Edge,Version,DigestMatchesService,LastSyncUTC,RefreshInSeconds,OfflineLeaseSeconds,LeaseValid,KillSwitch,AuditQueued,AuditBytes,AuditOldestSeconds -AutoSize
    Write-Host 'Edge durable audit queues' -ForegroundColor Cyan
    $rows | Format-Table Edge,AuditQueued,AuditBytes,AuditOldestSeconds -AutoSize
    Write-Host 'Edge identities and state locations' -ForegroundColor Cyan
    $rows | Select-Object Edge,InstanceID,StateDirectory | Format-List
}

$captureDirectory = Join-Path $PSScriptRoot '.bap\captures'
$fixtures = if (Test-Path -LiteralPath $captureDirectory) { @(Get-ChildItem -LiteralPath $captureDirectory -Filter '*.json' -File | Where-Object Name -NotMatch 'manifest').Count } else { 0 }
$manifest = Test-Path -LiteralPath (Join-Path $captureDirectory 'certification-manifest.json')
Write-Host "Claude certification: fixtures=$fixtures manifest=$manifest"
