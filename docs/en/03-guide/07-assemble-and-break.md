# 7. Assemble a Real System, Then Break It on Purpose

Every article so far has stopped short of a real database — `demo/caller`'s resource dependency has been a warning to read, not something actually bound. This one closes that loop: `demo/hello` and `demo/caller` running against a real PostgreSQL, fully wired, then broken two different ways on purpose to see exactly what each failure actually looks like — not predicted from the source, observed.

## A real database, reachable from a container

```bash
docker run -d --name guide-pg -p 15432:5432 \
  -e POSTGRES_PASSWORD=devpass -e POSTGRES_DB=callerdb \
  postgres:16-alpine
```

Point `demo/caller`'s resource binding at it with `host.docker.internal` — Docker's own name for "the host machine, from inside a container" — at the port actually published on the host:

```yaml
resources:
  - kind: database
    engine: postgresql
    id: caller-db
    host: host.docker.internal
    port: 15432
    username: postgres
    password: ${DB_PASSWORD}
    bindings:
      - componentId: demo/caller
        database: callerdb
```

Writing a bare service-sounding name here instead (`host: guide-pg`, say — plausible if you're used to Compose services resolving each other by name) gets caught before it ever reaches Docker:

```
⚠️ A resource's host looks like a service name, which may not resolve inside the container
   Resource: caller-db
   host: guide-pg
   Reason: The platform doesn't deploy resources, so compose has no service by that name
   Suggestions:
   1. If the resource runs on this machine, write host: host.docker.internal (the platform adds extra_hosts automatically)
   2. If the resource runs elsewhere, write its IP or domain name
```

## Bring it up for real

```bash
export DB_PASSWORD=devpass
brickkit up
```

```
📌 These base resources have to be running first (the platform doesn't deploy them for you):
   caller-db                   postgresql   host.docker.internal:15432  used by demo/caller
🔧 Database migrations that run before startup (on failure that component won't start):
   demo/caller@1.0.0  /app/caller migrate
🐳 Starting (docker)...
   demo-hello-1-0-0             running (healthy)
   demo-caller-1-0-0            running (healthy)
✅ All components started (2)
```

The migration actually ran against the real database this time — no warning about it being unsatisfied, because it isn't anymore. Confirm the whole chain end to end:

```bash
curl http://localhost:8090/api/v1/call
```
```json
{"component":"demo/caller","endpoint":"http://demo-hello-1-0-0:8080","upstream":{"component":"demo/hello","greeting":"Hello","message":"Hello, I'm demo/hello@1.0.0","version":"1.0.0"},"version":"1.0.0"}
```

A live cross-container call, not a cached or fabricated response — `demo/caller` really reached `demo/hello` just now and is showing you what came back.

## Break it: stop the dependency it actually calls

```bash
docker stop brickkit-hello-world-demo-hello-1-0-0-1
curl -w "\nHTTP %{http_code}\n" http://localhost:8090/api/v1/call
```
```json
{"endpoint":"http://demo-hello-1-0-0:8080","error":"call to demo/hello failed: Get \"http://demo-hello-1-0-0:8080/api/v1/hello\": dial tcp: lookup demo-hello-1-0-0 on 127.0.0.11:53: server misbehaving"}
```
```
HTTP 502
```

A real `502`, with a real, specific error naming exactly which call failed and why — this is `demo/caller`'s own error-handling code, not anything the platform generated on its behalf; there's no communication-governance layer sitting in between that could have caught this and retried or degraded it for you (AGENTS.md §4.1). Now check `demo/caller` itself:

```bash
docker inspect brickkit-hello-world-demo-caller-1-0-0-1 --format '{{.State.Health.Status}}'
```
```
healthy
```

`demo/caller` is reporting **healthy** while the one thing it actually depends on is completely down — not a bug, the entire point of "a health check only checks this process itself" (AGENTS.md §10). If `demo/caller`'s own `/healthz` pinged `demo/hello` to decide its own status, one dependency going down would get `demo/caller` restarted too, for no reason — the restart fixes nothing, since the problem was never in `demo/caller`'s own process.

```bash
docker start brickkit-hello-world-demo-hello-1-0-0-1
curl http://localhost:8090/api/v1/call   # back to a normal response, no extra step required
```

## Break it differently: take down the database before startup

```bash
docker stop guide-pg
docker rm brickkit-hello-world-demo-caller-1-0-0-1
brickkit up
```

```
❌ Error: docker failed to run
   Command: docker compose ... up -d --wait --remove-orphans demo-hello-1-0-0 demo-caller-1-0-0
   Output: Container brickkit-hello-world-demo-caller-1-0-0-migration-1 Error
         service "demo-caller-1-0-0-migration" didn't complete successfully: exit 1
         Container brickkit-hello-world-demo-hello-1-0-0-1 Healthy
```

A completely different failure shape from the previous one, on purpose: this isn't a running component reporting an error at request time, it's the whole `up` command itself failing, with a non-zero exit — because the migration container couldn't reach the database, exited 1, and "a failed migration blocks the main service" (AGENTS.md §5.5) means `demo/caller` never gets created at all. `demo/hello`, which has no database dependency, comes up and stays healthy regardless — one component's resource failure doesn't reach a completely unrelated one.

## Clean up

```bash
brickkit down
docker rm -f guide-pg
```

---

Next: [Consume someone else's component](08-consuming-artifacts.md) — what a component publishes for others to discover and use, artifacts and API docs, from the consuming side.
