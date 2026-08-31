# Manual execution boundary for privileged access

## Decision

BAP allows Claude to research, explain, draft, and validate a privileged
operation, but it does not let Claude execute selected interactive access
clients. A signed command rule with effect `manual-only` returns deny with the
reason code `MANUAL_EXECUTION_REQUIRED`.

This behavior requires Edge protocol 2. Policy source version 3 declares that
minimum, so an older Edge fails closed instead of silently losing the distinct
handoff semantics.

The employee must review the operation, obtain any required Access to
Production (A2P) approval, and type or paste the command into a separate
terminal. From that point, the employee's OS identity, database or platform
permissions, A2P process, and resource-side audit are authoritative. A willful
manual policy violation is an employee action; it is outside BAP's Claude-tool
enforcement boundary.

## Why this scales

This design does not add a BAP database proxy, a client for every database, or
a special prompt such as `bap-db`. Administrators maintain a small central list
of executable boundaries. The initial signed policy covers both native and
Windows names for:

- `mysql`, `psql`, `sqlcmd`, and `sqlplus`;
- `ssh`; and
- `kubectl`.

The rule applies regardless of the target written in the command. BAP does not
try to infer whether a hostname is production from natural language or command
syntax; that inference would be incomplete and spoofable. Administrators can
add another executable by adding a tested `manual-only` rule, incrementing the
policy version, and distributing the newly signed bundle.

## Runtime flow

```text
Claude proposes a tool call
          |
          v
Managed PreToolUse hook -> BAP Edge parses the direct executable
          |
          +-- explicit forbid -> deny (forbid wins)
          |
          +-- manual-only ----> deny + safe manual handoff
          |
          +-- signed permit --> normal Cedar evaluation
          |
          +-- no match -------> default deny
```

The handoff deliberately does not echo the attempted command, hostname,
username, credential, connection string, or query. Claude retains the command
in its conversation and can explain it, but the BAP denial message only tells
the employee to review it and use a separate terminal.

Chaining, shell operators, command substitution, encoded launchers, and
wrapping a client inside `powershell`, `cmd`, or `bash -c` do not qualify for a
manual handoff. They remain parse-denied or explicitly forbidden. This prevents
the friendly handoff outcome from becoming a command-smuggling mechanism.

## Managed-hook coverage

The company deployment installs BAP through administrator-owned managed Claude
settings with `PreToolUse` matching every tool. Developers and Claude cannot
replace or disable that managed hook with user, project, local, or command-line
settings. Bash commands, WebFetch/WebSearch, MCP tools, and browser capabilities
exposed as Claude tools therefore reach BAP before execution; unknown tool
names fail closed.

Natural language cannot itself contact a database, browser, or API. A real
external action requires a Claude tool call and is subject to the managed hook.
The intentionally separate terminal is not a Claude tool call and is governed
by the employee's existing enterprise access controls.

## Audit and test contract

The local decision is durably recorded with the signed policy version, bundle
digest, matched rule ID, and `MANUAL_EXECUTION_REQUIRED`. Privacy-safe audit
transformation continues to exclude command plaintext. BAP does not claim that
the employee later ran the command; the target system's audit is the source of
truth for that separate action.

Run the native one-click verification with:

```powershell
.\Start-BapNativeLocal.ps1 -Rebuild -VerifyOnly
```

The verification checks that a representative MySQL invocation is denied with
the manual-execution message. The full container-backed test also verifies the
distinct central audit reason and absence of the sample hostname in audit data.
