# Native local Windows test with project-local Claude hooks

Use this procedure when BAP Edge, BAP Service, and Claude Code run as ordinary
Windows executables on the same company laptop. It does not install or modify
Claude managed settings and does not require Administrator rights.

This mode cannot be combined with an existing `allowManagedHooksOnly`
installation: Claude would ignore the temporary project-local hooks. The
launcher detects that condition and stops with instructions. For managed hooks
plus a local model, follow the [root README build/test commands](../../README.md#buildtest-commands)
and launch `start-local-claude.bat` instead.

## One-click test and Claude launch

From the repository root, double-click or run the BAT launcher:

```powershell
.\Start-BapNativeLocal.bat
```

Do not substitute a bare `claude` command. The BAT launcher configures the
temporary hooks and starts Claude with the local bridge, selected model, and a
reduced `Bash` tool contract. A bare launch can load a much larger default tool
context and exceed a local model's context window before the first prompt.

Use `-Rebuild` to compile both EXEs from current source first:

```powershell
.\Start-BapNativeLocal.bat -Rebuild
```

The default is the local model bridge at `http://127.0.0.1:8080`; start
`start-ccbridge.bat` separately first. For a company-authenticated Claude Code
session instead, run:

```powershell
.\Start-BapNativeLocal.bat -UseCompanyClaude
```

Company mode is interactive by default. BAP invokes the company `claude.cmd`
with no `--model`, `--tools`, `--system-prompt`, prompt, or other Claude CLI
arguments. This supports company launchers that perform initialization and are
not general-purpose CLI wrappers.

That switch removes the local bridge override and local demo credential. When
`-Model` is omitted, the company path uses the company's configured default.
All commands are collected in the [root README build/test commands](../../README.md#buildtest-commands).

For a one-command, non-interactive local classifier check, run:

```powershell
.\Start-BapNativeLocal.bat -Print -Prompt "Please connect to the MySQL orders database and reindex it"
```

The launcher:

1. builds missing Windows Edge and Service EXEs with native Go;
2. creates a unique retained run under `.bap\native-local\runs`;
3. generates a dedicated local API key plus development TLS/signing keys;
4. starts `bap-service-windows-amd64.exe` hidden on loopback HTTPS;
5. writes the Edge YAML with the matching CA, bundle key, state, and credential;
6. verifies signed policy synchronization, allow/deny behavior, and the
   privileged-client manual handoff;
7. temporarily merges all six BAP hooks into `.claude\settings.local.json`;
8. launches Claude in the current repository;
9. restores the exact previous local settings and stops the Service it started
   after Claude exits.

To test BAP without launching Claude, run:

```powershell
.\Start-BapNativeLocal.bat -VerifyOnly
```

This isolated verification uses separate ephemeral Agent STS issue and consume
credentials. Ambient company STS variables cannot change the test, and the
resource-PEP consume credential is not inherited by Claude.

If port 8443 is occupied, select another loopback port:

```powershell
.\Start-BapNativeLocal.bat -Port 18443
```

The exact current run directory is printed by the launcher and written to
`.bap\native-local\latest-run.txt`. Service logs are
`bap-service.stdout.log` and `bap-service.stderr.log` inside that run. This mode
intentionally uses the development JSONL audit/proposal store when
`BAP_DATABASE_DSN` is unset.

Every invocation gets separate TLS/signing keys, policy state, audit chain,
Edge state, and logs. This prevents simultaneous or previous test Services from
writing competing heads into one JSONL audit chain. Runs are retained rather
than silently deleted so a failed chain remains available for investigation.
Older state directly under `.bap\native-local\service-state` is no longer used
by the launcher.

## Hooks installed for the local session

Claude Code reads these from `.claude\settings.local.json`, whose source is
shown as `Local` in `/hooks`. The generated absolute paths are equivalent to:

```json
{
  "$schema": "https://json.schemastore.org/claude-code-settings.json",
  "hooks": {
    "SessionStart": [
      {
        "matcher": "startup|resume|clear|compact",
        "hooks": [
          {
            "type": "command",
            "command": "\"C:/path/to/bap-edge/dist/bap-edge-windows-amd64.exe\" --config \"C:/path/to/bap-edge/.bap/native-local/bap-edge.yaml\"",
            "timeout": 10
          }
        ]
      }
    ],
    "PreToolUse": [
      { "matcher": "*", "hooks": [{ "type": "command", "command": "<same command>", "timeout": 10 }] }
    ],
    "PostToolUse": [
      { "matcher": "*", "hooks": [{ "type": "command", "command": "<same command>", "timeout": 10 }] }
    ],
    "PostToolUseFailure": [
      { "matcher": "*", "hooks": [{ "type": "command", "command": "<same command>", "timeout": 10 }] }
    ],
    "UserPromptSubmit": [
      { "matcher": "*", "hooks": [{ "type": "command", "command": "<same command>", "timeout": 10 }] }
    ],
    "SessionEnd": [
      { "matcher": "*", "hooks": [{ "type": "command", "command": "<same command>", "timeout": 10 }] }
    ]
  }
}
```

There is no `allowManagedHooksOnly`, managed permissions setting, or Program
Files installation in this mode. The API key is not written into Claude
settings; Claude and its hook processes inherit it from the launcher process.

## Verify inside Claude

1. Run `/hooks` and confirm six BAP command hooks with source `Local`.
2. Ask Claude to run `git status --short`; expect allow and execution.
3. Ask Claude to run `ls -al`; expect allow and execution.
4. Ask Claude to run `git reset --hard`; expect a BAP denial and no execution.
5. Ask Claude to run `mysql -h orders-prod -u dba`; expect a
   `REQUIRES MANUAL EXECUTION` denial and no execution. BAP will not echo the
   command in its denial message.
6. Ask Claude: `Please connect to the MySQL orders database and reindex it`.
   Before tool selection, expect a visible BAP manual-only advisory. Claude may
   explain or prepare the operation; if it still proposes a client invocation,
   the existing `PreToolUse` rule denies it.
7. Ask Claude: `Explain how database indexes work`; expect no intent advisory.
8. Ask Claude to read `.env`; expect a BAP denial and no file contents.
9. Exit Claude normally so `SessionEnd` runs.

The startup verification performs both prompt cases automatically. Existing
secret filtering remains first in the `UserPromptSubmit` path: a secret match
still exits with code 2, while the intent classifier only sees prompts that
passed that filter.

This setup is appropriate for local functional testing. Because the user owns
the settings, EXEs, state, and credential, it is not tamper-resistant and is not
the company pilot deployment model.
