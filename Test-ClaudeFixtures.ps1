param(
    [ValidateSet('Auto', 'Podman', 'Docker', 'Native')][string]$Runtime = 'Auto',
    [string]$CaptureDirectory = '',
    [string[]]$RequiredModels = @('sonnet'),
    [string[]]$RequiredScenarios = @('git-status-allow', 'git-reset-hard-deny', 'mysql-manual-only-deny'),
    [switch]$UpdateManifest,
    [switch]$RequireCompanyFixtures
)

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'
$nativeMode = $Runtime -eq 'Native'
$engine = ''
if ($nativeMode) {
    . (Join-Path $PSScriptRoot 'scripts\GoToolchain.ps1')
    $goCommand = Get-BapGoCommand -Required
    $latestRunPath = Join-Path $PSScriptRoot '.bap\native-local\latest-run.txt'
    if (-not (Test-Path -LiteralPath $latestRunPath -PathType Leaf)) {
        throw 'No native test run exists. Run Start-BapNativeLocal.bat -VerifyOnly first.'
    }
    $latestRun = (Get-Content -LiteralPath $latestRunPath -Raw).Trim()
    $runtimeDirectory = Join-Path $latestRun 'service-state'
} else {
    . (Join-Path $PSScriptRoot 'scripts\Runtime.ps1')
    $engine = Get-BapContainerEngine -Runtime $Runtime
    $runtimeDirectory = Get-BapRuntimeDirectory -Engine $engine
}
if (-not $CaptureDirectory) { $CaptureDirectory = Join-Path $PSScriptRoot '.bap\captures' }
$captureDirectory = [IO.Path]::GetFullPath($CaptureDirectory)
$manifestPath = Join-Path $captureDirectory 'certification-manifest.json'
$bundlePath = Join-Path $runtimeDirectory 'active-policy-bundle.json'
$publicKeyPath = Join-Path $runtimeDirectory 'bundle-public.pem'
$fixtures = @(if (Test-Path -LiteralPath $captureDirectory) { Get-ChildItem -LiteralPath $captureDirectory -Filter '*.json' -File | Where-Object Name -NotMatch 'manifest' })

if ($fixtures.Count -eq 0) {
    if ($RequireCompanyFixtures) { throw "No exact company Claude fixtures exist. Follow README.md 'Capture and certify company Claude fixtures without containers', then rerun with -Runtime $Runtime." }
    Write-Host 'PENDING: no exact company Claude fixtures captured; model-independent MVP-0 gates remain active.'
    exit 0
}
if (-not (Test-Path -LiteralPath $bundlePath)) { throw "Active signed bundle is missing: $bundlePath" }
if (-not (Test-Path -LiteralPath $publicKeyPath)) { throw "Policy-bundle public key is missing: $publicKeyPath" }
if ($RequireCompanyFixtures -and $RequiredModels.Count -eq 0) { $RequiredModels = @('sonnet') }
$fixtureRecords = @($fixtures | ForEach-Object {
    try { Get-Content -LiteralPath $_.FullName -Raw | ConvertFrom-Json } catch { throw "Fixture is not valid JSON: $($_.FullName)" }
})
foreach ($scenario in $RequiredScenarios) {
    foreach ($model in $RequiredModels) {
        $found = @($fixtureRecords | Where-Object {
            $_.scenario -eq $scenario -and
            ([string]$_.model).IndexOf($model, [StringComparison]::OrdinalIgnoreCase) -ge 0
        }).Count -gt 0
        if (-not $found) {
            throw "Missing required company fixture: scenario '$scenario', model '$model'. Run .\Capture-CompanyClaudeFixtures.ps1 -Runtime $Runtime."
        }
    }
}
$required = ($RequiredModels -join ',')
$root = [IO.Path]::GetFullPath($PSScriptRoot).TrimEnd([char[]]@('\','/'))
if (-not $captureDirectory.StartsWith($root + [IO.Path]::DirectorySeparatorChar, [StringComparison]::OrdinalIgnoreCase)) {
    throw 'CaptureDirectory must be inside the repository so the pinned test container can verify it.'
}
$relativeCapture = $captureDirectory.Substring($root.Length).TrimStart([char[]]@('\','/')).Replace('\','/')
if ($nativeMode) {
    Push-Location $PSScriptRoot
    try {
        if ($UpdateManifest) {
            & $goCommand run -mod=vendor ./cmd/bap-fixture -mode manifest -directory $captureDirectory -manifest $manifestPath -bundle $bundlePath -public-key $publicKeyPath "-require-models=$required"
            if ($LASTEXITCODE -ne 0) { throw 'Could not create the Claude certification manifest.' }
        }
        if (-not (Test-Path -LiteralPath $manifestPath)) {
            throw "Captured fixtures exist but have not been reviewed and manifested. Run .\Test-ClaudeFixtures.ps1 -Runtime $Runtime -UpdateManifest -RequiredModels @('sonnet')."
        }
        & $goCommand run -mod=vendor ./cmd/bap-fixture -mode verify -directory $captureDirectory -manifest $manifestPath -bundle $bundlePath -public-key $publicKeyPath "-require-models=$required"
        if ($LASTEXITCODE -ne 0) { throw 'Claude fixture certification failed.' }
    } finally {
        Pop-Location
    }
    Write-Host 'PASS: captured Claude schemas, decisions, policy identity, model equivalence, and fixture hashes are certified.'
    exit 0
}
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
    throw "Captured fixtures exist but have not been reviewed and manifested. Run .\Test-ClaudeFixtures.ps1 -Runtime $Runtime -UpdateManifest -RequiredModels @('sonnet')."
}
& $engine run --rm --volume $mount --workdir /src docker.io/library/golang:1.23-bookworm `
    go run ./cmd/bap-fixture -mode verify -directory $containerDirectory -manifest $containerManifest -bundle $containerBundle -public-key $containerPublicKey "-require-models=$required"
if ($LASTEXITCODE -ne 0) { throw 'Claude fixture certification failed.' }
Write-Host 'PASS: captured Claude schemas, decisions, policy identity, model equivalence, and fixture hashes are certified.'
