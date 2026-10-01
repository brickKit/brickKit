# A component's documentation

A component is read by three kinds of reader who want different things:

- **The projects that use it**, and the AI assistants writing code in them. They want to know what the component does,
  what to prepare, how to configure it and how to call it — without reading its source.
- **Whoever develops it**, very often an AI. It wants to find the code for a feature fast, run the tests, and know what
  not to break.
- **People on GitHub**, deciding whether to use it. They want a short answer and a pointer to the rest.

So a component carries one document per reader, plus the manifest. This page says what goes in each, how to write
them in more than one language, and what `brickkit lint` checks. `brickkit new` writes every one of them as a skeleton
(see [Generating a skeleton](04-new-and-skeleton.md)).

## Who reads what

| File | Required | Reader | Holds |
| --- | --- | --- | --- |
| `component.yaml` | ✅ | The CLI | Dependencies, config keys, ports, image — see [component.yaml](02-component-yaml-reference.md) |
| Contract files | When there is an interface | Callers and their AIs | Interfaces and event formats, registered under `artifacts` — see [Contracts and artifacts](06-artifacts-and-contracts.md) |
| `BRICKKIT.md` | ✅ | Projects using it; travels with every version | What it owns, what to prepare, how to configure and call it |
| `AGENTS.md` | ✅ | The AI developing it | Where the code is, how to build and test, what not to break |
| `CLAUDE.md` | ✅ | Claude Code | Exactly one line, `@AGENTS.md` |
| `README.md` | ✅ | People on GitHub | A short introduction and a map of the other files |
| `docs/` | Optional | Whoever goes deep | Design, numbered decisions, a data model |
| `CHANGELOG.md` | Optional, never checked | Anyone | Link it from the README if you keep one |

`CLAUDE.md` exists because Claude Code reads `CLAUDE.md` and other AI tools read `AGENTS.md`: one line imports the
other, and the content is written once.

## One fact, one home

The same fact written in two files is how documentation goes wrong: one copy gets updated, the other doesn't, and the
next reader trusts the stale one. So every fact has exactly one home, and every other file names it or links to it.

| Fact | Its home |
| --- | --- |
| Dependencies, config keys, ports, image, shell members | `component.yaml` |
| Interfaces and event formats | The contract files |
| What the component owns and doesn't; how to use, configure and prepare it | `BRICKKIT.md` |
| Where the code is; how to build, test and change it | `AGENTS.md` |
| Why it is designed this way, when that takes more than a few lines | `docs/` |
| What changed when | Git — tags and commits |

Explaining a fact is not restating it. `BRICKKIT.md`'s Configuration section explains how to choose a value for a key
`component.yaml` declares; writing the key's type or default there again is restating it. And no document carries
history ("v1.0.12: …", "added in phase 3"): documents say what is true now.

## `BRICKKIT.md`

It is the only document that leaves the repository. When a project adds the component, the CLI caches it next to the
component's `component.yaml` in that project — alone, without the repository around it. Six sections, in this order:

| Section | What goes in it |
| --- | --- |
| `Purpose` | One or two sentences on the problem it solves, then two short lists: what it **owns**, and what it **does not own** — each of those naming who does instead (another component, the caller, a person) |
| `Before you deploy` | What must exist before `brickkit up`: a database with its schema and role, a third-party account, a certificate. For each, who prepares it and how to check it's ready. "Nothing beyond the configuration below." when there's nothing |
| `Dependencies` | Each dependency by ID, what it is used for; for an optional one, what happens when it is absent |
| `Configuration` | What `configSchema` can't say: how to choose a value, what changing it does, which values go together. Every `required` key appears here |
| `Contracts` | Every file under `artifacts`, named as `artifacts` writes it, with the main interfaces it describes; the events it publishes; the events it consumes |
| `Shell declaration` | "Not a shell.", or the members it compiles in |

Two rules follow from where it is read:

- **No relative links.** In another project's cache, `[design](docs/design.md)` points at nothing. Name repository
  files as inline code (`api/openapi.yaml`); absolute URLs are fine.
- **The "does not own" list matters most.** A project that has only this cached file decides from it which component
  a new requirement belongs to (see [Judging and planning a requirement](../08-ai-guide/03-judging-a-requirement.md)).

## `AGENTS.md`

It is the developer's guide — what an AI working in this repository reads first, every session. It starts with the
component ID and a sentence pointing at `BRICKKIT.md` (for usage and boundaries) and `component.yaml` (for
dependencies and configuration). Then five sections:

| Section | What goes in it |
| --- | --- |
| `Code map` | Tables only. One maps each path to what it owns; one maps each feature to the file to start in. Paths in backticks; a directory ends in `/`. A backticked token starting with `/` (an HTTP route such as `/api/v1/call`) is not a path and is not checked |
| `Build and test` | The exact commands to build, test, run locally and check the contract, and what success looks like |
| `Design decisions` | Why it doesn't depend on some component; alternatives rejected and why. Longer reasoning goes in `docs/`, linked from here |
| `Pitfalls` | A table: never / symptom / why. Only what is specific to this component — rules for the whole project live in the project's `AGENTS.md` |
| `Before changing code` | Three to eight checks specific to this component |

