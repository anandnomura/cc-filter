# Windows managed settings and bypass resistance

## Installed locations

- BAP Edge: `C:\Program Files\BAP Edge\bap-edge.exe`
- Edge configuration/public keys: `C:\Program Files\BAP Edge\`
- Managed policy drop-in: `C:\Program Files\ClaudeCode\managed-settings.d\50-bap-edge.json`

Do not use the legacy `C:\ProgramData\ClaudeCode` path; current Claude Code no
longer reads managed settings there.

The installer applies Windows ACLs so SYSTEM and Administrators have full control
and standard Users receive read/execute only.

Re-run the installer after rebuilding or updating BAP Edge. The managed hook
executes the copy under Program Files, not the development binary under `dist`.

For a network BAP Service, pass `-EdgeBinaryPath`, `-GrantPublicKeyPath`,
`-CaBundlePath`, and `-ApiKey`. The Windows endpoint then needs no Podman, Docker,
Go runtime, or Linux binary.

## Enforced Claude settings

- `allowManagedHooksOnly: true`: user, project, local, and ordinary plugin hooks
  are ignored.
- `allowManagedPermissionRulesOnly: true`: lower scopes cannot add permission
  allow/ask/deny rules.
- `permissions.disableBypassPermissionsMode: "disable"`: blocks
  `--dangerously-skip-permissions` and bypass mode.
- `requiredMinimumVersion: "2.1.246"`: prevents use of an older official Claude
  Code version that lacks the tested controls.
- Six administrator-owned hooks cover session creation/cleanup, authorization,
  successful and failed tool outcomes, and prompt filtering.

The installer also sets the dedicated `BAP_EDGE_API_KEY` as a machine environment
variable. Restart Claude Code after installation so it inherits the value. This
key is separate from the Anthropic key. In the interim bearer model, a user who
can read their process environment may copy it; mTLS/workload identity is the
planned stronger replacement.

Managed policy outranks command-line, local, project, and user settings. Run:

```powershell
.\Test-ManagedSettings.ps1
```

Then verify `/status` and `/permissions` in a fresh Claude session. In Claude
Code 2.1.246, `/hooks` shows the editable hook registry and can report `0 hooks
configured` while administrator-managed policy hooks are active. Use the live
allow/deny checks in `Test-ManagedSettings.ps1` as the authoritative test. The
filesystem test must be run from a standard-user shell, not the elevated
installer shell.

## Exact local installation and verification

These steps use Docker and the local BAP Service at
`https://127.0.0.1:8443`. Use two different PowerShell windows as indicated.

First, from a normal, non-elevated PowerShell window:

```powershell
cd C:\Users\User\pyprj\bap-edge
.\Build-BapEdge.ps1 -Runtime Docker
.\Start-Bap.ps1 -Runtime Docker
.\Test-Bap.ps1 -Runtime Docker
```

Then open **PowerShell as Administrator** and install the current Edge binary,
configuration, CA, grant verification key, machine credential, and managed
Claude settings:

```powershell
cd C:\Users\User\pyprj\bap-edge
.\Install-ManagedSettings.ps1 -Runtime Docker
```

Close every running Claude Code process and terminal that will launch Claude.
This is required so the next process reads both the managed settings and the
new machine-level `BAP_EDGE_API_KEY` environment variable.

Open a fresh **non-elevated** PowerShell window and run:

```powershell
cd C:\Users\User\pyprj\bap-edge
.\Test-ManagedSettings.ps1
```

The output must include both of these lines:

```text
PASS: current standard user cannot write the managed settings file.
PASS: managed-only hooks, managed-only permission rules, bypass-mode lockout, and Windows ACL checks.
```

If the first line says the check was skipped, the shell is elevated. Close it
and repeat the test from a non-elevated shell.

Start the official Claude executable, or use the local-Qwen launcher:

```powershell
claude
```

```powershell
.\start-local-claude.bat
```

Inside Claude, verify:

