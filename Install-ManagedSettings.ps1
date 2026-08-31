[CmdletBinding(SupportsShouldProcess = $true, ConfirmImpact = 'High')]
param(
    [Alias('Uninstall')][switch]$Undo,
    [ValidateSet('Auto', 'Podman', 'Docker')][string]$Runtime = 'Auto',
    [string]$ServiceUrl = 'https://127.0.0.1:8443',
    [string]$GrantPublicKeyPath = '',
    [string]$BundlePublicKeyPath = '',
    [string]$CaBundlePath = '',
    [string]$ClientCertificatePath = '',
    [string]$ClientKeyPath = '',
    [string]$ApiKey = '',
    [string]$EdgeBinaryPath = ''
)

#Requires -RunAsAdministrator
Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'
. (Join-Path $PSScriptRoot 'scripts\Runtime.ps1')
$isLocalDevelopment = $ServiceUrl -eq 'https://127.0.0.1:8443'
if (($ClientCertificatePath -and -not $ClientKeyPath) -or ($ClientKeyPath -and -not $ClientCertificatePath)) {
    throw 'Provide both -ClientCertificatePath and -ClientKeyPath for mutual TLS.'
}

$installDirectory = Join-Path $env:ProgramFiles 'BAP Edge'
$managedDirectory = Join-Path $env:ProgramFiles 'ClaudeCode\managed-settings.d'
$binaryPath = Join-Path $installDirectory 'bap-edge.exe'
$managedPath = Join-Path $managedDirectory '50-bap-edge.json'

if ($Undo) {
    if (-not (Test-Path -LiteralPath $managedPath -PathType Leaf)) {
        Write-Host "BAP managed settings are already absent: $managedPath"
        Write-Host 'No installed binaries, configuration, certificates, or credentials were changed.'
        exit 0
    }

    try {
        $installedManagedSettings = Get-Content -LiteralPath $managedPath -Raw | ConvertFrom-Json
        $managedOnlyProperty = $installedManagedSettings.PSObject.Properties['allowManagedHooksOnly']
        $preToolProperty = $installedManagedSettings.hooks.PSObject.Properties['PreToolUse']
        $preToolGroups = if ($null -ne $preToolProperty) { @($preToolProperty.Value) } else { @() }
        $ownsDropIn = $null -ne $managedOnlyProperty -and $managedOnlyProperty.Value -eq $true
        $ownsDropIn = $ownsDropIn -and @(
            foreach ($group in $preToolGroups) {
                foreach ($handler in @($group.hooks)) {
                    $commandProperty = $handler.PSObject.Properties['command']
                    if ($null -ne $commandProperty -and $commandProperty.Value -like "*$binaryPath*") { $true }
                }
            }
        ).Count -gt 0
    } catch {
        throw "Refusing to remove an unreadable managed-settings file: $managedPath"
    }
    if (-not $ownsDropIn) {
        throw "Refusing to remove $managedPath because it is not recognizable as the BAP-managed drop-in."
    }

    if (-not $PSCmdlet.ShouldProcess($managedPath, 'Remove the BAP managed Claude settings drop-in')) {
        Write-Host 'No managed settings were removed.'
        exit 0
    }
    Remove-Item -LiteralPath $managedPath -Force
    if (Test-Path -LiteralPath $managedPath) {
        throw "BAP managed-settings removal did not complete: $managedPath still exists."
    }
    Write-Host "Removed BAP managed Claude settings: $managedPath"
    Write-Host "Retained BAP Edge files at $installDirectory and retained the machine BAP_EDGE_API_KEY."
    Write-Host 'Managed-to-native transition is ready. Close every existing Claude Code session, then run .\Start-BapNativeLocal.bat from a normal PowerShell window.'
    Write-Host 'The native launcher will create a new isolated run; it will not reuse an older development audit chain.'
    Write-Host 'Re-run Install-ManagedSettings.ps1 to restore managed enforcement.'
    exit 0
}

