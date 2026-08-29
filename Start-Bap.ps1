param([ValidateSet('Auto', 'Podman', 'Docker')][string]$Runtime = 'Auto')

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'
. (Join-Path $PSScriptRoot 'scripts\Runtime.ps1')

$engine = Get-BapContainerEngine -Runtime $Runtime
$runtimeDirectory = Get-BapRuntimeDirectory -Engine $engine
New-Item -ItemType Directory -Force -Path $runtimeDirectory | Out-Null

& $engine image inspect bap-service:local *> $null
if ($LASTEXITCODE -ne 0) {
    & (Join-Path $PSScriptRoot 'Build-Bap.ps1') -Runtime $Runtime
}

& (Join-Path $PSScriptRoot 'Initialize-Certificates.ps1') -Runtime $Runtime
$apiKey = (Get-Content -LiteralPath (Join-Path $runtimeDirectory 'edge-api-key.txt') -Raw).Trim()
$auditPath = Join-Path $runtimeDirectory 'audit.jsonl'
if (Test-Path -LiteralPath $auditPath) {
    $first = Get-Content -LiteralPath $auditPath -TotalCount 1 | ConvertFrom-Json
    if ('signature' -notin $first.PSObject.Properties.Name -or -not $first.signature) {
        $legacyPath = Join-Path $runtimeDirectory ("audit-legacy-unsigned-{0}.jsonl" -f (Get-Date -Format 'yyyyMMddHHmmss'))
        Move-Item -LiteralPath $auditPath -Destination $legacyPath
        Write-Warning "Moved the unsigned prototype audit log to $legacyPath. New events use a signed hash chain."
    }
}

& $engine rm --force bap-service-local *> $null
$mount = "$($runtimeDirectory):/var/lib/bap"
& $engine run --detach --name bap-service-local --publish 127.0.0.1:8443:8443 --volume $mount --env BAP_DEVELOPMENT_TLS=true --env "BAP_EDGE_API_KEY=$apiKey" --env BAP_EDGE_PRINCIPAL=local-developer bap-service:local | Out-Host
if ($LASTEXITCODE -ne 0) { throw 'Could not start the BAP Service container.' }

Wait-BapHealth -CaBundle (Join-Path $runtimeDirectory 'dev-ca.pem')
Set-Content -LiteralPath (Join-Path $runtimeDirectory 'container-engine.txt') -Value $engine
Write-Host "BAP Service is healthy. Runtime: $engine; URL: https://127.0.0.1:8443"
