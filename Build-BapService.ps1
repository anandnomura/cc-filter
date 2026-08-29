param([ValidateSet('Auto', 'Podman', 'Docker')][string]$Runtime = 'Auto')

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'
. (Join-Path $PSScriptRoot 'scripts\Runtime.ps1')
$engine = Get-BapContainerEngine -Runtime $Runtime
Write-Host "Building the Linux BAP Service OCI image with $engine..."
& $engine build --file (Join-Path $PSScriptRoot 'Containerfile') --tag bap-service:local $PSScriptRoot
if ($LASTEXITCODE -ne 0) { throw 'BAP Service image build failed.' }
Write-Host 'BAP Service image build complete: bap-service:local'