At the end is a block maintained by brickkit, between `<!-- brickkit:managed:begin lang=… -->` and
`<!-- brickkit:managed:end -->`: the few platform rules every component author must keep. Only what is between those
markers is ever written by brickkit, and `brickkit skills status` says when that text is older than what this CLI
writes; `brickkit skills update` refreshes it.

`AGENTS.md` is written once, in the team's working language, and not translated: an AI reads either language, and a
second copy is a second thing to keep in step. Two writing habits help an AI that may see only part of the file: no
"as mentioned above", and every "never" carries its symptom and its reason.

## `README.md`

A short page whose main job is to send people to the right file. Three sections:

| Section | What goes in it |
| --- | --- |
| `Use it in a project` | `brickkit add <id>@<version>` and `brickkit up`, then a pointer to "Before you deploy" |
| `Documentation` | A table: what you want to know → the file that answers it (`BRICKKIT.md`, the contracts, `component.yaml`, `AGENTS.md`) |
| `Development` | How to clone it and run it with its dependencies, then a pointer to `AGENTS.md` |

Its first line under the title is the same sentence as `metadata.description`. A project lists that sentence and the
optional `metadata.repository` (the component's repository or page) in the component table of its own `AGENTS.md`.

## `docs/`

Optional. Create it when the files above can't hold something: `docs/design.md` (revise it before the code when the
design changes), `docs/decisions/NNNN-<title>.md` (one decision per file, numbered, never renumbered),
`docs/data-model.md`. Link every file in it from `AGENTS.md` or `README.md`. A component's design lives in the
component repository, so it travels with the component's versions.

## More than one language

- **The file without a suffix is the primary language**, which the author chooses. A translation sits next to it with
  the language code before `.md`: `README.zh.md`, `BRICKKIT.zh.md`, `docs/design.zh.md`. A file and its translation are
  in the same directory, so their relative links are identical.
- Language codes are lowercase: `zh`, `ja`, `pt-br`.
- Translations are optional, per file. Typical: `README` and `BRICKKIT` translated (people and other teams read them),
  `AGENTS.md` and `docs/` not.
- **The primary file is right** when the two disagree. A change updates its translations in the same commit.
- A file with translations starts with a line linking every language version, for example
  `[English](README.md) · [Chinese](README.zh.md)` — except `BRICKKIT*.md`, which has no relative links at all. Every
  version carries the whole line: with three languages, each of the three links the other two.
- Headings are recognised in English and Chinese.

`brickkit add` caches every `BRICKKIT.<lang>.md` along with `BRICKKIT.md`, and `brickkit publish` uploads them.

## What `brickkit lint` checks

`lint` checks what a program can decide for certain; whether the writing is good is for review. Every finding is a
warning — `up` and `release` never stop on documentation, and `lint --strict` turns warnings into a failure for a team
that wants its CI to hold the line. It runs on a component repository, on a workbench, and in a project on every
local-source component. An invalid `component.yaml` doesn't stop it: the documents are still checked, only the
comparison with the manifest (`DOC_OUT_OF_STEP`) is skipped.

| Code | Means |
| --- | --- |
| `DOC_FILE_MISSING` | A required document is absent |
| `DOC_SECTION_MISSING` | A fixed section is absent |
| `DOC_PATH_MISSING` | A Code map path no longer exists |
| `DOC_LINK_BROKEN` | A relative link points at nothing |
| `DOC_LINK_NOT_PORTABLE` | `BRICKKIT.md` has a relative link, or a doc links out of the component |
| `DOC_OUT_OF_STEP` | `component.yaml` has a dependency, required key, contract file or shell member the doc doesn't mention where it belongs |
| `DOC_PLACEHOLDER` | `TODO`, `TBD` or `FIXME` is still in the text |
| `DOC_TRANSLATION_DRIFT` | A translation has no primary or a different number of sections, or a language version doesn't link every other one |
| `AGENTS_BLOCK_MISSING` | `AGENTS.md` has no block maintained by brickkit |
| `CLAUDE_IMPORT_MISSING` | `CLAUDE.md` doesn't import `AGENTS.md` |

