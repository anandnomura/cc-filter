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

function Wait-BapHealth {
    param(
        [string]$Url = 'https://127.0.0.1:8443/healthz',
        [string]$CaBundle = (Join-Path $PSScriptRoot '..\.bap\runtime\docker\dev-ca.pem'),
        [int]$Attempts = 30
    )
    for ($attempt = 1; $attempt -le $Attempts; $attempt++) {
        try {
            $result = & curl.exe --silent --show-error --fail --ssl-no-revoke --cacert $CaBundle $Url 2>$null
            if ($LASTEXITCODE -eq 0 -and ($result | ConvertFrom-Json).status -eq 'ok') { return }
        } catch {
            Start-Sleep -Milliseconds 500
        }
    }
    throw "BAP Service did not become healthy at $Url."
}
