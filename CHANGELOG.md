# Changelog

All notable changes to BrickKit are documented in this file, newest first.
The format loosely follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/).

Before 1.0.0, minor version bumps may include breaking changes — semver's
usual guarantee about backward compatibility doesn't fully apply yet. Every
entry below is reconstructed from real commit history (`git log`, and the
tags themselves); it groups related commits into the feature or fix they
add up to, rather than listing every commit individually.

## [Unreleased]

## [0.4.6] - 2026-09-17

### Fixed

- `brickkit publish --sign` crashing under cosign v3, where the CLI's
  `sign-blob` invocation still used the deprecated `--output-signature` flag
  cosign v3 silently ignores unless `--bundle` is also given
- A `local: true` component depending on a `servedBy` member getting a
  plausible-looking but unreachable `localhost:<port>` address for that
  member's extra ports — the host-port mapping now opens on the shell's
  compose service, using the member's own declared port
- `golangci-lint` findings (errcheck/staticcheck/unused)
- The release workflow referencing a nonexistent `cosign-installer@v4` tag

### Changed

- The release workflow upgraded to the Node 24–based major version of its
  GitHub Actions

## [0.4.5] - 2026-09-17

### Fixed

- `readDotEnv` splitting `.env` files by physical line, truncating any
  value that legitimately spans multiple lines (e.g. a PEM key)

## [0.4.4] - 2026-09-17

### Fixed

- `local-debug.*.env` serialization truncating or mis-parsing multi-line
  values and values containing shell-special characters

## [0.4.3] - 2026-09-16

### Changed

- A `servedBy` member's own config values now land in the shell's
  environment under a component-ID-prefixed variable name instead of
  unprefixed, and `BRICKKIT_SERVED_MEMBERS_CONFIG`'s config field was
  renamed to `configEnvVars` — this closes a real bug where a `${VAR}`
  secret placeholder in a member's config could corrupt the generated
  JSON once Docker Compose's own text substitution touched it
- Added the project's own development principle to `CLAUDE.md`: prefer the
  architecturally correct fix over the smallest patch, regardless of the
  extra work involved

## [0.4.2] - 2026-09-15

### Added

- Structured, per-member config injection for `servedBy` shells
  (`BRICKKIT_SERVED_MEMBERS_CONFIG`) and an `--ignore-served-by` validation
  flag for `brickkit up --dry-run`
- Documentation for how an out-of-band tool should construct a `servedBy`
  component's address

## [0.4.1] - 2026-09-14

### Fixed

- `collectTargets` not accounting for `servedBy` semantics, causing a real
  `up` to fail with "no such service"
- Resource-binding validation not accounting for `servedBy` semantics
- `servedBy` label merging missing its exclusion rule — a member's own
  labels no longer participate in the shell's label merge

## [0.4.0] - 2026-09-14

### Added

- The `servedBy` companion documentation set: a deployment checklist, a
  self-hosted-market deployment guide, shared-connection-pool methodology,
  seed/test-data planning methodology, and component design guidelines
  (English/Chinese pairs)
- Four architecture articles: dependency resolution, deployment-file
  generation, resource-binding mechanics, and signing/trust
  (English/Chinese pairs)
- The 12-part hands-on tutorial series under `docs/{en,zh}/guide/`, from
  the first running project through Kubernetes, local debugging,
  versioning, signing, building a component from scratch, network policy,
  and multi-project sharing
- The full CLI command reference (`docs/{en,zh}/architecture/cli-reference.md`)
- A topic-based documentation index for casual (non-AI) readers in the README
- Per-language `llms.txt`/`llms.zh.txt`, matching the README/AGENTS split

### Fixed

- A closed-source image hardening claim that was already true by default
  (digest pinning), corrected to reflect actual `brickkit publish` behavior
- A ~1/16-probability false positive in `check-install-sh.sh`
- A `TestUpDryRunIsRepeatable` flake racing against the wall clock

## [0.3.0] - 2026-09-14

### Added

- `servedBy`: components can declare that their workload is provided by
  another (shell) component, generating no container/Deployment of their
  own while still getting correctly routed `*_ENDPOINT` addresses on both
  Docker and Kubernetes — plus `internal/shell` (grouping, validation,
  merging), Compose and K8s renderer support, and `BRICKKIT_SERVED_MEMBERS`
  injection
- "Building a Qualified Shell" and closed-source image hardening guides
  (English/Chinese pairs)

### Changed

- The full documentation tree restructured: `design/`, the old hands-on
  guide, and dev-progress logs archived as historical record; `AI-CONTEXT.md`
  renamed to `AGENTS.md` with language
  routing rules; `README.md`/`README.zh.md` and `AGENTS.md`/`AGENTS.zh.md`
  became independently-written language pairs instead of one file with
  translated sections; `docs/en/`/`docs/zh/` established as the current,
  symmetric documentation tree with dedicated CI gates keeping them
  mirrored

## [0.2.2] - 2026-09-08

### Fixed

- A local source's cached `metadata.id` silently going stale after a
  component was renamed, instead of erroring

## [0.2.1] - 2026-09-06

### Fixed

- Three gaps around Git submodule registration: `restore --check` false
  positives, and `sync`/`remove` silently corrupting state instead of
  blocking it

## [0.2.0] - 2026-09-03

### Added

- `brickkit restore` and `brickkit restore --check`, a pre-commit
  consistency gate for projects that keep component source under version
  control (catches an archive-state move committed without its matching
  `enabled` change), plus `brickkit init --hooks` to install it
- `labels` passthrough on a component's deployment metadata — the platform
  doesn't interpret keys/values, so an out-of-band gateway (Traefik,
  Prometheus, etc.) can still hook in without the platform ever having to
  understand routing

## [0.1.1] - 2026-09-02

### Added

- AI-assistant skills: `brickkit init` now generates `.claude/skills/`
  (four skill files, each scoped to "what an AI is likely to guess wrong"),
  plus `brickkit skills status`/`update` and a `skills.lock` tracking
  whether a project's copy is still the one the platform shipped

## [0.1.0] - 2026-09-01

Initial release. The whole platform, built from an empty repository:

### Added

- CLI commands: `init`, `add`, `remove`, `fetch`, `up`, `down`, `status`,
  `sync`, `login`, `logout`, `publish`, `version`
- Docker Compose and Kubernetes deployment-file generation from one
  `component.yaml`/`brickkit.yaml` pair, including local debug mode
  (`local: true`) and database migrations
- Recursive dependency resolution with required/optional dependencies and
  topological sort
- Environment-variable injection for dependency addresses, resource
  connections, and a component's own config
- Versioned service names, giving multi-version coexistence for free
- The component marketplace (`market-server`): publishing, discovery,
  versions, visibility, cosign signing with Go-standard-library
  verification on the installer side
- NetworkPolicy/ServiceAccount generation from the dependency graph
- Ten real fixture components (`tests/components/`) used to test the
  platform itself against real Docker and Kubernetes deployments
