param(
    [switch]$Rebuild,
    [switch]$VerifyOnly,
    [switch]$UseCompanyClaude,
    [switch]$InteractiveClaude,
    [switch]$CompanyCliArguments,
    [ValidateRange(1, 65535)][int]$Port = 8443,
    [string]$Model = '',
    [string]$Tools = 'Bash',
    [string]$SystemPrompt = 'You are a Windows command agent using Git Bash. Copy exact commands from the user verbatim into the requested tool. Never substitute example paths or simulate results. Never claim a tool succeeded when it was blocked or denied; explicitly report the denial. After receiving a tool result, answer only from that result.',
    [string]$InputFile = '',
    [switch]$SequentialPrompts,
    [string]$SequentialSessionID = '',
    [Alias('p')][switch]$Print,
    [string]$Prompt = '',
    [Parameter(ValueFromRemainingArguments = $true)][string[]]$ClaudeArguments
)

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'
if ($InteractiveClaude -and $CompanyCliArguments) { throw '-InteractiveClaude and -CompanyCliArguments are mutually exclusive.' }
if (($InteractiveClaude -or $CompanyCliArguments) -and -not $UseCompanyClaude) { throw '-InteractiveClaude and -CompanyCliArguments require -UseCompanyClaude.' }

$managedSettingsPath = Join-Path $env:ProgramFiles 'ClaudeCode\managed-settings.d\50-bap-edge.json'
if (-not $VerifyOnly -and (Test-Path -LiteralPath $managedSettingsPath)) {
    [Console]::WriteLine("BAP launch stopped: managed hooks are installed at $managedSettingsPath.")
    [Console]::WriteLine("Claude would ignore this launcher's project-local hooks because allowManagedHooksOnly is enabled.")
    if ($UseCompanyClaude) {
        [Console]::WriteLine('Use the company-managed BAP Service and launch the company-authenticated Claude Code normally.')
    } else {
        [Console]::WriteLine('For local-model testing run: .\start-local-claude.bat -Runtime Docker -Model qwen-1.5b-local')
    }
    [Console]::WriteLine("See README.md 'Build/test commands'.")
    exit 2
}