1. `/status` identifies a managed settings source.
2. `/hooks` may show `0 hooks configured`. This is the editable hook registry,
   not proof that managed policy hooks are absent.
3. `/permissions` shows the managed source and does not offer bypass-permissions
   mode.

The normal Claude executable can list both `User settings` and `Enterprise
managed settings (drop-ins)` under `/status`. This means both sources were
loaded, not that user settings can override managed hooks or permission rules.
The `allowManagedHooksOnly` and `allowManagedPermissionRulesOnly` controls apply
specifically to those security-sensitive sections; ordinary preferences such as
theme can still come from the user source.

When managed BAP settings are installed, `start-local-claude.bat` passes an
empty `--setting-sources` selection. Managed policy is loaded independently, so
the local launcher suppresses user, project, and local settings and `/status`
should list only the enterprise managed source. This makes the demonstration
unambiguous, but it is not the security boundary: a developer can omit that CLI
flag, while the administrator-controlled managed-only settings still enforce
the hooks and permission rules.

Test an allowed request:

```text
Call Bash exactly once with this exact command: git status --short
```

Test a denied request:

```text
Call Bash exactly once with this exact command: git reset --hard
```

The denied request must display:

```text
PreToolUse:Bash says: BAP EDGE BLOCKED THIS TOOL CALL; IT DID NOT EXECUTE.
```

Claude's compact activity summary may still say `Ran 1 shell command`. That
counts a tool call attempted by the model; it does not prove that the operating
system launched the command. The PreToolUse result, absence of a successful
tool result, preserved working-tree changes, and BAP audit decision are the
authoritative evidence. The explicit BAP system message is emitted by the hook
so this distinction does not depend on the model describing the denial
correctly.

Verify the signed audit trail from PowerShell:

```powershell
.\View-AuditTrail.ps1 -Runtime Docker
```

The destructive request must have `"allowed": false` and
`"reason_code": "EXPLICIT_FORBID"`.

Finally, prove fail-closed behavior:

```powershell
.\Stop-Bap.ps1 -Runtime Docker
```

Start a fresh Claude session and request `git status --short`. Even this safe
operation must be denied because the policy service is unavailable. Restore
normal operation afterward:

```powershell
.\Start-Bap.ps1 -Runtime Docker
```

When managed settings are installed, the local-Qwen launcher does not pass its
repo-local hook settings. It prints the managed policy and executable paths at
startup. `allowManagedHooksOnly` also causes user, project, local, and ordinary
plugin hooks to be ignored.

The authoritative evidence that managed hooks ran is the live output from
`Test-ManagedSettings.ps1`, a `PreToolUse` allow/deny message in Claude, or a
session trace whose hook command is
`C:\Program Files\BAP Edge\bap-edge.exe`. The `/hooks` count is not an
enforcement test in Claude Code 2.1.246.

## Important managed-source precedence

Claude uses only the highest active managed source. Server-managed organization
settings outrank Windows registry policy, which outranks file-based managed
settings. If `/status` says a higher source is active, place the BAP keys and
hooks in that source; the Program Files drop-in will not merge with it.

## Security boundary

These controls prevent a standard user from overriding settings in the official
Claude Code client. They do not stop:

- a local administrator;
- a modified/unapproved Claude executable that ignores managed settings;
- a different process accessing files directly;
- compromise of a developer-controlled local BAP Service container.

For company enforcement, remove local-admin rights, use WDAC/AppLocker or an
equivalent application allowlist, deploy BAP Service on the network, and protect
resources with their own gateway where possible. A stopped/unreachable BAP
Service causes denial, not fallback access.

An account that can elevate to local Administrator can take ownership, change
the ACL, replace the managed settings or Edge binary, change the machine
credential, or run an unapproved client. Using an administrator account to
perform this local installation is fine for testing, but it cannot demonstrate
resistance to that same person deliberately elevating later. A production
developer account must not have local-administrator credentials available to
it; installation and updates must be performed by IT or endpoint management.
