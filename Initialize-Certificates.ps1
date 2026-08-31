param([ValidateSet('Auto', 'Podman', 'Docker')][string]$Runtime = 'Auto')

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'
. (Join-Path $PSScriptRoot 'scripts\Runtime.ps1')

$engine = Get-BapContainerEngine -Runtime $Runtime
$runtimeDirectory = Get-BapRuntimeDirectory -Engine $engine
New-Item -ItemType Directory -Force -Path $runtimeDirectory | Out-Null
$auditKeyExisted = Test-Path -LiteralPath (Join-Path $runtimeDirectory 'audit-private.pem')

& $engine image inspect bap-service:local *> $null
if ($LASTEXITCODE -ne 0) {
    & (Join-Path $PSScriptRoot 'Build-Bap.ps1') -Runtime $Runtime
}

$mount = "$($runtimeDirectory):/var/lib/bap"
& $engine run --rm --volume $mount bap-service:local initialize-certificates
if ($LASTEXITCODE -ne 0) { throw 'Certificate initialization failed.' }

$expected = @('dev-ca.pem', 'tls-cert.pem', 'tls-key.pem', 'grant-public.pem', 'grant-private.pem', 'audit-public.pem', 'audit-private.pem', 'bundle-public.pem', 'bundle-private.pem')
foreach ($name in $expected) {
    if (-not (Test-Path -LiteralPath (Join-Path $runtimeDirectory $name))) {
        throw "Certificate initialization did not create $name."
    }
}
if (-not $auditKeyExisted -and (Test-Path -LiteralPath (Join-Path $runtimeDirectory 'audit.jsonl'))) {
    $legacyPath = Join-Path $runtimeDirectory ("audit-legacy-before-dedicated-key-{0}.jsonl" -f (Get-Date -Format 'yyyyMMddHHmmss'))
    Move-Item -LiteralPath (Join-Path $runtimeDirectory 'audit.jsonl') -Destination $legacyPath
    Write-Warning "Moved the prototype audit log to $legacyPath because audit events now use a dedicated signing key."
}
$apiKeyPath = Join-Path $runtimeDirectory 'edge-api-key.txt'
if (-not (Test-Path -LiteralPath $apiKeyPath)) {
    $randomBytes = New-Object byte[] 32
    $random = [Security.Cryptography.RandomNumberGenerator]::Create()
    try { $random.GetBytes($randomBytes) } finally { $random.Dispose() }
    $apiKey = ([BitConverter]::ToString($randomBytes) -replace '-', '').ToLowerInvariant()
    Set-Content -LiteralPath $apiKeyPath -Value $apiKey -NoNewline
}
Write-Host 'Certificate initialization complete.'
Write-Host "Local CA for BAP Edge: $(Join-Path $runtimeDirectory 'dev-ca.pem')"
Write-Host "Grant verification key: $(Join-Path $runtimeDirectory 'grant-public.pem')"
Write-Host "Policy-bundle verification key: $(Join-Path $runtimeDirectory 'bundle-public.pem')"
Write-Host "Local development BAP API key: $apiKeyPath"
Write-Host 'Private keys remain under .bap/runtime and are excluded from Git and OCI build contexts.'
