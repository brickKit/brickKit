# Calling a Dependency's Address Reliably

AGENTS.md §5.1 promises that a dependency's address is stable and DNS-based: `http://<versioned-service-name>:<port>`, resolved by the underlying engine's own DNS, never by anything BrickKit runs itself. That promise is about the *name* — it says nothing about what your HTTP client does with that name once your process has already opened a connection through it, and the container behind it gets replaced (a redeploy, a crash restart, a rolling update). This document is an answer to that question, checked for real rather than assumed: does an ordinary, unmodified HTTP client in each of the three languages this repo's own guides and templates use — Go, Python, Node.js — survive a dependency's container being replaced out from under an already-open connection?

## The experiment

A `docker compose` project with one backend service (`traefik/whoami`, which just echoes its own container hostname — useful for telling "the old container" and "the new container" apart in the response body) and one client per language, each issuing a request to `http://backend/` every 500ms using nothing but that language's default persistent/pooled HTTP client — `net/http.Client` in Go, `requests.Session()` in Python, `http.Agent({keepAlive: true})` in Node.js. After roughly 25 requests had already succeeded against the first container, the backend was force-recreated (removed and started again as a brand-new container, on a genuinely different IP — confirmed with `docker network inspect`, not assumed), while the clients kept polling on their own schedule, unaware anything had changed. This mirrors what a real dependency call looks like during any redeploy — the caller never gets a signal that the callee is about to change out from under it, on either `docker` or `k8s`.

## What actually happened, per language

**Go — fails once, fast, then recovers on the very next request.** The one request that landed exactly during the swap failed immediately, in well under the 500ms polling interval — no hang:

```
[ 25] t=21:31:55.147 status=200 took=632.03µs body="Hostname: 8bda437b989b"
[ 26] t=21:31:55.648 ERROR: Get "http://backend/": dial tcp: lookup backend on 127.0.0.11:53: server misbehaving
[ 27] t=21:31:56.150 status=200 took=1.173938ms body="Hostname: ce0eda814117"
```

`127.0.0.11` is Docker's own embedded DNS server — the failure is a real DNS-resolution error, surfaced immediately as a Go `error` value, not a silent hang. The request on the next tick, 500ms later, already reaches the new container (`ce0eda814117`). This reproduced identically across two independent runs. Go's default `Transport` detects a dead pooled connection and dials fresh — including a fresh DNS lookup — the next time it's needed; nothing about this requires disabling keep-alives or writing custom retry logic to get this baseline behavior.

**Python (`requests.Session`) — no error at all, but a multi-second silent stall.** This is the one genuinely dangerous result. Every other request in the run took low single-digit milliseconds; the one that landed on the stale pooled connection took **2.506 seconds** and still returned `200`, with no exception raised anywhere:

```
[ 25] t=21:31:55 status=200 took=0.003s body='Hostname: 8bda437b989b'
[ 26] t=21:31:58 status=200 took=2.506s body='Hostname: ce0eda814117'
[ 27] t=21:31:58 status=200 took=0.001s body='Hostname: ce0eda814117'
```

Nothing in this call failed, logged a warning, or otherwise announced that anything unusual happened — the only trace is the `took=` timing, which nothing checks by default. `requests.Session()`'s underlying `urllib3` connection pool tried to reuse the now-dead socket, the write went into a connection whose peer had simply vanished (no FIN, no RST — the container's whole network namespace was gone), and the delay is consistent with the pool eventually giving up on that attempt and transparently opening a new one, DNS lookup included. The `timeout=2` argument in this test is what kept the stall to ~2.5 seconds rather than however long Linux's own TCP retransmission schedule would otherwise have taken (which defaults to considerably longer than 2 seconds) — **a `requests.Session()` call with no `timeout=` argument at all has no such ceiling.**

**Node.js (`http.Agent({keepAlive: true})`) — the stale request hangs until *your* timeout fires; nothing fires on its own.** Out of order in the log because Node's requests are async and the unlucky one didn't finish first:

```
[ 26] t=21:31:56 status=200 took=3ms body="Hostname: ce0eda814117"
[ 27] t=21:31:56 status=200 took=2ms body="Hostname: ce0eda814117"
[ 28] t=21:31:57 status=200 took=3ms body="Hostname: ce0eda814117"
[ 25] t=21:31:57 ERROR: timeout
[ 29] t=21:31:57 status=200 took=1ms body="Hostname: ce0eda814117"
```

