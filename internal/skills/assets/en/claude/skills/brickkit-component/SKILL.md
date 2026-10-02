---
name: brickkit-component
description: Use when writing a new BrickKit component from scratch, editing component.yaml or a component's documents (BRICKKIT.md, AGENTS.md, README.md), declaring configuration (configSchema) or dependencies, choosing between deployment.image and deployment.build, writing a shell (shell.members), adding a database migration or health check, or releasing a component version with brickkit release. Covers the hard contract every component must satisfy, config keys as environment variable names, the reserved-variable no-go zone, the health-check prohibition, and the release rules. Applies when the user says "write a component", "release a new version", "make a shell", or is editing component.yaml.
---

# Writing a BrickKit component

## When to use this skill

- Writing a new component (or a shell) from scratch
- Editing `component.yaml`, or the component's `BRICKKIT.md`
- Declaring configuration, dependencies, a migration, a health check, an extra port
- Releasing a new version of a component
- A component won't start, and a declaration might be the cause

## From scratch: `brickkit new`, then `brickkit lint`

`brickkit new <scope>/<name>` writes a `component.yaml` skeleton that already validates **and the
four documents** (`BRICKKIT.md`, `AGENTS.md`, `CLAUDE.md`, `README.md`, see rule 12) with `<!-- TODO: … -->`
hints, to `components/<scope>/<name>/` (the layout the local install source scans). `--shell` writes a shell skeleton to `shell/<scope>/<name>/` instead,
with one placeholder member to replace; `--contract openapi|proto` adds a placeholder contract under `artifacts`; `--path` writes a
standalone repository. No Dockerfile or code is generated — the platform doesn't pick a language — and nothing is
`add`ed to the project: that is a separate step you can review.

After every edit run `brickkit lint`: offline, read-only, instant. It reports missing fields, wrong
types, unknown keys, version format, port ranges, a misspelled key inside a `configSchema` property
(`defualt` silently does nothing), and config keys that collide with reserved names. It doesn't
resolve dependencies — that's `brickkit up --dry-run` in a project using the component.

## Where you'll guess wrong

**1. `component.yaml` has no extension fields.** `apiVersion: brickkit/v1`; an unknown key is
rejected on the spot. For metadata the engine should see (gateway routing, scrape config) use
`deployment.labels`; to ship a file for tooling (a contract, an SDK) use `artifacts` — the CLI
downloads it to `.brickkit/artifacts/<versioned-service-name>/<type>/` and never parses it.

**2. `configSchema` keys ARE the environment variable names.**

