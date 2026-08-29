param([ValidateSet('Auto', 'Podman', 'Docker')][string]$Runtime = 'Auto')

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'
. (Join-Path $PSScriptRoot 'scripts\Runtime.ps1')
$engine = Get-BapContainerEngine -Runtime $Runtime

Write-Host 'Running Go unit and integration tests inside the pinned toolchain...'
$mount = "$($PSScriptRoot):/src"
& $engine run --rm --volume $mount --workdir /src docker.io/library/golang:1.23-bookworm go test ./...
if ($LASTEXITCODE -ne 0) { throw 'Automated tests failed.' }

$runtimeDirectory = Get-BapRuntimeDirectory -Engine $engine
$caBundle = Join-Path $runtimeDirectory 'dev-ca.pem'
Wait-BapHealth -CaBundle $caBundle
$runtimeMount = "${runtimeDirectory}:/var/lib/bap"
$baselineEvents = (& $engine run --rm --volume $runtimeMount bap-service:local audit list) | ConvertFrom-Json
$baselineCount = if ($null -eq $baselineEvents) { 0 } else { @($baselineEvents).Count }
$metadata = (& curl.exe --silent --show-error --fail --ssl-no-revoke --cacert $caBundle 'https://127.0.0.1:8443/.well-known/authzen-configuration') | ConvertFrom-Json
if (-not $metadata.access_evaluation_endpoint) { throw 'AuthZEN metadata is missing the evaluation endpoint.' }
$unauthenticatedStatus = & curl.exe --silent --output NUL --write-out '%{http_code}' --ssl-no-revoke --cacert $caBundle -X POST -H 'Content-Type: application/json' --data '{}' 'https://127.0.0.1:8443/access/v1/evaluation'
if ($unauthenticatedStatus -ne '401') { throw "Expected unauthenticated evaluation to return 401, got $unauthenticatedStatus." }
Write-Host 'PASS: unauthenticated authorization request -> 401'

$edgeBinary = Join-Path $PSScriptRoot 'dist\bap-edge-windows-amd64.exe'
if (-not (Test-Path -LiteralPath $edgeBinary)) { & (Join-Path $PSScriptRoot 'Build-Bap.ps1') -Runtime $Runtime }
$edgeConfig = Join-Path $runtimeDirectory 'test-edge.yaml'
$publicKey = (Join-Path $runtimeDirectory 'grant-public.pem').Replace('\', '\\')
$caPath = $caBundle.Replace('\', '\\')
$env:BAP_EDGE_API_KEY = (Get-Content -LiteralPath (Join-Path $runtimeDirectory 'edge-api-key.txt') -Raw).Trim()
@"
service_url: "https://127.0.0.1:8443"
public_key_path: "$publicKey"
ca_bundle_path: "$caPath"
subject_id: "claude-code-local"
timeout_ms: 3000
api_key_env: "BAP_EDGE_API_KEY"
state_directory: "$((Join-Path $runtimeDirectory 'test-edge-state').Replace('\', '\\'))"
"@ | Set-Content -LiteralPath $edgeConfig -Encoding utf8

$cases = @(
    @{ Name = 'safe workspace read'; Want = 'allow'; Tool = 'Read'; Input = @{ file_path = 'README.md' } },
    @{ Name = 'secret read'; Want = 'deny'; Tool = 'Read'; Input = @{ file_path = '.env' } },
    @{ Name = 'outside-workspace read'; Want = 'deny'; Tool = 'Read'; Input = @{ file_path = '..\outside.txt' } },
    @{ Name = 'destructive command'; Want = 'deny'; Tool = 'Bash'; Input = @{ command = 'git reset --hard' } },
    @{ Name = 'unknown tool'; Want = 'deny'; Tool = 'UnknownTool'; Input = @{} }
)
$testSession = 'test-session-' + [Guid]::NewGuid().ToString('N')
foreach ($case in $cases) {
    $hookInput = @{ hook_event_name = 'PreToolUse'; session_id = $testSession; tool_use_id = "test-$($case.Name.Replace(' ', '-'))"; cwd = $PSScriptRoot; tool_name = $case.Tool; tool_input = $case.Input } | ConvertTo-Json -Compress -Depth 8
    $result = $hookInput | & $edgeBinary --config $edgeConfig | ConvertFrom-Json
    $actual = $result.hookSpecificOutput.permissionDecision
    if ($actual -ne $case.Want) { throw "$($case.Name): expected $($case.Want), got $actual" }
    Write-Host "PASS: $($case.Name) -> $actual"
}
$replayInput = @{ hook_event_name = 'PreToolUse'; session_id = $testSession; tool_use_id = 'test-safe-workspace-read'; cwd = $PSScriptRoot; tool_name = 'Read'; tool_input = @{ file_path = 'README.md' } } | ConvertTo-Json -Compress -Depth 8
$replay = $replayInput | & $edgeBinary --config $edgeConfig | ConvertFrom-Json
if ($replay.hookSpecificOutput.permissionDecisionReason -notmatch 'centrally recorded') { throw 'Expected an exact retry to consume a cached grant with central audit acknowledgement.' }
Write-Host 'PASS: exact retry -> cached grant consumed and centrally recorded'

$outcomeInput = @{ hook_event_name = 'PostToolUse'; session_id = $testSession; tool_use_id = 'test-safe-workspace-read'; cwd = $PSScriptRoot; tool_name = 'Read'; tool_input = @{ file_path = 'README.md' } } | ConvertTo-Json -Compress -Depth 8
$outcomeInput | & $edgeBinary --config $edgeConfig | Out-Null
$outcomeInput | & $edgeBinary --config $edgeConfig | Out-Null

& $engine run --rm --volume $runtimeMount bap-service:local audit verify | Out-Host
if ($LASTEXITCODE -ne 0) { throw 'Signed audit-chain verification failed.' }
$allEvents = (& $engine run --rm --volume $runtimeMount bap-service:local audit list) | ConvertFrom-Json
$events = @($allEvents) | Select-Object -Skip $baselineCount
$sources = @($events | ForEach-Object { $_.source })
foreach ($requiredSource in @('pdp_evaluation', 'cached_grant_consumption', 'local_edge_filter', 'bap_edge_report')) {
    if ($requiredSource -notin $sources) { throw "Audit trail is missing source $requiredSource." }
}
if (@($events | Where-Object { $_.source -eq 'bap_edge_report' }).Count -ne 1) { throw 'Retried tool outcome was not idempotently deduplicated.' }
if (@($events | Where-Object { $_.source -in @('pdp_evaluation', 'cached_grant_consumption') -and -not $_.policy_version }).Count -gt 0) { throw 'A service authorization event is missing its Cedar policy version.' }
if (@($events | Where-Object { -not $_.workload_id -or -not $_.credential_fingerprint -or -not $_.signature }).Count -gt 0) {
    throw 'One or more audit events is missing workload, credential fingerprint, or signature data.'
}
if (($events | ConvertTo-Json -Depth 10) -match 'git reset --hard') { throw 'Audit trail contains plaintext command content.' }
if (@($events | Where-Object { 'target_summary' -in $_.PSObject.Properties.Name -and $_.target_summary -match '^[A-Za-z]:\\' }).Count -gt 0) { throw 'Audit trail contains an absolute Windows target path.' }
Write-Host 'PASS: signed audit trail covers PDP, cache, local denial, and tool outcome without command plaintext'
Remove-Item -LiteralPath $edgeConfig -Force
Write-Host 'PASS: code, Cedar, API authentication, workload IDs, grants, cache audit, outcomes, HTTPS, AuthZEN, and end-to-end decisions.'
