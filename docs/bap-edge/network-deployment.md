# Network deployment with Podman

The local container proves the protocol. The intended company layout is one or
more BAP Service control-plane instances distributing rules to many endpoint
data planes over authenticated HTTPS.

## Build

From the repository root:

```sh
./Build-BapService.sh podman
podman tag bap-service:local registry.company.example/security/bap-service:0.1
podman push registry.company.example/security/bap-service:0.1
```

The same `Containerfile` works with Docker. It runs as non-root UID 10001 and
does not contain generated private keys.

`Start-BapService.sh` is a local HTTPS demonstration launcher. Production should
use the explicit Podman/systemd/Kubernetes configuration below with company PKI,
secret mounts, company MySQL, and a managed API credential.

## Required production inputs

- `/run/bap-tls/tls-cert.pem` and `/run/bap-tls/tls-key.pem` from company PKI;
- `/run/bap-signing/bundle-private.pem` and corresponding Edge public key;
- legacy grant and dedicated audit signing keys while migration APIs remain;
- company MySQL 8.4 with TLS, backup/restore, and a dedicated BAP schema/account;
- a mounted MySQL DSN secret and CA bundle;
- a dedicated audit signing key;
- a client CA and unique per-device mTLS certificate lifecycle;
- network firewall allowing edge clients to TCP 8443;
- a stable DNS name present in the server certificate.

Expose `/metrics` only to the approved Prometheus-compatible scraper through a
network policy, service mesh, or monitoring proxy. Forward Service JSON stdout
and endpoint Edge JSONL through approved log agents as described in
[end-to-end observability](observability.md).

Example Podman command:

```sh
podman run -d --name bap-service \
  --read-only \
  --user 10001:10001 \
  -p 8443:8443 \
  -v /etc/bap-service/tls:/run/bap-tls:ro,Z \
  -v /etc/bap-service/signing:/run/bap-signing:ro,Z \
  -v /etc/bap-service/client-ca.pem:/run/bap-client-ca.pem:ro,Z \
  -v /etc/bap-service/mysql-dsn:/run/secrets/bap-mysql-dsn:ro,Z \
  -v /etc/bap-service/mysql-ca.pem:/run/secrets/bap-mysql-ca.pem:ro,Z \
  -v bap-runtime:/var/lib/bap:Z \
  -e BAP_LISTEN_ADDRESS=:8443 \
  -e BAP_TLS_CERT_PATH=/run/bap-tls/tls-cert.pem \
  -e BAP_TLS_KEY_PATH=/run/bap-tls/tls-key.pem \
  -e BAP_BUNDLE_PRIVATE_KEY_PATH=/run/bap-signing/bundle-private.pem \
  -e BAP_BUNDLE_PUBLIC_KEY_PATH=/run/bap-signing/bundle-public.pem \
  -e BAP_GRANT_PRIVATE_KEY_PATH=/run/bap-signing/grant-private.pem \
  -e BAP_GRANT_PUBLIC_KEY_PATH=/run/bap-signing/grant-public.pem \
  -e BAP_AUDIT_PRIVATE_KEY_PATH=/run/bap-signing/audit-private.pem \
  -e BAP_AUDIT_PUBLIC_KEY_PATH=/run/bap-signing/audit-public.pem \
  -e BAP_CLIENT_CA_PATH=/run/bap-client-ca.pem \
  -e BAP_DATABASE_DSN_FILE=/run/secrets/bap-mysql-dsn \
  -e BAP_DATABASE_TLS_CA_PATH=/run/secrets/bap-mysql-ca.pem \
  -e BAP_DATABASE_TLS_SERVER_NAME=mysql.company.example \
  registry.company.example/security/bap-service:0.1
```

Do not set `BAP_DEVELOPMENT_TLS=true` in production. The service refuses to
invent a missing signing key during normal startup.

## Current authentication boundary

HTTPS authenticates BAP Service to Edge. With `BAP_CLIENT_CA_PATH`, the service
requires a verified client certificate and derives principal/fingerprint from
it. The pilot still needs enrollment, revocation, rotation, and preferably
TPM-backed non-exportable keys. The bearer credential is for local development.

MySQL is the audit/proposal system of record. Keep `/var/lib/bap` only for local
key/runtime needs, export signed events plus chain checkpoints to the company
SIEM, and keep audit/bundle/legacy-grant private keys on read-only secret mounts. Follow the
[enterprise MySQL procedure](storage.md) for schema grants, DSN/TLS configuration,
cutover, validation, backup, and rotation.
