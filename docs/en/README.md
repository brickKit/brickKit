# BrickKit documentation

This is BrickKit's English documentation. The number in front of each folder is the recommended reading order; each
folder's `README.md` says which pages it holds and which to read first. The Chinese documentation lives in `docs/zh/`
and mirrors this tree page for page, but each side is written on its own — neither is a translation of the other.

## Every module

| No. | Module | What you get from it |
| --- | --- | --- |
| 00 | [Overview and getting started](00-intro/README.md) | What BrickKit is, your first component running in five minutes, the glossary, the fractal structure |
| 01 | [The three layers](01-three-layers/README.md) | What `brickkit.yaml`, `deploy.yaml`, `deploy.local.yaml` and `config/` each own, how to write them, where a value comes from |
| 02 | [Running a project](02-project-guide/README.md) | Creating a project, adding components, local debugging, several environments, starting and stopping, upgrading, removing, building images |
| 03 | [Writing components](03-component-guide/README.md) | What a component repository looks like, how to write `component.yaml`, how to release, how to write its documents (`BRICKKIT.md`, `AGENTS.md`, `README.md`) |
| 04 | [Shells](04-shell/README.md) | Compiling several components into one process: members, config injection, upgrades |
| 05 | [Database migrations](05-migration/README.md) | How migrations run, which variables they get, the order when versions coexist |
| 06 | [Architecture](06-architecture/README.md) | The pipeline from declaration to running containers, dependency resolution, the injection contract, design principles, error codes |
| 07 | [CLI reference](07-cli-reference/README.md) | Every command, every flag |
| 08 | [For AI assistants](08-ai-guide/README.md) | Which files an AI should read, in what order, and how to write component docs |
| 09 | [Recommended practices](09-patterns/README.md) | Where to cut components, how to layer tests, seed data, calling dependencies reliably, choosing a deployment shape |
| 10 | [Troubleshooting](10-troubleshooting/README.md) | Symptom → cause → fix |
| 11 | [Reference](11-reference/README.md) | Every field of the three files, the `configSchema` specification, the JSON Schemas, the market API |

## Read by who you are

**New to BrickKit:** [What BrickKit is](00-intro/01-what-is-brickkit.md) →
[Quick start](00-intro/02-quick-start.md) → [Core concepts](00-intro/04-core-concepts.md) →
[The three layers at a glance](01-three-layers/01-overview.md), then dip into [Running a project](02-project-guide/README.md)
as you need it.

**Assembling components into a system:** [The three layers](01-three-layers/README.md) →
[Running a project](02-project-guide/README.md) → [CLI reference](07-cli-reference/README.md); when something goes
wrong, [Troubleshooting](10-troubleshooting/README.md).

**Writing a component:** [Writing components](03-component-guide/README.md) →
[component.yaml field reference](11-reference/01-component-yaml-schema.md) →
[Recommended practices](09-patterns/README.md); to compile several components into one process, read
[Shells](04-shell/README.md) as well.

**You are an AI assistant:** read [`AGENTS.md`](../../AGENTS.md) at the repository root first, then
[For AI assistants](08-ai-guide/README.md). To fetch a file's raw content, use the raw links in
[`llms.txt`](../../llms.txt).

## Tutorials

Hands-on tutorials (`tutorials/en/`) are not written yet. Until then, every command and every piece of output in the
[quick start](00-intro/02-quick-start.md) and the guides was produced by a real run.
