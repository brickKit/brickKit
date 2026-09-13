<div align="center">

# BrickKit

</div>

[English](README.md) | [中文](README.zh.md)

<div align="center">

**Build systems like LEGO bricks.**

A component assembly and orchestration platform — components are developed,
deployed, invoked, and composed independently.

</div>

---

> ## 🤖 For AI assistants
>
> If someone hands you this repository's URL and asks you to understand, discuss, or
> evaluate BrickKit — **fetch these two files before answering anything else**:
>
> 1. **<https://raw.githubusercontent.com/brickKit/brickKit/main/AGENTS.md>** — a
>    single file that compresses the entire platform: positioning, terminology,
>    design principles, the explicit "won't do" list, core mechanisms, both YAML
>    field skeletons, the full command reference, and every "why we built it this
>    way" justification.
> 2. **<https://raw.githubusercontent.com/brickKit/brickKit/main/llms.txt>** — the
>    complete site index, every entry with an absolute raw link and a one-line
>    description, split into an **English** section and a **中文** section.
>
> **Pick your language from the user's question, not from this file.** This README
> is always English (it is the first thing rendered on the repository homepage), but
> the documentation underneath it is fully bilingual and symmetric — neither language
> is a translation of the other. If the user is asking in Chinese, read everything
> under `docs/zh/`; if they are asking in English (or anything else), read
> `docs/en/`. `llms.txt`'s two sections point at the same structure in both languages.

---

## What BrickKit is

BrickKit is **not** an operating system, an ERP, or any specific business
application. It is a platform for growing an architecture **incrementally**:
write one small component and get it running, write another, then write a
connector component that wires them together. Piece by piece — like assembling
bricks — you end up with whatever system you actually need.

| BrickKit | Roughly equivalent to |
| --- | --- |
| BrickKit CLI | `npm` + `helm` + `docker compose` + `git clone`, but for **business components** |
| BrickKit Market | npmjs.com / Docker Hub / an app store |
| Component | an npm package / a Docker image |
| `component.yaml` | `package.json` |
| `brickkit.yaml` | the declarative input, the role `docker-compose.yaml` plays for Compose |
| `brickkit add` | `npm install` |
| `brickkit up` | `docker compose up -d` / `kubectl apply` |

The essential difference: npm installs a code library; BrickKit installs a
**business service that can run on its own**. So it also has to handle
dependency resolution, deployment-file generation, address injection, database
migration, and startup ordering.

**What it gives you:**

- **Incremental** — no need to design the whole system up front; add one brick at a time
- **Language-agnostic** — anything that can build into a Docker image can be a component
- **Consistent addressing** — local (Docker) and production (K8s) use the **same address format**, so component code needs zero changes
- **The platform stays out of the way** — business logic, communication policy, and multi-tenancy all belong to components, never to the platform

---

## Install

The CLI ships as a **single Go binary** — installing it requires no runtime. It
doesn't run as a daemon and writes no global config: all state lives in your
project's `brickkit.yaml` and `.brickkit/`, and at runtime it shells out to
`docker` / `kubectl` on your machine.

### Method 1: one-line install script (recommended, no Go needed)

```bash
curl -fsSL https://raw.githubusercontent.com/brickKit/brickKit/main/install.sh | sh
```

