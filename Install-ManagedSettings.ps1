param(
    [ValidateSet('Auto', 'Podman', 'Docker')][string]$Runtime = 'Auto',
    [string]$ServiceUrl = 'https://127.0.0.1:8443',
    [string]$GrantPublicKeyPath = '',
    [string]$CaBundlePath = '',
    [string]$ApiKey = '',
    [string]$EdgeBinaryPath = ''
)

#Requires -RunAsAdministrator
Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'
. (Join-Path $PSScriptRoot 'scripts\Runtime.ps1')
$isLocalDevelopment = $ServiceUrl -eq 'https://127.0.0.1:8443'
$binarySource = if ($EdgeBinaryPath) { $EdgeBinaryPath } else { Join-Path $PSScriptRoot 'dist\bap-edge-windows-amd64.exe' }
$engine = ''
$runtimeDirectory = ''
if ($EdgeBinaryPath -and -not (Test-Path -LiteralPath $binarySource)) {
    throw "The supplied prebuilt Edge binary does not exist: $binarySource"
}
if ($isLocalDevelopment -or (-not $EdgeBinaryPath -and -not (Test-Path -LiteralPath $binarySource))) {
    $engine = Get-BapContainerEngine -Runtime $Runtime
    $runtimeDirectory = Get-BapRuntimeDirectory -Engine $engine
}
if (-not $isLocalDevelopment -and -not $GrantPublicKeyPath) {
    throw 'Network installation requires -GrantPublicKeyPath from the BAP Service administrator.'
}
if (-not (Test-Path -LiteralPath $binarySource)) {
    & (Join-Path $PSScriptRoot 'Build-BapEdge.ps1') -Runtime $Runtime
}
if ($isLocalDevelopment) {
    $localPublicKey = Join-Path $runtimeDirectory 'grant-public.pem'
}
$publicKeySource = if ($GrantPublicKeyPath) { $GrantPublicKeyPath } else { $localPublicKey }
if ($isLocalDevelopment -and -not (Test-Path -LiteralPath $publicKeySource)) {
    & (Join-Path $PSScriptRoot 'Start-Bap.ps1') -Runtime $Runtime
}
if (-not (Test-Path -LiteralPath $publicKeySource)) {
    throw "Place the BAP Service grant verification public key at $publicKeySource before installing."
}
if (-not $ApiKey -and $isLocalDevelopment) {
    $ApiKey = (Get-Content -LiteralPath (Join-Path $runtimeDirectory 'edge-api-key.txt') -Raw).Trim()
}
if (-not $ApiKey) {
    throw 'Provide the dedicated BAP Edge credential with -ApiKey. Do not use an Anthropic API key.'
}

$installDirectory = Join-Path $env:ProgramFiles 'BAP Edge'
$managedDirectory = Join-Path $env:ProgramFiles 'ClaudeCode\managed-settings.d'
$binaryPath = Join-Path $installDirectory 'bap-edge.exe'
$configPath = Join-Path $installDirectory 'bap-edge.yaml'
$publicKeyPath = Join-Path $installDirectory 'grant-public.pem'
$installedCaPath = Join-Path $installDirectory 'service-ca-bundle.pem'
$managedPath = Join-Path $managedDirectory '50-bap-edge.json'

New-Item -ItemType Directory -Force -Path $installDirectory, $managedDirectory | Out-Null
Copy-Item -LiteralPath $binarySource -Destination $binaryPath -Force
Copy-Item -LiteralPath $publicKeySource -Destination $publicKeyPath -Force
$configuredCaPath = ''
if (-not $CaBundlePath -and $isLocalDevelopment) {
    $CaBundlePath = Join-Path $runtimeDirectory 'dev-ca.pem'
}
if ($CaBundlePath) {
    Copy-Item -LiteralPath $CaBundlePath -Destination $installedCaPath -Force
    $configuredCaPath = $installedCaPath
}
@"
service_url: "$ServiceUrl"
public_key_path: "$($publicKeyPath.Replace('\', '\\'))"
ca_bundle_path: "$($configuredCaPath.Replace('\', '\\'))"
subject_id: "claude-code-local"
timeout_ms: 3000
cache_directory: ""
state_directory: ""
api_key_env: "BAP_EDGE_API_KEY"
"@ | Set-Content -LiteralPath $configPath -Encoding utf8
[Environment]::SetEnvironmentVariable('BAP_EDGE_API_KEY', $ApiKey, 'Machine')

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
Write-Host 'Restart Claude Code, then run /status, /hooks, and /permissions to verify the managed source.'
