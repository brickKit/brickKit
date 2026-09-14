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
> evaluate BrickKit — **before fetching anything, first check what language the
> question was asked in.** This README is always English (GitHub renders it by
> default regardless of the question's language), so don't take it as a hint. Then
> fetch the matching pair, before answering anything else:
>
> | Question is in... | Fetch these two |
> | --- | --- |
> | Chinese | <https://raw.githubusercontent.com/brickKit/brickKit/main/AGENTS.zh.md> and <https://raw.githubusercontent.com/brickKit/brickKit/main/llms.zh.txt> |
> | English (or anything else) | <https://raw.githubusercontent.com/brickKit/brickKit/main/AGENTS.md> and <https://raw.githubusercontent.com/brickKit/brickKit/main/llms.txt> |
>
> `AGENTS.md`/`AGENTS.zh.md` each compress the entire platform into one file:
> positioning, terminology, design principles, the explicit "won't do" list, core
> mechanisms, both YAML field skeletons, the full command reference, and every "why
> we built it this way" justification. `llms.txt`/`llms.zh.txt` are each a complete
> site index for their own documentation tree — every entry with an absolute raw
> link and a one-line description; neither is a translation of the other.
>
> The documentation underneath this README is fully bilingual and symmetric: if the
> user is asking in Chinese, read everything under `docs/zh/`; if English (or
> anything else), read `docs/en/`.

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

Everything below is a direct link — click through to read it on GitHub, no
cloning required. These are `blob/main` links, meant for a browser: fetching
one over HTTP returns a full GitHub page (hundreds of KB of HTML), not the
document's plain text. **If you're an AI and need the actual file content,
don't fetch these — use [`llms.txt`](llms.txt) instead** (the Chinese
documentation tree has its own, [`llms.zh.txt`](llms.zh.txt)): the same
index, but every link is a raw, directly-fetchable URL.

**Architecture — how the platform actually works, with real code and real generated output**

