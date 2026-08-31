param([ValidateSet('Auto', 'Podman', 'Docker')][string]$Runtime = 'Auto')

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'
. (Join-Path $PSScriptRoot 'scripts\Runtime.ps1')

$engine = Get-BapContainerEngine -Runtime $Runtime
$mount = "$($PSScriptRoot):/src"

Write-Host 'Running the data-driven command and bypass policy corpus...'
& $engine run --rm --volume $mount --workdir /src docker.io/library/golang:1.23-bookworm `
    go test -v ./internal/policybundle -run '^TestCommandPolicyCorpus$'
if ($LASTEXITCODE -ne 0) { throw 'The command and bypass policy corpus failed.' }

Write-Host 'Running the modeled Claude tool-schema and local-decision corpus...'
& $engine run --rm --volume $mount --workdir /src docker.io/library/golang:1.23-bookworm `
    go test -v ./internal/bapedge -run '^TestMVP0ToolContractCorpus$'
if ($LASTEXITCODE -ne 0) { throw 'The modeled Claude tool contract corpus failed.' }

Write-Host 'Running the signed Service-to-Edge policy rollout lifecycle...'
& $engine run --rm --volume $mount --workdir /src docker.io/library/golang:1.23-bookworm `
    go test -v ./bap-service/internal/server -run '^TestSignedPolicyRolloutEndToEnd$'
if ($LASTEXITCODE -ne 0) { throw 'The signed policy rollout lifecycle failed.' }

Write-Host 'PASS: command/bypass corpus.'
Write-Host 'PASS: modeled Claude tool schemas and local policy decisions.'
Write-Host 'PASS: v1 allow -> v2 rule removal deny.'
Write-Host 'PASS: CURRENT, forced update, rollback, equivocation, tamper, kill switch, and offline-expiry controls.'
