param(
    [ValidateSet('Auto', 'Podman', 'Docker')][string]$Runtime = 'Auto',
    [switch]$VerifyHooksOnly,
    [switch]$UseCompanyClaude,
    [string]$Model = 'claude-3-5-sonnet-20241022',
    [string]$Tools = 'Bash',
    [string]$SystemPrompt = 'You are a Windows command agent using Git Bash. Copy exact commands from the user verbatim into the requested tool. Never substitute example paths or simulate results. Never claim a tool succeeded when it was blocked or denied; explicitly report the denial. After receiving a tool result, answer only from that result.',
    [Alias('p')][switch]$Print,
    [Parameter(Position = 0)][string]$Prompt = '',
    [Parameter(ValueFromRemainingArguments = $true)][string[]]$ClaudeArguments
)

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'
. (Join-Path $PSScriptRoot 'scripts\Runtime.ps1')

function Get-ClaudeExecutablePath {
    foreach ($name in @('claude.exe', 'claude.cmd', 'claude')) {
        $command = Get-Command $name -ErrorAction SilentlyContinue | Select-Object -First 1
        if ($null -eq $command) { continue }
        foreach ($propertyName in @('Path', 'Source', 'Definition')) {
            $property = $command.PSObject.Properties[$propertyName]
            if ($null -ne $property -and $property.Value -and (Test-Path -LiteralPath $property.Value -PathType Leaf)) {
                return (Resolve-Path -LiteralPath $property.Value).Path
            }
        }
    }
    $fallback = Join-Path $env:USERPROFILE '.local\bin\claude.exe'
    if (Test-Path -LiteralPath $fallback -PathType Leaf) { return (Resolve-Path -LiteralPath $fallback).Path }
    return $null
}

