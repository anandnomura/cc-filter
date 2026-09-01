param(
    [ValidateSet('Auto', 'Podman', 'Docker', 'Native')][string]$Runtime = 'Auto',
    [switch]$SeparateAgentSTS
)

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'
. (Join-Path $PSScriptRoot 'scripts\GoToolchain.ps1')
Write-Host 'Building the independently deployable BAP Edge and BAP Service components...'
if ($Runtime -eq 'Native') {
    & (Join-Path $PSScriptRoot 'Build-BapEdge.ps1') -Runtime Native
    & (Join-Path $PSScriptRoot 'Build-BapService.ps1') -Runtime Native -SeparateAgentSTS:$SeparateAgentSTS
    Write-Warning 'Native mode produced Windows Edge and Service executables. Creating a BAP Service OCI image still requires a container/Linux packaging pipeline.'
    return
}
if ($Runtime -eq 'Auto') {
    . (Join-Path $PSScriptRoot 'scripts\Runtime.ps1')
    try {
        $resolvedRuntime = Get-BapContainerEngine -Runtime Auto
    } catch {
        if (-not (Get-BapGoCommand)) { throw }
        & (Join-Path $PSScriptRoot 'Build-BapEdge.ps1') -Runtime Native
        & (Join-Path $PSScriptRoot 'Build-BapService.ps1') -Runtime Native -SeparateAgentSTS:$SeparateAgentSTS
        Write-Warning 'No container runtime is usable, so native Windows binaries were built. Use an explicit Linux target if the company packaging pipeline needs a Linux Service binary.'
        return
    }
} else {
    $resolvedRuntime = $Runtime
}
& (Join-Path $PSScriptRoot 'Build-BapEdge.ps1') -Runtime $resolvedRuntime
& (Join-Path $PSScriptRoot 'Build-BapService.ps1') -Runtime $resolvedRuntime -SeparateAgentSTS:$SeparateAgentSTS
Write-Host 'Combined development build complete.'
