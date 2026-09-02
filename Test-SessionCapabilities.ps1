param(
    [ValidateSet('Auto', 'Podman', 'Docker', 'Native')][string]$Runtime = 'Auto',
    [string]$AttestationPath = ''
)

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'
. (Join-Path $PSScriptRoot 'scripts\GoToolchain.ps1')

$packages = @('./bap-edge/internal/bapedge', './internal/policybundle', './internal/agentgrant', './bap-service/internal/agentsts', './bap-service/internal/mysqlstore')
$goCommand = Get-BapGoCommand
$actualRuntime = $Runtime
Push-Location $PSScriptRoot
try {
    if ($Runtime -eq 'Native' -or ($Runtime -eq 'Auto' -and $goCommand)) {
        if (-not $goCommand) { $goCommand = Get-BapGoCommand -Required }
        $actualRuntime = 'Native'
        & $goCommand test -mod=vendor -count=1 @packages
    } else {
        . (Join-Path $PSScriptRoot 'scripts\Runtime.ps1')
        $engine = Get-BapContainerEngine -Runtime $Runtime
        $actualRuntime = if ((Split-Path $engine -Leaf) -match 'podman') { 'Podman' } else { 'Docker' }
        $mount = "$($PSScriptRoot):/src"
        & $engine run --rm --volume $mount --workdir /src docker.io/library/golang:1.23-bookworm `
            go test -mod=vendor -count=1 @packages
    }
    if ($LASTEXITCODE -ne 0) { throw 'Session capability security suite failed.' }
} finally {
    Pop-Location
}

$policyPath = Join-Path $PSScriptRoot 'bap-service\policies\edge-policy-source.json'
$policy = Get-Content -LiteralPath $policyPath -Raw | ConvertFrom-Json
$sourceRoots = @(
    'bap-edge\cmd', 'bap-edge\internal\bapedge', 'internal\policybundle',
    'internal\agentgrant', 'bap-service\internal\agentsts',
    'bap-service\internal\mysqlstore'
) | ForEach-Object { Join-Path $PSScriptRoot $_ }
$sourceFiles = @($sourceRoots | ForEach-Object { Get-ChildItem -LiteralPath $_ -File -Recurse }) + @(
    (Get-Item -LiteralPath $policyPath)
    (Get-Item -LiteralPath (Join-Path $PSScriptRoot 'go.mod'))
    (Get-Item -LiteralPath (Join-Path $PSScriptRoot 'Test-SessionCapabilities.ps1'))
)
$sourceEntries = @($sourceFiles | Sort-Object FullName -Unique | ForEach-Object {
    $relative = $_.FullName.Substring($PSScriptRoot.Length).TrimStart('\')
    "$relative=$((Get-FileHash -Algorithm SHA256 -LiteralPath $_.FullName).Hash.ToLowerInvariant())"
})
$sourceBytes = [Text.Encoding]::UTF8.GetBytes(($sourceEntries -join "`n"))
$sourceHasher = [Security.Cryptography.SHA256]::Create()
try { $sourceDigest = ([BitConverter]::ToString($sourceHasher.ComputeHash($sourceBytes))).Replace('-', '').ToLowerInvariant() } finally { $sourceHasher.Dispose() }
if (-not $AttestationPath) {
    $attestationDirectory = Join-Path $PSScriptRoot '.bap\attestations'
    New-Item -ItemType Directory -Force -Path $attestationDirectory | Out-Null
    $AttestationPath = Join-Path $attestationDirectory ("session-capabilities-{0}.json" -f (Get-Date).ToUniversalTime().ToString('yyyyMMddTHHmmssZ'))
}
$commit = (& git -C $PSScriptRoot rev-parse HEAD 2>$null)
$evidence = [ordered]@{
    evidence_type = 'bap-session-capability-test-evidence-v1'
    generated_at = (Get-Date).ToUniversalTime().ToString('o')
    runtime = $actualRuntime
    git_commit = "$commit".Trim()
    working_tree_clean = -not [bool](& git -C $PSScriptRoot status --porcelain 2>$null)
    tested_source_sha256 = $sourceDigest
    policy_version = $policy.version
    policy_sha256 = (Get-FileHash -Algorithm SHA256 -LiteralPath $policyPath).Hash.ToLowerInvariant()
    tests = @('cross-process-lock-path', 'concurrent-session-budget', 'session-isolation', 'pending-counts', 'failure-does-not-accrue', 'intent-issuance-budget', 'grant-one-use', 'policy-validation')
    result = 'pass'
    note = 'Evidence is bound to tested source and policy hashes; sign it in the company CI attestation system for cryptographic provenance.'
}
$evidence | ConvertTo-Json -Depth 5 | Set-Content -LiteralPath $AttestationPath -Encoding utf8

Write-Host 'PASS: concurrent Claude instances sharing a session cannot race through signed session limits.'
Write-Host 'PASS: different Claude session IDs have isolated workloads and capability ledgers.'
Write-Host 'PASS: one classified prompt intent cannot mint more grants than the signed policy budget.'
Write-Host "EVIDENCE: $AttestationPath"