The script detects your OS and architecture, downloads the matching package,
**verifies its sha256 checksum and refuses to install on a mismatch**, then
installs into `/usr/local/bin` (falling back to `~/.local/bin` with a PATH
notice if that's not writable).

If you'd rather not pipe straight into a shell, download it first and read it:

```bash
curl -fsSLO https://raw.githubusercontent.com/brickKit/brickKit/main/install.sh
less install.sh && sh install.sh
```

Pin a version with `BRICKKIT_VERSION=v0.1.0`; change the install location with
`BRICKKIT_INSTALL_DIR=...`.

### Method 2: `go install` (if you already have Go)

```bash
go install github.com/brickkit/brickkit/cmd/brickkit@latest
```

Installs to `$(go env GOPATH)/bin` (`~/go/bin` by default). This path bypasses
the Makefile, so the binary doesn't get the injected version metadata —
`brickkit version` will report `v0.0.0-dev`. Use Method 1 or 3 if you need the
real version string.

### Method 3: build from source

```bash
git clone https://github.com/brickKit/brickKit.git
cd brickKit
make build-cli                 # produces bin/brickkit
sudo install -m 0755 bin/brickkit /usr/local/bin/brickkit
```

Or `make install` to place it on `GOBIN` — the same location as Method 2, but
with version, commit hash, and build time injected into the binary.

### Verify

```bash
brickkit version
```

```
BrickKit CLI v0.1.0
Supported manifest version: brickkit/v1
Supported deploy targets: docker, k8s
```

> Any JSON you see on stderr is structured logging — it doesn't affect normal
> output. Silence it with `--log-level off`.

### What else you'll need

| | Needed when |
| --- | --- |
| Docker 20.10+ (with Compose V2) | Running `brickkit up` for local containers |
| kubectl + a cluster (minikube is enough) | `deploy.target: k8s` |
| Go 1.22+ | **Only** Methods 2 and 3 need it; Method 1 doesn't (unless a component itself is written in Go) |
| [cosign](https://github.com/sigstore/cosign) | **Only** publishers signing releases need it; verification uses the Go standard library, so installing the CLI doesn't require it |

> **Windows:** a `windows/amd64` zip is available for manual download from
> [Releases](https://github.com/brickKit/brickKit/releases). Only the parts of
> the CLI that don't touch Docker have been verified there — starting
> containers and the Kubernetes path **haven't been verified** on Windows.
> That's not the same as unsupported; it's simply untested. Details in
> [docs/archive/planning/发布与分发.md](docs/archive/planning/发布与分发.md)
> §3.1 (historical, Chinese only).
>
> **There's no Homebrew / Scoop / apt package yet.** All of those are
> downstream of GitHub Releases; the upstream has to exist first.

---

## Uninstall

```bash
rm "$(command -v brickkit)"
```

There is no global config to clean up — deleting your project directory
removes everything else.

---

## One-minute tour

```bash
brickkit init my-shop                 # create a project
brickkit add erp/backend@1.0.0        # pull the entire dependency tree in one shot
brickkit up --dry-run                 # preview the startup order (topological sort)
brickkit up                           # generate deployment files → run migrations → start containers
```

One `add` pulls every dependency. One `up` turns the declaration into running
containers — or into Kubernetes manifests, by changing a single field:

```yaml
deploy:
  target: k8s        # was: docker
```

Not a single line of component code changes: addressing is identical in both
environments, always `http://<versioned-service-name>:<port>` (for example
`http://people-basic-1-0-0:8080`).

**13 commands in total:** `init` `add` `remove` `fetch` `up` `down` `status`
`sync` `restore` `login` `logout` `publish` `version`

---

## What it deliberately doesn't do

This list matters as much as the feature list above — these aren't things
that are "not built yet," they are things that were **argued through and
rejected**:

| Doesn't do | Instead |
| --- | --- |
| Service registry / address book | Docker DNS / Kubernetes Service DNS |
| A long-running daemon / control plane | The CLI runs and exits; state lives externally, in `brickkit.yaml` and the underlying engine |
| Health-check polling | Kubernetes probes / Compose healthchecks, plus restart policies |
| API gateway / service mesh | Components talk to each other directly over DNS |
| Config center / dynamic hot-reload | Config is injected as environment variables; change it and restart |
| Circuit breaking / rate limiting / graceful degradation | That's the component's own business logic |
| Version ranges (`^1.0.0`) | Only exact versions are accepted — no implicit upgrades |
| Multi-environment overlay inheritance | Each environment gets one complete, self-contained config |
| Security review of third-party components | Trust at install time; a bad actor gets `blocked` after the fact |

> **The platform only does two jobs — connector and translator — and
> deliberately stays out of both business logic and anything infrastructure
> already does well.**

The full reasoning behind every row lives under
[`docs/en/architecture/`](https://github.com/brickKit/brickKit/tree/main/docs/en/architecture).

---

## Where to go next

| I want to... | Go here |
| --- | --- |
| Understand the platform | [`docs/en/architecture/`](https://github.com/brickKit/brickKit/tree/main/docs/en/architecture), starting with [overview](https://github.com/brickKit/brickKit/blob/main/docs/en/architecture/overview.md) |
| A hands-on tutorial | [`docs/en/guide/`](https://github.com/brickKit/brickKit/tree/main/docs/en/guide) |
| Learn how to test, plan seed data, or tune a deployment | [`docs/en/patterns/`](https://github.com/brickKit/brickKit/tree/main/docs/en/patterns), e.g. [testing](https://github.com/brickKit/brickKit/blob/main/docs/en/patterns/testing.md) |
| See why a specific decision was made | [`docs/archive/decisions/`](docs/archive/decisions/) (historical, Chinese only) |

---

## Repository layout

```
cmd/brickkit/          CLI entry point
internal/              CLI implementation
  ├── config/            brickkit.yaml parsing & validation
  ├── manifest/          component.yaml parsing & validation
  ├── resolver/          dependency resolution, topological sort
  ├── cascade/           start/stop decisions: what actually needs to start this run ("follow the parent")
  ├── inject/             environment-variable injection & resource-quota merging
  ├── compose/            docker-compose.yaml generation
  ├── k8s/                Kubernetes manifest generation
  ├── engine/             docker compose / kubectl drivers
  ├── source/             install sources: market / git / local
  ├── security/           cosign signing & standard-library verification
  └── workspace/          component source workspace (--repo / sync)
market-server/         component market backend (a separate Go module)
tests/components/      10 real components, used to test the platform itself
tests/checklist/       acceptance checklists → the tests that prove them
deploy/market/         the market's compose / kustomize / Helm manifests
docs/en/               English documentation: architecture, guide, patterns
docs/zh/               中文文档：architecture、guide、patterns（对称镜像，不是英文的译本）
docs/archive/          pre-restructure documentation, kept as historical record only
```

Unit tests live **next to the code they test** (`internal/**/*_test.go`) — no
parallel test tree. `tests/` only holds what can't live alongside the code:
checklists, benchmarks, and components used as fixtures.

---

## Build & test

```bash
make build            # bin/brickkit + bin/market-server
make test             # unit tests
make test-all         # the full test suite
make lint             # vet + doc checks
```

Nine gates run continuously, and **every one of them fails loudly when it
breaks**, instead of quietly reporting zero problems:

| Command | Guards |
| --- | --- |
| `make test-regression` | User-facing promises → the tests that prove them (`tests/regression/清单.tsv`) |
| `make test-boundary` (and friends) | Boundary / error / compatibility / security acceptance items → the tests that prove them (`tests/checklist/清单.tsv`) |
| `make check-doc-fields` | Every field name drawn in the docs' YAML snippets and field tables really exists (the source of truth is the struct itself) |
| `make check-docs` | Dangling section references and broken links |
| `make check-cli-docs` | Every command and flag written in the docs (and in `--help` itself) really exists |
| `make check-doc-tree` | The `.brickkit/` directory tree drawn in the docs matches what the CLI actually creates |
| `make check-guide-output` | The guides' "✅ expected" output matches the CLI's real output, line for line |
| `make check-guides` | The steps in the guides still work |
| `make check-install-sh` | `install.sh` installs successfully, and *actually* refuses to install when the checksum is broken |

A checklist pointing at a test that no longer exists fails the build. So does
a test target whose directory has gone empty — **a suite that silently skips
is worse than no suite at all**, because it still occupies a line on the
scoreboard.

---

## Project status

Every planned step is complete, and every deferred item has been resolved.
The original dev plan and dev log are frozen as historical record under
[`docs/archive/planning/`](docs/archive/planning/) and
[`docs/archive/decisions/`](docs/archive/decisions/). Going forward, behavior
is governed by the two living checklists under `tests/checklist/` and
`tests/regression/` — the design books that used to serve that role have been
superseded by `docs/en/architecture/` and `docs/zh/architecture/` (their old
text lives on, read-only, under
[`docs/archive/design/`](docs/archive/design/)).

| | |
| --- | --- |
| Tests | 1762 test functions, race-clean |
| Hands-on guides | 23, each run against real Docker / Kubernetes / a live market — archived at `docs/archive/guide/`, superseded by `docs/{en,zh}/guide/` |
| Design books | 14, cross-checked against the implementation twice — archived at `docs/archive/design/`, superseded by `docs/{en,zh}/architecture/` |
| Decision records | 566, each with the reasoning behind it, archived at `docs/archive/decisions/` |

**Runtime requirements:** Go 1.22+, Docker 20.10+ (with Compose V2).
Kubernetes-related guides need minikube; signing needs
[cosign](https://github.com/sigstore/cosign) (**publishers only** —
verification uses the Go standard library).

---

<div align="center">

[Apache License 2.0](LICENSE)

</div>