function Test-BapHealthWithCa {
    param([Parameter(Mandatory)][string]$CaBundle)
    if (-not (Test-Path -LiteralPath $CaBundle)) { return $false }
    try {
        $response = & curl.exe --silent --show-error --fail --max-time 3 --ssl-no-revoke --cacert $CaBundle "$ServiceUrl/healthz" 2>$null
        return $LASTEXITCODE -eq 0 -and ($response | ConvertFrom-Json).status -eq 'ok'
    } catch {
        return $false
    }
}

$binarySource = if ($EdgeBinaryPath) { $EdgeBinaryPath } else { Join-Path $PSScriptRoot 'dist\bap-edge-windows-amd64.exe' }
$engine = ''
$runtimeDirectory = ''
$localPublicKey = ''
$localBundlePublicKey = ''
if ($EdgeBinaryPath -and -not (Test-Path -LiteralPath $binarySource)) {
    throw "The supplied prebuilt Edge binary does not exist: $binarySource"
}
if ($isLocalDevelopment -and $Runtime -eq 'Auto') {
    foreach ($candidate in @('podman', 'docker')) {
        $candidateRuntime = Get-BapRuntimeDirectory -Engine $candidate
        if (Test-BapHealthWithCa -CaBundle (Join-Path $candidateRuntime 'dev-ca.pem')) {
            $engine = $candidate
            $runtimeDirectory = $candidateRuntime
            break
        }
    }
}
if (($isLocalDevelopment -or (-not $EdgeBinaryPath -and -not (Test-Path -LiteralPath $binarySource))) -and -not $engine) {
    $engine = Get-BapContainerEngine -Runtime $Runtime
    $runtimeDirectory = Get-BapRuntimeDirectory -Engine $engine
}
if (-not $isLocalDevelopment -and -not $BundlePublicKeyPath) {
    throw 'Network installation requires -BundlePublicKeyPath from the BAP Service administrator.'
}
if (-not (Test-Path -LiteralPath $binarySource)) {
    & (Join-Path $PSScriptRoot 'Build-BapEdge.ps1') -Runtime $Runtime
}
if ($isLocalDevelopment) {
    $localPublicKey = Join-Path $runtimeDirectory 'grant-public.pem'
    $localBundlePublicKey = Join-Path $runtimeDirectory 'bundle-public.pem'
}
$publicKeySource = if ($GrantPublicKeyPath) { $GrantPublicKeyPath } else { $localPublicKey }
$bundlePublicKeySource = if ($BundlePublicKeyPath) { $BundlePublicKeyPath } else { $localBundlePublicKey }
if ($isLocalDevelopment -and -not (Test-Path -LiteralPath $publicKeySource)) {
    & (Join-Path $PSScriptRoot 'Start-Bap.ps1') -Runtime $Runtime
}
if ($publicKeySource -and -not (Test-Path -LiteralPath $publicKeySource)) {
    throw "Place the BAP Service grant verification public key at $publicKeySource before installing."
}
if (-not (Test-Path -LiteralPath $bundlePublicKeySource)) {
    throw "Place the BAP Service policy bundle verification public key at $bundlePublicKeySource before installing."
}
if (-not $ApiKey -and $isLocalDevelopment) {
    $ApiKey = (Get-Content -LiteralPath (Join-Path $runtimeDirectory 'edge-api-key.txt') -Raw).Trim()
}
if (-not $ApiKey -and -not $ClientCertificatePath) {
    throw 'Provide a per-device mTLS identity or the local-development BAP Edge -ApiKey. Do not use an Anthropic API key.'
}

$configPath = Join-Path $installDirectory 'bap-edge.yaml'
$publicKeyPath = Join-Path $installDirectory 'grant-public.pem'
$bundlePublicKeyPath = Join-Path $installDirectory 'bundle-public.pem'
$installedCaPath = Join-Path $installDirectory 'service-ca-bundle.pem'
$installedClientCertificatePath = Join-Path $installDirectory 'client-certificate.pem'
$installedClientKeyPath = Join-Path $installDirectory 'client-key.pem'

