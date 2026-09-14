# Self-Hosting the BrickKit Market

This document is about deploying **the marketplace** — the independent SaaS
service that answers "what's available to install, who's allowed to install
it" (AGENTS.md §5.9). That's a different job from deploying **your own
project** (the components you've assembled with `brickkit`), which is
covered by the platform's own `deploy.target: docker | k8s` (AGENTS.md
§5.5) and needs no separate document — it's just `brickkit up`. The market
gets deployed once, by whoever runs it for a team or a company; a project
gets deployed by every user of that project, every time.

## Three ways to run it, one image

| Mode | Runs where | Fits | Data lives |
| --- | --- | --- | --- |
| Local development | Your own laptop | Trying the platform, working on the market itself, offline demos | A local directory |
| Self-hosted, single machine | One server on your network | Most teams — private components never leave the network | That machine's disk |
| Cloud | A cloud VM or container platform + managed database and object storage | Multi-region teams, high-availability requirements | Cloud-managed |

All three run the exact same image with the exact same configuration
surface — the only thing that changes is whether PostgreSQL and object
storage are containers you run yourself or managed services you point the
market at. The market process itself is stateless: metadata lives in
PostgreSQL, artifacts live in object storage, so the market can be rebuilt
or scaled out at any time without carrying any state of its own.

## Getting a single-machine deployment running

The compose file already exists in the repository at
`deploy/market/docker-compose.yaml`, alongside `deploy/market/.env.example`
— copy both, don't hand-write either:

```bash
cd deploy/market
cp .env.example .env
vi .env                      # at minimum, change POSTGRES_PASSWORD / RUSTFS_SECRET_KEY / ADMIN_PASSWORD
chmod 600 .env

docker compose up -d --build   # first run builds the market image from source
```

Or the repository-root shortcuts: `make market-up` / `make market-logs` /
`make market-down` (data preserved on `down`).

Four services come up, two of which exist specifically because of real
incidents — don't remove them:

| Service | What it does | Exposed? |
| --- | --- | --- |
| `postgres` | Metadata: components, versions, users, access policy | No — internal to the compose network only |
| `rustfs-init` | One-shot container that `chown`s `./data/rustfs` to uid 10001 before the real RustFS container starts. **Without it, RustFS crash-loops with `Permission denied (os error 13)`** — it runs as a non-root user inside the container, but a bind-mounted directory is root-owned by default | No — runs once and exits |
| `rustfs` | S3-compatible object storage for artifacts (proto files, OpenAPI specs, SDKs). Swappable for any S3-compatible service; the market only depends on the S3 protocol | Console only, optionally, on `RUSTFS_CONSOLE_PORT` |
| `market-api` | The market itself. Health check sends a `HEAD` via `wget --spider`; `start_period: 30s` gives first boot enough time to create tables and the bucket | Yes, on `MARKET_PORT` |

No separate bucket-initialization step exists — table creation, bucket
creation, and admin bootstrap all happen automatically on startup, and all
three are idempotent (restarting is always safe).

**Before your first `up`, get an image in place.** `MARKET_IMAGE` in
`.env.example` defaults to `brickkit/market-server:dev` — a purely local
tag that doesn't exist in any public registry. Running `docker compose up
-d` on a clean machine without building first fails with `pull access
denied`. Two paths:

- **Build from source (simplest for self-use):** run the build from the
  repository root, not from wherever you copied the compose file to — the
  compose file's `build.context` (`../../market-server`) is relative to
  the compose file's own location, and that relative path stops resolving
  once you've copied it elsewhere.
  ```bash
  docker build -t brickkit/market-server:dev --build-arg VERSION=dev market-server/
  ```
- **Push to your own registry (for deploying to more than one machine):**
  build with a real version tag, push it, and point `MARKET_IMAGE` at that
  reference instead. A `:dev`-style mutable tag can silently mean two
  different images on two different machines — don't rely on it past a
  single machine.
  ```bash
  docker build -t harbor.mycompany.com/brickkit/market-server:1.0.0 --build-arg VERSION=1.0.0 market-server/
  docker push harbor.mycompany.com/brickkit/market-server:1.0.0
  ```

Verify it's actually up:

```bash
curl http://localhost:8080/api/v1/health
# {"success":true,"data":{"status":"ok","version":"1.0.0","time":"..."}}
```

## The one environment-variable trap worth knowing before you start

**`docker compose` gives a host shell's already-exported environment
variable priority over the same name in `.env` — silently, with no error
anywhere.** If `~/.bashrc` exports `POSTGRES_PASSWORD` for some unrelated
reason, it overrides whatever you carefully set in `.env`. This was hit
for real while writing this guide: `.env` had a strong password, the host
shell happened to export `POSTGRES_PASSWORD=q`, and every container came
up healthy, every health check passed, registration and publishing worked
end to end — with the database password silently set to `q`. Nothing
anywhere flags this.

Check what's actually taking effect before you trust it:

```bash
docker compose config | grep -E "POSTGRES_(USER|PASSWORD)|RUSTFS_(ACCESS|SECRET)"
```

Whatever this prints is what's really in effect. If it doesn't match
`.env`, `unset` the host shell's variable and try again — editing `.env`
again does nothing, since the host environment wins regardless of what's
in the file.

## Admin account bootstrap

The admin account is (re-)provisioned on every startup from
`ADMIN_USERNAME`/`ADMIN_PASSWORD`, and the logic is idempotent and
deliberately conservative:

| State | What happens |
| --- | --- |
| Account doesn't exist | Created with `ADMIN_PASSWORD`, marked admin |
| Account exists, not admin | Admin privilege is granted, nothing else changes |
| Account exists, already admin | Nothing happens |

**The bootstrap never overwrites an existing account's password.**
Whoever operates the market has very likely changed it since first boot;
silently resetting it to whatever's still sitting in `.env` on every
restart would be a nasty thing to debug. To actually change it, use the
explicit reset path:

```bash
# 1. In .env: set the new password and flip the reset switch on
#    ADMIN_PASSWORD=<new strong password>
#    ADMIN_PASSWORD_RESET=true

