Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'

$managedPath = Join-Path $env:ProgramFiles 'ClaudeCode\managed-settings.d\50-bap-edge.json'
$binaryPath = Join-Path $env:ProgramFiles 'BAP Edge\bap-edge.exe'
$configPath = Join-Path $env:ProgramFiles 'BAP Edge\bap-edge.yaml'
foreach ($path in @($managedPath, $binaryPath, $configPath)) {
    if (-not (Test-Path -LiteralPath $path)) { throw "Missing managed installation file: $path" }
}

$settings = Get-Content -LiteralPath $managedPath -Raw | ConvertFrom-Json
if (-not $settings.allowManagedHooksOnly) { throw 'allowManagedHooksOnly is not enabled.' }
if (-not $settings.allowManagedPermissionRulesOnly) { throw 'allowManagedPermissionRulesOnly is not enabled.' }
if ($settings.permissions.disableBypassPermissionsMode -ne 'disable') { throw 'Bypass permission mode is not disabled.' }
foreach ($hookName in @('SessionStart', 'PreToolUse', 'PostToolUse', 'PostToolUseFailure', 'UserPromptSubmit', 'SessionEnd')) {
    if (-not $settings.hooks.$hookName) { throw "The managed $hookName hook is missing." }
}
$machineApiKey = [Environment]::GetEnvironmentVariable('BAP_EDGE_API_KEY', 'Machine')
if ($machineApiKey -eq '') { throw 'The machine BAP_EDGE_API_KEY is missing.' }

$dangerousRights = [System.Security.AccessControl.FileSystemRights]::WriteData -bor
    [System.Security.AccessControl.FileSystemRights]::AppendData -bor
    [System.Security.AccessControl.FileSystemRights]::WriteExtendedAttributes -bor
    [System.Security.AccessControl.FileSystemRights]::WriteAttributes -bor
    [System.Security.AccessControl.FileSystemRights]::Delete -bor
    [System.Security.AccessControl.FileSystemRights]::DeleteSubdirectoriesAndFiles -bor
    [System.Security.AccessControl.FileSystemRights]::ChangePermissions -bor
    [System.Security.AccessControl.FileSystemRights]::TakeOwnership
foreach ($protectedPath in @($managedPath, $binaryPath, $configPath)) {
    $acl = Get-Acl -LiteralPath $protectedPath
    foreach ($entry in $acl.Access) {
        if ($entry.AccessControlType -eq 'Allow' -and
            $entry.IdentityReference.Value -match '(BUILTIN\\Users|Authenticated Users)' -and
            (($entry.FileSystemRights -band $dangerousRights) -ne 0)) {
            throw "Standard users have unsafe rights on ${protectedPath}: $($entry.FileSystemRights)"
        }
    }
}

$identity = [Security.Principal.WindowsIdentity]::GetCurrent()
$principal = [Security.Principal.WindowsPrincipal]::new($identity)
$isAdmin = $principal.IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)
if (-not $isAdmin) {
    try {
        $stream = [IO.File]::Open($managedPath, [IO.FileMode]::Open, [IO.FileAccess]::Write, [IO.FileShare]::Read)
        $stream.Dispose()
        throw 'Current standard user was unexpectedly able to open managed settings for writing.'
    } catch [System.UnauthorizedAccessException] {
        Write-Host 'PASS: current standard user cannot write the managed settings file.'
    }
} else {
    Write-Host 'INFO: write-denial test skipped because this shell is elevated. Re-run as a standard user.'
}

# Exercise the installed binary and trust material, not the development copy.
# This catches a Docker/Podman CA, grant-key, or API-key mismatch.
$previousProcessApiKey = $env:BAP_EDGE_API_KEY
$env:BAP_EDGE_API_KEY = $machineApiKey
try {
    $testSession = 'managed-settings-test-' + [Guid]::NewGuid().ToString('N')
    $cases = @(
        @{ Command = 'git status --short'; Expected = 'allow' },
        @{ Command = 'git reset --hard'; Expected = 'deny' }
    )
    foreach ($case in $cases) {
        $inputJson = @{
            hook_event_name = 'PreToolUse'
            session_id = $testSession
            tool_use_id = 'managed-' + $case.Expected
            cwd = $PSScriptRoot
            tool_name = 'Bash'
            tool_input = @{ command = $case.Command }
        } | ConvertTo-Json -Compress -Depth 8
        $result = $inputJson | & $binaryPath --config $configPath | ConvertFrom-Json
        $actual = $result.hookSpecificOutput.permissionDecision
        $reason = $result.hookSpecificOutput.permissionDecisionReason
        if ($actual -ne $case.Expected) {
            throw "Installed managed Edge expected $($case.Expected) for '$($case.Command)', got $actual ($reason)"
        }
        Write-Host "PASS: installed managed Edge: $($case.Command) -> $actual ($reason)"
    }

    $endJson = @{
        hook_event_name = 'SessionEnd'
        session_id = $testSession
        cwd = $PSScriptRoot
    } | ConvertTo-Json -Compress
    $endJson | & $binaryPath --config $configPath | Out-Null
} finally {
    $env:BAP_EDGE_API_KEY = $previousProcessApiKey
}

Write-Host 'PASS: managed-only hooks, managed-only permission rules, bypass-mode lockout, and Windows ACL checks.'
Write-Host 'Restart Claude Code and verify: /status shows Managed and /permissions shows the managed source.'
Write-Host 'INFO: /hooks may show 0 hooks because it lists the editable registry, not administrator-managed policy hooks.'
