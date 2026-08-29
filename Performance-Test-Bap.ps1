param(
    [ValidateSet('Auto', 'Podman', 'Docker')][string]$Runtime = 'Auto',
    [int]$ServiceRequests = 200,
    [int]$ServiceConcurrency = 10,
    [int]$EdgeRequests = 20
)

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'
. (Join-Path $PSScriptRoot 'scripts\Runtime.ps1')
$engine = Get-BapContainerEngine -Runtime $Runtime
$runtimeDirectory = Get-BapRuntimeDirectory -Engine $engine
$caPath = Join-Path $runtimeDirectory 'dev-ca.pem'
Wait-BapHealth -CaBundle $caPath
$env:BAP_EDGE_API_KEY = (Get-Content -LiteralPath (Join-Path $runtimeDirectory 'edge-api-key.txt') -Raw).Trim()

$loadBinary = Join-Path $PSScriptRoot 'dist\bap-perftest-windows-amd64.exe'
$mount = "$($PSScriptRoot):/src"
& $engine run --rm --volume $mount --workdir /src --env CGO_ENABLED=0 --env GOOS=windows --env GOARCH=amd64 docker.io/library/golang:1.23-bookworm go build -mod=vendor -trimpath -o dist/bap-perftest-windows-amd64.exe ./cmd/bap-perftest
if ($LASTEXITCODE -ne 0) { throw 'Could not compile the performance client.' }

Write-Host "BAP Service load: $ServiceRequests requests at concurrency $ServiceConcurrency"
& $loadBinary -url https://127.0.0.1:8443 -ca $caPath -requests $ServiceRequests -concurrency $ServiceConcurrency
if ($LASTEXITCODE -ne 0) { throw 'BAP Service performance run had failures.' }

$edgeBinary = Join-Path $PSScriptRoot 'dist\bap-edge-windows-amd64.exe'
if (-not (Test-Path -LiteralPath $edgeBinary)) { & (Join-Path $PSScriptRoot 'Build-BapEdge.ps1') -Runtime $Runtime }
$edgeConfig = Join-Path $runtimeDirectory 'performance-edge.yaml'
$publicKey = (Join-Path $runtimeDirectory 'grant-public.pem').Replace('\', '\\')
$escapedCA = $caPath.Replace('\', '\\')
$state = (Join-Path $runtimeDirectory 'performance-edge-state').Replace('\', '\\')
@"
service_url: "https://127.0.0.1:8443"
public_key_path: "$publicKey"
ca_bundle_path: "$escapedCA"
subject_id: "claude-code-local"
api_key_env: "BAP_EDGE_API_KEY"
state_directory: "$state"
timeout_ms: 5000
"@ | Set-Content -LiteralPath $edgeConfig -Encoding utf8

Write-Host "Full BAP Edge cold authorization path: $EdgeRequests sequential hook processes"
$samples = New-Object System.Collections.Generic.List[double]
$session = 'edge-performance-' + [Guid]::NewGuid().ToString('N')
for ($index = 0; $index -lt $EdgeRequests; $index++) {
    $input = @{ hook_event_name = 'PreToolUse'; session_id = $session; tool_use_id = "perf-edge-$index"; cwd = $PSScriptRoot; tool_name = 'Read'; tool_input = @{ file_path = 'README.md' } } | ConvertTo-Json -Compress -Depth 8
    $watch = [Diagnostics.Stopwatch]::StartNew()
    $result = $input | & $edgeBinary --config $edgeConfig | ConvertFrom-Json
    $watch.Stop()
    if ($result.hookSpecificOutput.permissionDecision -ne 'allow') { throw "Edge performance operation $index was denied." }
    $samples.Add($watch.Elapsed.TotalMilliseconds)
}
$sorted = @($samples | Sort-Object)
$p50 = $sorted[[Math]::Min($sorted.Count - 1, [Math]::Floor($sorted.Count * 0.50))]
$p95 = $sorted[[Math]::Min($sorted.Count - 1, [Math]::Floor($sorted.Count * 0.95))]
[ordered]@{ requests = $EdgeRequests; failures = 0; p50_ms = [Math]::Round($p50, 2); p95_ms = [Math]::Round($p95, 2) } | ConvertTo-Json
Remove-Item -LiteralPath $edgeConfig -Force
Write-Host 'Performance run complete. Results describe this machine and durable JSONL configuration; they are not a universal production capacity claim.'
