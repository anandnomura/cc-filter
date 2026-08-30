# Company internal-pilot MVP test

Use this runbook for the formal go/no-go decision in the company environment.
It assumes the [local laptop gate](local-laptop-mvp-test.md) already passes.
Passing on a developer-owned local container is not a substitute for this test.

## 1. Freeze the supported pilot scope

Record and approve:

- exact Claude Code version and approved Sonnet and Opus model versions;
- Windows version and endpoint-management baseline;
- enabled built-in tools, MCP servers/tools, plugins, shells, and delegation;
- pilot users, repositories, environments, and data classifications;
- excluded access paths such as direct database clients or unmediated tools;
- named security, policy, service, database, endpoint, and incident owners.

Every enabled tool requires a sanitized hook fixture and expected Cedar result.
Unknown or malformed operations must deny.

## 2. Deploy the company BAP Service and MySQL

Use company PKI and secret management. Create the enterprise database and DSN
as described in [MySQL storage and enterprise configuration](storage.md). For a
locally built service container connecting to company MySQL:

```powershell
.\Start-Bap.ps1 `
  -Runtime Docker `
  -DatabaseDsnFile 'C:\approved\bap\mysql-dsn.txt' `
  -DatabaseCaPath 'C:\approved\bap\mysql-ca.pem' `
  -DatabaseTlsServerName 'mysql.company.example'
```

In the real service platform, mount secrets read-only, use `/healthz` for
liveness and `/readyz` for readiness, and never expose the MySQL password in an
image, Git, endpoint configuration, or command history.

Verify from an approved administrative host:

```powershell
curl.exe --cacert C:\approved\bap\company-ca.pem https://bap.company.example:8443/healthz
curl.exe --cacert C:\approved\bap\company-ca.pem https://bap.company.example:8443/readyz
.\Test-Bap.ps1 -Runtime Docker
.\View-AuditTrail.ps1 -Runtime Docker -VerifyOnly
```

Adapt the last two commands to execute against the deployed service container.
The logs must say `MySQL storage initialized` and must not use JSONL fallback.

## 3. Prove database operations

With the DBA and service owner, demonstrate:

1. encrypted, server-name-validated BAP-to-MySQL connectivity;
2. database/network outage makes `/readyz` return 503 and denies new decisions;
3. recovery restores readiness without breaking the signed audit chain;
4. point-in-time backup and restore meet the agreed RPO and RTO;
5. restored events and the chain head pass signature/hash verification;
6. retention and legal-hold behavior;
7. external chain-head checkpoint and SIEM/WORM export;
8. database credential rotation without an allow-all or JSONL window.

Do not run `Test-DatabaseFailure.ps1` expecting it to stop company MySQL; that
script controls only the local `bap-mysql-local` container.

## 4. Deploy and test managed Windows endpoints

IT installs the signed Edge artifact, company CA, grant public key, unique pilot
credential, configuration, and managed settings. A representative installation
is:

```powershell
.\Install-ManagedSettings.ps1 `
  -ServiceUrl 'https://bap.company.example:8443' `
  -EdgeBinaryPath '.\dist\bap-edge-windows-amd64.exe' `
  -GrantPublicKeyPath 'C:\approved\bap\grant-public.pem' `
  -CaBundlePath 'C:\approved\bap\company-ca.pem' `
  -ApiKey $env:PROVISIONED_BAP_EDGE_API_KEY
```

On at least two endpoints, a standard non-administrator user runs:

```powershell
.\Test-ManagedSettings.ps1
curl.exe --cacert C:\approved\bap\company-ca.pem https://bap.company.example:8443/readyz
claude
```

Prove that the user cannot alter the Program Files Edge binary, configuration,
or managed settings; cannot select bypass mode; and cannot replace the approved
Claude or Edge executable because application control blocks it. `/hooks` may
show zero and is not enforcement evidence.

## 5. Run the Sonnet/Opus policy certification corpus

Run the same operation fixtures with every approved Sonnet and Opus option.
Require identical authorization for equivalent normalized operations. Cover:

- safe reads and writes within approved workspaces;
- secrets and protected paths;
- workspace escape and path normalization variants;
- destructive Git/filesystem commands;
- privilege, persistence, security-control, and exfiltration commands;
- network destinations and production mutation;
- every enabled MCP server/tool and plugin;
- notebooks, delegation/subagents, malformed inputs, and unknown tools;
- grant replay, expiry, cache corruption, service timeout, and outcome retry.

Explicit forbids must never create bypass proposals. No unexpected allow is
acceptable.

## 6. Identity and credential gate

For each endpoint, prove provisioning, expiration, rotation overlap, revocation,
offboarding, and audit attribution. Revoking endpoint A must not interrupt
endpoint B, and a revoked credential must not evaluate or consume a grant.

SPIFFE may be deferred for this bounded pilot only through documented risk
acceptance. The current shared bearer key does not satisfy per-device revocation
and remains a go-live blocker unless the approving authority explicitly accepts
that narrowed risk and the pilot scope prevents credential tampering.

## 7. Observability and operational failure tests

Require privacy-safe logs, metrics, dashboards, alerts, and trace correlation
for decision latency, allow/deny reason, authentication failure, policy version,
audit failure, database readiness, cache source, and Edge outcome-spool age.
Prove alerting for service outage, slow MySQL, capacity exhaustion, certificate
expiry, key failure, and audit corruption. Prompt text, plaintext commands,
credentials, file contents, and disallowed absolute paths must not leak.

Run sustained load at twice expected pilot peak and record zero unauthorized
allows, p95/p99 latency, throughput, resource use, and audit verification. Test
graceful restart, network partition, slow storage, disk full, certificate/key
rotation, and restore.

## 8. Release, UI, and governance gate

Before go-live, retain evidence for:

- signed Windows and OCI artifacts, checksums, SBOM, vulnerability scan, and
  protected release provenance;
- policy bundle version, positive/negative tests, author, approver, staging,
  activation, and exercised rollback;
- separately authenticated admin/read APIs and role tests;
- operator UI for audit search, correlated decision/outcome investigation, and
  proposal review, without audit update/delete capability;
- proposal workflow that never activates policy automatically;
- SLO, RTO/RPO, on-call ownership, incident/break-glass runbooks, and drill;
- threat-model review, bypass corpus, parser fuzzing, and independent pilot
  security assessment.

## Go/no-go decision

Use the release-gate table in the [MVP readiness ledger](mvp-roadmap.md). Every
gate needs a named owner, dated evidence, and pass result. A deviation needs a
named approving authority, expiry date, compensating controls, and documented
residual risk. Any unexpected allow, silent audit fallback, unverifiable audit
chain, bypassable endpoint control, or unowned critical alert is an automatic
**no-go**.
