param([ValidateSet('Auto', 'Podman', 'Docker', 'Native')][string]$Runtime = 'Auto')

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'
. (Join-Path $PSScriptRoot 'scripts\GoToolchain.ps1')

$packages = @('./internal/policybundle', './bap-edge/cmd', './internal/hooks', './bap-service/cmd', './bap-service/internal/server')
$goCommand = Get-BapGoCommand
Push-Location $PSScriptRoot
try {
    if ($Runtime -eq 'Native' -or ($Runtime -eq 'Auto' -and $goCommand)) {
        if (-not $goCommand) { $goCommand = Get-BapGoCommand -Required }
        & $goCommand test -mod=vendor -count=1 @packages
    } else {
        . (Join-Path $PSScriptRoot 'scripts\Runtime.ps1')
        $engine = Get-BapContainerEngine -Runtime $Runtime
        $mount = "$($PSScriptRoot):/src"
        & $engine run --rm --volume $mount --workdir /src docker.io/library/golang:1.23-bookworm `
            go test -mod=vendor -count=1 @packages
    }
    if ($LASTEXITCODE -ne 0) { throw 'Shadow-mode Go security tests failed.' }
} finally {
    Pop-Location
}

$python = Get-Command py -ErrorAction SilentlyContinue
if ($python) {
    & $python.Source -3 -m unittest discover -s (Join-Path $PSScriptRoot 'scripts') -p 'test_analyze_shadow.py' -v
} else {
    $python = Get-Command python -ErrorAction SilentlyContinue
    if (-not $python) { throw 'Python 3 is required for the shadow recommendation tests.' }
    & $python.Source -m unittest discover -s (Join-Path $PSScriptRoot 'scripts') -p 'test_analyze_shadow.py' -v
}
if ($LASTEXITCODE -ne 0) { throw 'Shadow recommendation analyzer tests failed.' }

Write-Host 'PASS: signed shadow mode, expiry, production rejection, hard boundaries, cc-filter redaction, evaluated/effective audit, and offline ML-ranked human-review recommendations.'