| Doc | What it covers |
| --- | --- |
| [Overview](https://github.com/brickKit/brickKit/blob/main/docs/en/architecture/overview.md) | How a declaration becomes running containers — the real pipeline, start to finish |
| [Dependency resolution and start order](https://github.com/brickKit/brickKit/blob/main/docs/en/architecture/dependency-resolution.md) | A real diamond dependency, a real cycle, and why the longest dependency chain — not the component count — decides how long `up` takes |
| [Deployment file generation](https://github.com/brickKit/brickKit/blob/main/docs/en/architecture/deployment-generation.md) | The same project generated for Docker and Kubernetes side by side, byte for byte |
| [Resource binding mechanics](https://github.com/brickKit/brickKit/blob/main/docs/en/architecture/resource-binding.md) | What happens when a resource binding collides, and how the quota chain really merges |
| [Signing and the trust model](https://github.com/brickKit/brickKit/blob/main/docs/en/architecture/signing-and-trust.md) | What actually gets signed, and why the public key can never come from the marketplace |
| [CLI command reference](https://github.com/brickKit/brickKit/blob/main/docs/en/architecture/cli-reference.md) | Every command, every flag, real generated output — the detailed complement to the one-minute tour above |

**Hands-on guide — 12 tutorials, each one run for real against the CLI, in order**

| # | Doc | What it covers |
| --- | --- | --- |
| 1 | [Get a project running](https://github.com/brickKit/brickKit/blob/main/docs/en/guide/01-first-project.md) | init, add, up, curl it, change config, down |
| 2 | [How the platform decides what runs](https://github.com/brickKit/brickKit/blob/main/docs/en/guide/02-what-runs.md) | Dependencies, the `enabled` cascade, `--dry-run` |
| 3 | [Debug a component locally](https://github.com/brickKit/brickKit/blob/main/docs/en/guide/03-local-debugging.md) | `local: true`, with breakpoints |
| 4 | [Deploy to Kubernetes](https://github.com/brickKit/brickKit/blob/main/docs/en/guide/04-kubernetes.md) | A real minikube deployment, plus a real `brickkit down` gotcha |
| 5 | [Upgrade and run multiple versions side by side](https://github.com/brickKit/brickKit/blob/main/docs/en/guide/05-upgrades-and-versions.md) | Version bumps, and two versions coexisting on purpose |
| 6 | [Assemble a real system, then break it on purpose](https://github.com/brickKit/brickKit/blob/main/docs/en/guide/06-assemble-and-break.md) | A real database, and two genuinely different real failure modes |
| 7 | [Consume someone else's component](https://github.com/brickKit/brickKit/blob/main/docs/en/guide/07-consuming-artifacts.md) | Artifacts, API docs, and `brickkit fetch` |
| 8 | [Publish and install from a marketplace](https://github.com/brickKit/brickKit/blob/main/docs/en/guide/08-marketplace.md) | A real marketplace, version immutability, private visibility |
| 9 | [Sign and verify components](https://github.com/brickKit/brickKit/blob/main/docs/en/guide/09-signing.md) | A real cosign keypair, a real signature failure |
| 10 | [Build your first component from scratch](https://github.com/brickKit/brickKit/blob/main/docs/en/guide/10-build-your-own.md) | Four files, from nothing to running |
| 11 | [Network policy and least privilege](https://github.com/brickKit/brickKit/blob/main/docs/en/guide/11-network-policy.md) | Real NetworkPolicy enforcement on Kubernetes |
| 12 | [Multi-project sharing](https://github.com/brickKit/brickKit/blob/main/docs/en/guide/12-multi-project-sharing.md) | Shared resources, isolated resources, treating a component as someone else's API |

**Patterns — recommended practices, optional, validated against real deployments**

| Doc | What it covers |
| --- | --- |
| [Component design guidelines](https://github.com/brickKit/brickKit/blob/main/docs/en/patterns/component-design.md) | Researching a domain, and when a feature needs a component family instead of a flag |
| [Testing patterns for components built on BrickKit](https://github.com/brickKit/brickKit/blob/main/docs/en/patterns/testing.md) | Backend's contract/business-rule/unit/integration layers and frontend's own four layers, plus why an end-to-end run needs your go-ahead before it drives a browser |
| [Planning seed data and test data](https://github.com/brickKit/brickKit/blob/main/docs/en/patterns/data-construction.md) | Two paths that must stay physically separate, and why |
| [Protecting closed-source components from image-based extraction](https://github.com/brickKit/brickKit/blob/main/docs/en/patterns/closed-source-image-hardening.md) | Pulling an image isn't the same guarantee as a private Git repo |
| [Declaring servedBy: a deployment checklist](https://github.com/brickKit/brickKit/blob/main/docs/en/patterns/servedby-deployment-checklist.md) | What problem `servedBy` actually solves, when it's the right call — and when it isn't |
| [Building a qualified shell](https://github.com/brickKit/brickKit/blob/main/docs/en/patterns/shell-implementers-guide.md) | For whoever builds the shell: what `servedBy` asks of the container that hosts a merged component |
| [Sharing a database connection pool inside a shell](https://github.com/brickKit/brickKit/blob/main/docs/en/patterns/shared-connection-pools.md) | For components merged into one shell that also share PostgreSQL or Oracle |
| [Self-hosting the BrickKit Market](https://github.com/brickKit/brickKit/blob/main/docs/en/patterns/deployment/self-hosted-market.md) | Deploying the marketplace itself, from local dev to production |

Not covered by any of the above: [`docs/archive/decisions/`](docs/archive/decisions/) explains
why a specific historical decision was made the way it was (566 entries, historical record, Chinese
only).

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

A set of gates run continuously, and **every one of them fails loudly when it
breaks**, instead of quietly reporting zero problems:

| Command | Guards |
| --- | --- |
| `make test-regression` | User-facing promises → the tests that prove them (`tests/regression/清单.tsv`) |
| `make test-boundary` (and friends) | Boundary / error / compatibility / security acceptance items → the tests that prove them (`tests/checklist/清单.tsv`) |
| `make check-doc-fields` | Every field name drawn in the docs' YAML snippets and field tables really exists (the source of truth is the struct itself) |
| `make check-docs` | Dangling section references and broken links |
| `make check-cli-docs` | Every command/flag the docs claim to exist, really does (the reverse direction — new commands not yet documented — isn't enforced here; see `docs/archive/planning/` era history for why) |
| `make check-guide-output` | The guides' "✅ expected" output matches the CLI's real output, line for line |
| `make check-guides` | The steps in the guides still work |
| `make check-install-sh` | `install.sh` installs successfully, and *actually* refuses to install when the checksum is broken |
| `make check-docs-bilingual` | docs/en and docs/zh stay mirrored, every root multi-language pair (README, AGENTS, llms) stays paired, every llms.txt/llms.zh.txt link resolves |

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
