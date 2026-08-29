# Certificates and key material

Run the idempotent initializer before first startup:

```bat
Initialize-Certificates.bat -Runtime Docker
```

or:

```bat
Initialize-Certificates.bat -Runtime Podman
```

Linux operators can run `scripts/initialize-certificates.sh`.

## Generated development files

| File | Secret? | Used by |
|---|---|---|
| `.bap/runtime/<engine>/dev-ca.pem` | No | BAP Edge trusts the local HTTPS server certificate |
| `.bap/runtime/<engine>/tls-cert.pem` | No | BAP Service HTTPS endpoint |
| `.bap/runtime/<engine>/tls-key.pem` | **Yes** | BAP Service only |
| `.bap/runtime/<engine>/grant-public.pem` | No | BAP Edge verifies grants |
| `.bap/runtime/<engine>/grant-private.pem` | **Yes** | BAP Service signs grants |
| `.bap/runtime/<engine>/audit-public.pem` | No | Operators verify the audit chain |
| `.bap/runtime/<engine>/audit-private.pem` | **Yes** | BAP Service signs audit events |
| `.bap/runtime/<engine>/edge-api-key.txt` | **Yes** | Local-only BAP Edge bearer credential |

`<engine>` is `docker` or `podman`. Separate directories avoid incompatible
Windows VM file ownership. `.bap/runtime` is excluded from Git and the OCI build
context. Never commit,
email, or copy the private-key/API-key files outside their intended hosts in a
network deployment.

Development initialization uses an ECDSA P-256 CA/server certificate for broad
Windows, Podman, and Docker compatibility. Grants and audit events use two
different Ed25519 keys. Separating all three purposes limits key compromise.

## Company network deployment

Use certificates issued by company PKI rather than development certificates.
Mount the TLS private key and grant signing private key into the BAP Service
container as read-only secrets. Distribute only these two public artifacts to
BAP Edge administrators:

1. Company CA bundle for HTTPS server verification.
2. BAP grant public key for authorization-grant verification.

The audit public key goes to audit verifiers/SIEM tooling, not necessarily every
Edge. Provision a unique BAP API key per user/device and its server-side
principal mapping through your secret-management system.

Install a network edge with:

```powershell
.\Install-ManagedSettings.ps1 `
  -ServiceUrl 'https://bap.company.example' `
  -GrantPublicKeyPath 'C:\staging\grant-public.pem' `
  -CaBundlePath 'C:\staging\company-ca.pem' `
  -ApiKey $env:PROVISIONED_BAP_EDGE_API_KEY
```

The installer copies both public artifacts to administrator-owned Program Files;
the managed hook never trusts a user-controlled path.

## Rotation

Development certificates are recreated automatically when the server
certificate has less than 24 hours remaining. For production, schedule a
maintenance window: deploy the new public verification material to edges, then
switch the relevant service signing key. The current implementation supports one
active grant key, one audit key, and one Edge API credential per service
instance, so overlapping rotation requires a coordinated rollout.