function ConvertTo-YamlPath {
    param([Parameter(Mandatory)][string]$Path)
    return $Path.Replace('\', '\\')
}

function Test-BapReadiness {
    param([Parameter(Mandatory)][string]$CaBundle)
    if (-not (Test-Path -LiteralPath $CaBundle)) { return $false }
    try {
        $response = & curl.exe --silent --show-error --fail --max-time 3 --ssl-no-revoke --cacert $CaBundle 'https://127.0.0.1:8443/readyz' 2>$null
        return $LASTEXITCODE -eq 0 -and ($response | ConvertFrom-Json).status -eq 'ready'
    } catch {
        return $false
    }
}

function Invoke-HookVerification {
    param(
        [Parameter(Mandatory)][string]$EdgeBinary,
        [Parameter(Mandatory)][string]$EdgeConfig
    )
    $sessionID = 'local-launcher-test-' + [Guid]::NewGuid().ToString('N')
    $cases = @(
        @{ Label = 'safe command'; Command = 'git status --short'; Expected = 'allow' },
        @{ Label = 'centrally configured command'; Command = 'ls -al'; Expected = 'allow' },
        @{ Label = 'destructive command'; Command = 'git reset --hard'; Expected = 'deny' },
        @{ Label = 'unclassified command'; Command = 'python -c "print(1)"'; Expected = 'deny' }
    )
    foreach ($case in $cases) {
        $inputObject = @{
            hook_event_name = 'PreToolUse'
            session_id = $sessionID
            tool_use_id = 'launcher-' + $case.Expected
            cwd = $PSScriptRoot
            tool_name = 'Bash'
            tool_input = @{ command = $case.Command }
        }
        $inputJson = $inputObject | ConvertTo-Json -Compress -Depth 8
        $result = $inputJson | & $EdgeBinary --config $EdgeConfig | ConvertFrom-Json
        $actual = $result.hookSpecificOutput.permissionDecision
        $reason = $result.hookSpecificOutput.permissionDecisionReason
        if ($actual -ne $case.Expected) {
            throw "BAP hook verification failed for $($case.Label): expected $($case.Expected), got $actual ($reason)"
        }
        Write-Host "PASS: $($case.Command) -> $actual ($reason)"
    }

    $endJson = @{
        hook_event_name = 'SessionEnd'
        session_id = $sessionID
        cwd = $PSScriptRoot
    } | ConvertTo-Json -Compress
    $endJson | & $EdgeBinary --config $EdgeConfig | Out-Null
}

$edgeBinary = Join-Path $PSScriptRoot 'dist\bap-edge-windows-amd64.exe'
$engine = ''
if ($Runtime -eq 'Auto') {
    foreach ($candidate in @('podman', 'docker')) {
        $candidateRuntime = Get-BapRuntimeDirectory -Engine $candidate
        if (Test-BapReadiness -CaBundle (Join-Path $candidateRuntime 'dev-ca.pem')) {
            $engine = $candidate
            break
        }
    }
}
if (-not $engine) { $engine = Get-BapContainerEngine -Runtime $Runtime }
$runtimeDirectory = Get-BapRuntimeDirectory -Engine $engine
$caBundle = Join-Path $runtimeDirectory 'dev-ca.pem'

if (-not (Test-BapReadiness -CaBundle $caBundle)) {
    Write-Host "BAP Service is not ready; starting it with $engine..."
    & (Join-Path $PSScriptRoot 'Start-Bap.ps1') -Runtime $engine
}

Write-Host 'Building BAP Edge from the current source...'
& (Join-Path $PSScriptRoot 'Build-BapEdge.ps1') -Runtime $engine

$requiredRuntimeFiles = @('dev-ca.pem', 'bundle-public.pem', 'edge-api-key.txt')
foreach ($name in $requiredRuntimeFiles) {
    $path = Join-Path $runtimeDirectory $name
    if (-not (Test-Path -LiteralPath $path)) { throw "BAP runtime file is missing: $path" }
}

$localDirectory = Join-Path $PSScriptRoot '.bap\local-claude'
$edgeStateDirectory = Join-Path $localDirectory 'edge-state'
$edgeConfig = Join-Path $localDirectory 'bap-edge.yaml'
$claudeSettings = Join-Path $localDirectory 'claude-settings.json'
New-Item -ItemType Directory -Force -Path $localDirectory, $edgeStateDirectory | Out-Null

$bundlePublicKey = ConvertTo-YamlPath (Join-Path $runtimeDirectory 'bundle-public.pem')
$caPath = ConvertTo-YamlPath $caBundle
$statePath = ConvertTo-YamlPath $edgeStateDirectory
@"
service_url: "https://127.0.0.1:8443"
bundle_public_key_path: "$bundlePublicKey"
ca_bundle_path: "$caPath"
subject_id: "claude-code-local"
timeout_ms: 3000
state_directory: "$statePath"
api_key_env: "BAP_EDGE_API_KEY"
"@ | Set-Content -LiteralPath $edgeConfig -Encoding utf8

$hookCommand = '"' + $edgeBinary.Replace('\', '/') + '" --config "' + $edgeConfig.Replace('\', '/') + '"'
$hook = @{ type = 'command'; command = $hookCommand; timeout = 10 }
$settings = [ordered]@{
    '$schema' = 'https://json.schemastore.org/claude-code-settings.json'
    hooks = [ordered]@{
        SessionStart = @(@{ matcher = 'startup|resume|clear|compact'; hooks = @($hook) })
        PreToolUse = @(@{ matcher = '*'; hooks = @($hook) })
        PostToolUse = @(@{ matcher = '*'; hooks = @($hook) })
        PostToolUseFailure = @(@{ matcher = '*'; hooks = @($hook) })
        UserPromptSubmit = @(@{ matcher = '*'; hooks = @($hook) })
        SessionEnd = @(@{ matcher = '*'; hooks = @($hook) })
    }
}
$settings | ConvertTo-Json -Depth 12 | Set-Content -LiteralPath $claudeSettings -Encoding utf8

$env:BAP_EDGE_API_KEY = (Get-Content -LiteralPath (Join-Path $runtimeDirectory 'edge-api-key.txt') -Raw).Trim()

$managedSettings = Join-Path $env:ProgramFiles 'ClaudeCode\managed-settings.d\50-bap-edge.json'
$managedEdgeDirectory = Join-Path $env:ProgramFiles 'BAP Edge'
$usingManagedHooks = Test-Path -LiteralPath $managedSettings
if ($usingManagedHooks) {
    $managedEdgeBinary = Join-Path $managedEdgeDirectory 'bap-edge.exe'
    if (-not (Test-Path -LiteralPath $managedEdgeBinary)) {
        throw "Managed BAP Edge executable is missing: $managedEdgeBinary"
    }
    $installedEdgeHash = (Get-FileHash -LiteralPath $managedEdgeBinary -Algorithm SHA256).Hash
    $currentEdgeHash = (Get-FileHash -LiteralPath $edgeBinary -Algorithm SHA256).Hash
    if ($installedEdgeHash -ne $currentEdgeHash) {
        throw "Managed BAP Edge is older or different from the current build. From an elevated PowerShell window run .\Install-ManagedSettings.ps1 -Runtime $engine, then retry .\start-local-claude.bat."
    }
    $managedCa = Join-Path $managedEdgeDirectory 'service-ca-bundle.pem'
    $managedBundleKey = Join-Path $managedEdgeDirectory 'bundle-public.pem'
    foreach ($pair in @(
        @{ Installed = $managedCa; Active = $caBundle; Label = 'CA bundle' },
        @{ Installed = $managedBundleKey; Active = (Join-Path $runtimeDirectory 'bundle-public.pem'); Label = 'policy-bundle public key' }
    )) {
        if (-not (Test-Path -LiteralPath $pair.Installed)) {
            throw "Managed BAP Edge $($pair.Label) is missing: $($pair.Installed)"
        }
        $installedHash = (Get-FileHash -LiteralPath $pair.Installed -Algorithm SHA256).Hash
        $activeHash = (Get-FileHash -LiteralPath $pair.Active -Algorithm SHA256).Hash
        if ($installedHash -ne $activeHash) {
            throw "Managed BAP Edge $($pair.Label) does not match the running $engine BAP Service. Re-run .\Install-ManagedSettings.ps1 -Runtime $engine from an elevated PowerShell window."
        }
    }
}

if ($VerifyHooksOnly) {
    Invoke-HookVerification -EdgeBinary $edgeBinary -EdgeConfig $edgeConfig
    Write-Host "PASS: all six Claude hooks are configured in $claudeSettings"
    exit 0
}

$claudeExecutable = Get-ClaudeExecutablePath
if (-not $claudeExecutable) {
    throw 'Claude Code was not found on PATH or at %USERPROFILE%\.local\bin\claude.exe'
}

if (-not $UseCompanyClaude) {
    try {
        $bridge = Invoke-RestMethod 'http://127.0.0.1:8080/health' -TimeoutSec 5
        if (-not $bridge.status) { throw 'ccbridge returned an unhealthy response.' }
    } catch {
        throw 'The updated ccbridge is not ready at http://127.0.0.1:8080/health. Run start-ccbridge.bat in a separate window first, or use -UseCompanyClaude.'
    }
    $env:ANTHROPIC_BASE_URL = 'http://127.0.0.1:8080'
    $env:ANTHROPIC_API_KEY = 'local-demo-key'
} else {
    Remove-Item Env:ANTHROPIC_BASE_URL -ErrorAction SilentlyContinue
    $processAnthropicKey = [Environment]::GetEnvironmentVariable('ANTHROPIC_API_KEY', 'Process')
    if ($processAnthropicKey -eq 'local-demo-key') {
        Remove-Item Env:ANTHROPIC_API_KEY -ErrorAction SilentlyContinue
    }
}
$env:CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC = '1'

Write-Host ''
if ($usingManagedHooks) {
    Write-Host "Local Claude is using managed BAP Edge hooks from $managedSettings"
    Write-Host "Managed hook executable: $managedEdgeDirectory\bap-edge.exe"
    Write-Host 'User, project, and local setting sources are disabled for this launcher.'
} else {
    Write-Host "Local Claude is using repo-local BAP Edge hooks from $claudeSettings"
}
Write-Host 'ALLOW test: Call Bash exactly once with this exact command: git status --short'
Write-Host 'ALLOW test: Call Bash exactly once with this exact command: ls -al'
Write-Host 'DENY test:  Call Bash exactly once with this exact command: git reset --hard'
Write-Host 'DENY test:  Call Bash exactly once with this exact command: python -c "print(1)"'
Write-Host ''

$defaultArguments = @(
    '--model', $Model,
    '--tools', $Tools,
    '--system-prompt', $SystemPrompt
)
if ($usingManagedHooks) {
    # Managed policy is always loaded independently. Excluding ordinary sources
    # makes /status unambiguous and prevents unrelated user/project preferences
    # from affecting this controlled local-LLM test session.
    $defaultArguments += '--setting-sources='
} else {
    $defaultArguments += @('--settings', $claudeSettings)
}
if ($Print) { $defaultArguments += '--print' }
if ($Prompt) { $defaultArguments += $Prompt }
& $claudeExecutable @defaultArguments @ClaudeArguments
exit $LASTEXITCODE
