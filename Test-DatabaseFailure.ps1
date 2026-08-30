param([ValidateSet('Auto', 'Podman', 'Docker')][string]$Runtime = 'Auto')

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'
. (Join-Path $PSScriptRoot 'scripts\Runtime.ps1')
$engine = Get-BapContainerEngine -Runtime $Runtime
$runtimeDirectory = Get-BapRuntimeDirectory -Engine $engine
$caBundle = Join-Path $runtimeDirectory 'dev-ca.pem'
$requestPath = Join-Path $runtimeDirectory 'database-failure-request.json'

if ('bap-mysql-local' -notin @(& $engine ps --filter 'name=^/bap-mysql-local$' --format '{{.Names}}')) {
    throw 'The local MySQL container is not running. Start BAP with Start-Bap.ps1 first.'
}

$request = @{
    subject = @{ type = 'agent'; id = 'claude-code-local' }
    action = @{ name = 'file.read' }
    resource = @{
        type = 'tool-invocation'; id = 'database-failure-test'
        properties = @{
            tool = 'Read'; target = 'README.md'; path = 'README.md'; command = ''
            protected = $false; outsideWorkspace = $false; securityControl = $false
            destructive = $false; privileged = $false; exfiltration = $false; obfuscated = $false
        }
    }
    context = @{
        session_id = 'database-failure-test'; workload_id = 'pilot-correlation-only'
        tool_use_id = 'database-failure-test'; workspace = $PSScriptRoot
    }
} | ConvertTo-Json -Compress -Depth 8
[IO.File]::WriteAllText($requestPath, $request, (New-Object Text.UTF8Encoding($false)))

try {
    & $engine stop --time 30 bap-mysql-local | Out-Null
    if ($LASTEXITCODE -ne 0) { throw 'Could not stop local MySQL for the failure test.' }

    $readyStatus = & curl.exe --silent --output NUL --write-out '%{http_code}' --ssl-no-revoke --cacert $caBundle 'https://127.0.0.1:8443/readyz'
    if ($readyStatus -ne '503') { throw "Expected readiness HTTP 503 with MySQL stopped, got $readyStatus." }

    $apiKey = (Get-Content -LiteralPath (Join-Path $runtimeDirectory 'edge-api-key.txt') -Raw).Trim()
    $evaluationStatus = & curl.exe --silent --output NUL --write-out '%{http_code}' --max-time 10 --ssl-no-revoke --cacert $caBundle `
        -H "Authorization: Bearer $apiKey" -H 'Content-Type: application/json' --data-binary "@$requestPath" `
        'https://127.0.0.1:8443/access/v1/evaluation'
    if ($evaluationStatus -ne '500') { throw "Expected fail-closed evaluation HTTP 500 with MySQL stopped, got $evaluationStatus." }
    Write-Host 'PASS: MySQL outage makes readiness fail and prevents a fresh authorization decision.'
} finally {
    & $engine start bap-mysql-local | Out-Null
    if ($LASTEXITCODE -ne 0) { throw 'Could not restore local MySQL after the failure test.' }
    Wait-BapHealth -CaBundle $caBundle -Attempts 60
    Remove-Item -LiteralPath $requestPath -Force -ErrorAction SilentlyContinue
}

Write-Host 'PASS: MySQL and BAP Service readiness recovered.'
