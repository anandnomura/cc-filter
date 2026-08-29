# Audit trail

## What is recorded

All records use a common correlation model:

```text
principal + credential fingerprint
  -> Claude session_id
     -> BAP workload_id
        -> Claude tool_use_id
           -> authorization / grant consumption / outcome
```

Event sources are:

- `pdp_evaluation`: Cedar was evaluated by BAP Service;
- `cached_grant_consumption`: an exact cached grant was re-verified and used;
- `local_edge_filter`: inherited cc-filter denied before Cedar;
- `bap_edge_report`: the tool subsequently succeeded or failed.

Events include action, tool, decision/reason, resource hash, principal, credential
fingerprint, and correlation IDs. File targets are workspace-relative when
possible. Commands are stored only as SHA-256 summaries. Prompts, command text,
tool input bodies, tool output, file content, API keys, and secrets are excluded.

## Integrity

BAP Service appends events to `audit.jsonl`. Each event contains:

- the previous event hash;
- an event hash;
- an Ed25519 signature made with the dedicated audit signing key;
- the Cedar policy SHA-256 for service decisions and cached grants.

This detects event modification, insertion, and deletion/reordering inside the
chain. Protect the audit volume and periodically export the last event hash to a
separate log/SIEM; without an external checkpoint, truncating only the tail of a
local file cannot be distinguished from restoring an older complete copy.

The audit key is separate from the grant-signing key. Keep both private keys on
BAP Service. Distribute only `audit-public.pem` to verifiers and
`grant-public.pem` to BAP Edges.

## View and verify locally

```powershell
.\View-AuditTrail.ps1 -Runtime Docker
.\View-AuditTrail.ps1 -Runtime Docker -VerifyOnly
```

For Podman replace `Docker` with `Podman`. Verification occurs before records are
printed. A nonzero exit or `audit verification failed` message means the trail
must be treated as potentially altered.

Direct container commands are also available:

```powershell
docker run --rm --volume "${PWD}\.bap\runtime\docker:/var/lib/bap" bap-service:local audit verify
docker run --rm --volume "${PWD}\.bap\runtime\docker:/var/lib/bap" bap-service:local audit list
```

## Availability behavior

- A direct Cedar decision is not returned until its audit event is durable.
- A cached grant cannot authorize execution until consumption is durable.
- Local denials remain denied and queue their audit report if offline.
- Post-execution outcomes cannot undo an action, so they use durable local retry.

The outcome spool is under the configured Edge state directory. It contains only
minimal correlation metadata. Successful service acknowledgement deletes the
corresponding spool file.

## Production storage

Mount `/var/lib/bap` on durable, access-controlled storage. Back it up, ship
events and chain checkpoints to a SIEM, alert on verification failures, restrict
audit-key access to the BAP Service identity, and establish retention rules. A
container's writable layer is not an acceptable audit system of record.
