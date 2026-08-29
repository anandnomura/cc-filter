# Windows managed settings and bypass resistance

## Installed locations

- BAP Edge: `C:\Program Files\BAP Edge\bap-edge.exe`
- Edge configuration/public keys: `C:\Program Files\BAP Edge\`
- Managed policy drop-in: `C:\Program Files\ClaudeCode\managed-settings.d\50-bap-edge.json`

Do not use the legacy `C:\ProgramData\ClaudeCode` path; current Claude Code no
longer reads managed settings there.

The installer applies Windows ACLs so SYSTEM and Administrators have full control
and standard Users receive read/execute only.

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

Then verify `/status`, `/hooks`, and `/permissions` in a fresh Claude session.
`/hooks` must show SessionStart, PreToolUse, PostToolUse,
PostToolUseFailure, UserPromptSubmit, and SessionEnd from the managed source.
The filesystem test must be run from a standard-user shell, not the elevated
installer shell.

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
