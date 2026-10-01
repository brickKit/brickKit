# Component and project documentation: one fact, one home; one reader, one file

Date: 2026-10-01. Status: design, decided in conversation (the maintainer delegated every content decision to the
author of this spec); this is the written spec.

## 1. Goal

The maintainer will restructure `be-assembly-standard` (a 62-component ERP/CRM assembly built on an old BrickKit) to
follow this spec, then develop new requirements on it and report back. The spec is therefore written for the reader who
will use it most: **an AI assistant developing in such a project**. Priority of readers, from the maintainer:

1. **The developing AI, locally.** From the project's `AGENTS.md` it reaches, in a few reads: what the project is and
   what each component does; the rules the project follows; where any feature's code is; each component's contracts,
   dependencies and boundaries. Given a new requirement, it can judge which component owns it and whether it is sound,
   plan the change across components, and make it.
2. **Users on GitHub.** A component's `README.md` says what it does, how to deploy it, what to prepare first, where the
   contracts are, what it depends on and how it communicates — mostly by pointing at the file that owns each answer.
3. **Web / API readers.** The same files, reached from the README or the project's `AGENTS.md`.

Multilingual means natural languages (a component documented in English and Chinese). Programming languages are each
component's own choice; nothing here depends on one.

Decided in conversation:

- `AGENTS.md` is the standard cross-tool AI guide and belongs to the author — in projects and in components alike.
  BrickKit stops shipping a file under that name.
- Docs get **mechanical checks in `brickkit lint`, warnings only** (`lint --strict` already turns warnings into
  failures for a team that wants a gate). `up` and `release` never block on documentation.
- The component doc that travels to consumers (`BRICKKIT.md`) may have **language versions** (`BRICKKIT.zh.md`); the
  CLI caches and publishes all of them.
- **No `CHANGELOG.md` requirement.** History lives in Git (tags, commits). Required files are only those the work
  produces anyway or that someone actually reads.

## 2. What is wrong today

### 2.1 In BrickKit

| Problem | Effect |
| --- | --- |
| `brickkit skills` installs `AGENTS.md` at the project root as a CLI-owned asset | The project has nowhere to write its own AI guide; the moment the user edits the file, the CLI stops updating it |
| The project map is called `BRICKKIT.md`, the same name as each component's consumer doc | Two jobs under one name; every explanation of either has to disambiguate |
| `skills.lock` lives in `.brickkit/`, which `.gitignore` excludes | On a teammate's fresh clone every skill file is "untracked" and never updated again; the project's skill language is lost |
| The project map's managed block is rendered in the language of whoever ran the last `add` | Two teammates with different CLI languages rewrite the committed file back and forth |
| The component `BRICKKIT.md` spec has no boundary ("what this does not do") and no "before you deploy" | A project that has only the cached doc cannot tell which component a new requirement belongs to, nor what to prepare |
| `BRICKKIT.md` may contain relative links | It is cached alone under `.brickkit/manifests/…`, without the repository: every relative link is dead there |
| No spec for a component's `AGENTS.md`, `README.md` or code map; `brickkit new` writes only `component.yaml` and `BRICKKIT.md` | Each team invents its own; locating code means grepping |

### 2.2 In be-assembly-standard (the case this spec is checked against)

| Observation | Cost to an AI |
| --- | --- |
| Contract lists and dependencies are written in `README.md`, `AGENTS.md`, `docs/手册.md` and the design plan | One of four copies is always stale, and that is the one the AI codes against |
| Version history in `component.yaml` comments (erp/sales: 60+ lines) | Every read of the manifest starts with a page of history |
| Per-component design plans live in the assembly repository | The "why" does not travel with the component's versions |
| Root `AGENTS.md` is 62 KB with phase narrative, loaded every session | The rules drown in history; context is spent on the past |
| "Feature reference: to be filled in with Task 17" placeholders | Nothing to navigate code by |
| Component docs point at assembly-repository files (`registry/ports.tsv`, "Design Book §12.4") | The component cannot be understood outside that one project |

Its documentation standard also has ideas worth keeping, adopted below: write "not my responsibility" first; "why not
depend on X"; pitfalls as *never / symptom / why*; a self-check before changing code; no backward references ("as
mentioned above"); routing tables keyed by what a reader would search for; one job per document.

## 3. Principles