Write `DB_HOST`, `LOG_LEVEL` — exactly what your code reads. There is no camelCase conversion, so a key
must be a valid environment variable name (letters, digits, `_`, not starting with a digit). The
project fills values in `config/<scope>-<name>.yaml` under the same keys. Put keys the project must
supply (a database password, another project's address) in `required` **without** a `default`: `up`
refuses until they're filled. Everything else gets a `default`. Mark credentials `secret: true` —
on K8s they go through a generated Secret, never plaintext env. `type` / `enum` / `minimum` /
`maximum` / `pattern` / `items` are documentation only; values are never validated. A `default` is
injected exactly as written: `default: 1.10` arrives as `1.10`, not `1.1`.

There are no resource declarations: a database, cache or queue your component uses is simply a set
of config keys you declare (`DB_HOST`, `DB_PORT`, `DB_PASSWORD` with `secret: true`, …). Sharing one
value between several components is the project's business (`config/vars.yaml`); declare only what you need.

**3. Never use a reserved name as a config key.**

| Reserved | How it matches |
| --- | --- |
| `COMPONENT_ID`, `COMPONENT_VERSION`, `PORT`, `BRICKKIT_SERVED_MEMBERS`, `BRICKKIT_SERVED_MEMBERS_CONFIG` | Exact |
| `*_ENDPOINT` | Suffix |

These are injected by the platform (`PORT` is the listen port given to a `mode: local` process). A colliding key warns at `lint` / `up`, and the platform's value
wins — your item silently has no effect.

**4. Dependencies are exact, and one id appears once.**

`dependencies.components` only: `- erp/api@1.0.0` (required) or `- id: infra/cache@1.0.0` +
`optional: true`. You can't depend on two versions of one id — the variable `ERP_API_ENDPOINT`
carries no version, so they would collide. Diamonds (A→X@1, B→X@2) are fine; each gets its own.

**5. A missing optional dependency injects nothing, not an empty string.**

Read it with `os.environ.get()` / `System.getenv()`; `os.environ["X"]` crashes — deliberately, so
absence shows up at startup instead of as a request to an empty address. Degradation is your code.

**6. `/healthz` only checks this process.** Never check a database or a dependency in it: one
downstream hiccup would mark every upstream unhealthy at once. `interval`/`timeout`/`failureThreshold`
are fixed (10s/3s/3), so the startup grace `startPeriodSeconds` defaults to 60. A cold start longer
than that (heavy Spring Boot, Django preloading, .NET JIT) must raise it: on Docker the component turns
`unhealthy` and `up` fails; on K8s it CrashLoopBackOffs forever while the logs look fine. The grace
period only delays "declared dead", never "declared alive", so setting it generously costs nothing.

**7. The migration runs from the same image — fail fast on unknown arguments.**

`migration.command` (array form) runs before the main service, with every config env var. If your
entrypoint falls through to "start the service" on an argument it doesn't recognize, a typo turns
the migration into a second server that never exits, and the deployment hangs. Validate the argument
before reading env vars or connecting to anything. Migration state is yours to keep; when two components
share a database, the state table's key must include the component's identity.

**8. `deployment.image` and `deployment.build`: at least one.**

`build: { context: ., dockerfile: Dockerfile }` lets `brickkit build` build it; `image:` lets git or
market consumers pull it. The image **tag is always `metadata.version`**. `up` never builds, and
`brickkit build` skips a version whose image already exists — so changed code without a version bump
needs either a bump or `brickkit build <id> --force` on the user's side.

**9. `deployment.resources`: write `requests`, leave `limits` to the deployer.** Quotas merge field by
field (deploy entry > `component.yaml` > default), so a `limits.cpu` you write can never be removed.

**10. `deployment.labels` values must be quoted strings**, and reserved keys (`app`, `brickkit.io/*`,
`com.docker.compose.*`) are rejected. Write only facts you own (`prometheus.io/port: "9090"`).

**11. A shell declares the exact member versions it compiles in.**

`shell: { members: [erp/api@1.2.0, erp/auth@1.2.0] }` makes the component a shell. Those versions
are a promise about what's inside the image: a project may only host exactly those versions in it,
and a locally built shell image records them in a label that `up` checks. Change a member version →
bump and rebuild the shell. Each member still needs its own image (its migration runs with it). At
start the shell reads two variables:

- `BRICKKIT_SERVED_MEMBERS`: the versioned service names hosted this run, comma-separated. Initialise
  only those modules; an empty string means none is hosted — not "the variable is missing, start all".
- `BRICKKIT_SERVED_MEMBERS_CONFIG`: a JSON array, one object per hosted member —
  `{componentId, version, httpPort, extraPorts: [{name, port}], config}`. `config` is the member's whole
  environment (its config keys and its dependencies' `*_ENDPOINT`) with every value already resolved
  (`$var:`, `${VAR}`, `file://`). Nothing of a member's config is put into the shell's own environment:
  read it from here, by the member's own key names. A member key written as `existingSecret` is refused,
  because the CLI can't read a value that lives only in the cluster. With no member hosted it is `[]`,
  never absent. One entry:

  ```json
  [{"componentId": "erp/api", "version": "1.2.0", "httpPort": 8080,
    "extraPorts": [{"name": "grpc", "port": 9090}],
    "config": {"DB_HOST": "db.internal", "ERP_AUTH_ENDPOINT": "http://erp-shell-1-0-0:8081"}}]
  ```

**Compiled in ≠ hosted.** `shell.members` is what the image contains, and lists at least one member —
`members: []` fails `lint` and `add` with `MANIFEST_INVALID`. Which of them run inside it this time is
the deploy file's choice (members nested under the shell entry), and may be none. So a new shell
enters the project together with its first member: build that member, list it in `shell.members`,
then `brickkit add` the shell. Callers keep using a hosted member's own service name — it resolves to
the shell (a network alias on Docker / Podman, a Service selecting the shell's Pod on Kubernetes).

**12. A component carries five documents, each for one reader — keep them in step with the code.**

| File | Reader | Holds |
| --- | --- | --- |
| `BRICKKIT.md` | Projects using it (their AIs read the cached copy at `.brickkit/manifests/<scope>/<name>/<version>/BRICKKIT.md`) | Six sections: `Purpose` (what it owns, what it does not own and who does), `Before you deploy`, `Dependencies`, `Configuration`, `Contracts`, `Shell declaration` |
| `AGENTS.md` | The AI developing it | Five sections — `Code map` (tables; paths in backticks, directories end in `/`), `Build and test`, `Design decisions`, `Pitfalls` (never / symptom / why), `Before changing code` — then the block maintained by brickkit |
| `CLAUDE.md` | Claude Code | Exactly `@AGENTS.md` |
| `README.md` | People on GitHub | `Use it in a project`, `Documentation` (a table pointing at the file that answers each question), `Development` |
| `component.yaml` | The CLI | Dependencies, config keys, ports, image; optional `metadata.repository` is the link a project's component table shows |

- One fact, one home: dependencies and config keys live in `component.yaml`, interfaces in the contract files, history in Git. The docs explain what those can't say — they don't restate it.
- `BRICKKIT.md` has **no relative links**: it is read alone in other projects' caches. Name files as inline code.
- No component doc links out of the component (`../…`) — not `AGENTS.md`, `README.md` or `docs/` either: the repository is cloned and read on its own (`DOC_LINK_NOT_PORTABLE`).
- A shell's `Shell declaration` section lists the same members as `shell.members`.
- Translations are siblings — `BRICKKIT.zh.md`, `README.zh.md`, `docs/design.zh.md` — or, for a whole bilingual `docs/`, a tree per language: `docs/<primary>/` and `docs/<lang>/` with the same relative paths, every page in every tree (the primary language is the `lang=` of the `AGENTS.md` block). The primary is canonical; each language version links every other near the top (not `BRICKKIT*.md`); a translation has the same `##` sections (the brickkit-maintained block isn't counted). `AGENTS.md` is usually not translated; an `AGENTS.<lang>.md` for human reviewers is checked like any translation. Read and write the primary only.
- Fixed section headings are matched exactly, in English or Chinese — a Chinese translation uses these names, not its own: `BRICKKIT.md` 组件定位 / 部署前准备 / 依赖说明 / 配置指南 / 契约索引 / 外壳声明; `AGENTS.md` 代码地图 / 构建与测试 / 设计取舍 / 易错点 / 改代码前自查; `README.md` 在项目里使用 / 文档 / 开发.
- `brickkit lint` checks all of this (warnings; `--strict` fails on them): `DOC_FILE_MISSING`, `DOC_SECTION_MISSING`, `DOC_PATH_MISSING` (a code-map path that's gone — every inline-code token in the first column of the first table is a path, `main.go` and `Dockerfile` included; elsewhere only tokens containing `/` count, and a token starting with `/` is a route such as `/healthz`, never a path), `DOC_LINK_BROKEN`, `DOC_LINK_NOT_PORTABLE`, `DOC_OUT_OF_STEP` (a dependency, required key, contract file or shell member that `component.yaml` has and the doc doesn't mention), `DOC_PLACEHOLDER`, `DOC_TRANSLATION_DRIFT`, `AGENTS_BLOCK_MISSING`, `CLAUDE_IMPORT_MISSING`. Change the docs in the same commit as the code: the next AI reads what you left.
- The docs are part of the version, like the code. Work towards a new version — bump `metadata.version` first, test, then release — and edit it freely until it's released. Never change a released version in place: a machine where it is a local source and a machine that takes it from the tag then write different rows into the component table of the project's `AGENTS.md`, back and forth.

## Releasing a version

A release is a Git tag. Bump `metadata.version`, commit, push, then `brickkit release` (or
`--path <dir>`; `--local` releases every local-source component). It refuses (`RELEASE_BLOCKED`)
unless `component.yaml` validates, the component directory is clean, the branch has an upstream with nothing unpushed, and the
tag doesn't exist. Tags are `1.2.0` (no `v`), or `<scope>-<name>/1.2.0` for a monorepo subdirectory.
A failed push deletes the local tag again. A `brickkit.yaml` next to `component.yaml` (a local
workbench) plays no part in what gets released — `release` reads only `component.yaml` — but its
files still count for "the component directory is clean": commit the workbench (`brickkit.yaml`,
`deploy.yaml`, `config/`, `.gitignore`) or `release` refuses. A tag that exists only locally (never
pushed) is not a release: push it or delete it. `brickkit publish` to a market is separate.

Release notes are optional but are what projects read before they upgrade: `brickkit release
--notes-file <file>` (or `--notes "<text>"`) writes Markdown, verbatim, into an annotated tag, and
`brickkit upgrade` prints the notes of every version it crosses before changing anything. Lead with
what a project must do — a key whose meaning or unit changed, an endpoint removed — then what was
added. Keep the file outside the component directory (an untracked file there fails the clean check).
Not with `--local` (one note per component version); `publish` takes the same two flags.

## How the mechanism works

**Addresses**: each dependency's main port is `{ID}_ENDPOINT` (`/` and `-` → `_`, uppercase), extra
ports `{ID}_{NAME}_ENDPOINT` (the port name the same way: `admin-api` → `ERP_API_ADMIN_API_ENDPOINT`);
the value points at the versioned service name — identical on Docker and K8s, so code never changes:

```
PEOPLE_BASIC_ENDPOINT=http://people-basic-1-0-0:8080
PEOPLE_BASIC_GRPC_ENDPOINT=http://people-basic-1-0-0:9090
```

The service name is the id and the exact version joined by `-`, with `/` and `.` → `-`. A dependency
hosted in a shell is reached at the shell's address, on the port the dependency itself declares — the
caller neither knows nor needs to. `COMPONENT_ID` and `COMPONENT_VERSION` are always injected.
`deployment.port` (required) serves the health check and `_ENDPOINT`. `deployment.type` is always
`container`, frontends included (an nginx container, `port: 80`).

**Local runs**: in `mode: local` BrickKit detects how to start your code from its source; the optional
`local: { language, runCommand }` block overrides detection. The repo's `metadata.version` must equal
the project's default version of the component. The process inherits the terminal's environment except
the names the platform owns — `COMPONENT_ID`, `COMPONENT_VERSION`, `PORT`, `BRICKKIT_SERVED_MEMBERS`,
`BRICKKIT_SERVED_MEMBERS_CONFIG`, every `*_ENDPOINT` and your own `configSchema` keys: those come only
from BrickKit, so a value left exported in your shell can't stand in for one the project didn't give.

**A workbench**: the component's repository can have its own `brickkit.yaml` for local integration work
(`brickkit init` in the repository, or `brickkit add --local --init` in the project above). `up` and `add`
treat it as a project; `release` reads only `component.yaml`.

## Where to dig deeper

- Flags: `brickkit new --help`, `brickkit lint --help`, `brickkit build --help`, `brickkit release --help`
- The full specification: <https://github.com/brickKit/brickKit> and its root `AGENTS.md`
- Examples: the cached `BRICKKIT.md` and `component.yaml` of any component under `.brickkit/manifests/`
