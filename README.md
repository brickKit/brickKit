<div align="center">

# BrickKit

**Build systems like stacking bricks.**

A component assembly platform — develop, deploy, call, and compose components independently.

[![Docs in Chinese](https://img.shields.io/badge/📖_文档-简体中文-blue?style=for-the-badge)](试用指南/README.md)
[![Design Books](https://img.shields.io/badge/设计书-14_本-lightgrey?style=for-the-badge)](design/000%20阅读指南与文档导航.md)

> **All documentation is written in Chinese.**
> Click the blue button above to jump straight to the hands-on guide.

</div>

---

## What is BrickKit

BrickKit is **not** an operating system, an ERP, or any particular business application.
It is a platform that lets you build your architecture *gradually*: write one small
component and get it running, write another, then write a connector component that
wires them together. Like stacking bricks, you end up with any system you need.

| BrickKit | Roughly equivalent to |
| --- | --- |
| BrickKit CLI | `npm` + `helm` + `docker compose` + `git clone`, but for **business components** |
| BrickKit Market | npmjs.com / Docker Hub / App Store |
| Component | npm package / Docker image |
| `component.yaml` | `package.json` |
| `brickkit.yaml` | the declarative input to `docker-compose.yaml` |
| `brickkit add` | `npm install` |
| `brickkit up` | `docker compose up -d` / `kubectl apply` |

### What it gives you

- **Incremental** — no need to design the whole system up front
- **Language-agnostic** — anything that builds into a Docker image can be a component
- **Same address everywhere** — local (Docker) and production (K8s) use one address
  format, so component code needs zero changes between them
- **A platform that stays out of the way** — business logic, traffic governance and
  multi-tenancy all belong to components, not to the platform

---

## A 60-second look

```bash
brickkit init my-shop                 # create a project
brickkit add erp/backend@1.0.0        # pulls the whole dependency tree
brickkit order                        # show start order (topological)
brickkit up                           # generate deploy files, run migrations, start
```

One `add` pulls every dependency. One `up` turns the declaration into running
containers — or into Kubernetes manifests, by changing a single field:

```yaml
deploy:
  target: k8s        # was: docker
```

**12 commands total:** `init` `add` `remove` `up` `down` `status` `order` `sync`
`reset` `login` `publish` `version`

---

## Getting started

Everything below is in Chinese.

| I want to… | Go to |
| --- | --- |
| **Try it in 2 hours** | [00a · 两小时上手](试用指南/00a-两小时上手.md) |
| Follow all 23 guides in order | [试用指南](试用指南/README.md) |
| Know what to install first | [00b · 底层环境清单](试用指南/00b-底层环境清单.md) |
| Understand the design | [设计书导航](design/000%20阅读指南与文档导航.md) |
| Run the market backend | [市场部署与运维指南](市场部署与运维指南.md) |
| See why a decision was made | [决策索引](开发进度/决策索引.md)（503 条） |

---

## Repository layout

```
cmd/brickkit/          CLI entry point
internal/              CLI implementation
  ├── config/            brickkit.yaml parsing & validation
  ├── manifest/          component.yaml parsing & validation
  ├── resolver/          dependency resolution, topological sort
  ├── cascade/           which components actually run
  ├── inject/            environment variables & resource quotas
  ├── compose/           docker-compose.yaml generation
  ├── k8s/               Kubernetes manifest generation
  ├── engine/            docker compose / kubectl drivers
  └── security/          cosign signing & stdlib verification
market-server/         the component market (separate Go module)
design/                14 design books — the normative spec
试用指南/               23 hands-on guides, every one really executed
tests/components/      10 real components used to test the platform
tests/checklist/       acceptance checklists → the tests that prove them
deploy/market/         compose / kustomize / Helm for the market
```

Unit tests live **next to the code they test** (`internal/**/*_test.go`), not in a
parallel tree. `tests/` holds only what cannot live there: the checklists, the
benchmarks, and the components used as fixtures.

---

## Building & testing

```bash
make build            # bin/brickkit + bin/market-server
make test             # unit tests
make test-all         # every test suite
make lint             # vet + documentation checks
```

Five checks run continuously, and **each one fails loudly when it breaks** rather
than quietly reporting zero problems:

| Command | Guards |
| --- | --- |
| `make test-regression` | 25 user-facing promises → the tests that prove them |
| `make test-boundary` etc. | 75 acceptance items → the tests that prove them |
| `make check-docs` | dangling section references and broken links |
| `make check-cli-docs` | every command and flag written in the docs actually exists |
| `make check-guides` | the steps in the hands-on guides still run |

A checklist that points at a test which no longer exists fails the build. So does
a test target whose directory turned up empty — a suite that silently skips is
worse than no suite at all, because it still occupies a line on the scoreboard.

---

## Status

Every planned step is complete, and so is every deferred item.

| | |
| --- | --- |
| Tests | 1667 test functions, race-clean |
| Guides | 23, all really executed against Docker / Kubernetes / a live market |
| Design books | 14, cross-checked against the implementation twice |
| Decision log | 503 entries, each with the reasoning behind it |

**Requirements:** Go 1.22+, Docker 20.10+ with Compose V2.
Kubernetes guides need minikube; signing needs
[cosign](https://github.com/sigstore/cosign) (publishers only — verification uses
the Go standard library).

---

<div align="center">

**[📖 开始阅读中文文档 →](试用指南/README.md)**

</div>
