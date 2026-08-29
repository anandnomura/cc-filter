# Performance testing and measured baseline

Run against a started local service:

```powershell
.\Performance-Test-Bap.ps1 `
  -Runtime Podman `
  -ServiceRequests 500 `
  -ServiceConcurrency 25 `
  -EdgeRequests 100
```

The script compiles a disposable Windows load client, sends authenticated HTTPS
AuthZEN evaluations, reports throughput/p50/p95/p99, then measures complete cold
BAP Edge hook processes. Every service evaluation still signs and synchronously
fsyncs an audit event; the test does not disable safety work to improve numbers.

## Baseline from this development workstation

Measured 2026-08-28 using the rootless Podman machine and its bind-mounted JSONL
audit volume:

| Path | Load | Failures | Result |
|---|---|---:|---|
| BAP Service | 200 requests, concurrency 10 | 0 | 57.48 req/s; p50 168.82 ms; p95 183.51 ms; p99 232.14 ms |
| BAP Service | 500 requests, concurrency 25 | 0 | 58.56 req/s; p50 423.23 ms; p95 450.78 ms; p99 525.71 ms |
| Full cold Edge hook | 100 sequential new operations | 0 | p50 84.12 ms; p95 92.96 ms |

These are development measurements, not a capacity guarantee. The flat
synchronous audit writer reaches roughly 58 decisions/second on this storage and
queues concurrent callers behind durable fsync. Cedar and grant signing are not
the observed bottleneck. A 10-sample Edge run also showed a 1.08-second p95
outlier, which is why stable conclusions use the 100-sample result and production
tests must cover endpoint security/AV behavior.

## Interpreting cache performance

An exact cached grant skips Cedar evaluation but still calls the service for a
durable, authenticated consumption event. It reduces policy computation and
response size; it intentionally does not eliminate the network/audit latency.

## Production load gate

Define SLOs before declaring readiness—for example expected peak decisions per
second, p95/p99 authorization latency, maximum audit acknowledgement time, and
outcome backlog recovery time. Test at least 2x expected peak for a sustained
period, with realistic policy size, network latency, audit retention volume,
key service, multiple principals, cache mix, node failure, audit-backend failure,
and certificate/key rotation. Require zero unauthorized allows and verify the
audit chain/checkpoint after every run.