function Get-ClaudeExecutablePath {
    # Prefer the company wrapper when both it and an underlying executable are
    # present. Interactive company capture intentionally invokes it with no args.
    foreach ($name in @('claude.cmd', 'claude.exe', 'claude')) {
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
        [Parameter(Mandatory)][string]$EdgeConfig,
        [Parameter(Mandatory)][string]$ServiceURL,
        [Parameter(Mandatory)][string]$CaBundle,
        [Parameter(Mandatory)][string]$RuntimeDirectory,
        [Parameter(Mandatory)][string]$STSGatewayAPIKey
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
        @{ Label = 'unclassified command'; Command = 'python -c "print(1)"'; Expected = 'deny' },
        @{ Label = 'unapproved shell route to readable file'; Command = 'wc -l data/dummy_customers.csv'; Expected = 'deny' },
        @{ Label = 'manual-only privileged client'; Command = 'mysql -h orders-prod -u dba'; Expected = 'deny'; ReasonPattern = 'REQUIRES MANUAL EXECUTION' }
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
        if ($case.ContainsKey('ReasonPattern') -and $result.hookSpecificOutput.permissionDecisionReason -notmatch $case.ReasonPattern) {
            throw "Native hook verification failed for $($case.Label): expected reason matching $($case.ReasonPattern)."
        }
        Write-Host "PASS: $($case.Command) -> $actual"
    }

    $readInput = @{
        hook_event_name = 'PreToolUse'
        session_id = $sessionID
        tool_use_id = 'native-read-' + [Guid]::NewGuid().ToString('N')
        cwd = $PSScriptRoot
        tool_name = 'Read'
        tool_input = @{ file_path = 'data/dummy_customers.csv' }
    }
    $readResult = (($readInput | ConvertTo-Json -Compress -Depth 8) | & $EdgeBinary --config $EdgeConfig | ConvertFrom-Json)
    if ($readResult.hookSpecificOutput.permissionDecision -ne 'allow') {
        throw "Native route verification failed: direct Read was expected to be allowed after the separate Bash denial. $($readResult.hookSpecificOutput.permissionDecisionReason)"
    }
    Write-Host 'PASS: separate Read data/dummy_customers.csv -> allow (proves the current tool-route behavior)'

    $promptCases = @(
        @{ Label = 'privileged database intent'; Prompt = 'Please connect to the MySQL orders database and reindex it'; ExpectedAdvisory = $true },
        @{ Label = 'ordinary explanation'; Prompt = 'Explain how database indexes work'; ExpectedAdvisory = $false }
    )
    foreach ($case in $promptCases) {
        $inputObject = @{
            hook_event_name = 'UserPromptSubmit'
            session_id = $sessionID
            cwd = $PSScriptRoot
            prompt = $case.Prompt
        }
        $result = ($inputObject | ConvertTo-Json -Compress -Depth 8) | & $EdgeBinary --config $EdgeConfig | ConvertFrom-Json
        $hookOutputProperty = $result.PSObject.Properties['hookSpecificOutput']
        $hookOutput = if ($null -ne $hookOutputProperty) { $hookOutputProperty.Value } else { $null }
        $hasAdvisory = $null -ne $hookOutput -and `
            $hookOutput.hookEventName -eq 'UserPromptSubmit' -and `
            $hookOutput.additionalContext -match 'manual-only'
        if ($hasAdvisory -ne $case.ExpectedAdvisory) {
            throw "Native prompt verification failed for $($case.Label): expected advisory=$($case.ExpectedAdvisory), got advisory=$hasAdvisory."
        }
        if ($hasAdvisory -and $null -ne $hookOutput.PSObject.Properties['permissionDecision']) {
            throw "Native prompt verification failed for $($case.Label): an advisory must not authorize or deny."
        }
        Write-Host "PASS: prompt $($case.Label) -> $(if ($hasAdvisory) { 'manual-only advisory' } else { 'no advisory' })"
    }

    $grantPrompt = @{
        hook_event_name = 'UserPromptSubmit'
        session_id = $sessionID
        cwd = $PSScriptRoot
        prompt = 'Deploy release 2026.08 of orders to staging'
    }
    $grantPromptResult = (($grantPrompt | ConvertTo-Json -Compress -Depth 8) | & $EdgeBinary --config $EdgeConfig | ConvertFrom-Json)
    if ($grantPromptResult.hookSpecificOutput.additionalContext -notmatch 'AgentGrant intent') {
        throw 'Native AgentGrant verification failed: signed prompt intent did not match.'
    }

    $gatewayCall = @{
        hook_event_name = 'PreToolUse'
        session_id = $sessionID
        tool_use_id = 'native-grant-' + [Guid]::NewGuid().ToString('N')
        cwd = $PSScriptRoot
        tool_name = 'mcp__bap_gateway__execute'
        tool_input = @{
            method = 'POST'
            url = 'https://api.staging.company.example/orders/deploy'
            body = @{ release = '2026.08' }
        }
    }
    $grantResult = (($gatewayCall | ConvertTo-Json -Compress -Depth 12) | & $EdgeBinary --config $EdgeConfig | ConvertFrom-Json)
    if ($grantResult.hookSpecificOutput.permissionDecision -ne 'allow' -or
        -not $grantResult.hookSpecificOutput.updatedInput._bap_agent_grant -or
        -not $grantResult.hookSpecificOutput.updatedInput._bap_operation) {
        throw 'Native AgentGrant verification failed: STS grant was not verified and injected into trusted tool input.'
    }
    if ($grantResult.hookSpecificOutput.permissionDecisionReason -match 'eyJ') {
        throw 'Native AgentGrant verification failed: opaque token leaked into the user-facing decision reason.'
    }
    Write-Host 'PASS: signed prompt intent -> exact gateway operation -> Agent STS -> trusted one-use grant injection'

    # Keep the bearer grant out of console output and process arguments. curl
    # reads it from a current-run file that is removed immediately after the
    # live consume/replay check.
    $transactionID = [Guid]::NewGuid().ToString('N')
    $consumeRequestPath = Join-Path $RuntimeDirectory "agent-grant-consume-$transactionID.json"
    $consumeResponsePath = Join-Path $RuntimeDirectory "agent-grant-response-$transactionID.json"
    try {
        $consumeRequest = @{
            agent_grant = $grantResult.hookSpecificOutput.updatedInput._bap_agent_grant
            resource = 'https://api.staging.company.example/orders/deploy'
            operation = $grantResult.hookSpecificOutput.updatedInput._bap_operation
        } | ConvertTo-Json -Compress -Depth 20
        [IO.File]::WriteAllText($consumeRequestPath, $consumeRequest)

        $curlArguments = @(
            '--silent', '--show-error', '--output', $consumeResponsePath,
            '--write-out', '%{http_code}', '--ssl-no-revoke', '--cacert', $CaBundle,
            '--request', 'POST', '--header', "Authorization: Bearer $STSGatewayAPIKey",
            '--header', 'Content-Type: application/json', '--data-binary', "@$consumeRequestPath",
            "$ServiceURL/bap/v1/agent-sts/consume"
        )
        $consumeStatus = (& curl.exe @curlArguments | Out-String).Trim()
        if ($LASTEXITCODE -ne 0 -or $consumeStatus -ne '200') {
            throw "Native AgentGrant verification failed: first live consume returned HTTP $consumeStatus."
        }
        $consumeResponse = Get-Content -LiteralPath $consumeResponsePath -Raw | ConvertFrom-Json
        if (-not $consumeResponse.consumed -or -not $consumeResponse.grant_id) {
            throw 'Native AgentGrant verification failed: live STS did not confirm consumption.'
        }
        Write-Host 'PASS: live Agent STS consume -> HTTP 200'

        $replayStatus = (& curl.exe @curlArguments | Out-String).Trim()
        if ($LASTEXITCODE -ne 0 -or $replayStatus -ne '403') {
            throw "Native AgentGrant verification failed: replay returned HTTP $replayStatus instead of 403."
        }
        Write-Host 'PASS: exact AgentGrant replay -> HTTP 403'

        $auditPath = Join-Path (Join-Path $RuntimeDirectory 'service-state') 'audit.jsonl'
        $auditEvents = @()
        if (Test-Path -LiteralPath $auditPath) {
            $auditEvents = @(Get-Content -LiteralPath $auditPath | ForEach-Object { $_ | ConvertFrom-Json })
        }
        if (-not ($auditEvents | Where-Object { $_.reason_code -eq 'AGENT_GRANT_ISSUED' }) -or
            -not ($auditEvents | Where-Object { $_.reason_code -eq 'AGENT_GRANT_CONSUMED' })) {
            throw 'Native AgentGrant verification failed: signed issue/consume audit events were not recorded.'
        }
        Write-Host 'PASS: signed audit -> AGENT_GRANT_ISSUED + AGENT_GRANT_CONSUMED'
    } finally {
        foreach ($sensitivePath in @($consumeRequestPath, $consumeResponsePath)) {
            if (Test-Path -LiteralPath $sensitivePath) { [IO.File]::Delete($sensitivePath) }
        }
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

$runtimeRoot = Join-Path $PSScriptRoot '.bap\native-local'
$runID = (Get-Date).ToUniversalTime().ToString('yyyyMMddTHHmmssfffZ') + "-$PID-" + [Guid]::NewGuid().ToString('N').Substring(0, 8)
$runtimeDirectory = Join-Path (Join-Path $runtimeRoot 'runs') $runID
$serviceState = Join-Path $runtimeDirectory 'service-state'
$edgeState = Join-Path $runtimeDirectory 'edge-state'
$edgeConfig = Join-Path $runtimeDirectory 'bap-edge.yaml'
$apiKeyPath = Join-Path $runtimeDirectory 'edge-api-key.txt'
$stdoutLog = Join-Path $runtimeDirectory 'bap-service.stdout.log'
$stderrLog = Join-Path $runtimeDirectory 'bap-service.stderr.log'
New-Item -ItemType Directory -Force -Path $runtimeRoot, $runtimeDirectory, $serviceState, $edgeState | Out-Null
$runtimeDirectory | Set-Content -LiteralPath (Join-Path $runtimeRoot 'latest-run.txt') -Encoding utf8
Write-Host "Native test run state: $runtimeDirectory"

function New-NativeLocalSecret {
    $random = New-Object byte[] 32
    $generator = [Security.Cryptography.RandomNumberGenerator]::Create()
    try { $generator.GetBytes($random) } finally { $generator.Dispose() }
    return [Convert]::ToBase64String($random)
}

if (-not (Test-Path -LiteralPath $apiKeyPath)) {
    New-NativeLocalSecret | Set-Content -LiteralPath $apiKeyPath -Encoding ascii -NoNewline
}
$env:BAP_EDGE_API_KEY = (Get-Content -LiteralPath $apiKeyPath -Raw).Trim()
$env:BAP_EDGE_PRINCIPAL = 'native-local-developer'
$env:BAP_AGENT_STS_EDGE_API_KEY = New-NativeLocalSecret
$stsGatewayAPIKey = New-NativeLocalSecret
$env:BAP_AGENT_STS_GATEWAY_API_KEY = $stsGatewayAPIKey
$env:BAP_AGENT_STS_EDGE_PRINCIPAL = 'native-local-edge'
$env:BAP_AGENT_STS_GATEWAY_PRINCIPAL = 'native-local-resource-pep'
$env:BAP_AGENT_STS_ISSUER = 'bap-agent-sts-local'
# Do not let machine/user-level production consumer configuration leak into
# this isolated development transaction.
Remove-Item Env:BAP_AGENT_STS_CONSUMERS_JSON -ErrorAction SilentlyContinue
$env:BAP_STATE_DIRECTORY = $serviceState
$env:BAP_POLICY_PATH = Join-Path $PSScriptRoot 'bap-service\policies\agent-tools.cedar'
$env:BAP_BUNDLE_SOURCE_PATH = Join-Path $PSScriptRoot 'bap-service\policies\edge-policy-source.json'
$env:BAP_LISTEN_ADDRESS = "127.0.0.1:$Port"
$env:BAP_DEVELOPMENT_TLS = 'true'
Remove-Item Env:BAP_DATABASE_DSN -ErrorAction SilentlyContinue
Remove-Item Env:BAP_DATABASE_DSN_FILE -ErrorAction SilentlyContinue

$caBundle = Join-Path $serviceState 'dev-ca.pem'
$bundlePublicKey = Join-Path $serviceState 'bundle-public.pem'
$grantPublicKey = Join-Path $serviceState 'grant-public.pem'
$serviceURL = "https://127.0.0.1:$Port"
if (-not (Test-Path -LiteralPath $caBundle) -or -not (Test-Path -LiteralPath $bundlePublicKey) -or -not (Test-Path -LiteralPath $grantPublicKey)) {
    Write-Host 'Initializing native local TLS and signing keys...'
    & $serviceBinary initialize-certificates
    if ($LASTEXITCODE -ne 0) { throw 'BAP Service certificate initialization failed.' }
}

@"
service_url: "$serviceURL"
agent_sts_url: "$serviceURL"
agent_sts_issuer: "bap-agent-sts-local"
public_key_path: "$(ConvertTo-YamlPath $grantPublicKey)"
bundle_public_key_path: "$(ConvertTo-YamlPath $bundlePublicKey)"
ca_bundle_path: "$(ConvertTo-YamlPath $caBundle)"
subject_id: "claude-code-local"
timeout_ms: 3000
state_directory: "$(ConvertTo-YamlPath $edgeState)"
api_key_env: "BAP_EDGE_API_KEY"
agent_sts_api_key_env: "BAP_AGENT_STS_EDGE_API_KEY"
"@ | Set-Content -LiteralPath $edgeConfig -Encoding utf8

$serviceProcess = $null
$settingsPath = Join-Path $PSScriptRoot '.claude\settings.local.json'
$settingsDirectory = Split-Path -Parent $settingsPath
$settingsExisted = Test-Path -LiteralPath $settingsPath
$originalSettings = if ($settingsExisted) { [IO.File]::ReadAllBytes($settingsPath) } else { $null }
$settingsWritten = $false
$claudeExitCode = 0
try {
    if (Test-NativeServiceReady -CaBundle $caBundle -ServiceURL $serviceURL) {
        throw "Another BAP Service is already listening on $serviceURL. Stop it or choose another -Port; this isolated run will not reuse credentials or state."
    }
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

    try {
        Invoke-NativeHookVerification -EdgeBinary $edgeBinary -EdgeConfig $edgeConfig `
            -ServiceURL $serviceURL -CaBundle $caBundle -RuntimeDirectory $runtimeDirectory `
            -STSGatewayAPIKey $stsGatewayAPIKey
    } finally {
        # The resource-PEP consume credential is required by the Service and
        # this verification transaction only. Never expose it to Claude or its
        # hook/tool child processes.
        Remove-Item Env:BAP_AGENT_STS_GATEWAY_API_KEY -ErrorAction SilentlyContinue
        $stsGatewayAPIKey = $null
    }

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
                    if ($null -ne $commandProperty -and ($commandProperty.Value -eq $hookCommand -or ($commandProperty.Value -match '(?i)bap-edge[^\r\n]*--config'))) { $isBapGroup = $true }
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
        Write-Host 'PASS: native BAP Service, signed policy synchronization, Edge policy, AgentGrant issue/consume/replay, audit, and local hook settings merge.'
        return
    }

    Write-Host "Local hooks written to $settingsPath"
    Write-Host 'Launching Claude. Run /hooks and confirm six hooks with source Local.'

    $claudeExecutable = Get-ClaudeExecutablePath
    if (-not $claudeExecutable) {
        throw 'Claude Code was not found on PATH or at %USERPROFILE%\.local\bin\claude.exe'
    }

    $effectiveModel = $Model
    if (-not $UseCompanyClaude) {
        try {
            $bridge = Invoke-RestMethod 'http://127.0.0.1:8080/health' -TimeoutSec 5
            if (-not $bridge.status) { throw 'ccbridge returned an unhealthy response.' }
        } catch {
            throw 'The local model bridge is not ready at http://127.0.0.1:8080/health. Run start-ccbridge.bat in a separate window first, or use -UseCompanyClaude.'
        }
        $env:ANTHROPIC_BASE_URL = 'http://127.0.0.1:8080'
        $env:ANTHROPIC_API_KEY = 'local-demo-key'
        if (-not $effectiveModel) { $effectiveModel = 'claude-3-5-sonnet-20241022' }
        Write-Host "Claude provider: local model bridge at $env:ANTHROPIC_BASE_URL"
    } else {
        # Do not inherit local bridge overrides into the company-authenticated
        # Claude session. Claude Code uses its normal company login/config.
        Remove-Item Env:ANTHROPIC_BASE_URL -ErrorAction SilentlyContinue
        $processAnthropicKey = [Environment]::GetEnvironmentVariable('ANTHROPIC_API_KEY', 'Process')
        if ($processAnthropicKey -eq 'local-demo-key') {
            Remove-Item Env:ANTHROPIC_API_KEY -ErrorAction SilentlyContinue
        }
        Write-Host 'Claude provider: company Claude Code authentication'
    }
    $env:CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC = '1'

    $launchCompanyInteractive = $UseCompanyClaude -and (-not $CompanyCliArguments)
    if ($InteractiveClaude -or $launchCompanyInteractive) {
        Write-Host 'Launching the company Claude UI without command-line arguments.'
        & $claudeExecutable
    } else {
        $launchArguments = @('--tools', $Tools, '--system-prompt', $SystemPrompt)
        if ($effectiveModel) { $launchArguments = @('--model', $effectiveModel) + $launchArguments }
        if ($Print) { $launchArguments += '--print' }
        if ($Prompt) { $launchArguments += $Prompt }
        if ($SequentialPrompts) {
            if (-not $InputFile -or -not (Test-Path -LiteralPath $InputFile -PathType Leaf)) { throw '-SequentialPrompts requires an existing -InputFile JSONL file.' }
            if (-not $SequentialSessionID) { $SequentialSessionID = [Guid]::NewGuid().ToString() }
            $turn = 0
            foreach ($line in Get-Content -LiteralPath $InputFile) {
                if (-not $line.Trim()) { continue }
                $turn++
                $message = $line | ConvertFrom-Json
                $promptText = [string]$message.message.content[0].text
                if (-not $promptText) { throw "Sequential prompt line $turn has no text content." }
                Write-Host "BAP SESSION ACCRETION TURN $turn"
                $turnArguments = @($launchArguments) + @('--output-format', 'stream-json', '--verbose', '--include-hook-events')
                if ($turn -eq 1) { $turnArguments += @('--session-id', $SequentialSessionID) } else { $turnArguments += @('--resume', $SequentialSessionID) }
                $turnArguments += $promptText
                & $claudeExecutable @turnArguments
                if ($LASTEXITCODE -ne 0) { throw "Claude session accretion turn $turn failed with exit code $LASTEXITCODE." }
            }
        } elseif ($InputFile) {
            if (-not (Test-Path -LiteralPath $InputFile -PathType Leaf)) { throw "Claude stream input file does not exist: $InputFile" }
            Get-Content -LiteralPath $InputFile -Raw | & $claudeExecutable @launchArguments @ClaudeArguments
        } else {
            & $claudeExecutable @launchArguments @ClaudeArguments
        }
    }
    $claudeExitCode = $LASTEXITCODE
} finally {
    Remove-Item Env:BAP_AGENT_STS_GATEWAY_API_KEY -ErrorAction SilentlyContinue
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
    Write-Host "Native test run state retained at $runtimeDirectory"
}

if ($claudeExitCode -ne 0) { exit $claudeExitCode }
