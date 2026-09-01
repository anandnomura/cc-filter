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
| `.bap/runtime/<engine>/grant-public.pem` | No | BAP Edge verifies one-use AgentGrants |
| `.bap/runtime/<engine>/grant-private.pem` | **Yes** | BAP Service signs grants |
| `.bap/runtime/<engine>/bundle-public.pem` | No | BAP Edge verifies signed policy bundles |
| `.bap/runtime/<engine>/bundle-private.pem` | **Yes** | BAP Service signs policy bundles |
| `.bap/runtime/<engine>/audit-public.pem` | No | Operators verify the audit chain |
| `.bap/runtime/<engine>/audit-private.pem` | **Yes** | BAP Service signs audit events |
| `.bap/runtime/<engine>/edge-api-key.txt` | **Yes** | Local-only BAP Edge bearer credential |

`<engine>` is `docker` or `podman`. Separate directories avoid incompatible
Windows VM file ownership. `.bap/runtime` is excluded from Git and the OCI build
context. Never commit,
email, or copy the private-key/API-key files outside their intended hosts in a
network deployment.

Development initialization uses an ECDSA P-256 CA/server certificate for broad
Windows, Podman, and Docker compatibility. Legacy grants, policy bundles, and
audit events use separate Ed25519 keys.

## Company network deployment

Use certificates issued by company PKI rather than development certificates.
Mount TLS, bundle-signing, and audit private keys into BAP Service as protected
read-only secrets. Distribute these public artifacts to Edge administrators:

1. Company CA bundle for HTTPS server verification.
2. BAP bundle public key for signed rule verification.

The audit public key goes to audit verifiers/SIEM tooling, not necessarily every
Edge. For the pilot, provision a unique mTLS client certificate per device and
set `BAP_CLIENT_CA_PATH`; bearer keys remain local-development compatibility.

Install a network edge with:

```powershell
.\Install-ManagedSettings.ps1 `
  -ServiceUrl 'https://bap.company.example' `
  -GrantPublicKeyPath 'C:\staging\grant-public.pem' `
  -BundlePublicKeyPath 'C:\staging\bundle-public.pem' `
  -CaBundlePath 'C:\staging\company-ca.pem' `
  -ClientCertificatePath 'C:\staging\device-cert.pem' `
  -ClientKeyPath 'C:\staging\device-key.pem'
```

The installer copies both public artifacts to administrator-owned Program Files;
the managed hook never trusts a user-controlled path.

## Rotation

Development certificates are recreated automatically when the server
certificate has less than 24 hours remaining. For production, schedule a
maintenance window: deploy the new public verification material to edges, then
switch the relevant service signing key. The current implementation supports one
active bundle key, one legacy grant key, and one audit key per service instance,
so overlapping signing-key rotation still requires a coordinated rollout.
