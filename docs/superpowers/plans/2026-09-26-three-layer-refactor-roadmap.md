# Three-layer refactor — Roadmap (P1–P9)

**Spec:** `new_plan/提案.md` (RFC, **Appendix A overrides the body where they disagree**), `new_plan/命令表.md`
(final command surface), `new_plan/文档修改-布局.md` + `new_plan/文档修改-本地文件.md` (documentation layout).

This file is the index of the whole refactor. Each phase gets its own detailed implementation plan
(`2026-09-26-three-layer-pN-*.md`), written **after the previous phase is merged**, against the code as it
actually is at that point — later phases depend on names and shapes the earlier ones land, so writing them all
up front would produce plans that are wrong by the time they run.

## Working rules for the whole refactor (user decisions, 2026-09-26)

- **Goal over intermediate health.** Between phases, things outside the phase's own packages may be broken
  (old tests failing, `make lint` doc checks failing, old commands not working). Nobody fixes collateral
  breakage mid-way; P8 restores a fully green `make lint` + `make test-all`.
- **Each task still verifies its own deliverable**: the package(s) it touches must build, pass `go vet`,
  pass their own tests and `golangci-lint`, and `go test ./tests/i18nguard/...` must stay green (every
  user-visible string goes through `internal/msgid` + both catalogs from day one — retrofitting i18n later
  is far more expensive than doing it inline).
- **Commit after every task** on `main` directly (no worktrees), message ends with the Co-Authored-By line.
- **No backward compatibility and no migration command** for old `brickkit.yaml` / `override.yaml`
  (Appendix A17). Old code paths are deleted, not kept behind flags.
- **Market stays** (Appendix A2/A14): `market-server/` untouched; CLI keeps `sources[].type: market`,
  `publish`, `login`/`logout`, signature verification (`installer` in `brickkit.yaml`).

## Phases

| # | Plan file | Scope | Done when |
| --- | --- | --- | --- |
| P1 | `…-p1-data-model.md` | Archive old docs, create new doc/tutorial skeleton dirs. New packages built **alongside** the old ones, nothing wired into commands yet: `internal/project` (layout, local-mode state, loader + cross-file consistency), `internal/projfile` (`brickkit.yaml` v2), `internal/deployfile` (`deploy.yaml` / `deploy.local.yaml` / `-f` file), `internal/configdir` (`config/` + `vars.yaml`, value references `$var:` / `${VAR}` / `file://` / existingSecret, duplicate-key conflict detection and marker format, resolution with precedence, skeleton generation), `internal/envref`, `internal/yamlfile`. | `project.Load` loads a three-layer project fixture end to end and every loud-failure rule in the spec's §6.3 / §7 / Appendix A5 has a test. |
| P2 | `…-p2-pipeline-switch.md` | Switch every command's pipeline from `config.Config` to `project.Project`: resolver, cascade, inject (config values from `configdir.Resolve`, reserved-var protection, key == env var name per A10), compose, k8s. Manifest changes: `deployment.image` optional, `deployment.build`, `shell.members`, configSchema key rule, drop `dependencies.resources`. Delete `resources`, `servedBy`, `internal/override`, `internal/config` (Layout moves to `internal/project`), `override` command, `--config`, `up --context`. Add `-f/--file`, `--no-local`. Secrets per A6/A7 (CLI-resolved values → 0600 env files with `$` escaped on Docker, Secret on K8s; `existingSecret` kept). Migration service gets every config var + per-ID version chain (`depends_on … service_completed_successfully`). `target: podman` from deploy files. JSON schemas regenerated (`brickkit.schema.json`, new `deploy.schema.json`, `override.schema.json` removed). NetworkPolicy egress without `resource:` (A15). `up`/`down`/`status`/`sync`/`graph`/`restore` read the new model. | `brickkit up --dry-run` on a three-layer project produces correct compose and K8s output; old model code is gone. |
| P3 | `…-p3-shell.md` | Shell mechanism: `members` in deploy files, `kind: shell` marker maintained by CLI (A11), `shell.members` capability in `component.yaml`, standalone-image rule for members (§8.1 rules 1–3), JSON injection `BRICKKIT_SERVED_MEMBERS_CONFIG` with resolved values (A7 escaping), `_ENDPOINT` rewrite to the shell via reverse index, member migrations with the member's own image, member `mode` semantics (members **must** be allowed `mode: debug` / `mode: local` — A18), `--ignore-served-by`, graph drawing. | A shell project renders correctly on Docker and K8s, including PEM-style multi-line secrets. |
| P4 | `…-p4-git-distribution.md` | Git sources with `baseUrl` + per-component `source.repo`/`source.path`, user-level bare-repo cache `~/.cache/brickkit/repos` (A12), 4-level read priority, permanent manifest + `BRICKKIT.md` cache, latest-tag lookup, monorepo tag namespace (A9), local sources with real versions (A8), git auth error pass-through. Rewrite `add` (writes all three layers, config skeleton, `$var:` prompt, shell auto-membership, multi-version auto-coexistence), `remove` (config → `config/.archive/`, deploy sync, shell member release), `fetch`. | `add`/`remove`/`fetch` work against a real local bare-repo fixture offline after first fetch. |
| P5 | `…-p5-images.md` | `brickkit build` (`--force`, cache reuse, tag == `metadata.version`), `up` image check that never builds (local: present or fail; git: present → pull if `image` → fail), hint texts. | `up` fails loudly with a `build` hint when an image is missing; `build` produces correctly tagged images. |
| P6 | `…-p6-upgrade.md` | `brickkit upgrade` (single / all in topological order / `--dry-run`), config migration algorithm (§12.2), conflict handling per A4 (interactive on TTY, duplicate-key markers otherwise), archive restore on re-`add` via the same algorithm, shell upgrade (members kept, unsupported members moved out). | Upgrade scenarios from spec §12 each have a test, including the round trip marker → `up` refusal → hand fix → `up` success. |
| P7 | `…-p7-commands.md` | `init` (create + complement modes, `.gitignore` guard, closing lint, project `BRICKKIT.md`), `new --shell` + component `BRICKKIT.md` skeleton, `add --local --init`, `release` (+ `--local`, atomic tag rollback), `local on/off/status/refresh` (backup + diff summary), `deps`, `lint` new checks (§11.3), `skills` asset content update. | Every command in `命令表.md` exists with the documented flags. |
| P8 | `…-p8-green.md` | Rewrite `tests/components` fixtures to three layers, `tests/checklist`, `tests/regression`, perf, e2e scripts; restore `make lint` (doc checks updated for the new tree) and `make test-all` to green; coverage gate. | `make lint && make test-all` green. |
| P9 | `…-p9-docs.md` | `AGENTS.zh.md` → `llms.zh.txt` → `README.zh.md` → `docs/zh/` (order 00→01→07→02→03→04→05→06→08→09→10→11) → English counterparts written independently. Tutorials stay as empty directories (A13). | `make lint` doc checks green against the new tree. |

Documentation may also be written earlier inside a phase when the phase itself makes a reference page
obvious (e.g. the field reference right after the schemas are regenerated) — each phase plan says so
explicitly if it does.