New-Item -ItemType Directory -Force -Path $installDirectory, $managedDirectory | Out-Null
Copy-Item -LiteralPath $binarySource -Destination $binaryPath -Force
if ($publicKeySource) {
    Copy-Item -LiteralPath $publicKeySource -Destination $publicKeyPath -Force
}
Copy-Item -LiteralPath $bundlePublicKeySource -Destination $bundlePublicKeyPath -Force
$configuredCaPath = ''
if (-not $CaBundlePath -and $isLocalDevelopment) {
    $CaBundlePath = Join-Path $runtimeDirectory 'dev-ca.pem'
}
if ($CaBundlePath) {
    Copy-Item -LiteralPath $CaBundlePath -Destination $installedCaPath -Force
    $configuredCaPath = $installedCaPath
}
$configuredClientCertificatePath = ''
$configuredClientKeyPath = ''
if ($ClientCertificatePath) {
    Copy-Item -LiteralPath $ClientCertificatePath -Destination $installedClientCertificatePath -Force
    Copy-Item -LiteralPath $ClientKeyPath -Destination $installedClientKeyPath -Force
    $configuredClientCertificatePath = $installedClientCertificatePath
    $configuredClientKeyPath = $installedClientKeyPath
}
@"
service_url: "$ServiceUrl"
public_key_path: "$(if ($publicKeySource) { $publicKeyPath.Replace('\', '\\') } else { '' })"
bundle_public_key_path: "$($bundlePublicKeyPath.Replace('\', '\\'))"
ca_bundle_path: "$($configuredCaPath.Replace('\', '\\'))"
client_certificate_path: "$($configuredClientCertificatePath.Replace('\', '\\'))"
client_key_path: "$($configuredClientKeyPath.Replace('\', '\\'))"
subject_id: "claude-code-local"
timeout_ms: 3000
cache_directory: ""
state_directory: ""
api_key_env: "BAP_EDGE_API_KEY"
"@ | Set-Content -LiteralPath $configPath -Encoding utf8
if ($ApiKey) {
    [Environment]::SetEnvironmentVariable('BAP_EDGE_API_KEY', $ApiKey, 'Machine')
}

$quotedBinary = '"' + $binaryPath + '"'
$quotedConfig = '"' + $configPath + '"'
$hookCommand = "$quotedBinary --config $quotedConfig"
$hook = @{ type = 'command'; command = $hookCommand; timeout = 10 }
$managed = [ordered]@{
    '$schema' = 'https://json.schemastore.org/claude-code-settings.json'
    allowManagedHooksOnly = $true
    allowManagedPermissionRulesOnly = $true
    requiredMinimumVersion = '2.1.246'
    permissions = [ordered]@{ disableBypassPermissionsMode = 'disable' }
    hooks = [ordered]@{
        SessionStart = @(@{ matcher = 'startup|resume|clear|compact'; hooks = @($hook) })
        PreToolUse = @(@{ matcher = '*'; hooks = @($hook) })
        PostToolUse = @(@{ matcher = '*'; hooks = @($hook) })
        PostToolUseFailure = @(@{ matcher = '*'; hooks = @($hook) })
        UserPromptSubmit = @(@{ matcher = '*'; hooks = @($hook) })
        SessionEnd = @(@{ matcher = '*'; hooks = @($hook) })
    }
}
$managed | ConvertTo-Json -Depth 12 | Set-Content -LiteralPath $managedPath -Encoding utf8

# Remove inherited write access. Standard users receive read/execute only.
& icacls $installDirectory /inheritance:r /grant:r 'SYSTEM:(OI)(CI)F' 'BUILTIN\Administrators:(OI)(CI)F' 'BUILTIN\Users:(OI)(CI)RX' | Out-Host
& icacls $managedPath /inheritance:r /grant:r 'SYSTEM:F' 'BUILTIN\Administrators:F' 'BUILTIN\Users:R' | Out-Host

Write-Host "Installed BAP Edge at $binaryPath"
Write-Host "Installed managed Claude policy at $managedPath"
Write-Host 'Restart Claude Code, then run /status and /permissions to verify the managed source.'
Write-Host 'The /hooks screen may show 0 editable hooks even while managed policy hooks are active; run Test-ManagedSettings.ps1 for an end-to-end check.'
