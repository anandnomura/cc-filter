param(
    [ValidateSet('Auto', 'Podman', 'Docker')][string]$Runtime = 'Auto',
    [Parameter(Mandatory)][string]$Version,
    [Parameter(Mandatory)][string]$Registry,
    [Parameter(Mandatory)][string]$BuildImage,
    [Parameter(Mandatory)][string]$RuntimeImage
)

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'
. (Join-Path $PSScriptRoot 'scripts\Runtime.ps1')
$engine = Get-BapContainerEngine -Runtime $Runtime
$dist = Join-Path $PSScriptRoot 'dist'
New-Item -ItemType Directory -Force -Path $dist | Out-Null

$commit = (& git -C $PSScriptRoot rev-parse HEAD).Trim()
if ($LASTEXITCODE -ne 0) { throw 'The source tree must be a Git checkout for a company release build.' }
if (& git -C $PSScriptRoot status --porcelain) {
    throw 'Company release builds require a clean source tree.'
}

& (Join-Path $PSScriptRoot 'Build-BapEdge-Native.ps1') -Version $Version
if ($LASTEXITCODE -ne 0) { throw 'BAP Edge company build failed.' }

$imageTag = "$($Registry.TrimEnd('/'))/bap-service:$Version"
& (Join-Path $PSScriptRoot 'Build-BapService.ps1') -Runtime $Runtime -Tag $imageTag -Version $Version -BuildImage $BuildImage -RuntimeImage $RuntimeImage
if ($LASTEXITCODE -ne 0) { throw 'BAP Service company image build failed.' }

$archive = Join-Path $dist "bap-service-$Version.oci.tar"
& $engine save --output $archive $imageTag
if ($LASTEXITCODE -ne 0) { throw 'BAP Service OCI archive export failed.' }

$artifacts = @(
    (Join-Path $dist 'bap-edge-windows-amd64.exe'),
    $archive
)
$manifest = [ordered]@{
    version = $Version
    source_commit = $commit
    built_at_utc = [DateTime]::UtcNow.ToString('o')
    go_version = ((& go version) -join ' ').Trim()
    container_engine = $engine
    build_image = $BuildImage
    runtime_image = $RuntimeImage
    service_image = $imageTag
    artifacts = @($artifacts | ForEach-Object {
        [ordered]@{
            name = [IO.Path]::GetFileName($_)
            sha256 = (Get-FileHash -Algorithm SHA256 -LiteralPath $_).Hash.ToLowerInvariant()
            bytes = (Get-Item -LiteralPath $_).Length
        }
    })
}
$manifestPath = Join-Path $dist "company-build-$Version.json"
$manifest | ConvertTo-Json -Depth 5 | Set-Content -LiteralPath $manifestPath -Encoding utf8
Write-Host "Company artifacts and manifest are ready under $dist"
