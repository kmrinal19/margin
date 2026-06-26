---
title: "RFC: Edge Rate Limiting"
date: 2026-06-26
status: in review
lead: A proposal to move per-client rate limiting from the application tier to the edge, cutting wasted origin work and giving abusive clients a fast, cheap "no".
---

We currently rate-limit inside each service, after authentication and routing.
That means a flood of abusive requests still pays for TLS termination, auth, and
a round-trip to the limiter before we say no. This RFC proposes enforcing limits
at the edge proxy instead, so rejected traffic never reaches origin.

## Background

Every service embeds the same token-bucket middleware backed by a shared Redis.
Under a recent credential-stuffing burst, the limiter itself became the
bottleneck: the auth service spent 40% of its CPU rejecting requests it should
never have seen. The limit was working — it was just enforced too late.

:::info
"Edge" here means the L7 proxy fleet that already terminates TLS and routes by
host/path. We are not introducing a new hop; we are moving an existing check
earlier in the one we already have.
:::

## Goals

- Reject over-limit requests **before** auth/routing, at the edge.
- Keep the limit decision **consistent** across the proxy fleet (no per-node drift).
- Add **no more than 1ms p99** to the allowed-request path.
- Make limits **configurable per route** without a redeploy.

## Non-goals

- Replacing application-tier quotas (billing/plan limits stay in the services).
- Global exactly-once accounting — we accept small over-admission during failover.
- DDoS protection at L3/L4 (that stays with the network provider).

## Design

### Token bucket, evaluated at the edge

Each proxy node keeps an in-process bucket per `(client_id, route)` and
asynchronously reconciles with a shared store. The hot path touches only local
memory; reconciliation is best-effort and off the request path.

```go
// allow reports whether one token is available, refilling lazily.
func (b *Bucket) allow(now time.Time) bool {
    elapsed := now.Sub(b.last).Seconds()
    b.tokens = min(b.cap, b.tokens+elapsed*b.refill)
    b.last = now
    if b.tokens < 1 {
        return false
    }
    b.tokens--
    return true
}
```

:::warn
A purely local bucket lets a client burst up to `N × cap` across `N` nodes. We
bound this by sizing local caps to `cap/N` and letting reconciliation lend
capacity — see "Open questions" for whether that bound is tight enough.
:::

### Storage

Reconciliation state lives in a single Redis hash per client, read on a timer,
never on the request path:

<div class="schema">
  <div class="schema-head">ratelimit:&lt;client_id&gt; — Redis hash</div>
  <div class="schema-row"><span>tokens</span><span class="f-type">float</span><span class="f-note">tokens remaining in the global bucket</span></div>
  <div class="schema-row"><span>updated_at</span><span class="f-type">int64</span><span class="f-note">epoch ms of the last reconcile</span></div>
  <div class="schema-row"><span>route</span><span class="f-type">string</span><span class="f-note">route key the limit applies to</span></div>
</div>

### Limit configuration

Limits are pushed to proxies as a small table, hot-reloaded on change:

| Route            | Limit (req/s) | Burst | Scope        |
|------------------|---------------|-------|--------------|
| `POST /login`    | 5             | 10    | per client   |
| `GET /search`    | 50            | 100   | per client   |
| `POST /api/*`    | 200           | 400   | per API key  |
| `GET /assets/*`  | —             | —     | unlimited    |

## API

Rejected requests get a `429` with the standard hint headers so well-behaved
clients can back off:

```json
{
  "error": "rate_limited",
  "retry_after_ms": 850,
  "limit": 50,
  "scope": "client"
}
```

:::good
Returning `Retry-After` and `RateLimit-*` headers means most SDKs back off
automatically — we saw a 60% drop in retry storms in the staging trial.
:::

## Rollout

- [x] Prototype the local bucket + reconciler on one proxy node.
- [x] Shadow-mode in staging (decide, log, but don't enforce).
- [ ] Enforce on `POST /login` only, 5% of traffic.
- [ ] Ramp to 100% of `/login`, then expand route coverage.
- [ ] Remove the application-tier limiter once edge enforcement is trusted.

:::danger
Do not enable enforcement and remove the app-tier limiter in the same change. If
edge enforcement misfires we must be able to fall back instantly to the existing
limiter without a deploy.
:::

## Open questions

1. Is `cap/N` local sizing too conservative for bursty-but-legitimate clients
   (e.g. a CI job that fans out)? Should we lend capacity faster?
2. What is the right reconcile interval — 100ms keeps drift low but adds Redis
   load; 1s halves the load but doubles worst-case over-admission.
3. Should the limit config be per-route, or per-route-**and**-method? `/api/*`
   probably wants different limits for reads vs writes.

> "Make the common path cheap and the abusive path cheaper." — the whole point of
> moving this to the edge.
