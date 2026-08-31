param([ValidateSet('Auto', 'Podman', 'Docker', 'Native')][string]$Runtime = 'Auto')

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'
Write-Host 'Building the independently deployable BAP Edge and BAP Service components...'
if ($Runtime -eq 'Native') {
    & (Join-Path $PSScriptRoot 'Build-BapEdge.ps1') -Runtime Native
    & (Join-Path $PSScriptRoot 'Build-BapService.ps1') -Runtime Native
    Write-Warning 'Native mode produced the Windows Edge and Linux Service binaries. Creating the final BAP Service OCI image still requires a container/Linux packaging pipeline.'
    return
}
if ($Runtime -eq 'Auto') {
    . (Join-Path $PSScriptRoot 'scripts\Runtime.ps1')
    try {
        $resolvedRuntime = Get-BapContainerEngine -Runtime Auto
    } catch {
        if (-not (Get-Command go -ErrorAction SilentlyContinue)) { throw }
        & (Join-Path $PSScriptRoot 'Build-BapEdge.ps1') -Runtime Native
        & (Join-Path $PSScriptRoot 'Build-BapService.ps1') -Runtime Native
        Write-Warning 'No container runtime is usable, so native binaries were built. Package the Linux BAP Service binary into OCI in the company container pipeline.'
        return
    }
} else {
    $resolvedRuntime = $Runtime
}
& (Join-Path $PSScriptRoot 'Build-BapEdge.ps1') -Runtime $resolvedRuntime
& (Join-Path $PSScriptRoot 'Build-BapService.ps1') -Runtime $resolvedRuntime
Write-Host 'Combined development build complete.'
