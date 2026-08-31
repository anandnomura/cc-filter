param(
    [switch]$Rebuild,
    [switch]$VerifyOnly,
    [ValidateRange(1, 65535)][int]$Port = 8443,
    [Parameter(ValueFromRemainingArguments = $true)][string[]]$ClaudeArguments
)

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'

function ConvertTo-YamlPath {
    param([Parameter(Mandatory)][string]$Path)
    return $Path.Replace('\', '\\')
}

function Set-JsonProperty {
    param(
        [Parameter(Mandatory)]$Object,
        [Parameter(Mandatory)][string]$Name,
        [Parameter(Mandatory)]$Value
    )
    if ($null -ne $Object.PSObject.Properties[$Name]) {
        $Object.$Name = $Value
    } else {
        $Object | Add-Member -MemberType NoteProperty -Name $Name -Value $Value
    }
}

function Test-NativeServiceReady {
    param(
        [Parameter(Mandatory)][string]$CaBundle,
        [Parameter(Mandatory)][string]$ServiceURL
    )
    if (-not (Test-Path -LiteralPath $CaBundle)) { return $false }
    try {
        $response = & curl.exe --silent --show-error --fail --max-time 2 --ssl-no-revoke --cacert $CaBundle "$ServiceURL/readyz" 2>$null
        return $LASTEXITCODE -eq 0 -and ($response | ConvertFrom-Json).status -eq 'ready'
    } catch {
        return $false
    }
}

function Invoke-NativeHookVerification {
    param(
        [Parameter(Mandatory)][string]$EdgeBinary,
        [Parameter(Mandatory)][string]$EdgeConfig
    )
    $sessionID = 'native-local-' + [Guid]::NewGuid().ToString('N')
    $sessionStart = @{
        hook_event_name = 'SessionStart'
        session_id = $sessionID
        cwd = $PSScriptRoot
        source = 'startup'
    } | ConvertTo-Json -Compress
    $sessionStart | & $EdgeBinary --config $EdgeConfig | Out-Null

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
            tool_use_id = 'native-' + [Guid]::NewGuid().ToString('N')
            cwd = $PSScriptRoot
            tool_name = 'Bash'
            tool_input = @{ command = $case.Command }
        }
        $result = ($inputObject | ConvertTo-Json -Compress -Depth 8) | & $EdgeBinary --config $EdgeConfig | ConvertFrom-Json
        $actual = $result.hookSpecificOutput.permissionDecision
        if ($actual -ne $case.Expected) {
            $reason = $result.hookSpecificOutput.permissionDecisionReason
            throw "Native hook verification failed for $($case.Label): expected $($case.Expected), got $actual ($reason)."
        }
        Write-Host "PASS: $($case.Command) -> $actual"
    }
}

$edgeBinary = Join-Path $PSScriptRoot 'dist\bap-edge-windows-amd64.exe'
$serviceBinary = Join-Path $PSScriptRoot 'dist\bap-service-windows-amd64.exe'
if ($Rebuild -or -not (Test-Path -LiteralPath $edgeBinary) -or -not (Test-Path -LiteralPath $serviceBinary)) {
    Write-Host 'Building native Windows BAP Edge and BAP Service executables...'
    & (Join-Path $PSScriptRoot 'Build-Bap.ps1') -Runtime Native
}
foreach ($binary in @($edgeBinary, $serviceBinary)) {
    if (-not (Test-Path -LiteralPath $binary)) { throw "Required native executable is missing: $binary" }
}

$runtimeDirectory = Join-Path $PSScriptRoot '.bap\native-local'
$serviceState = Join-Path $runtimeDirectory 'service-state'
$edgeState = Join-Path $runtimeDirectory 'edge-state'
$edgeConfig = Join-Path $runtimeDirectory 'bap-edge.yaml'
$apiKeyPath = Join-Path $runtimeDirectory 'edge-api-key.txt'
$stdoutLog = Join-Path $runtimeDirectory 'bap-service.stdout.log'
$stderrLog = Join-Path $runtimeDirectory 'bap-service.stderr.log'
New-Item -ItemType Directory -Force -Path $runtimeDirectory, $serviceState, $edgeState | Out-Null

if (-not (Test-Path -LiteralPath $apiKeyPath)) {
    $random = New-Object byte[] 32
    $generator = [Security.Cryptography.RandomNumberGenerator]::Create()
    try { $generator.GetBytes($random) } finally { $generator.Dispose() }
    [Convert]::ToBase64String($random) | Set-Content -LiteralPath $apiKeyPath -Encoding ascii -NoNewline
}
$env:BAP_EDGE_API_KEY = (Get-Content -LiteralPath $apiKeyPath -Raw).Trim()
$env:BAP_EDGE_PRINCIPAL = 'native-local-developer'
$env:BAP_STATE_DIRECTORY = $serviceState
$env:BAP_POLICY_PATH = Join-Path $PSScriptRoot 'bap-service\policies\agent-tools.cedar'
$env:BAP_BUNDLE_SOURCE_PATH = Join-Path $PSScriptRoot 'bap-service\policies\edge-policy-source.json'
$env:BAP_LISTEN_ADDRESS = "127.0.0.1:$Port"
$env:BAP_DEVELOPMENT_TLS = 'true'
Remove-Item Env:BAP_DATABASE_DSN -ErrorAction SilentlyContinue
Remove-Item Env:BAP_DATABASE_DSN_FILE -ErrorAction SilentlyContinue

