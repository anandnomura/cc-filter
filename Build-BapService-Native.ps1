param(
    [ValidateSet('Windows', 'Linux', 'All')][string]$Target = 'Windows',
    [ValidateSet('amd64', 'arm64', 'All')][string]$Architecture = 'amd64',
    [string]$Version = '',
    [switch]$SeparateAgentSTS
)

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'
. (Join-Path $PSScriptRoot 'scripts\GoToolchain.ps1')
$goCommand = Get-BapGoCommand -Required
$goVersionText = (& $goCommand version)
if ($LASTEXITCODE -ne 0 -or $goVersionText -notmatch 'go version go(?<major>\d+)\.(?<minor>\d+)(?:\.(?<patch>\d+))?') {
    throw "Could not validate the Go compiler version: $goVersionText"
}
$major = [int]$Matches.major
$minor = [int]$Matches.minor
$patch = if ($Matches.patch) { [int]$Matches.patch } else { 0 }
if ($major -lt 1 -or ($major -eq 1 -and ($minor -lt 23 -or ($minor -eq 23 -and $patch -lt 12)))) {
    throw "Go 1.23.12 or newer is required; found: $goVersionText"
}
if (-not $Version) {
    $commit = (& git rev-parse --short=12 HEAD 2>$null)
    $Version = if ($LASTEXITCODE -eq 0 -and $commit) { "company-$commit" } else { 'company-build' }
}

$dist = Join-Path $PSScriptRoot 'dist'
New-Item -ItemType Directory -Force -Path $dist | Out-Null
$targets = @()
if ($Target -in @('Windows', 'All')) {
    $targets += @{ OS = 'windows'; Architecture = 'amd64'; Output = (Join-Path $dist 'bap-service-windows-amd64.exe') }
}
if ($Target -in @('Linux', 'All')) {
    $architectures = if ($Architecture -eq 'All') { @('amd64', 'arm64') } else { @($Architecture) }
    foreach ($targetArchitecture in $architectures) {
        $targets += @{ OS = 'linux'; Architecture = $targetArchitecture; Output = (Join-Path $dist "bap-service-linux-$targetArchitecture") }
    }
}

Push-Location $PSScriptRoot
$previousCGO = $env:CGO_ENABLED
$previousGOOS = $env:GOOS
$previousGOARCH = $env:GOARCH
try {
    Write-Host "Running BAP Service tests with $goVersionText from vendored dependencies..."
    & $goCommand test -mod=vendor ./bap-service/... ./internal/authzen ./internal/auditwire ./internal/grants ./internal/policybundle ./internal/tracecontext
    if ($LASTEXITCODE -ne 0) { throw 'BAP Service tests failed.' }
    foreach ($buildTarget in $targets) {
        $output = $buildTarget.Output
        $env:CGO_ENABLED = '0'
        $env:GOOS = $buildTarget.OS
        $env:GOARCH = $buildTarget.Architecture
        & $goCommand build -mod=vendor -trimpath -ldflags "-s -w -X main.version=$Version" -o $output ./bap-service/cmd
        if ($LASTEXITCODE -ne 0) { throw "Native BAP Service compilation failed for $($buildTarget.OS)/$($buildTarget.Architecture)." }
        $hash = (Get-FileHash -Algorithm SHA256 -LiteralPath $output).Hash
        "$($hash.ToLowerInvariant())  $([IO.Path]::GetFileName($output))" | Set-Content -LiteralPath "$output.sha256" -Encoding ascii
        Write-Host "BAP Service $($buildTarget.OS)/$($buildTarget.Architecture) binary: $output"
        Write-Host "SHA-256: $hash"
		if ($SeparateAgentSTS) {
			$extension = if ($buildTarget.OS -eq 'windows') { '.exe' } else { '' }
			$stsOutput = Join-Path $dist "bap-agent-sts-$($buildTarget.OS)-$($buildTarget.Architecture)$extension"
			& $goCommand build -mod=vendor -trimpath -ldflags "-s -w -X main.version=$Version -X main.defaultRole=agent-sts" -o $stsOutput ./bap-service/cmd
			if ($LASTEXITCODE -ne 0) { throw "Native BAP Agent STS compilation failed for $($buildTarget.OS)/$($buildTarget.Architecture)." }
			$stsHash = (Get-FileHash -Algorithm SHA256 -LiteralPath $stsOutput).Hash
			"$($stsHash.ToLowerInvariant())  $([IO.Path]::GetFileName($stsOutput))" | Set-Content -LiteralPath "$stsOutput.sha256" -Encoding ascii
			Write-Host "Separate BAP Agent STS $($buildTarget.OS)/$($buildTarget.Architecture) binary: $stsOutput"
			Write-Host "SHA-256: $stsHash"
		}
    }
} finally {
    $env:CGO_ENABLED = $previousCGO
    $env:GOOS = $previousGOOS
    $env:GOARCH = $previousGOARCH
    Pop-Location
}
