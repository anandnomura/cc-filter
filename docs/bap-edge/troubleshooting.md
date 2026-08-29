# Troubleshooting

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

Run `Test-Bap.ps1`. Check the HTTPS URL, CA path, grant public key, service logs,
the inherited `BAP_EDGE_API_KEY`, and Cedar policy. Restart Claude after changing
a machine environment variable. Failure is intentionally closed.

## A repeated operation still calls the network

The grant cache is exact-request and session-bound and lasts at most 30 seconds.
A different path, action, command, tool-use ID, session, workload, expired grant,
or changed context is a cache miss. Deleting the user cache is safe and merely
causes another PDP call. Even an exact cache hit makes a lightweight service call
so grant consumption is centrally verified and audited; it skips Cedar policy
evaluation, not the audit trail.

## Audit verification fails

Stop the service and preserve `.bap/runtime/<engine>/audit.jsonl` plus the audit public
key for investigation. Do not edit or silently discard the file. Verify that the
correct audit-key mount is present. A signature/hash failure indicates wrong key
material, corruption, or tampering.
