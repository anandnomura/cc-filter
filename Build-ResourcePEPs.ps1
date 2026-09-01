param(
    [ValidateSet('Auto', 'Native', 'Docker', 'Podman')][string]$Runtime = 'Auto',
    [ValidateSet('Windows', 'Linux', 'All')][string]$Target = 'Windows',
    [ValidateSet('amd64', 'arm64')][string]$Architecture = 'amd64',
    [string]$MCPTag = 'bap-mcp-pep:local',
    [string]$APITag = 'bap-api-gateway-springcloud:local'
)

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'
. (Join-Path $PSScriptRoot 'scripts\GoToolchain.ps1')
. (Join-Path $PSScriptRoot 'scripts\Runtime.ps1')

$goCommand = Get-BapGoCommand
$mavenCommand = Get-Command mvn -ErrorAction SilentlyContinue | Select-Object -First 1
if ($Runtime -eq 'Auto') {
    if ($goCommand -and $mavenCommand) { $Runtime = 'Native' }
    else { $Runtime = Get-BapContainerEngine -Runtime Auto }
}

if ($Runtime -eq 'Native') {
    if (-not $goCommand) { $goCommand = Get-BapGoCommand -Required }
    if (-not $mavenCommand) { throw 'Native Spring Cloud build requires Maven on PATH and Java 21. Use -Runtime Docker or -Runtime Podman otherwise.' }
    New-Item -ItemType Directory -Force -Path (Join-Path $PSScriptRoot 'dist') | Out-Null
    Push-Location $PSScriptRoot
    $savedGOOS, $savedGOARCH, $savedCGO = $env:GOOS, $env:GOARCH, $env:CGO_ENABLED
    try {
        foreach ($osName in $(if ($Target -eq 'All') { @('windows', 'linux') } else { @($Target.ToLowerInvariant()) })) {
            $env:GOOS, $env:GOARCH, $env:CGO_ENABLED = $osName, $Architecture, '0'
            $suffix = if ($osName -eq 'windows') { '.exe' } else { '' }
            & $goCommand build -mod=vendor -trimpath -o "dist/bap-mcp-pep-$osName-$Architecture$suffix" ./bap-mcp-pep/cmd
            if ($LASTEXITCODE -ne 0) { throw "BAP MCP PEP $osName/$Architecture build failed." }
            if ($osName -eq 'windows') {
                & $goCommand build -mod=vendor -trimpath -o "dist/bap-mock-resources-windows-$Architecture.exe" ./examples/protected-resources/cmd
                if ($LASTEXITCODE -ne 0) { throw 'BAP protected-resource demo build failed.' }
            }
        }
        & $mavenCommand.Source --batch-mode -f (Join-Path $PSScriptRoot 'bap-api-gateway-springcloud\pom.xml') clean package
        if ($LASTEXITCODE -ne 0) { throw 'Spring Cloud API Gateway PEP build failed.' }
        $jar = Get-ChildItem -LiteralPath (Join-Path $PSScriptRoot 'bap-api-gateway-springcloud\target') -Filter 'bap-api-gateway-springcloud-*.jar' |
            Where-Object { $_.Name -notlike '*.original' } | Select-Object -First 1
        if (-not $jar) { throw 'Spring Cloud API Gateway PEP JAR was not produced.' }
        Copy-Item -LiteralPath $jar.FullName -Destination (Join-Path $PSScriptRoot 'dist\bap-api-gateway-springcloud.jar') -Force
    } finally {
        $env:GOOS, $env:GOARCH, $env:CGO_ENABLED = $savedGOOS, $savedGOARCH, $savedCGO
        Pop-Location
    }
    Write-Host 'PASS: native resource PEP artifacts built in .\dist (Go MCP PEP + Java Spring Cloud API Gateway PEP).'
    return
}

$engine = Get-BapContainerEngine -Runtime $Runtime
& $engine build --file (Join-Path $PSScriptRoot 'bap-mcp-pep\Containerfile') --tag $MCPTag $PSScriptRoot
if ($LASTEXITCODE -ne 0) { throw 'BAP MCP PEP OCI build failed.' }
& $engine build --file (Join-Path $PSScriptRoot 'bap-api-gateway-springcloud\Containerfile') --tag $APITag (Join-Path $PSScriptRoot 'bap-api-gateway-springcloud')
if ($LASTEXITCODE -ne 0) { throw 'Spring Cloud API Gateway PEP OCI build failed.' }
Write-Host "PASS: resource PEP OCI images built with $engine."
