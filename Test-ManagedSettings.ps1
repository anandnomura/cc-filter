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
if ([Environment]::GetEnvironmentVariable('BAP_EDGE_API_KEY', 'Machine') -eq '') { throw 'The machine BAP_EDGE_API_KEY is missing.' }

$dangerousRights = [System.Security.AccessControl.FileSystemRights]::Write -bor
    [System.Security.AccessControl.FileSystemRights]::Modify -bor
    [System.Security.AccessControl.FileSystemRights]::FullControl -bor
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

Write-Host 'PASS: managed-only hooks, managed-only permission rules, bypass-mode lockout, and Windows ACL checks.'
Write-Host 'Restart Claude Code and verify: /status shows Managed; /hooks shows only managed hooks; /permissions shows the managed source.'
