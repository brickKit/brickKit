# The component's BRICKKIT.md

## Why every component needs one

`component.yaml` states the "what": what it depends on, which config it needs, what the defaults are. It can't state the
"why" and the "how to use it": what happens when an optional dependency is missing? What should this config item be set
to, and what does changing it do? Where do I read about the interface?

The people using your component — and, more and more, the AI assistants writing code for them — need exactly the latter,
and need it without reading your source. `BRICKKIT.md` is that document: it sits at the component repository root, is
released with every version, and the CLI carries it into the projects that use the component.

## Five sections

The skeleton `brickkit new` generates already lists them:

| Section | What to write |
| --- | --- |
| Purpose | A sentence or two: the business problem it solves, and for whom |
| Dependencies | Which components it depends on, and what it uses each for; how it degrades when an optional one is missing |
| Configuration | What each item in `configSchema` means for the business — especially what the default can't say: how to choose a value, what changing it does |
| Contracts | Where the contract files are, and which interfaces each describes |
| Shell declaration | Whether it's a shell; if so, which members it compiles in |

Write for someone meeting the component for the first time: don't repeat what `component.yaml` already states (types, the
defaults themselves); write what it can't say.

## A complete example

```markdown
# demo/quote

## Purpose

Returns a quotation each time, prefixed with `demo/hello`'s greeting. Used by the "quote of the day" at the top of the page.

## Dependencies

- `demo/hello@1.1.0` (required): supplies the greeting. When it's unreachable, the endpoint still returns a quotation, with the greeting replaced by "(demo/hello unreachable)".

## Configuration

| Variable | Required | Meaning |
|---|---|---|
| `QUOTE_PREFIX` | No | The prefix in front of the quotation, "Quote of the day: " by default. To drop the prefix, set it to the empty string `""` |

## Contracts

- `api/openapi.yaml`: `GET /api/v1/quote`, returning `{"greeting": "...", "quote": "..."}`

## Shell declaration

Not a shell.
```

Note the Configuration row: the empty string drops the prefix — something the declaration `default: "Quote of the day: "`
can't express on its own, and exactly what a user is most likely to want to know.

## How it reaches users

When a user `add`s or `fetch`es your component, the CLI takes `BRICKKIT.md` from your tag (when the component carries
one), caches it at

```text
.brickkit/manifests/<scope>/<name>/<version>/BRICKKIT.md
```

and writes the path into the user's project map, `BRICKKIT.md`:

```text
| Component | Version | Doc | Contracts |
|---|---|---|---|
| demo/quote | 0.1.0 | `.brickkit/manifests/demo/quote/0.1.0/BRICKKIT.md` | `.brickkit/artifacts/demo-quote-0-1-0/` |
```

When the user cloned the component's source into `components/` and the working copy is at exactly this version, the doc
path points at the working copy's file — that's the one the author is editing; the cached copy is only a snapshot.

When publishing to a component market (`brickkit publish`), `BRICKKIT.md` is uploaded with the version. It should hold
only what callers need: a file that's too large or isn't text is stopped before the version is created.

## How it relates to the project's BRICKKIT.md

Two files with the same name, with different jobs:

| | The project's `BRICKKIT.md` | The component's `BRICKKIT.md` |
| --- | --- | --- |
| Where | The project root | The component repository root |
| Says | Which components the project uses, and where each one's docs and contracts are | What this one component is, how to configure it, how to call it |
| Written by | The CLI maintains the component table (`add` / `remove` / `upgrade` rewrite the part between the markers); the rest is yours | The component's author |
| Acts as | A routing table | The content |

Reading an unfamiliar project, start from the project's `BRICKKIT.md`: it tells you where each component's docs are; to
understand a component, read its own file rather than digging through source.
