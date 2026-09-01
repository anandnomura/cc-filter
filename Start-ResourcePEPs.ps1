param(
    [ValidateSet('Auto', 'Native', 'Docker', 'Podman')][string]$Runtime = 'Auto',
    [string]$AgentSTSURL = 'https://127.0.0.1:8443',
    [string]$AgentSTSCAPath = '',
    [string]$OrdersBackendURL = 'http://127.0.0.1:19090',
    [string]$MCPUpstreamURL = 'http://127.0.0.1:19090/mcp',
    [ValidateRange(1, 65535)][int]$APIPort = 9443,
    [ValidateRange(1, 65535)][int]$MCPPort = 8765,
    [switch]$NoBuild
)

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'
. (Join-Path $PSScriptRoot 'scripts\GoToolchain.ps1')
. (Join-Path $PSScriptRoot 'scripts\Runtime.ps1')
$stateDirectory = Join-Path $PSScriptRoot '.bap\resource-peps'
New-Item -ItemType Directory -Force -Path $stateDirectory | Out-Null
$statePath = Join-Path $stateDirectory 'running.json'
if (Test-Path -LiteralPath $statePath) { throw 'Resource PEP state already exists. Run .\Stop-ResourcePEPs.ps1 before starting another instance.' }
if (-not $env:BAP_API_PEP_STS_API_KEY -or -not $env:BAP_MCP_PEP_STS_API_KEY -or -not $env:BAP_ORDERS_BACKEND_API_KEY -or -not $env:BAP_MCP_UPSTREAM_API_KEY) {
    throw 'Set BAP_API_PEP_STS_API_KEY, BAP_MCP_PEP_STS_API_KEY, BAP_ORDERS_BACKEND_API_KEY, and BAP_MCP_UPSTREAM_API_KEY in the process environment.'
}
$goCommand = Get-BapGoCommand
$mavenCommand = Get-Command mvn -ErrorAction SilentlyContinue | Select-Object -First 1
if ($Runtime -eq 'Auto') {
    if ($goCommand -and $mavenCommand) { $Runtime = 'Native' } else { $Runtime = Get-BapContainerEngine -Runtime Auto }
}
if (-not $NoBuild) { & (Join-Path $PSScriptRoot 'Build-ResourcePEPs.ps1') -Runtime $Runtime }

function Convert-LoopbackForContainer([string]$Value) {
    return $Value.Replace('127.0.0.1', 'host.containers.internal').Replace('localhost', 'host.containers.internal')
}
function Write-MCPRuntimeConfig([string]$STSURL, [string]$UpstreamURL, [string]$CAPath, [string]$OutputPath, [string]$ListenAddress, [bool]$AllowDevelopmentHostGateway = $false) {
    $config = Get-Content -LiteralPath (Join-Path $PSScriptRoot 'bap-mcp-pep\mcp-pep.example.json') -Raw | ConvertFrom-Json
    $config.listen_address = $ListenAddress
    $config.agent_sts_url = $STSURL
    $config.agent_sts_ca_path = $CAPath
    $config.upstream_url = $UpstreamURL
    $config | Add-Member -NotePropertyName allow_development_cleartext_host_gateway -NotePropertyValue $AllowDevelopmentHostGateway -Force
    $json = $config | ConvertTo-Json -Depth 20
    [IO.File]::WriteAllText($OutputPath, $json, (New-Object Text.UTF8Encoding($false)))
}

