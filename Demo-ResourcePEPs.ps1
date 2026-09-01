param(
    [ValidateSet('Native', 'Docker', 'Podman')][string]$Runtime = 'Native',
    [switch]$Rebuild,
    [ValidateRange(1, 65535)][int]$ServicePort = 18443,
    [ValidateRange(1, 65535)][int]$APIPort = 19443,
    [ValidateRange(1, 65535)][int]$MCPPort = 18765,
    [ValidateRange(1, 65535)][int]$MockPort = 19090
)

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'
function New-Secret {
    $bytes = New-Object byte[] 32
    $generator = [Security.Cryptography.RandomNumberGenerator]::Create()
    try { $generator.GetBytes($bytes) } finally { $generator.Dispose() }
    return [Convert]::ToBase64String($bytes)
}
function ConvertTo-YamlPath([string]$Path) { return $Path.Replace('\', '\\') }
function Wait-HTTP([string]$URL, [string]$CA = '') {
    for ($attempt = 1; $attempt -le 60; $attempt++) {
        try {
            if ($CA) { & curl.exe --silent --fail --ssl-no-revoke --cacert $CA $URL 2>$null | Out-Null; if ($LASTEXITCODE -eq 0) { return } }
            else { Invoke-RestMethod $URL -TimeoutSec 1 | Out-Null; return }
        } catch {}
        Start-Sleep -Milliseconds 500
    }
    throw "Endpoint did not become ready: $URL"
}
function Invoke-Edge($Value, [string]$Binary, [string]$Config) {
    return (($Value | ConvertTo-Json -Compress -Depth 30) | & $Binary --config $Config | ConvertFrom-Json)
}

$edgeBinary = Join-Path $PSScriptRoot 'dist\bap-edge-windows-amd64.exe'
$serviceBinary = Join-Path $PSScriptRoot 'dist\bap-service-windows-amd64.exe'
$mockBinary = Join-Path $PSScriptRoot 'dist\bap-mock-resources-windows-amd64.exe'
if ($Rebuild -or -not (Test-Path -LiteralPath $edgeBinary) -or -not (Test-Path -LiteralPath $serviceBinary)) { & (Join-Path $PSScriptRoot 'Build-Bap.ps1') -Runtime Native }
if ($Rebuild -or -not (Test-Path -LiteralPath $mockBinary)) {
    . (Join-Path $PSScriptRoot 'scripts\GoToolchain.ps1')
    $goCommand = Get-BapGoCommand -Required
    & $goCommand build -mod=vendor -trimpath -o $mockBinary ./examples/protected-resources/cmd
    if ($LASTEXITCODE -ne 0) { throw 'Protected-resource demo build failed.' }
}
if ($Rebuild -or ($Runtime -eq 'Native' -and -not (Test-Path -LiteralPath (Join-Path $PSScriptRoot 'dist\bap-api-gateway-springcloud.jar')))) {
    & (Join-Path $PSScriptRoot 'Build-ResourcePEPs.ps1') -Runtime $Runtime
}

$runID = (Get-Date).ToUniversalTime().ToString('yyyyMMddTHHmmssfffZ') + "-$PID"
$runtimeDirectory = Join-Path $PSScriptRoot ".bap\resource-pep-demo\$runID"
$serviceState = Join-Path $runtimeDirectory 'service-state'
$edgeState = Join-Path $runtimeDirectory 'edge-state'
New-Item -ItemType Directory -Force -Path $serviceState, $edgeState | Out-Null
Write-Host "Resource PEP demo state: $runtimeDirectory"

$env:BAP_EDGE_API_KEY = New-Secret
$env:BAP_EDGE_PRINCIPAL = 'demo-edge-policy-client'
$env:BAP_AGENT_STS_EDGE_API_KEY = New-Secret
$env:BAP_AGENT_STS_EDGE_PRINCIPAL = 'demo-edge-agent-sts-client'
$env:BAP_AGENT_STS_GATEWAY_API_KEY = New-Secret
$env:BAP_AGENT_STS_GATEWAY_PRINCIPAL = 'demo-unused-gateway-client'
$env:BAP_API_PEP_STS_API_KEY = New-Secret
$env:BAP_MCP_PEP_STS_API_KEY = New-Secret
$env:BAP_ORDERS_BACKEND_API_KEY = New-Secret
$env:BAP_MCP_UPSTREAM_API_KEY = New-Secret
$env:BAP_AGENT_STS_CONSUMERS_JSON = @(
    @{ principal='demo-api-pep'; api_key_env='BAP_API_PEP_STS_API_KEY'; audiences=@('https://api.staging.company.example/orders/deploy') },
    @{ principal='demo-mcp-pep'; api_key_env='BAP_MCP_PEP_STS_API_KEY'; audiences=@('https://bap-mcp-pep.company.example/mcp') }
) | ConvertTo-Json -Compress
$env:BAP_STATE_DIRECTORY = $serviceState
$env:BAP_POLICY_PATH = Join-Path $PSScriptRoot 'bap-service\policies\agent-tools.cedar'
$env:BAP_BUNDLE_SOURCE_PATH = Join-Path $PSScriptRoot 'bap-service\policies\edge-policy-source.json'
$serviceListenHost = if ($Runtime -eq 'Native') { '127.0.0.1' } else { '0.0.0.0' }
$env:BAP_LISTEN_ADDRESS = "${serviceListenHost}:$ServicePort"
$env:BAP_DEVELOPMENT_TLS = 'true'
Remove-Item Env:BAP_DATABASE_DSN, Env:BAP_DATABASE_DSN_FILE -ErrorAction SilentlyContinue

$caBundle = Join-Path $serviceState 'dev-ca.pem'; $bundlePublicKey = Join-Path $serviceState 'bundle-public.pem'; $grantPublicKey = Join-Path $serviceState 'grant-public.pem'
& $serviceBinary initialize-certificates
if ($LASTEXITCODE -ne 0) { throw 'Demo certificate initialization failed.' }
$serviceURL = "https://127.0.0.1:$ServicePort"
$edgeConfig = Join-Path $runtimeDirectory 'bap-edge.yaml'
@"
service_url: "$serviceURL"
agent_sts_url: "$serviceURL"
public_key_path: "$(ConvertTo-YamlPath $grantPublicKey)"
bundle_public_key_path: "$(ConvertTo-YamlPath $bundlePublicKey)"
ca_bundle_path: "$(ConvertTo-YamlPath $caBundle)"
subject_id: "claude-code-local"
timeout_ms: 3000
state_directory: "$(ConvertTo-YamlPath $edgeState)"
api_key_env: "BAP_EDGE_API_KEY"
agent_sts_api_key_env: "BAP_AGENT_STS_EDGE_API_KEY"
"@ | Set-Content -LiteralPath $edgeConfig -Encoding utf8

$serviceProcess, $mockProcess = $null, $null
try {
    $serviceProcess = Start-Process -FilePath $serviceBinary -WorkingDirectory $PSScriptRoot -WindowStyle Hidden -PassThru -RedirectStandardOutput (Join-Path $runtimeDirectory 'service.log') -RedirectStandardError (Join-Path $runtimeDirectory 'service.err.log')
    Wait-HTTP "$serviceURL/readyz" $caBundle
    $mockProcess = Start-Process -FilePath $mockBinary -ArgumentList @('--listen', "127.0.0.1:$MockPort") -WorkingDirectory $PSScriptRoot -WindowStyle Hidden -PassThru -RedirectStandardOutput (Join-Path $runtimeDirectory 'mock.log') -RedirectStandardError (Join-Path $runtimeDirectory 'mock.err.log')
    Wait-HTTP "http://127.0.0.1:$MockPort/healthz"
    & (Join-Path $PSScriptRoot 'Start-ResourcePEPs.ps1') -Runtime $Runtime -NoBuild -AgentSTSURL $serviceURL -AgentSTSCAPath $caBundle -OrdersBackendURL "http://127.0.0.1:$MockPort" -MCPUpstreamURL "http://127.0.0.1:$MockPort/mcp" -APIPort $APIPort -MCPPort $MCPPort

    $sessionID = 'pep-demo-' + [Guid]::NewGuid().ToString('N')
    Invoke-Edge @{hook_event_name='SessionStart';session_id=$sessionID;cwd=$PSScriptRoot;source='startup'} $edgeBinary $edgeConfig | Out-Null

    Invoke-Edge @{hook_event_name='UserPromptSubmit';session_id=$sessionID;cwd=$PSScriptRoot;prompt='Deploy release 2026.08 of orders to staging'} $edgeBinary $edgeConfig | Out-Null
    $apiTool = Invoke-Edge @{hook_event_name='PreToolUse';session_id=$sessionID;tool_use_id='api-demo';cwd=$PSScriptRoot;tool_name='mcp__bap_gateway__execute';tool_input=@{method='POST';url='https://api.staging.company.example/orders/deploy';body=@{release='2026.08'}}} $edgeBinary $edgeConfig
    $apiInput = $apiTool.hookSpecificOutput.updatedInput
    if (-not $apiInput._bap_agent_grant) { throw 'API PEP demo did not receive an Edge-injected AgentGrant.' }
    $apiResult = Invoke-RestMethod "http://127.0.0.1:$APIPort/bap/v1/api/execute" -Method Post -ContentType 'application/json' -Body ($apiInput | ConvertTo-Json -Compress -Depth 30)
    if (-not $apiResult.deployed) { throw 'Protected Orders API did not execute.' }
    Write-Host 'PASS: Claude-style API tool -> Edge -> Agent STS -> Spring Cloud API PEP -> protected Orders API'
    try { Invoke-RestMethod "http://127.0.0.1:$APIPort/bap/v1/api/execute" -Method Post -ContentType 'application/json' -Body ($apiInput | ConvertTo-Json -Compress -Depth 30) | Out-Null; throw 'API AgentGrant replay was accepted.' } catch { if ($_.Exception.Message -eq 'API AgentGrant replay was accepted.') { throw } }
    Write-Host 'PASS: Spring Cloud API PEP rejected exact AgentGrant replay'

    Invoke-Edge @{hook_event_name='UserPromptSubmit';session_id=$sessionID;cwd=$PSScriptRoot;prompt='Create a change request for orders release 2026.08'} $edgeBinary $edgeConfig | Out-Null
    $mcpTool = Invoke-Edge @{hook_event_name='PreToolUse';session_id=$sessionID;tool_use_id='mcp-demo';cwd=$PSScriptRoot;tool_name='mcp__bap_mcp_pep__change_create';tool_input=@{service='orders';environment='staging';release='2026.08';summary='Orders staging release'}} $edgeBinary $edgeConfig
    $mcpInput = $mcpTool.hookSpecificOutput.updatedInput
    if (-not $mcpInput._bap_agent_grant) { throw 'MCP PEP demo did not receive an Edge-injected AgentGrant.' }
    $mcpRequest = @{jsonrpc='2.0';id=1;method='tools/call';params=@{name='change_create';arguments=$mcpInput}}
    $mcpResult = Invoke-RestMethod "http://127.0.0.1:$MCPPort/mcp" -Method Post -ContentType 'application/json' -Body ($mcpRequest | ConvertTo-Json -Compress -Depth 30)
    if (-not $mcpResult.result.structuredContent.created) { throw 'Protected upstream MCP tool did not execute.' }
    Write-Host 'PASS: Claude-style MCP call -> Edge -> Agent STS -> MCP PEP -> protected upstream MCP server'
    $mcpReplay = Invoke-RestMethod "http://127.0.0.1:$MCPPort/mcp" -Method Post -ContentType 'application/json' -Body ($mcpRequest | ConvertTo-Json -Compress -Depth 30)
    if (-not $mcpReplay.error) { throw 'MCP AgentGrant replay was accepted.' }
    Write-Host 'PASS: MCP PEP rejected exact AgentGrant replay'

    try { Invoke-RestMethod "http://127.0.0.1:$MockPort/orders/deploy" -Method Post -ContentType 'application/json' -Body '{"release":"direct"}' | Out-Null; throw 'Direct backend call was accepted.' } catch { if ($_.Exception.Message -eq 'Direct backend call was accepted.') { throw } }
    $state = Invoke-RestMethod "http://127.0.0.1:$MockPort/state"
    if ($state.api_calls -ne 1 -or $state.mcp_calls -ne 1) { throw "Protected resources executed unexpected counts: API=$($state.api_calls) MCP=$($state.mcp_calls)." }
    Write-Host 'PASS: protected resources accepted only PEP-owned identities and executed once each'
} finally {
    & (Join-Path $PSScriptRoot 'Stop-ResourcePEPs.ps1') -ErrorAction SilentlyContinue
    foreach ($process in @($mockProcess, $serviceProcess)) { if ($process -and -not $process.HasExited) { Stop-Process -Id $process.Id -Force; $process.WaitForExit() } }
    Write-Host "Resource PEP demo evidence retained at $runtimeDirectory"
}