Requests `26`–`29`, all issued *after* the swap, went through cleanly and immediately against the new container — the `Agent`'s pool opened fresh connections for them without any trouble. Request `25` was the one already in flight against the dying connection at the moment of the swap, and it never resolved on its own; it only surfaced as an error because this test's client code explicitly set a 2-second `timeout` on the request and called `req.destroy()` when that fired. **Without that explicit timeout, this exact request would have hung indefinitely** — Node's `http` module applies no default timeout to a request at all.

## What this means, concretely

**Every mainstream HTTP client survives a dependency's container being replaced — none of them get permanently stuck talking to a dead address.** AGENTS.md §5.1's stability promise holds: you don't need DNS-cache-busting tricks, connection-pool-disabling flags, or any BrickKit-specific client code to make ordinary service-to-service calls work across a redeploy. The versioned service name keeps resolving correctly on the very next attempt in every language tested here.

**But "eventually recovers" and "your caller doesn't notice anything went wrong" are different guarantees, and only Go's default behavior gives you both for free.** The concrete, testable, language-specific advice that follows from this:

- **Always set an explicit request-level timeout, in every language, on every call to a dependency.** This is the one piece of advice this experiment actually validates rather than merely asserts: it was the `timeout=2` on the Python call and the `timeout: 2000` on the Node call — not anything about the platform, and not anything default to those libraries — that turned an open-ended hang into a bounded few seconds. Go's `http.Client{Timeout: ...}` should be set explicitly too, even though this particular failure mode happened not to need it.
- **Treat a failed or slow call to a dependency as an ordinary, retryable business-logic event**, not a fatal error. A single retry with a short backoff absorbs exactly the one-request blip seen in every language above. This is deliberately your code's responsibility and not something BrickKit does for you — AGENTS.md §9.7 explains why the platform stays out of circuit-breaking, retries, and backoff strategy entirely: what counts as an acceptable retry policy is a business decision that differs by call, and a platform-wide default would be wrong for most of them.
- **A slow-but-successful call is a real production risk, not just a Python quirk.** The 2.5-second stall happened with zero errors and zero log lines pointing at it. If your own service has a caller with a tighter timeout than yours, that caller sees a failure with no explanation anywhere in *your* logs. Whatever timeout you set on a call to a dependency should be shorter than the timeout your own callers are willing to wait on you.

## Docker vs. Kubernetes: this experiment used Compose, and that matters

This test ran entirely under `deploy.target: docker`, where Docker's embedded DNS server (`127.0.0.11`) genuinely re-resolves `backend` to a new IP address every time the container behind it changes — which is exactly the mechanism that produced the Go DNS error and the stale-connection stalls above.

Under `deploy.target: k8s`, the mechanism is different in a way that removes one failure mode entirely: a Kubernetes `Service`'s `ClusterIP` is stable for the Service's whole lifetime — it's `kube-proxy`'s routing rules (iptables or IPVS), sitting *below* DNS, that redirect traffic to whichever Pod is currently healthy. A client's already-resolved IP (the Service's ClusterIP, not any individual Pod's) never goes stale the way this experiment's Compose IP did, so there's no equivalent of the Go client's one DNS-lookup failure. What's still identical to what this experiment measured: the *TCP connection itself* still dies the moment the old Pod's process exits, exactly as it did here — so the pooled-connection stalls seen in Python and Node.js are not Docker-specific, and the same guidance (explicit timeouts, treat failures as retryable) applies unchanged on `k8s`.

## Read further

- AGENTS.md §5.1 — the versioned-service-name and address-format guarantee this document tests empirically
- AGENTS.md §9.1, §9.7 — why there's no platform-run registry and no platform-run retry/circuit-breaking logic; both are deliberately your code's job
- [environment-variables.md](../architecture/environment-variables.md) — exactly how a dependency's address lands in your environment as `{PREFIX}_ENDPOINT` in the first place
- [shared-connection-pools.md](shared-connection-pools.md) — the connection-pooling questions that come up once several components share one process inside a `servedBy` shell, a different problem from this document's single-component-calling-another-component case
