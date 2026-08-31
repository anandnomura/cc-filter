# Troubleshooting

## Podman reports `Local MySQL did not become ready`

Current startup uses the Linux-native Podman volume
`bap-mysql-local-data`. Older startup code bind-mounted
`.bap/runtime/podman/mysql` from Windows; MySQL could then abort with
`Cannot change permissions ... Operation not permitted`. Re-run
`Start-Bap.ps1 -Runtime Podman` with the current script. The obsolete host
directory is no longer used and can remain as recovery evidence.

Inspect a failure with:

```powershell
podman ps --all --filter "name=bap-mysql-local"
podman logs --tail 100 bap-mysql-local
podman volume inspect bap-mysql-local-data
```

The startup script now includes the MySQL log tail when the container exits,
instead of waiting for only the generic readiness timeout.

## No Go executable

Expected. Build and test scripts run `golang:1.23-bookworm` inside Podman or
Docker. Install Go only if you personally want editor integration.

## Container runtime is unavailable

```powershell
podman machine start
podman info
```

or start Docker Desktop and run `docker info`.

## HTTPS health check fails

```powershell
.\Initialize-Certificates.bat -Runtime Docker
.\Start-Bap.ps1 -Runtime Docker
docker logs bap-service-local
curl.exe --ssl-no-revoke --cacert .bap\runtime\docker\dev-ca.pem https://127.0.0.1:8443/healthz
```

`--ssl-no-revoke` is needed only for the private development CA because it has no
online revocation service. It does not disable certificate-chain verification.

## Claude does not show managed settings

1. Confirm the file is under `C:\Program Files\ClaudeCode\managed-settings.d`.
2. Restart Claude Code.
3. Run `/status` and `claude doctor`.
4. Check whether a higher-priority server-managed or registry policy is active.
5. Do not place the file under the obsolete ProgramData location.

## All tool calls are denied

Run `Test-Bap.ps1`. Check the HTTPS URL, CA path, bundle public key, service
logs, inherited credential or mTLS identity, local policy state, and bundle
expiry/offline lease. Restart Claude after changing machine configuration.

If the error contains `certificate signed by unknown authority` after managed
installation, the Program Files CA may come from a different local runtime than
the service bound to port 8443. For example, Podman trust files cannot verify a
Docker service initialized with a different development CA. Reinstall from an
elevated PowerShell with the runtime that is actually serving BAP:

```powershell
.\Install-ManagedSettings.ps1 -Runtime Docker
```

Use `-Runtime Podman` instead only when Podman owns port 8443. The installer's
`Auto` mode detects the live service by validating it against each runtime CA;
the local launcher also checks installed CA and bundle verification material.

## Claude says `Ran 1 shell command` for a denied call

The compact Claude activity label counts an attempted Bash tool call. It does
not mean the command reached the operating system. For a BAP denial, expand the
tool event and confirm that `PreToolUse:Bash` says `BAP EDGE BLOCKED THIS TOOL
CALL; IT DID NOT EXECUTE`. The Claude session record will contain an error tool
result with `toolDenialKind` set to `permission-rule`, and the BAP audit trail
will contain `allowed: false`. A small local model may otherwise misdescribe or
repeat an earlier tool result, so do not treat its prose as enforcement
evidence.

## A repeated operation does or does not call the network

Traffic decisions do not call BAP Service while the signed bundle is fresh.
Edge synchronizes at SessionStart, when policy is missing, and after
`refresh_after_seconds`; audit delivery is asynchronous. Deleting local policy
state forces synchronization. Once `max_offline_seconds` elapses without a
successful sync, operations deny until the control plane is reachable.

## Audit verification fails

Stop the service and preserve the MySQL database/backup plus the audit public
key for investigation. Do not edit or silently discard database rows. Verify
that the correct audit-key mount is present. A signature/hash failure indicates
wrong key material, corruption, or tampering.
