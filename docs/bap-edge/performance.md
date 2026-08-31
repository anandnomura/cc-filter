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
policy synchronization requests, reports throughput/p50/p95/p99, then measures
complete BAP Edge hook processes making local signed-policy decisions. Audit
delivery remains enabled.

## Historical baseline from the former per-operation service path

The 2026-08-29 numbers predate local traffic decisions. Retain them only for
comparison; rerun `Performance-Test-Bap.ps1` to establish policy-sync and local
decision baselines for the current architecture.

| Path | Load | Failures | Result |
|---|---|---:|---|
| BAP Service + MySQL | 200 requests, concurrency 10 | 0 | 90.67 req/s; p50 103.22 ms; p95 142.60 ms; p99 170.85 ms |
| Full cold Edge hook | 20 sequential new operations | 0 | p50 80.36 ms; p95 115.11 ms |

This is a functional pilot baseline from one workstation, not a sizing result.
Run a longer soak against the enterprise topology before setting an SLO or
capacity target.

The following is the historical 2026-08-28 JSONL baseline. It predates the MySQL
pilot store and must not be used as the MySQL capacity result:

| Path | Load | Failures | Result |
|---|---|---:|---|
| BAP Service | 200 requests, concurrency 10 | 0 | 57.48 req/s; p50 168.82 ms; p95 183.51 ms; p99 232.14 ms |
| BAP Service | 500 requests, concurrency 25 | 0 | 58.56 req/s; p50 423.23 ms; p95 450.78 ms; p99 525.71 ms |
| Full cold Edge hook | 100 sequential new operations | 0 | p50 84.12 ms; p95 92.96 ms |

These are development measurements, not a capacity guarantee. The former flat
synchronous audit writer reached roughly 58 decisions/second on that storage.
A sustained MySQL load/soak result is still required. A 10-sample
Edge run also showed a 1.08-second p95
outlier, which is why stable conclusions use the 100-sample result and production
tests must cover endpoint security/AV behavior.

## Interpreting local-decision performance

While a bundle is fresh, the decision itself performs no control-plane round
trip. Edge still durably spools the audit record and attempts asynchronous
delivery. Separate sync and Edge measurements distinguish control-plane rollout
capacity from endpoint decision latency.

## Production load gate

Define SLOs before declaring readiness—for example expected peak decisions per
second, p95/p99 authorization latency, maximum audit acknowledgement time, and
outcome backlog recovery time. Test at least 2x expected peak for a sustained
period, with realistic policy size, network latency, audit retention volume,
key service, multiple principals, cache mix, node failure, audit-backend failure,
and certificate/key rotation. Require zero unauthorized allows and verify the
audit chain/checkpoint after every run.