1. **One fact, one home.** Every fact has exactly one file that owns it; every other file links or names it, never
   restates it.

   | Fact | Home |
   | --- | --- |
   | Dependencies, config keys, ports, image, shell members | `component.yaml` |
   | Interfaces and event formats | The contract files, registered under `artifacts` |
   | What the component owns and does not own; how to use, configure and prepare it | `BRICKKIT.md` |
   | Where the code is; how to build, test and change it | `AGENTS.md` |
   | Why it is designed this way (beyond a few lines) | `docs/` |
   | History | Git |

   Explaining a fact is not restating it: `BRICKKIT.md`'s Configuration section explains how to choose a value of a
   key that `component.yaml` declares. Writing the key's type or default again is restating.

2. **State, not history.** Docs say what is true now. No phase narrative, no "v1.0.12: …", no "added in Task 14".
3. **Required = produced anyway or actually read.** Required files: `component.yaml`, contracts (when there is an
   interface), `BRICKKIT.md`, `AGENTS.md` + `CLAUDE.md`, `README.md`. Everything else is optional.
4. **Portable component docs.** A component is used by projects its author never sees. Its docs never depend on one
   project's files; a value a project convention dictates (a port from a registry) is written as the value.
5. **Only mechanical checks.** `lint` checks what a program can decide for certain (sections present, paths exist,
   links resolve, manifest facts mentioned, no placeholders, translations in step). Quality is review's job.

## 4. The component repository

### 4.1 Files

| File | Required | Reader | Holds |
| --- | --- | --- | --- |
| `component.yaml` | ✅ | The CLI | The manifest |
| Contract files | When it has an interface | Callers and their AIs | Interfaces (`type: api-contract`) and event formats (`type: event-contract`), registered under `artifacts` |
| `BRICKKIT.md` (+ `BRICKKIT.<lang>.md`) | ✅ | Consumers' AIs and people; travels with every version | §4.2 |
| `AGENTS.md` | ✅ | The AI developing this component | §4.3 |
| `CLAUDE.md` | ✅ | Claude Code | Exactly `@AGENTS.md` (Claude Code reads `CLAUDE.md`, other tools read `AGENTS.md`; content is written once) |
| `README.md` (+ `README.<lang>.md`) | ✅ | People on GitHub; web readers | §4.4 |
| `docs/` | Optional | Whoever goes deep | §4.5 |
| `CHANGELOG.md` | Optional, never checked | Anyone | Link it from the README if it exists |

Nested `CLAUDE.md`/`AGENTS.md` work inside a project too: a component under `components/<scope>/<name>/` brings its own
guide, which Claude Code loads when working in that directory.

### 4.2 `BRICKKIT.md`: six sections

Written for someone meeting the component for the first time, who may have only this file (it is cached alone).
Headings are fixed (English / Chinese):