What each one asks you to do is in [Error codes](../06-architecture/09-error-codes.md#documentation-checks).

## A complete example

`demo/quote` returns a quotation prefixed with `demo/hello`'s greeting. Its repository:

```text
demo-quote/
├── component.yaml
├── BRICKKIT.md
├── AGENTS.md
├── CLAUDE.md
├── README.md
├── api/openapi.yaml
├── main.go
└── Dockerfile
```

`BRICKKIT.md`:

```markdown
# demo/quote

## Purpose

Returns a quotation each time, prefixed with demo/hello's greeting; used by the "quote of the day" at the top of the page.

Owns:
- choosing and formatting the quotation

Does not own:
- the greeting itself: demo/hello
- showing it on the page: the caller

## Before you deploy

Nothing beyond the configuration below.

## Dependencies

- demo/hello (required): supplies the greeting. When it's unreachable, the endpoint still answers, with the greeting replaced by "(demo/hello unreachable)".

## Configuration

| Variable | Meaning |
|---|---|
| QUOTE_PREFIX | The text in front of the quotation. To drop the prefix, set it to the empty string "" |

## Contracts

- `api/openapi.yaml`: `GET /api/v1/quote`, returning `{"greeting": "...", "quote": "..."}`. No events.

## Shell declaration

Not a shell.
```

`AGENTS.md`:

```markdown
# demo/quote

Returns a quotation prefixed with demo/hello's greeting. How to use it and its boundaries: BRICKKIT.md. Dependencies and configuration: component.yaml.

## Code map

| Path | Owns |
|---|---|
| `main.go` | The HTTP server, the quotation list, the call to demo/hello |
| `api/openapi.yaml` | The contract |

| Feature | Start here | Then |
|---|---|---|
| The quotation endpoint | `main.go` (`/api/v1/quote`) | `api/openapi.yaml` |

## Build and test

`go build ./...`; `go test ./...` prints `ok`. To run it with demo/hello: `brickkit up` in this directory (a focus run in a project, or the workbench).

## Design decisions

It degrades instead of failing when demo/hello is down: the quotation is the content, the greeting is decoration.

## Pitfalls

| Never | Symptom | Why |
|---|---|---|
| Check demo/hello in `/healthz` | demo/quote goes unhealthy whenever demo/hello restarts | A health check checks only its own process |

## Before changing code

1. Does the change alter `/api/v1/quote`'s response? Update `api/openapi.yaml` and BRICKKIT.md's Contracts in the same commit.
2. A new config key? Declare it in `configSchema` and explain it in BRICKKIT.md's Configuration.

<!-- brickkit:managed:begin lang=en -->
… written by brickkit …
<!-- brickkit:managed:end -->
```

`README.md`:

```markdown
# demo/quote

Returns a quotation each time, prefixed with demo/hello's greeting.

## Use it in a project

    brickkit add demo/quote@0.1.0
    brickkit up

Prepare first: see "Before you deploy" in BRICKKIT.md.

## Documentation

| To find out | Read |
|---|---|
| What it does and doesn't, how to configure it, what to prepare | BRICKKIT.md |
| Its interface | api/openapi.yaml |
| What it depends on (explained in BRICKKIT.md) | component.yaml |
| How to develop it | AGENTS.md |

## Development

Clone it, run `brickkit init` for a workbench and `brickkit up` to run it with demo/hello; then read AGENTS.md.
```

In the real file each name in the Documentation table is a relative link to that file.

Note the Configuration row in `BRICKKIT.md`: the empty string drops the prefix. `component.yaml` can say
`default: "Quote of the day: "`, but not that — which is exactly what a user most wants to know.

## How it reaches users

When a project `add`s or `fetch`es the component, the CLI takes `BRICKKIT.md` and every `BRICKKIT.<lang>.md` from the
version (a local source's directory, a git tag, or the market) and caches them at

```text
.brickkit/manifests/<scope>/<name>/<version>/BRICKKIT.md
```

The project's `AGENTS.md` ends with a component table maintained by brickkit: each component, its version, its
`metadata.description`, which docs it carries (`BRICKKIT.md +zh`) and its `metadata.repository`. That table is where an AI
in the project starts; it then reads only the `BRICKKIT.md` of the components the task touches. When a local source
holds that exact version, the copy in its source directory is the one being edited and the one to read.

The cache follows the version it came from: fetching the version again drops a cached translation the version no
longer has, and `docs.list` next to the files records which ones the cache holds. When the cache holds no record for a
component — it isn't cached on this machine, or an older CLI cached it — the table keeps the Docs cell it already had rather than guessing.

So the documents are part of the version, like the code. Changing `BRICKKIT.md`, a translation or
`metadata.description` means a new version: bump `metadata.version`, `brickkit release`, then `brickkit upgrade` in
the project. Edited in a local source without a bump, one version number has two contents — the machine with the local
source writes the new row into the project's `AGENTS.md`, a machine without it writes the old one back, and
`brickkit skills status` on each calls the other's block outdated.

`brickkit publish` uploads `BRICKKIT.md` and its translations with the version: at most 16 translations, each at most
256 KiB, all of them together at most 1 MiB. Resuming an interrupted publish compares every one of them with what the
draft registered and stops if any differs. A market too old to store translations publishes the version without them,
and `publish` warns.