docker compose up -d market-api

# 2. Confirm the reset happened
docker compose logs market-api | grep -i reset

# 3. Flip the switch back off, then restart once more
sed -i 's/^ADMIN_PASSWORD_RESET=true/ADMIN_PASSWORD_RESET=false/' .env
docker compose up -d market-api
```

The reset also revokes every token that account had already issued —
reaching for this path usually means the old credential is no longer
trusted, and leaving old tokens valid would defeat the point. Any
developer machine using that account needs to `brickkit login` again
afterward.

## RustFS won't start: `Permission denied (os error 13)`

RustFS runs as uid 10001 inside its container; a freshly bind-mounted host
directory is root-owned by default. `rustfs-init` exists specifically to
fix this before RustFS starts, so this shouldn't come up in the default
layout — it surfaces if you've pointed the volume at a different path:

```bash
docker run --rm -v /your/new/path:/data busybox chown -R 10001:10001 /data
docker compose up -d rustfs
```

The same ownership applies to cleanup: the `data/` directories are created
by the containers and owned by the container-side user, so a host-side
`rm -rf` fails with a permission error. Clear them the same way:

```bash
docker run --rm -v "$PWD/data:/data" busybox rm -rf /data/postgres /data/rustfs
```

## Upgrading

```bash
docker pull brickkit/market-server:1.1.0
sed -i 's/brickkit\/market-server:1.0.0/brickkit\/market-server:1.1.0/' docker-compose.yaml
docker compose up -d market-api      # database migrations run automatically on startup
docker compose logs -f market-api    # confirm the migration succeeded
```

Upgrade one version at a time rather than skipping versions. The market is
briefly unavailable during the restart (typically under 30 seconds on a
single-machine deployment); already-published components and their
artifacts are untouched by a market upgrade. Rolling back is the same
procedure with the version numbers reversed — stop `market-api`, point the
compose file back at the previous tag, restart it.

## Moving to the cloud

The market image makes no assumptions about its environment — it only
needs a PostgreSQL address and an S3-compatible endpoint, so removing the
`postgres` and `rustfs` services from the compose file and pointing the
same environment variables at managed services runs it on any cloud
without any code changes:

```bash
DATABASE_HOST=your-db.rds.amazonaws.com   # or any managed Postgres
DATABASE_SSLMODE=require                   # required on a real network; the compose default (disable) only suits an internal docker network

RUSTFS_ENDPOINT=https://s3.ap-east-1.amazonaws.com   # or any S3-compatible endpoint, must include the scheme
RUSTFS_ACCESS_KEY=<...>
RUSTFS_SECRET_KEY=<...>
```

Three things the market deliberately does not do for you, and that you
have to add at this layer:

| Concern | Why it's yours | Status |
| --- | --- | --- |
| **HTTPS** | The market only ever speaks plain HTTP. Expose it publicly without TLS termination in front of it (a gateway, nginx, an ALB) and bearer tokens travel in cleartext | Your ingress layer's job |
| **Backups** | Managed database automated backups + object storage versioning/cross-region replication | Your cloud provider's job |
| **Multiple replicas** | The market process holds no local state, so running several behind a load balancer is safe by design | Supported by design; not something brickKit load-tests for you |

**Kustomize and a Helm chart both exist and are both verified against a
real cluster** — `deploy/market/k8s/` (with an `in-cluster-deps` overlay
that deploys PostgreSQL and object storage alongside the market) and
`deploy/market/helm/brickkit-market/`. Use kustomize for running it
yourself — it needs no extra tooling. Reach for the Helm chart when the
market is being handed to a third party as a product to operate: it adds
`values.schema.json` (rejects a bad value before install rather than
after) and `helm rollback`.

## Backups and moving machines

Everything of value lives in two directories:

```
deploy/market/data/
├── postgres/     # component metadata, versions, users, access policy
└── rustfs/       # artifact files
```

A backup is `docker compose down` (data is preserved, not deleted) plus
archiving that directory. Moving to a new machine is copying the whole
`deploy/market/` directory across and running `docker compose up -d`
again — there's no separate migration step.

## Security checklist

| Item | Setting |
| --- | --- |
| `.env` file permissions | `chmod 600`, never committed to Git |
| PostgreSQL | Never publish its port to the host — the compose file already doesn't |
| Object storage API | Only the console port is ever optionally exposed; the S3 API port isn't |
| Market API port | Fine to expose, but put it behind a firewall or reverse proxy regardless |
| Passwords | At least 16 characters, mixed case, digits, symbols |