$caBundle = Join-Path $serviceState 'dev-ca.pem'
$bundlePublicKey = Join-Path $serviceState 'bundle-public.pem'
$serviceURL = "https://127.0.0.1:$Port"
if (-not (Test-Path -LiteralPath $caBundle) -or -not (Test-Path -LiteralPath $bundlePublicKey)) {
    Write-Host 'Initializing native local TLS and signing keys...'
    & $serviceBinary initialize-certificates
    if ($LASTEXITCODE -ne 0) { throw 'BAP Service certificate initialization failed.' }
}

@"
service_url: "$serviceURL"
bundle_public_key_path: "$(ConvertTo-YamlPath $bundlePublicKey)"
ca_bundle_path: "$(ConvertTo-YamlPath $caBundle)"
subject_id: "claude-code-local"
timeout_ms: 3000
state_directory: "$(ConvertTo-YamlPath $edgeState)"
api_key_env: "BAP_EDGE_API_KEY"
"@ | Set-Content -LiteralPath $edgeConfig -Encoding utf8

$serviceProcess = $null
$settingsPath = Join-Path $PSScriptRoot '.claude\settings.local.json'
$settingsDirectory = Split-Path -Parent $settingsPath
$settingsExisted = Test-Path -LiteralPath $settingsPath
$originalSettings = if ($settingsExisted) { [IO.File]::ReadAllBytes($settingsPath) } else { $null }
$settingsWritten = $false
$claudeExitCode = 0
try {
    if (-not (Test-NativeServiceReady -CaBundle $caBundle -ServiceURL $serviceURL)) {
        Write-Host "Starting native BAP Service on $serviceURL ..."
        $serviceProcess = Start-Process -FilePath $serviceBinary -WorkingDirectory $PSScriptRoot -WindowStyle Hidden -PassThru `
            -RedirectStandardOutput $stdoutLog -RedirectStandardError $stderrLog
        for ($attempt = 1; $attempt -le 40; $attempt++) {
            if ($serviceProcess.HasExited) {
                $tail = if (Test-Path -LiteralPath $stderrLog) { (Get-Content -LiteralPath $stderrLog -Tail 20) -join "`n" } else { '' }
                throw "BAP Service exited before readiness. $tail"
            }
            if (Test-NativeServiceReady -CaBundle $caBundle -ServiceURL $serviceURL) { break }
            Start-Sleep -Milliseconds 250
        }
        if (-not (Test-NativeServiceReady -CaBundle $caBundle -ServiceURL $serviceURL)) { throw 'BAP Service did not become ready within 10 seconds.' }
    } else {
        Write-Host "Using the BAP Service already listening on $serviceURL."
    }

    Invoke-NativeHookVerification -EdgeBinary $edgeBinary -EdgeConfig $edgeConfig

    New-Item -ItemType Directory -Force -Path $settingsDirectory | Out-Null
    $settings = if ($settingsExisted) { Get-Content -LiteralPath $settingsPath -Raw | ConvertFrom-Json } else {
        [pscustomobject]@{ '$schema' = 'https://json.schemastore.org/claude-code-settings.json' }
    }
    if ($null -eq $settings.PSObject.Properties['hooks']) {
        Set-JsonProperty -Object $settings -Name 'hooks' -Value ([pscustomobject]@{})
    }
    $hookCommand = '"' + $edgeBinary.Replace('\', '/') + '" --config "' + $edgeConfig.Replace('\', '/') + '"'
    $hookHandler = [pscustomobject]@{ type = 'command'; command = $hookCommand; timeout = 10 }
    foreach ($event in @('SessionStart', 'PreToolUse', 'PostToolUse', 'PostToolUseFailure', 'UserPromptSubmit', 'SessionEnd')) {
        $matcher = if ($event -eq 'SessionStart') { 'startup|resume|clear|compact' } else { '*' }
        $group = [pscustomobject]@{ matcher = $matcher; hooks = @($hookHandler) }
        $groups = @()
        if ($null -ne $settings.hooks.PSObject.Properties[$event]) {
            foreach ($existingGroup in @($settings.hooks.$event)) {
                $isBapGroup = $false
                $existingHooks = if ($null -ne $existingGroup.PSObject.Properties['hooks']) { @($existingGroup.hooks) } else { @() }
                foreach ($existingHandler in $existingHooks) {
                    $commandProperty = $existingHandler.PSObject.Properties['command']
                    if ($null -ne $commandProperty -and $commandProperty.Value -eq $hookCommand) { $isBapGroup = $true }
                }
                if (-not $isBapGroup) { $groups += $existingGroup }
            }
        }
        $groups += $group
        Set-JsonProperty -Object $settings.hooks -Name $event -Value @($groups)
    }
    $settings | ConvertTo-Json -Depth 12 | Set-Content -LiteralPath $settingsPath -Encoding utf8
    $settingsWritten = $true

    if ($VerifyOnly) {
        Write-Host 'PASS: native BAP Service, signed policy synchronization, Edge allow/deny verification, and local hook settings merge.'
        return
    }

    Write-Host "Local hooks written to $settingsPath"
    Write-Host 'Launching Claude. Run /hooks and confirm six hooks with source Local.'
    & claude @ClaudeArguments
    $claudeExitCode = $LASTEXITCODE
} finally {
    if ($settingsWritten) {
        if ($settingsExisted) {
            [IO.File]::WriteAllBytes($settingsPath, $originalSettings)
        } elseif (Test-Path -LiteralPath $settingsPath) {
            [IO.File]::Delete($settingsPath)
        }
    }
    if ($serviceProcess -and -not $serviceProcess.HasExited) {
        Stop-Process -Id $serviceProcess.Id -Force
        $serviceProcess.WaitForExit()
    }
}

if ($claudeExitCode -ne 0) { exit $claudeExitCode }
