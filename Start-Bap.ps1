param(
    [ValidateSet('Auto', 'Podman', 'Docker')][string]$Runtime = 'Auto',
    [string]$DatabaseDsn = '',
    [string]$DatabaseDsnFile = '',
    [string]$DatabaseCaPath = '',
    [string]$DatabaseTlsServerName = ''
)

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'
. (Join-Path $PSScriptRoot 'scripts\Runtime.ps1')

$engine = Get-BapContainerEngine -Runtime $Runtime
$runtimeDirectory = Get-BapRuntimeDirectory -Engine $engine
New-Item -ItemType Directory -Force -Path $runtimeDirectory | Out-Null

& $engine image inspect bap-service:local *> $null
if ($LASTEXITCODE -ne 0) {
    & (Join-Path $PSScriptRoot 'Build-Bap.ps1') -Runtime $Runtime
}

& (Join-Path $PSScriptRoot 'Initialize-Certificates.ps1') -Runtime $Runtime
$apiKey = (Get-Content -LiteralPath (Join-Path $runtimeDirectory 'edge-api-key.txt') -Raw).Trim()
if ($DatabaseDsn -and $DatabaseDsnFile) { throw 'Use either -DatabaseDsn or -DatabaseDsnFile, not both.' }
if ($DatabaseDsnFile -and -not (Test-Path -LiteralPath $DatabaseDsnFile)) { throw "MySQL DSN file does not exist: $DatabaseDsnFile" }
$auditPath = Join-Path $runtimeDirectory 'audit.jsonl'
if (Test-Path -LiteralPath $auditPath) {
    $first = Get-Content -LiteralPath $auditPath -TotalCount 1 | ConvertFrom-Json
    if ('signature' -notin $first.PSObject.Properties.Name -or -not $first.signature) {
        $legacyPath = Join-Path $runtimeDirectory ("audit-legacy-unsigned-{0}.jsonl" -f (Get-Date -Format 'yyyyMMddHHmmss'))
        Move-Item -LiteralPath $auditPath -Destination $legacyPath
        Write-Warning "Moved the unsigned prototype audit log to $legacyPath. New events use a signed hash chain."
    }
}

function New-RandomHexSecret {
    $bytes = New-Object byte[] 32
    $generator = [Security.Cryptography.RandomNumberGenerator]::Create()
    try { $generator.GetBytes($bytes) } finally { $generator.Dispose() }
    return ([BitConverter]::ToString($bytes)).Replace('-', '').ToLowerInvariant()
}

