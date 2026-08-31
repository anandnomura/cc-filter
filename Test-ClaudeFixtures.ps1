param(
    [ValidateSet('Auto', 'Podman', 'Docker')][string]$Runtime = 'Auto',
    [string]$CaptureDirectory = '',
    [string[]]$RequiredModels = @(),
    [switch]$UpdateManifest,
    [switch]$RequireCompanyFixtures
)

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'
. (Join-Path $PSScriptRoot 'scripts\Runtime.ps1')

$engine = Get-BapContainerEngine -Runtime $Runtime
$runtimeDirectory = Get-BapRuntimeDirectory -Engine $engine
if (-not $CaptureDirectory) { $CaptureDirectory = Join-Path $PSScriptRoot '.bap\captures' }
$captureDirectory = [IO.Path]::GetFullPath($CaptureDirectory)
$manifestPath = Join-Path $captureDirectory 'certification-manifest.json'
$bundlePath = Join-Path $runtimeDirectory 'active-policy-bundle.json'
$publicKeyPath = Join-Path $runtimeDirectory 'bundle-public.pem'
$fixtures = @(if (Test-Path -LiteralPath $captureDirectory) { Get-ChildItem -LiteralPath $captureDirectory -Filter '*.json' -File | Where-Object Name -NotMatch 'manifest' })

if ($fixtures.Count -eq 0) {
    if ($RequireCompanyFixtures) { throw 'No exact company Claude fixtures exist. Run Capture-ClaudeFixtures.ps1 for every required scenario on Sonnet and Opus.' }
    Write-Host 'PENDING: no exact company Claude fixtures captured; model-independent MVP-0 gates remain active.'
    exit 0
}
if (-not (Test-Path -LiteralPath $bundlePath)) { throw "Active signed bundle is missing: $bundlePath" }
if (-not (Test-Path -LiteralPath $publicKeyPath)) { throw "Policy-bundle public key is missing: $publicKeyPath" }
if ($RequireCompanyFixtures -and $RequiredModels.Count -eq 0) { $RequiredModels = @('sonnet','opus') }
$required = ($RequiredModels -join ',')
$root = [IO.Path]::GetFullPath($PSScriptRoot).TrimEnd([char[]]@('\','/'))
if (-not $captureDirectory.StartsWith($root + [IO.Path]::DirectorySeparatorChar, [StringComparison]::OrdinalIgnoreCase)) {
    throw 'CaptureDirectory must be inside the repository so the pinned test container can verify it.'
}
$relativeCapture = $captureDirectory.Substring($root.Length).TrimStart([char[]]@('\','/')).Replace('\','/')
$mount = "$($PSScriptRoot):/src"
$containerDirectory = "/src/$relativeCapture"
$containerManifest = "$containerDirectory/certification-manifest.json"
$containerBundle = "/src/.bap/runtime/$engine/active-policy-bundle.json"
$containerPublicKey = "/src/.bap/runtime/$engine/bundle-public.pem"

if ($UpdateManifest) {
    & $engine run --rm --volume $mount --workdir /src docker.io/library/golang:1.23-bookworm `
        go run ./cmd/bap-fixture -mode manifest -directory $containerDirectory -manifest $containerManifest -bundle $containerBundle -public-key $containerPublicKey "-require-models=$required"
    if ($LASTEXITCODE -ne 0) { throw 'Could not create the Claude certification manifest.' }
}
if (-not (Test-Path -LiteralPath $manifestPath)) {
    throw 'Captured fixtures exist but have not been reviewed and manifested. Run Test-ClaudeFixtures.ps1 -UpdateManifest with the required model families.'
}
& $engine run --rm --volume $mount --workdir /src docker.io/library/golang:1.23-bookworm `
    go run ./cmd/bap-fixture -mode verify -directory $containerDirectory -manifest $containerManifest -bundle $containerBundle -public-key $containerPublicKey "-require-models=$required"
if ($LASTEXITCODE -ne 0) { throw 'Claude fixture certification failed.' }
Write-Host 'PASS: captured Claude schemas, decisions, policy identity, model equivalence, and fixture hashes are certified.'