| # | Heading | Holds |
| --- | --- | --- |
| 1 | `Purpose` / `组件定位` | One or two sentences, then two short lists: **Owns** / `负责` and **Does not own** / `不负责` — each item of the second saying who owns it instead (another component, the caller, a person) |
| 2 | `Before you deploy` / `部署前准备` | What must exist before `up`: a database with its schema and role, a third-party account, a certificate… For each: who prepares it, and how to check it is ready. "Nothing beyond the configuration below." when there is nothing |
| 3 | `Dependencies` / `依赖说明` | Each dependency by ID (no version — the version is `component.yaml`'s), what it is used for; for an optional one, what happens when it is absent |
| 4 | `Configuration` / `配置指南` | What `configSchema` cannot say: how to choose a value, what changing it does, which values go together. Every `required` key appears here |
| 5 | `Contracts` / `契约索引` | Every file under `artifacts`, by its path as written there, with the main interfaces it describes; events published; events consumed. This is how callers learn how the component communicates |
| 6 | `Shell declaration` / `外壳声明` | "Not a shell." or the member IDs it compiles in |

Rules:

- **No relative links** (they are dead in the cache). Repository files are named as inline code; contract paths exactly
  as `artifacts[].files` writes them (the consumer finds them under `.brickkit/artifacts/<service-name>/<type>/`).
  Absolute URLs are fine.
- What `component.yaml` already says (types, defaults, versions) is not repeated.
- Implementation details are not promises and stay out; they belong in `AGENTS.md`.

### 4.3 `AGENTS.md`: the developer's guide

```markdown
# <scope>/<name>

<one or two sentences: what it is and its role>. How to use it, its boundaries and contracts: `BRICKKIT.md`.
Dependencies, configuration and deployment: `component.yaml`.

## Code map
| Path | Owns |
|---|---|
| `backend/module/module.go` | The module entry |
| `backend/internal/tcc/` | The confirm-order compensation chain |

| Feature | Start here | Then |
|---|---|---|
| Confirm an order | `backend/internal/tcc/confirm.go` | `backend/internal/repo/` |

## Build and test
The exact commands — build, unit tests, local run, contract check — and what success looks like.

## Design decisions
Why it does not depend on X; alternatives rejected and why. Longer reasoning lives in `docs/`, linked from here.

## Pitfalls
| Never | Symptom | Why |
|---|---|---|
Only what is specific to this component; project-wide rules stay in the project's AGENTS.md.

## Before changing code
3–8 checks specific to this component.

<!-- brickkit:managed:begin lang=en -->
… the platform rules for component authors, maintained by the CLI (§6.1) …
<!-- brickkit:managed:end -->
```

Headings (English / Chinese): `Code map` / `代码地图`, `Build and test` / `构建与测试`, `Design decisions` / `设计取舍`,
`Pitfalls` / `易错点`, `Before changing code` / `改代码前自查`.

**Code map format.** Tables only. In the Code map section, every inline-code token in a table cell that contains `/`
and no whitespace, `*` or `://` is a path relative to the component root, and `lint` checks it exists. Directories end
with `/`.

**Written once, in the team's working language.** `AGENTS.md` is read by an AI every session; a translation doubles
the upkeep and buys nothing (an AI reads either language). No `AGENTS.<lang>.md` is expected; one that exists is checked
like any translation.

Writing rules (from be-assembly-standard's standard, kept): no backward references ("as mentioned above") — an AI may see
only part of the file; every prohibition carries its symptom and its reason.

### 4.4 `README.md`: a router for people

```markdown
# <scope>/<name>

<the same sentence as metadata.description>

## Use it in a project
brickkit add <scope>/<name>@<version>
brickkit up
Prepare first: see "Before you deploy" in BRICKKIT.md.

## Documentation
| To find out | Read |
|---|---|
| What it does and does not do; how to configure it; what to prepare | [BRICKKIT.md](BRICKKIT.md) |
| Its interfaces and events | [contracts/…](contracts/…) |
| What it depends on | [component.yaml](component.yaml) (`dependencies`), explained in BRICKKIT.md |
| How to develop it | [AGENTS.md](AGENTS.md) |

## Development
Clone, build the workbench (`brickkit init`), run (`brickkit up`); then AGENTS.md.
```

Headings (English / Chinese): `Use it in a project` / `在项目里使用`, `Documentation` / `文档`, `Development` / `开发`.
A line linking the language versions goes right under the title when there are any.

### 4.5 `docs/` (optional)

Created only when the files above can't hold something: `docs/design.md` (the design, revised before the code when the
design changes), `docs/decisions/NNNN-<title>.md` (one decision per file, numbered, never renumbered),
`docs/data-model.md`. Every file in `docs/` is linked from `AGENTS.md` or `README.md`. A component's own design lives
here, in the component repository, so it travels with the versions.

## 5. Multilingual

- **The unsuffixed file is the primary language**; the author chooses it. A translation is a **sibling**:
  `README.zh.md`, `BRICKKIT.zh.md`, `docs/design.zh.md`. One rule everywhere, and a file and its translation sit at the
  same depth, so their relative links are identical (be-assembly-standard's `{zh,en}/` split cost them a hand re-derivation
  of every `../` when files moved).
- Language codes: lowercase BCP 47, `^[a-z]{2,3}(-[a-z0-9]{2,8})*$` (`zh`, `ja`, `pt-br`).
- **Translations are optional, per file.** Typical: `README` and `BRICKKIT` translated (people and other teams read them),
  `AGENTS.md` and `docs/` not.
- **The primary file is canonical.** A change updates its translations in the same commit.
- Each file with translations starts with a line linking all its language versions
  (`[English](README.md) · [中文](README.zh.md)`).
- Required headings are recognised in English and Chinese. A translation in another language is checked only for being
  in step with its primary (§7.6). Adding a language to the CLI's catalog adds its heading names.

## 6. The project repository

### 6.1 `AGENTS.md`: the project's guide, with one CLI-maintained block

Author-owned sections (headings English / Chinese):

| Heading | Holds |
| --- | --- |
| `Overview` / `项目概述` | What the project is, its domains, how components group |
| `Conventions` / `项目约定` | Rules every component in this project follows (stack, port and schema registries, naming, review rules) — the project-wide copy; components never repeat them |
| `Where to look` / `查找路由` | A table: what you are doing (search-like words) → the file to read first. Fallback line: "Not here? The component table below, then the component's `AGENTS.md`." |
| `Pitfalls` / `易错点` | Project-wide, *never / symptom / why* |

Then the managed block, at the end of the file:

```markdown
<!-- brickkit:managed:begin lang=en -->
<!-- maintained by brickkit (init, add, remove, upgrade, skills update) — edits between these markers are overwritten -->
## BrickKit
<~10 lines: assembled with BrickKit; the three layers, one line each; five hard rules, one line each;
 skills in .claude/skills/; flags: brickkit <command> --help>

## Components
Docs: `<source>/BRICKKIT[.<lang>].md` for a local source at this version, otherwise
`.brickkit/manifests/<id>/<version>/BRICKKIT[.<lang>].md`. Contracts: `.brickkit/artifacts/<service-name>/`.
| Component | Version | What it does | Docs | Home |
|---|---|---|---|---|
| erp/sales | 1.0.26 | Sales quotations and orders | primary, zh | https://git.example.com/erp-sales |
| mdm/customer | 1.0.10 | Customer master data | primary | — |
<!-- brickkit:managed:end -->
```

- **What it does** is `metadata.description` from the manifest. **Docs** lists the versions present — `primary` for `BRICKKIT.md`,
  then each translation's code (`primary, zh`) — or `—` when the component carries no doc. **Home** is the new optional manifest field
  `metadata.repository` (the URL of the component's repository or page), so a web reader of the project repository,
  where `.brickkit/` does not exist, can still reach every component.
- **Only facts fixed by `brickkit.yaml` and the component version go into the block.** The file is committed and
  shared; anything that differs between machines (whether a local source holds a component, which install source served
  it) would make teammates rewrite it back and forth. So there is no local-sources table and no "where it was fetched
  from" column; the docs rule above covers local sources. When the cache lacks a component's manifest (a fresh clone),
  its row keeps the cells the existing block already has.
- One row per version line in `brickkit.yaml`, as today. A row costs ~120 bytes: 60 components ≈ 7 KB, acceptable in a
  file loaded every session; that budget is why paths are a rule stated once rather than a column.
- **The block records its language** (`lang=`), set by `init` from the CLI language and changed only by
  `brickkit skills update --lang`. Every rewrite uses the recorded language, never the language of whoever runs the
  command.
- **Where the block is rewritten:** `add` / `remove` / `upgrade` (where the old map was rewritten), `init`,
  `skills update`. Outside the markers the CLI never writes a byte.

### 6.2 `CLAUDE.md`

`init` creates it with `@AGENTS.md` when it does not exist. An existing `CLAUDE.md` without that line gets a warning
(`init`, `lint`) and is appended to only by `brickkit skills update` (an explicit request).

### 6.3 Workbench (component repository with `brickkit.yaml`)

One `AGENTS.md` serves both roles: the component's sections (§4.3), and one managed block holding the component-author
rules plus the component table of the workbench. The block's content follows what the directory has: `component.yaml` →
author rules; `brickkit.yaml` → the table.

### 6.4 Cross-component design, README

- Decisions that span components live in the project's `docs/decisions/NNNN-<title>.md`; decisions inside one component
  live in that component (§4.5). `Where to look` routes to them.
- A project `README.md` is the project's own business and is not checked. Recommended: one paragraph, how to bring it
  up, and a pointer to `AGENTS.md` for the component table.

### 6.5 The old project map goes away

`init` no longer writes a project-level `BRICKKIT.md`. In a project directory (no `component.yaml`), a root
`BRICKKIT.md` with the old markers is reported (`PROJECT_MAP_OBSOLETE`: the table now lives in `AGENTS.md`; move any notes
of your own and delete the file) by `init`, `skills update` and `lint`. The CLI never deletes it. From now on the name
`BRICKKIT.md` means one thing: a component's consumer doc.

## 7. How the AI works with it

This becomes a page of the AI guide and a project skill (§9). The routes:

### 7.1 Understanding a project

1. Project `AGENTS.md` (already loaded): conventions, pitfalls, the component table with what each does.
2. For a component the question touches: its `BRICKKIT.md` (boundary, contracts, configuration).
3. To change it: its `AGENTS.md` (code map, commands, pitfalls), then the code the map points to.
4. `brickkit deps <id>` / `brickkit graph` for the dependency structure; contracts under `.brickkit/artifacts/`.

Never load every component's docs: only the ones the task touches.

### 7.2 Judging a new requirement

1. **Find the owner.** The component table's "what it does" narrows it; the candidates' `Owns` / `Does not own` decide.
   "Does not own" items name who does.
2. **Check it against what is written:**
   - the owner's boundary — a requirement that needs it to own something it lists under "Does not own" is a boundary
     change, a person's decision;
   - project `Conventions` and `docs/decisions/` — conflicting with a recorded decision is a person's decision;
   - the dependency direction — a new dependency must not create a cycle (`brickkit deps`), and goes in the direction
     the existing design allows;
   - the contracts — additive changes are minor versions; removing or changing meaning is a major version and every
     consumer has to move.
3. **Outcomes:** fits one component; needs a provider's contract first (do the provider first); needs a new component
   (no owner and none should stretch); or conflicts — stop and put the conflict to the person, with the file that says
   so.

### 7.3 Planning and changing

- Order: providers before consumers (the dependency order `brickkit deps` prints).
- Per component: contract → code → `BRICKKIT.md` / `AGENTS.md` in the same commit → `metadata.version` (patch / minor /
  major by §7.2) → release → `brickkit upgrade` in the project.
- Test inside the project with a focus run (`brickkit up` in the component's directory).
- Done means `brickkit lint` shows no documentation warnings for what was changed: the next AI reads what this one left.

## 8. CLI changes

### 8.1 `init`

- Project or workbench: writes `AGENTS.md` (author section skeleton in the CLI language + managed block) and `CLAUDE.md`
  when missing; installs the skills; no project `BRICKKIT.md`.
- An existing `AGENTS.md` without a managed block: if it is the old CLI-installed asset unmodified (its content matches
  the sum an old `.brickkit/skills.lock` recorded for it), it is replaced by the new skeleton; otherwise a warning (`AGENTS_BLOCK_MISSING`) with the hint to run
  `brickkit skills update`, which appends the block. `init` itself never modifies an existing file.
- Old project `BRICKKIT.md`: §6.5.

### 8.2 `new`

Writes `component.yaml`, `BRICKKIT.md` (six sections), `AGENTS.md` (five sections + managed block), `CLAUDE.md`,
`README.md` (three sections), the contract placeholder with `--contract`. Placeholders are `<!-- TODO: … -->` comments,
so `lint` reports each until it is filled. Still no Dockerfile or code.

### 8.3 Skills without a lock file

`skills.lock` is removed. Each installed skill file ends with one line:

```text
<!-- brickkit:skill version=<cli version> sum=sha256:<hex> -->
```

`sum` is over the content without that line. States: missing; current (content equals the shipped asset); outdated
(the recorded sum matches the content, the asset differs); modified (the recorded sum does not match); untracked (no
marker). The file carries its own record, so a fresh clone knows everything. The skills' language is the managed
block's `lang` (§6.1); without an `AGENTS.md` block, the CLI language. An old `.brickkit/skills.lock` found is used once
to classify files, then deleted. The `AGENTS.md` asset leaves the skill assets: its content becomes the managed block's
text, rendered by the CLI.

### 8.4 Component docs: caching and publishing

- `add` / `fetch` cache `BRICKKIT.md` and every `BRICKKIT.<lang>.md` at the component root of that version (local:
  directory; git: the tag's tree; market: the docs endpoint) next to `component.yaml` under `.brickkit/manifests/…`.
- `publish` uploads each language version, each within `MaxDocBytes`, all of them together within
  `4 × MaxDocBytes` (so the market's request-size limit holds as it is); the market keeps `BRICKKIT.md` where it is and
  the translations keyed by language code, serves a translation at the existing docs endpoint with `?lang=<code>`, and
  rejects an invalid code or an oversized total. The market API reference is updated.
- `component.yaml` gains the optional `metadata.repository` (§6.1); `brickkit new` writes it commented out.

### 8.5 `lint`: documentation checks (warnings)

Run on: a component repository; a workbench (component checks + project checks); a project (project checks, plus
component checks for each local-source component under `components/`, grouped by component).

| Code | Fires when |
| --- | --- |
| `DOC_FILE_MISSING` | A required file is absent (`BRICKKIT.md`, `AGENTS.md`, `CLAUDE.md`, `README.md`) |
| `DOC_SECTION_MISSING` | A required heading is absent from a file (any recognised language per heading) |
| `DOC_PATH_MISSING` | A path in the Code map does not exist |
| `DOC_LINK_BROKEN` | A relative link (in `README*`, `AGENTS*`, `docs/**`) points at a file that does not exist |
| `DOC_LINK_NOT_PORTABLE` | `BRICKKIT*.md` has a relative link; or a component doc's relative link leaves the component directory |
| `DOC_OUT_OF_STEP` | A manifest fact is not mentioned where it belongs: a dependency ID in Dependencies, a `required` key in Configuration, an `artifacts` file in Contracts, a shell member in Shell declaration |
| `DOC_PLACEHOLDER` | `TODO`, `TBD`, `FIXME`, `待补`, `后补`, `待填` outside code, in the required files, their translations and `docs/**` |
| `DOC_TRANSLATION_DRIFT` | A translation has no primary, a different number of `##` sections, or a file with translations doesn't link all its language versions |
| `AGENTS_BLOCK_MISSING` | `AGENTS.md` exists without a managed block |
| `CLAUDE_IMPORT_MISSING` | `CLAUDE.md` exists without `@AGENTS.md` |
| `PROJECT_MAP_OBSOLETE` | §6.5 |

Each warning names the file and, where it applies, the line. They go into the error-code reference.

## 9. BrickKit's own docs and assets

- `docs/{en,zh}/03-component-guide/08-component-doc-spec.md` → **A component's documentation**: §3–§5 and §8.5 for authors,
  with a complete `demo/quote` example of every file.
- `docs/{en,zh}/08-ai-guide/`: `03-component-doc-spec.md` → reading and writing those files; a new page **Judging and
  planning a requirement** (§7) right after `02-ai-dev-workflow.md` (renumbered per the docs numbering convention);
  routing table and fractal-reading pages follow.
- A project-guide page on the project's `AGENTS.md` (§6), and every page that describes the project-level `BRICKKIT.md`,
  `init`'s or `new`'s output, the doc cache, the market docs API, lint, or error codes.
- `AGENTS.md` / `AGENTS.zh.md` §3.1, §3.3, §3.4; `llms*` regenerate.
- Skills: `brickkit-component` (the files and sections), `brickkit-assemble` (the table now lives in `AGENTS.md`), a new
  project skill **`brickkit-plan-change`** (§7.2–§7.3, triggered by "new requirement", "feature", "where should this go",
  "plan a change").
- Fixtures under `tests/components/` and the guide tutorials' projects follow the spec, so their `lint` is clean.

## 10. Testing

- Unit tests per check in §8.5, each written failing first, including: a path inside inline code outside the Code map
  (not checked), a link inside a code fence (not checked), a translation with an extra section, a dependency mentioned
  with a version (`erp/x@1.0.0` counts as mentioning `erp/x`).
- Managed block: rendered in the recorded language regardless of the CLI language; untouched bytes outside the markers
  (byte-for-byte); 60-row table size bound.
- Skills markers: each state, a fresh clone (no lock) classifying correctly, the one-time lock migration.
- Doc caching: a git tag with `BRICKKIT.md` + `BRICKKIT.zh.md` caches both; a market round trip with two languages.
- Guide tutorials' output checks updated; the regression checklist gets rows for the new behaviour.

## 11. Out of scope

- Restructuring be-assembly-standard (the maintainer does it with this spec).
- Judging doc quality; translating content; a per-component documentation bundle for the web (a component's few files,
  reached from its README, are already few fetches).
- Enforcing anything in `up` or `release`.

## 12. Calls made in writing this spec

Recorded so the maintainer can overrule any of them:

1. Paths in the project component table are a rule stated once, not a column (always-loaded budget, §6.1).
2. The map lists the language versions present instead of choosing one by the project's language: the file is committed
   and shared, while language preference is per person.
3. Translations are siblings (`X.<lang>.md`), also inside `docs/`, rather than `docs/{en,zh}/` trees (§5).
4. `AGENTS.md` is not translated (§4.3).
5. `skills.lock` is replaced by a marker inside each skill file, fixing the fresh-clone bug (§2.1, §8.3).
6. `init` never modifies an existing file; `skills update` may append the managed block or the `@AGENTS.md` line,
   because the user asked for exactly that.
7. Project `lint` checks local-source components' docs — those are the ones being developed here.
8. The component table's Home column comes from a new optional `metadata.repository` rather than from where a
   component was fetched, which differs between machines (§6.1).
