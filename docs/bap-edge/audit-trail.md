# Audit trail

## What is recorded

All records use a common correlation model:

```text
principal + credential fingerprint
  -> Claude session_id
     -> BAP workload_id
        -> Claude tool_use_id
           -> local policy decision / outcome
```

Event sources are:

- `edge_policy_evaluation`: Cedar was evaluated locally from a verified signed bundle;
- `local_edge_filter`: inherited cc-filter denied before Cedar;
- `bap_edge_report`: the tool subsequently succeeded or failed.

Events include action, tool, decision/reason, resource hash, principal, credential
fingerprint, and correlation IDs. File targets are workspace-relative when
possible. Commands are stored only as SHA-256 summaries. Prompts, command text,
tool input bodies, tool output, file content, API keys, and secrets are excluded.

New events also include a W3C-compatible `trace_id`, the BAP Service `span_id`,
and its Edge `parent_span_id`. MySQL indexes trace ID and timestamp, allowing an
local authorization and PostToolUse outcome to be rebuilt
as one operation trace. See [end-to-end observability](observability.md).

## Integrity

BAP Service appends events to MySQL in a transaction that also advances the
locked audit-chain head. Each event contains:

- the previous event hash;
- an event hash;
- an Ed25519 signature made with the dedicated audit signing key;
- the signed bundle version and combined Cedar/registry digest for Edge decisions.

This detects event modification, insertion, and deletion/reordering inside the
chain. Protect and back up MySQL and periodically export the last event hash to a
separate log/SIEM; without an external checkpoint, restoring an older internally
consistent database copy cannot be distinguished from an authorized restore.

The audit, legacy grant, and policy-bundle signing keys are separate. Keep all
private keys on BAP Service. Distribute `audit-public.pem` to verifiers and
`bundle-public.pem` to BAP Edges.

## View and verify locally

```powershell
.\View-AuditTrail.ps1 -Runtime Docker
.\View-AuditTrail.ps1 -Runtime Docker -VerifyOnly
.\View-AuditTrail.ps1 -Runtime Native -Timeline -Last 30
```

For Podman replace `Docker` with `Podman`. Native mode discovers the most recent
retained run through `.bap\native-local\latest-run.txt` and verifies its signed
JSONL chain before displaying it. A nonzero exit or `audit verification failed`
message means the trail must be treated as potentially altered.

To investigate one Claude session, first display recent decisions, copy the
session ID printed below the table, and then request full correlation details:

```powershell
.\View-AuditTrail.ps1 -Runtime Native -Timeline -Last 30
.\View-AuditTrail.ps1 -Runtime Native -Timeline -SessionID 'COPY-SESSION-ID' -Details
```

The route-switch test should show two different tool-use IDs in timestamp order:
a denied `Bash`/`command.execute` event followed by an allowed
`Read`/`file.read` event. That proves Claude did not execute the denied Bash
call; it proposed a second operation which the current policy independently
allowed. Targets and commands remain privacy-safe summaries.

## Where local logs are stored

| Runtime | Authoritative Service audit | Edge operational log |
|---|---|---|
| Native | `.bap\native-local\runs\<run-id>\service-state\audit.jsonl` | `.bap\native-local\runs\<run-id>\edge-state\observability\edge.jsonl` |
| Docker | local MySQL files under `.bap\runtime\docker\mysql` | `.bap\runtime\docker\test-edge-state\observability\edge.jsonl` |
| Podman | `bap-mysql-local-data` named volume | `.bap\runtime\podman\test-edge-state\observability\edge.jsonl` |

For Native, `.bap\native-local\latest-run.txt` contains the absolute path of
the latest retained run. Service process output for that run is in
`bap-service.stdout.log` and `bap-service.stderr.log`. For containers, Service
process output is the `bap-service-local` container log. View both Service and
Edge activity live with:

```powershell
.\Watch-BapLogs.ps1 -Runtime Native -Component All -Tail 100
.\Watch-BapLogs.ps1 -Runtime Docker -Component All -Tail 100
.\Watch-BapLogs.ps1 -Runtime Podman -Component All -Tail 100
```

These process and Edge logs help troubleshooting, but the verified Service
audit is the decision record. `.bap\shadow-logs` contains copied snapshots for
offline recommendation analysis and is not the authoritative live store.

## Clear local development history

Only clear local test history after stopping BAP and after preserving anything
needed for an investigation. Never use these commands for pilot or production
evidence.

Native launchers isolate every run, so old events cannot break a new run. To
remove all retained Native test runs:

```powershell
Remove-Item -LiteralPath '.\.bap\native-local' -Recurse -Force
```

For Docker local testing:

```powershell
.\Stop-Bap.ps1 -Runtime Docker
Remove-Item -LiteralPath '.\.bap\runtime\docker\mysql', '.\.bap\runtime\docker\test-edge-state', '.\.bap\runtime\docker\audit.jsonl' -Recurse -Force -ErrorAction SilentlyContinue
```

For Podman local testing:

```powershell
.\Stop-Bap.ps1 -Runtime Podman
podman volume rm bap-mysql-local-data
Remove-Item -LiteralPath '.\.bap\runtime\podman\test-edge-state', '.\.bap\runtime\podman\audit.jsonl' -Recurse -Force -ErrorAction SilentlyContinue
```

The next start recreates the local database and Edge state. Clearing
`.bap\shadow-logs` is a separate choice because those files are exported
analysis snapshots.

Direct commands execute inside the running service container so they use its
configured MySQL connection and mounted keys:

```powershell
docker exec bap-service-local bap-service audit verify
docker exec bap-service-local bap-service audit list
```

## Availability behavior

- Edge durably spools its local decision before returning allow.
- Central delivery is asynchronous and does not put BAP Service in the traffic
  hot path while the signed bundle lease is fresh.
- Local denials remain denied and queue their audit report if offline.
- Post-execution outcomes cannot undo an action, so they use durable local retry.

The decision/outcome spool is under the configured Edge state directory.
Successful service acknowledgement deletes the corresponding spool file. The
current user-local spool is interim pending the protected Edge Agent.

## Production storage

Use company-managed MySQL with replication and point-in-time recovery. Back it
up, restore-test it, ship events and chain checkpoints to a SIEM, alert on
verification/readiness failures, restrict audit-key access to the BAP Service
identity, and establish retention rules. See [MySQL storage](storage.md).
