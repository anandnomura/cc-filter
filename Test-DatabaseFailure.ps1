param([ValidateSet('Auto', 'Podman', 'Docker')][string]$Runtime = 'Auto')

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'
. (Join-Path $PSScriptRoot 'scripts\Runtime.ps1')
$engine = Get-BapContainerEngine -Runtime $Runtime
$runtimeDirectory = Get-BapRuntimeDirectory -Engine $engine
$caBundle = Join-Path $runtimeDirectory 'dev-ca.pem'
$requestPath = Join-Path $runtimeDirectory 'database-failure-sync-request.json'

if ('bap-mysql-local' -notin @(& $engine ps --filter 'name=^/bap-mysql-local$' --format '{{.Names}}')) {
    throw 'The local MySQL container is not running. Start BAP with Start-Bap.ps1 first.'
}

$request = @{ edge_instance_id = 'database-failure-test'; edge_version = '1'; installed_version = 0; revocation_epoch = 0; nonce = 'database-failure-test' } | ConvertTo-Json -Compress
[IO.File]::WriteAllText($requestPath, $request, (New-Object Text.UTF8Encoding($false)))

try {
    & $engine stop --time 30 bap-mysql-local | Out-Null
    if ($LASTEXITCODE -ne 0) { throw 'Could not stop local MySQL for the failure test.' }

    $readyStatus = & curl.exe --silent --output NUL --write-out '%{http_code}' --ssl-no-revoke --cacert $caBundle 'https://127.0.0.1:8443/readyz'
    if ($readyStatus -ne '503') { throw "Expected readiness HTTP 503 with MySQL stopped, got $readyStatus." }

    $apiKey = (Get-Content -LiteralPath (Join-Path $runtimeDirectory 'edge-api-key.txt') -Raw).Trim()
    $syncStatus = & curl.exe --silent --output NUL --write-out '%{http_code}' --max-time 10 --ssl-no-revoke --cacert $caBundle `
        -H "Authorization: Bearer $apiKey" -H 'Content-Type: application/json' --data-binary "@$requestPath" `
        'https://127.0.0.1:8443/bap/v1/edge/sync'
    if ($syncStatus -ne '503') { throw "Expected policy sync HTTP 503 with MySQL stopped, got $syncStatus." }
    Write-Host 'PASS: MySQL outage makes the control plane unready and prevents policy synchronization.'
} finally {
    & $engine start bap-mysql-local | Out-Null
    if ($LASTEXITCODE -ne 0) { throw 'Could not restore local MySQL after the failure test.' }
    Wait-BapHealth -CaBundle $caBundle -Attempts 60
    Remove-Item -LiteralPath $requestPath -Force -ErrorAction SilentlyContinue
}

Write-Host 'PASS: MySQL and BAP Service readiness recovered.'
