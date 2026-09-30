# Reading BrickKit itself

The other pages of this module are about an AI working **in a project that uses BrickKit**. This one is about an AI
reading **BrickKit itself** — asked to explain or evaluate the platform, to answer a question about it, or to develop it
in a clone of this repository.

## Three routes

| The task | Read | How many fetches |
| --- | --- | --- |
| Understand or evaluate BrickKit, or read everything | [`llms/en/00-core.md`](../../../llms/en/00-core.md), then follow each file's "Next" line | One for the core; about nine for everything |
| Answer one question | [`llms.txt`](../../../llms.txt): find the page whose one-line description fits, fetch just that page | Two |
| Develop in a local clone | [`AGENTS.md`](../../../AGENTS.md): the doc map (§9) and the code map (§10) | Already loaded — Claude Code reads it every session through `CLAUDE.md` |

On the web, every path is relative to the repository root: prefix it with
`https://raw.githubusercontent.com/brickKit/brickKit/main/`. The Chinese tree has the same routes:
`llms/zh/00-core.md`, `llms.zh.txt`, `AGENTS.zh.md`.

## The bundles

Reading 94 pages one fetch at a time is slow and easy to leave half-done, so the documentation also comes as
**bundles**: a few files that together hold every page once.

- **`00-core.md`** is `AGENTS.md` plus the pages that explain what BrickKit is, how to get it running, the core concepts,
  the fractal structure and the three layers. On its own it is enough to discuss the platform.
- **`01.md` … `NN.md`** hold every other page, in reading order. A file is filled until the next page would take it
  over **100 KB**, so one fetch reads a whole file; a page is never split across two.
- Each file starts with which part it is, the pages it contains, and the address of the next part. Each page inside
  starts with `> File: docs/en/…`, so after reading a bundle you know which single file to fetch again later.
- Links inside a bundle point back at the original files, so they still say where each one goes.

## Locating code in a clone

`AGENTS.md` §10 is the code map: every package under `internal/` and `cmd/` with what it owns, a table from each
command and cross-cutting feature to the file it starts in, and where tests and checks live. A test fails when a package
is missing from it or a path in it no longer exists, so it can be trusted. Building, testing and conventions are in
[`CONTRIBUTING.md`](../../../CONTRIBUTING.md).

## How the bundles stay current

They are generated: `make generate-llms` rebuilds `llms/` and the bundle list in `llms.txt`. The repository's commit
hook (enable it once with `make hooks`) runs it whenever a commit touches the docs, and `make lint` fails when the
bundles are stale — so what a web AI reads is what the docs say.
