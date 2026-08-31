# Native local Windows test with project-local Claude hooks

Use this procedure when BAP Edge, BAP Service, and Claude Code run as ordinary
Windows executables on the same company laptop. It does not install or modify
Claude managed settings and does not require Administrator rights.

## One-click test and Claude launch

From the repository root, double-click `Start-BapNativeLocal.bat` or run:

```powershell
.\Start-BapNativeLocal.ps1
```

Use `-Rebuild` to compile both EXEs from current source first:

```powershell
.\Start-BapNativeLocal.ps1 -Rebuild
```

The launcher:

1. builds missing Windows Edge and Service EXEs with native Go;
2. creates isolated state under `.bap\native-local`;
3. generates a dedicated local API key plus development TLS/signing keys;
4. starts `bap-service-windows-amd64.exe` hidden on loopback HTTPS;
5. writes the Edge YAML with the matching CA, bundle key, state, and credential;
6. verifies signed policy synchronization and four allow/deny cases;
7. temporarily merges all six BAP hooks into `.claude\settings.local.json`;
8. launches Claude in the current repository;
9. restores the exact previous local settings and stops the Service it started
   after Claude exits.

To test BAP without launching Claude, run:

```powershell
.\Start-BapNativeLocal.ps1 -VerifyOnly
```

If port 8443 is occupied, select another loopback port:

```powershell
.\Start-BapNativeLocal.ps1 -Port 18443
```

Service logs are `.bap\native-local\bap-service.stdout.log` and
`.bap\native-local\bap-service.stderr.log`. This mode intentionally uses the
development JSONL audit/proposal store when `BAP_DATABASE_DSN` is unset.

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
5. Ask Claude to read `.env`; expect a BAP denial and no file contents.
6. Exit Claude normally so `SessionEnd` runs.

This setup is appropriate for local functional testing. Because the user owns
the settings, EXEs, state, and credential, it is not tamper-resistant and is not
the company pilot deployment model.
