# MySQL storage and enterprise database configuration

MySQL is the pilot system of record for authorization audit events and policy
proposals. BAP Service automatically creates its versioned schema, verifies the
complete signed audit chain at startup, and reports not-ready if MySQL cannot be
reached. New authorization decisions fail closed when their audit transaction
cannot commit.

The JSONL implementation remains only as an explicit development fallback when
`BAP_DATABASE_DSN` and `BAP_DATABASE_DSN_FILE` are both unset. Do not use that
fallback for the company pilot.

## Local Docker or Podman

Run the normal command:

```powershell
.\Start-Bap.ps1 -Runtime Docker
```

It creates:

- `bap-mysql-local` using the official MySQL 8.4 image;
- a private `bap-local-docker` or `bap-local-podman` network;
- persistent Docker database files under `.bap/runtime/docker/mysql`;
- a Linux-native `bap-mysql-local-data` named volume when using Podman Desktop,
  because MySQL cannot safely initialize its permission-sensitive files on a
  Windows bind mount exposed through the Podman WSL VM;
- generated root/application passwords under the selected engine's
  `.bap/runtime/<engine>` directory;
- `bap-service-local` connected to MySQL without publishing port 3306.

Only BAP HTTPS port 8443 is published to the workstation. Check both process
liveness and database-backed readiness:

```powershell
docker ps --filter "name=bap-"
curl.exe --ssl-no-revoke `
  --cacert .\.bap\runtime\docker\dev-ca.pem `
  https://127.0.0.1:8443/healthz
curl.exe --ssl-no-revoke `
  --cacert .\.bap\runtime\docker\dev-ca.pem `
  https://127.0.0.1:8443/readyz
```

Expected readiness:

```json
{"status":"ready"}
```

For Podman, substitute `podman` and `.bap/runtime/podman` in the commands. The
Podman database volume survives `Stop-Bap.ps1`; inspect it with:

```powershell
podman volume inspect bap-mysql-local-data
```

Run the database outage exercise:

```powershell
.\Test-DatabaseFailure.ps1 -Runtime Docker
```

It must prove readiness returns 503, a fresh evaluation is not authorized, and
readiness recovers after MySQL restarts.

## Data model and guarantees

| Table | Purpose |
|---|---|
| `bap_schema_migrations` | Applied schema versions |
| `bap_audit_chain` | Transactionally locked current chain head |
| `bap_audit_events` | Append-only signed/hash-chained events plus indexed correlation fields |
| `bap_policy_proposals` | Sanitized deduplicated proposals and occurrence counts |

Each audit transaction locks the single chain-head row, checks event
idempotency, signs the event, inserts it, advances the chain head, and commits.
The service returns an authorization decision only after that commit succeeds.
Outcome and cached-grant correlation queries use the complete identifier values;
bounded index prefixes only reduce MySQL index size and never replace full-value
comparison.

## Switch a development service to enterprise MySQL

The tested server family is MySQL 8.4 LTS with InnoDB and `utf8mb4`. Ask the DBA
to create a dedicated schema and service account. Restrict the account host to
the BAP Service network rather than `%`:

```sql
CREATE DATABASE bap
  CHARACTER SET utf8mb4
  COLLATE utf8mb4_0900_ai_ci;

CREATE USER 'bap_service'@'10.%'
  IDENTIFIED BY 'REPLACE_WITH_A_RANDOM_SECRET';

GRANT SELECT, INSERT, UPDATE, CREATE, ALTER, INDEX
  ON bap.* TO 'bap_service'@'10.%';
```

The current service performs forward-only startup migrations, so its account
needs schema-change privileges. A separate migrator identity is a post-pilot
hardening item; after it exists, remove `CREATE`, `ALTER`, and `INDEX` from the
runtime identity.

Create a protected DSN file containing one line:

```text
bap_service:REPLACE_WITH_URL_SAFE_SECRET@tcp(mysql.company.example:3306)/bap?charset=utf8mb4&parseTime=true&loc=UTC&timeout=3s&readTimeout=5s&writeTimeout=5s
```

For a company/private CA, start the locally built service against the network
database with:

```powershell
.\Start-Bap.ps1 `
  -Runtime Docker `
  -DatabaseDsnFile 'C:\approved\bap\mysql-dsn.txt' `
  -DatabaseCaPath 'C:\approved\bap\mysql-ca.pem' `
  -DatabaseTlsServerName 'mysql.company.example'
```

The script mounts both files read-only. BAP registers a TLS 1.2-or-newer MySQL
client configuration, validates the server name, and refuses an unencrypted
network database. If the MySQL certificate chains to roots already trusted by
the service container, put `tls=true` in the DSN and omit the CA parameters.
`tls=skip-verify` and `tls=preferred` are rejected unless the explicit local
development override is enabled.

Verify the switch:

```powershell
curl.exe --ssl-no-revoke `
  --cacert .\.bap\runtime\docker\dev-ca.pem `
  https://127.0.0.1:8443/readyz
.\Test-Bap.ps1 -Runtime Docker
.\View-AuditTrail.ps1 -Runtime Docker -VerifyOnly
docker logs --tail 100 bap-service-local
```

The service log must contain `MySQL storage initialized` and must not contain
the JSONL fallback warning.

## Company container deployment

Mount the DSN, database CA, TLS/grant/audit keys, and Edge credential from the
company secret manager. A representative Podman/Docker fragment is:

```sh
-v /etc/bap-service/mysql-dsn:/run/secrets/bap-mysql-dsn:ro \
-v /etc/bap-service/mysql-ca.pem:/run/secrets/bap-mysql-ca.pem:ro \
-e BAP_DATABASE_DSN_FILE=/run/secrets/bap-mysql-dsn \
-e BAP_DATABASE_TLS_CA_PATH=/run/secrets/bap-mysql-ca.pem \
-e BAP_DATABASE_TLS_SERVER_NAME=mysql.company.example \
-e BAP_DATABASE_MAX_OPEN_CONNECTIONS=20 \
-e BAP_DATABASE_MAX_IDLE_CONNECTIONS=10 \
-e BAP_DATABASE_CONNECTION_MAX_LIFETIME_SECONDS=300
```

Do not place the DSN/password in Git, an image layer, shell history, or BAP
configuration delivered to developers. In Kubernetes, mount it from a Secret or
external secret provider and use `/readyz` for readiness; keep `/healthz` as
liveness.

## Cutover, backup, and rotation

- JSONL history is not automatically imported. Verify and archive the old chain
  before cutover; the MySQL chain begins independently.
- Back up the MySQL schema with point-in-time recovery and perform a restore
  exercise before onboarding pilot users.
- Export events and external chain-head checkpoints to the company SIEM/WORM
  destination.
- Rotate database credentials by creating a second account/secret, updating the
  mounted DSN, restarting BAP Service, checking `/readyz`, and only then revoking
  the previous account.
- Do not respond to a MySQL outage by silently removing the DSN and falling back
  to JSONL. Restore the database or fail closed so audit authority does not split.

The database stores security evidence and proposal workflow data. Cedar policy
source remains reviewed/versioned code until the governed policy-bundle API is
implemented.
