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

BAP Service appends events to MySQL in a transaction that also advances the
locked audit-chain head. Each event contains:

- the previous event hash;
- an event hash;
- an Ed25519 signature made with the dedicated audit signing key;
- the Cedar policy SHA-256 for service decisions and cached grants.

This detects event modification, insertion, and deletion/reordering inside the
chain. Protect and back up MySQL and periodically export the last event hash to a
separate log/SIEM; without an external checkpoint, restoring an older internally
consistent database copy cannot be distinguished from an authorized restore.

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

Direct commands execute inside the running service container so they use its
configured MySQL connection and mounted keys:

```powershell
docker exec bap-service-local bap-service audit verify
docker exec bap-service-local bap-service audit list
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

Use company-managed MySQL with replication and point-in-time recovery. Back it
up, restore-test it, ship events and chain checkpoints to a SIEM, alert on
verification/readiness failures, restrict audit-key access to the BAP Service
identity, and establish retention rules. See [MySQL storage](storage.md).
