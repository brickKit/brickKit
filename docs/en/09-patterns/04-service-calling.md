# Calling dependencies reliably

A dependency's address is stable: `http://<versioned service name>:<port>`, like `http://demo-hello-1-0-0:8080`,
resolved by Docker's or Kubernetes' own DNS, with no registry run by the platform. That promise is about **the name
itself**. It doesn't answer another question: once your process has connected through that name and the container behind
it is replaced (a redeploy, a crash and restart, a rolling update), what does your HTTP client do next?

This page answers it with a real experiment: can the default HTTP clients of Go, Python and Node.js survive the
dependency's container being replaced behind an established connection?

## How the experiment was done

One `docker compose` project: a backend (`traefik/whoami`, which only echoes its own container's hostname — just right
for telling whether a response came from the old container or the new one), and one client per language sending a
request to `http://backend/` every 500 ms, each using its language's default keep-alive client: Go's `net/http.Client`,
Python's `requests.Session()`, Node.js's `http.Agent({keepAlive: true})`.

After about 25 requests had all hit the first container, the backend was force-recreated — deleted, and a brand-new
container started on a genuinely different IP (verified with `docker network inspect`) — while the clients kept polling
without knowing anything had happened behind them. That's exactly what a redeploy looks like in a real deployment: the
caller never gets a signal that "the other side is about to be replaced".

## DNS caching and connection reuse: what happened in each language

**Go: fails once, at once, and recovers on the next try.** The request that landed exactly at the switch failed
immediately, without hanging:

```text
[ 25] t=21:31:55.147 status=200 took=632.03µs body="Hostname: 8bda437b989b"
[ 26] t=21:31:55.648 ERROR: Get "http://backend/": dial tcp: lookup backend on 127.0.0.11:53: server misbehaving
[ 27] t=21:31:56.150 status=200 took=1.173938ms body="Hostname: ce0eda814117"
```

`127.0.0.11` is Docker's built-in DNS server — this is a real DNS resolution failure, surfacing at once as an `error`. The
next request 500 ms later already hit the new container. Two independent runs gave the same result. Go's default
`Transport` notices the pooled connection died, and next time dials again and looks up DNS again — with no need to turn off
keep-alive, and no retry code of your own.

**Python (`requests.Session`): no error, but a silent 2.5-second stall.** This is the only really dangerous result in the
experiment. Every other request took single-digit milliseconds; the one that landed on the dead connection took
**2.506 seconds**, and still returned `200`, with no exception at all:

```text
[ 25] t=21:31:55 status=200 took=0.003s body='Hostname: 8bda437b989b'
[ 26] t=21:31:58 status=200 took=2.506s body='Hostname: ce0eda814117'
[ 27] t=21:31:58 status=200 took=0.001s body='Hostname: ce0eda814117'
```

No error, no warning; the only trace is the `took=` timing, which nothing looks at by default. `requests`' underlying
connection pool tried to reuse the dead socket, writing into a connection whose other end had vanished entirely (no FIN,
no RST — that container's network namespace was simply gone). It stopped at about 2.5 seconds because the experiment's
code set `timeout=2`; without it, Linux's own TCP retransmission would drag on far longer. **A `requests` call without
`timeout=` has no upper bound at all.**

**Node.js (`http.Agent({keepAlive: true})`): that one request hangs until your own timeout.** The log is out of order
because the requests are asynchronous:

```text
[ 26] t=21:31:56 status=200 took=3ms body="Hostname: ce0eda814117"
[ 27] t=21:31:56 status=200 took=2ms body="Hostname: ce0eda814117"
[ 28] t=21:31:57 status=200 took=3ms body="Hostname: ce0eda814117"
[ 25] t=21:31:57 ERROR: timeout
[ 29] t=21:31:57 status=200 took=1ms body="Hostname: ce0eda814117"
```

26–29 were sent after the switch and all hit the new container at once. 25 hung on the dying connection and never
recovered by itself; it finally failed only because the experiment's code explicitly set a 2-second timeout and called
`req.destroy()`. **Without a timeout, that request would hang indefinitely** — Node's `http` module has no default
per-request timeout.

## What this means for your code

**All three clients survived the dependency being replaced — none got stuck on the old address forever.** The promise of
stable addresses holds: no DNS-caching tricks, no disabling the connection pool, no BrickKit-specific client code. The
versioned service name resolved correctly again on the next attempt.

**But "recovers eventually" and "the caller doesn't notice at all" are two different things, and only Go's defaults give
both for free.** Concretely:

- **Set an explicit per-request timeout on every call to a dependency, in every language.** This is the one thing the
  experiment really verified: Python's `timeout=2` and Node's `timeout: 2000` turned an open-ended hang into a bounded few
  seconds. Go's `http.Client{Timeout: ...}` should be set too, even though this failure happened not to need it.
- **Treat a failed or slow call to a dependency as an ordinary, retryable event**, not a fatal error. One retry with a
  short back-off absorbs the single hiccup every language showed above. Retrying is your own code's responsibility: what a
  sensible retry looks like varies by call and is a business judgment; the platform stays out of circuit breaking, retries
  and back-off, because a uniform default would be wrong for most calls.
- **"Slow but successful" is a real production risk, not just a Python quirk.** Those 2.5 seconds had zero errors and zero
  logs. When your caller's timeout is shorter than that, it sees a failure, and your own logs hold no explanation. **The
  timeout you set on a dependency must be shorter than the time your caller is willing to wait for you.**

## On Kubernetes: which failure the ClusterIP erases

The experiment ran on Docker: when the container was replaced, Docker's built-in DNS really did resolve `backend` to a new
IP — the direct cause of Go's DNS error and of the other languages' stalled connections.

On Kubernetes the mechanism differs, and happens to erase one of the failures: a `Service`'s `ClusterIP` stays the same for
the Service's whole lifetime, and what actually forwards traffic is `kube-proxy`'s rules (iptables or IPVS), working below
DNS and steering traffic to Pods that are currently healthy. The client resolves the ClusterIP, not a Pod's IP, and that
doesn't go stale — so **Go's one DNS failure doesn't happen on K8s**.

**What isn't erased**: once the old Pod's process exits, **the TCP connection itself** still dies. The connection-pool
stalls of Python and Node.js aren't unique to Docker, and the same advice (explicit timeouts, retryable failures) applies
unchanged on K8s.

## Calling a member inside a shell

When a component you depend on is compiled into a shell, its versioned service name is a network alias of the shell's
container on Docker, and on K8s a Service selecting the shell's Pod — the calling code doesn't change at all, and the
conclusions above apply in full. The only difference: when the shell restarts, every member it hosts is unavailable at
once, and callers see one and the same hiccup. How a shell hands requests to its members inside is covered in
[Writing a shell](../04-shell/05-shell-development.md).

## Further reading

- [The environment variable contract](../06-architecture/03-env-injection-contract.md): how a dependency's address becomes
  the `*_ENDPOINT` in your environment
- [Design principles](../06-architecture/05-design-principles.md): why there's no registry, and why retries and circuit
  breaking are left to components