$networkName = "bap-local-$engine"
$localDatabase = -not $DatabaseDsn -and -not $DatabaseDsnFile
if ($localDatabase) {
    $databaseDirectory = Join-Path $runtimeDirectory 'mysql'
    if ($engine -eq 'docker') {
        New-Item -ItemType Directory -Force -Path $databaseDirectory | Out-Null
    }
    $rootPasswordPath = Join-Path $runtimeDirectory 'mysql-root-password.txt'
    $applicationPasswordPath = Join-Path $runtimeDirectory 'mysql-application-password.txt'
    if (-not (Test-Path -LiteralPath $rootPasswordPath)) {
        Set-Content -LiteralPath $rootPasswordPath -Value (New-RandomHexSecret) -NoNewline
    }
    if (-not (Test-Path -LiteralPath $applicationPasswordPath)) {
        Set-Content -LiteralPath $applicationPasswordPath -Value (New-RandomHexSecret) -NoNewline
    }
    $rootPassword = (Get-Content -LiteralPath $rootPasswordPath -Raw).Trim()
    $databasePassword = (Get-Content -LiteralPath $applicationPasswordPath -Raw).Trim()

    $existingNetwork = @(& $engine network ls --filter "name=^${networkName}$" --format '{{.Name}}')
    if ($networkName -notin $existingNetwork) {
        & $engine network create $networkName *> $null
        if ($LASTEXITCODE -ne 0) { throw 'Could not create the local BAP container network.' }
    }
    $existingDatabase = @(& $engine ps --all --filter 'name=^/bap-mysql-local$' --format '{{.Names}}')
    if ($engine -eq 'podman' -and 'bap-mysql-local' -in $existingDatabase) {
        $databaseInspect = ((& $engine inspect bap-mysql-local) -join "`n") | ConvertFrom-Json
        $mysqlMount = @($databaseInspect[0].Mounts | Where-Object { $_.Destination -eq '/var/lib/mysql' }) | Select-Object -First 1
        $mysqlMountType = if ($null -eq $mysqlMount) { '' } else { [string]$mysqlMount.Type }
        if ($mysqlMountType -ne 'volume') {
            Write-Warning 'Replacing the legacy Podman MySQL container that used an incompatible Windows bind mount. The host directory is preserved.'
            & $engine rm --force bap-mysql-local *> $null
            if ($LASTEXITCODE -ne 0) { throw 'Could not replace the legacy Podman MySQL container.' }
            $existingDatabase = @()
        }
    }
    if ('bap-mysql-local' -notin $existingDatabase) {
        if ($engine -eq 'podman') {
            # Podman Desktop runs Linux containers in a WSL VM. A Windows bind
            # mount cannot provide the ownership/mode semantics MySQL requires
            # for its private keys and system tables, so keep database files on
            # the VM's Linux filesystem in an engine-managed named volume.
            $databaseVolume = 'bap-mysql-local-data'
            $previousErrorPreference = $ErrorActionPreference
            $ErrorActionPreference = 'SilentlyContinue'
            try {
                & $engine volume inspect $databaseVolume *> $null
                $volumeExists = $LASTEXITCODE -eq 0
            } finally {
                $ErrorActionPreference = $previousErrorPreference
            }
            if (-not $volumeExists) {
                & $engine volume create $databaseVolume *> $null
                if ($LASTEXITCODE -ne 0) { throw 'Could not create the local Podman MySQL volume.' }
            }
            $databaseMount = "${databaseVolume}:/var/lib/mysql"
        } else {
            $databaseMount = "${databaseDirectory}:/var/lib/mysql"
        }
        & $engine run --detach --name bap-mysql-local --network $networkName --volume $databaseMount `
            --env "MYSQL_ROOT_PASSWORD=$rootPassword" --env MYSQL_DATABASE=bap --env MYSQL_USER=bap `
            --env "MYSQL_PASSWORD=$databasePassword" mysql:8.4 `
            --character-set-server=utf8mb4 --collation-server=utf8mb4_0900_ai_ci | Out-Host
        if ($LASTEXITCODE -ne 0) { throw 'Could not start the local MySQL container.' }
    } else {
        $runningDatabase = @(& $engine ps --filter 'name=^/bap-mysql-local$' --format '{{.Names}}')
        if ('bap-mysql-local' -notin $runningDatabase) {
            & $engine start bap-mysql-local | Out-Host
            if ($LASTEXITCODE -ne 0) { throw 'Could not restart the local MySQL container.' }
        }
    }

    $databaseReady = $false
    for ($attempt = 1; $attempt -le 90; $attempt++) {
        $previousErrorPreference = $ErrorActionPreference
        $ErrorActionPreference = 'SilentlyContinue'
        try {
            & $engine exec --env "MYSQL_PWD=$databasePassword" bap-mysql-local mysqladmin ping --silent --host 127.0.0.1 --user bap *> $null
            $pingExitCode = $LASTEXITCODE
        } finally {
            $ErrorActionPreference = $previousErrorPreference
        }
        if ($pingExitCode -eq 0) { $databaseReady = $true; break }
        $previousErrorPreference = $ErrorActionPreference
        $ErrorActionPreference = 'SilentlyContinue'
        try {
            $databaseRunning = ((& $engine inspect --format '{{.State.Running}}' bap-mysql-local 2>$null) -join '').Trim().ToLowerInvariant()
        } finally {
            $ErrorActionPreference = $previousErrorPreference
        }
        if ($databaseRunning -eq 'false') {
            $previousErrorPreference = $ErrorActionPreference
            $ErrorActionPreference = 'Continue'
            try { $databaseLogs = & $engine logs --tail 40 bap-mysql-local 2>&1 } finally { $ErrorActionPreference = $previousErrorPreference }
            throw "Local MySQL exited before it became ready. Container log tail:`n$($databaseLogs -join "`n")"
        }
        Start-Sleep -Seconds 1
    }
    if (-not $databaseReady) { throw 'Local MySQL did not become ready.' }
    $DatabaseDsn = "bap:$databasePassword@tcp(bap-mysql-local:3306)/bap?charset=utf8mb4&parseTime=true&loc=UTC"
}

$existingService = @(& $engine ps --all --filter 'name=^/bap-service-local$' --format '{{.Names}}')
if ('bap-service-local' -in $existingService) { & $engine rm --force bap-service-local *> $null }
$mount = "$($runtimeDirectory):/var/lib/bap"
$runArguments = @(
    'run', '--detach', '--name', 'bap-service-local', '--publish', '127.0.0.1:8443:8443',
    '--volume', $mount, '--env', 'BAP_DEVELOPMENT_TLS=true', '--env', "BAP_EDGE_API_KEY=$apiKey",
    '--env', 'BAP_EDGE_PRINCIPAL=local-developer'
)
if ($DatabaseDsnFile) {
    $resolvedDsnFile = (Resolve-Path -LiteralPath $DatabaseDsnFile).Path
    $runArguments += @('--volume', "${resolvedDsnFile}:/run/secrets/bap-database-dsn:ro", '--env', 'BAP_DATABASE_DSN_FILE=/run/secrets/bap-database-dsn')
} else {
    $runArguments += @('--env', "BAP_DATABASE_DSN=$DatabaseDsn")
}
if ($localDatabase) {
    $runArguments += @('--network', $networkName, '--env', 'BAP_DATABASE_ALLOW_INSECURE=true')
}
if ($DatabaseCaPath) {
    if (-not (Test-Path -LiteralPath $DatabaseCaPath)) { throw "MySQL CA bundle does not exist: $DatabaseCaPath" }
    $resolvedCa = (Resolve-Path -LiteralPath $DatabaseCaPath).Path
    $runArguments += @('--volume', "${resolvedCa}:/run/bap-mysql-ca.pem:ro", '--env', 'BAP_DATABASE_TLS_CA_PATH=/run/bap-mysql-ca.pem')
}
if ($DatabaseTlsServerName) {
    $runArguments += @('--env', "BAP_DATABASE_TLS_SERVER_NAME=$DatabaseTlsServerName")
}
$runArguments += 'bap-service:local'
& $engine @runArguments | Out-Host
if ($LASTEXITCODE -ne 0) { throw 'Could not start the BAP Service container.' }

Wait-BapHealth -CaBundle (Join-Path $runtimeDirectory 'dev-ca.pem')
Set-Content -LiteralPath (Join-Path $runtimeDirectory 'container-engine.txt') -Value $engine
Write-Host "BAP Service and MySQL storage are ready. Runtime: $engine; URL: https://127.0.0.1:8443"