if ($Runtime -eq 'Native') {
    $mcpConfig = Join-Path $stateDirectory 'mcp-pep.runtime.json'
    Write-MCPRuntimeConfig -STSURL $AgentSTSURL -UpstreamURL $MCPUpstreamURL -CAPath $AgentSTSCAPath -OutputPath $mcpConfig -ListenAddress "127.0.0.1:$MCPPort"
    $env:BAP_AGENT_STS_URL = $AgentSTSURL
    $env:BAP_AGENT_STS_CA_PATH = $AgentSTSCAPath
    $env:BAP_ORDERS_BACKEND_URL = $OrdersBackendURL
    $env:BAP_API_PEP_PORT = "$APIPort"
    $apiLog = Join-Path $stateDirectory 'api-gateway.log'
    $mcpLog = Join-Path $stateDirectory 'mcp-pep.log'
    $apiProcess = Start-Process -FilePath 'java.exe' -ArgumentList @('-jar', (Join-Path $PSScriptRoot 'dist\bap-api-gateway-springcloud.jar')) -WorkingDirectory $PSScriptRoot -WindowStyle Hidden -PassThru -RedirectStandardOutput $apiLog -RedirectStandardError (Join-Path $stateDirectory 'api-gateway.err.log')
    $mcpProcess = Start-Process -FilePath (Join-Path $PSScriptRoot 'dist\bap-mcp-pep-windows-amd64.exe') -ArgumentList @('--config', $mcpConfig) -WorkingDirectory $PSScriptRoot -WindowStyle Hidden -PassThru -RedirectStandardOutput $mcpLog -RedirectStandardError (Join-Path $stateDirectory 'mcp-pep.err.log')
    @{ runtime='Native'; api_pid=$apiProcess.Id; mcp_pid=$mcpProcess.Id } | ConvertTo-Json | Set-Content -LiteralPath $statePath -Encoding utf8
} else {
    $engine = Get-BapContainerEngine -Runtime $Runtime
    $containerSTS = Convert-LoopbackForContainer $AgentSTSURL
    $containerBackend = Convert-LoopbackForContainer $OrdersBackendURL
    $containerMCP = Convert-LoopbackForContainer $MCPUpstreamURL
    $mcpConfig = Join-Path $stateDirectory 'mcp-pep.container.json'
    $containerCA = if ($AgentSTSCAPath) { '/certs/sts-ca.pem' } else { '' }
    Write-MCPRuntimeConfig -STSURL $containerSTS -UpstreamURL $containerMCP -CAPath $containerCA -OutputPath $mcpConfig -ListenAddress '0.0.0.0:8765' -AllowDevelopmentHostGateway $true
    $commonCA = @(); if ($AgentSTSCAPath) { $resolvedCA = (Resolve-Path -LiteralPath $AgentSTSCAPath).Path; $commonCA = @('--volume', "${resolvedCA}:/certs/sts-ca.pem:ro") }
    & $engine run --detach --name bap-api-gateway-springcloud --publish "${APIPort}:9443" `
        --env BAP_API_PEP_STS_API_KEY --env BAP_ORDERS_BACKEND_API_KEY `
        --env "BAP_AGENT_STS_URL=$containerSTS" --env "BAP_AGENT_STS_CA_PATH=$containerCA" --env "BAP_ORDERS_BACKEND_URL=$containerBackend" `
        @commonCA bap-api-gateway-springcloud:local | Out-Null
    if ($LASTEXITCODE -ne 0) { throw 'Start Spring Cloud API Gateway PEP container failed.' }
    & $engine run --detach --name bap-mcp-pep --publish "${MCPPort}:8765" `
        --env BAP_MCP_PEP_STS_API_KEY --env BAP_MCP_UPSTREAM_API_KEY `
        --volume "${mcpConfig}:/app/mcp-pep.json:ro" @commonCA bap-mcp-pep:local --config /app/mcp-pep.json | Out-Null
    if ($LASTEXITCODE -ne 0) { & $engine rm --force bap-api-gateway-springcloud | Out-Null; throw 'Start BAP MCP PEP container failed.' }
    @{ runtime=$Runtime; engine=$engine; containers=@('bap-api-gateway-springcloud','bap-mcp-pep') } | ConvertTo-Json | Set-Content -LiteralPath $statePath -Encoding utf8
}

$apiReady, $mcpReady = $false, $false
for ($attempt = 1; $attempt -le 60; $attempt++) {
    try {
        $apiReady = (Invoke-RestMethod "http://127.0.0.1:$APIPort/actuator/health" -TimeoutSec 1).status -eq 'UP'
        $mcpReady = (Invoke-RestMethod "http://127.0.0.1:$MCPPort/healthz" -TimeoutSec 1).status -eq 'ok'
        if ($apiReady -and $mcpReady) { break }
    } catch {}
    Start-Sleep -Milliseconds 500
}
if (-not $apiReady -or -not $mcpReady) { & (Join-Path $PSScriptRoot 'Stop-ResourcePEPs.ps1'); throw 'Resource PEPs did not become ready within 30 seconds.' }
Write-Host "PASS: Spring Cloud API PEP ready at http://127.0.0.1:$APIPort"
Write-Host "PASS: protected MCP PEP ready at http://127.0.0.1:$MCPPort/mcp"
