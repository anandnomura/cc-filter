Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'

function Get-BapContainerEngine {
    param([ValidateSet('Auto', 'Podman', 'Docker')][string]$Runtime = 'Auto')

    $candidates = if ($Runtime -eq 'Auto') { @('podman', 'docker') } else { @($Runtime.ToLowerInvariant()) }
    foreach ($candidate in $candidates) {
        if (-not (Get-Command $candidate -ErrorAction SilentlyContinue)) { continue }
        & $candidate info *> $null
        if ($LASTEXITCODE -eq 0) { return $candidate }

        if ($candidate -eq 'podman') {
            & podman machine start *> $null
        } elseif (Get-Command 'docker' -ErrorAction SilentlyContinue) {
            & docker desktop start *> $null
        }
        & $candidate info *> $null
        if ($LASTEXITCODE -eq 0) { return $candidate }
    }
    throw 'Neither Podman nor Docker is installed and running. Start one runtime and try again.'
}

function Get-BapRuntimeDirectory {
    param([Parameter(Mandatory)][string]$Engine)
    $root = Split-Path -Parent $PSScriptRoot
    return Join-Path $root ".bap\runtime\$($Engine.ToLowerInvariant())"
}

function Get-BapNativeLatestRunDirectory {
    $root = Split-Path -Parent $PSScriptRoot
    $runsRoot = Join-Path $root '.bap\native-local\runs'
    $latestPath = Join-Path $root '.bap\native-local\latest-run.txt'
    if (-not (Test-Path -LiteralPath $latestPath -PathType Leaf)) {
        throw 'No retained native BAP run was found. Run .\Start-BapNativeLocal.ps1 -VerifyOnly first.'
    }
    $candidate = (Get-Content -LiteralPath $latestPath -Raw).Trim().TrimStart([char]0xFEFF)
    if (-not $candidate -or -not (Test-Path -LiteralPath $candidate -PathType Container)) {
        throw "The latest native BAP run is missing: $candidate"
    }
    $resolvedRuns = (Resolve-Path -LiteralPath $runsRoot).Path.TrimEnd('\')
    $resolvedCandidate = (Resolve-Path -LiteralPath $candidate).Path
    if (-not $resolvedCandidate.StartsWith($resolvedRuns + '\', [StringComparison]::OrdinalIgnoreCase)) {
        throw "The native run pointer is outside the repository runtime directory: $resolvedCandidate"
    }
    return $resolvedCandidate
}

function Wait-BapHealth {
    param(
        [string]$Url = 'https://127.0.0.1:8443/readyz',
        [string]$CaBundle = (Join-Path $PSScriptRoot '..\.bap\runtime\docker\dev-ca.pem'),
        [int]$Attempts = 30
    )
    for ($attempt = 1; $attempt -le $Attempts; $attempt++) {
        try {
            $result = & curl.exe --silent --show-error --fail --ssl-no-revoke --cacert $CaBundle $Url 2>$null
            if ($LASTEXITCODE -eq 0 -and ($result | ConvertFrom-Json).status -in @('ok', 'ready')) { return }
        } catch {
            Start-Sleep -Milliseconds 500
        }
    }
    throw "BAP Service did not become healthy at $Url."
}
