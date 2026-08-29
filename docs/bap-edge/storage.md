# Storage model and database decision

This version does not require a database.

| Data | Format/location | Authority and durability |
|---|---|---|
| Cedar policy/schema | version-controlled `.cedar` and JSON | Administrator-reviewed source of authority |
| Edge configuration | admin-owned YAML | Installed read-only for standard users |
| TLS/grant/audit keys | PEM or API-key secret | Service/secret-store authority; never Git |
| Authorization audit | signed, hash-chained `audit.jsonl` | Durable BAP Service volume; export to SIEM |
| Policy proposals | sanitized `policy-proposals.jsonl` | Advisory only; never automatically enforced |
| Signed allow grants | Edge user-cache files | Non-authoritative; signature checked every use |
| Session/workload mapping | Edge user-state files | Interim correlation state |
| Unsent outcomes/denials | Edge retry-spool JSON | Deleted only after service acknowledgement |

Flat files keep the prototype inspectable and make Docker/Podman demonstrations
easy. The authoritative Cedar rules remain in Git, not a mutable learning table.

For a network production service, replace or supplement JSONL with an append-only
event store, durable queue, and SIEM/WORM archive. Preserve event IDs, signatures,
hash-chain fields, and correlation IDs. A relational database is optional; the
important properties are durable append, restricted writers, retention,
searchability, backup, and external chain checkpoints. Policy proposals may use a
database when an approval UI is added, but approved policy should still follow a
reviewed deployment process.
