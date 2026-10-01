# Component docs, from an AI's side

A component carries two documents written with an AI in mind: `BRICKKIT.md`, for whoever **uses** the component, and
`AGENTS.md`, for whoever **develops** it. What goes in each, and the rules they follow, are in
[A component's documentation](../03-component-guide/08-component-doc-spec.md); this page is about how an AI reads them
and how it writes them.

## Reading them

### Where they are

**`BRICKKIT.md`.** The component table at the end of the project's `AGENTS.md` says which docs each component carries
(the Docs column: `BRICKKIT.md`, `BRICKKIT.md +zh` when it carries a translation, `—` when it carries none), and the line
above the table gives the rule for where they are:

- when a local source holds the component's source at exactly this version, the `BRICKKIT.md` in that source directory;
- otherwise `.brickkit/manifests/<scope>/<name>/<version>/BRICKKIT.md`, cached by `add` or `fetch`.

A translation, `BRICKKIT.<lang>.md`, sits next to it. The file without a suffix is the primary one: when a translation
says something different, the primary wins. The Home column (`metadata.repository`) is the component's repository, for
reading it on the web, where there is no `.brickkit/`.

**`AGENTS.md`.** Only in the component's own repository: under `components/` when its source is cloned there (a local
source, or `brickkit add --repo`). It never travels into a project's cache — a project using the component has no need
for it.

### What each of `BRICKKIT.md`'s six sections gives you

| Section | Use it for |
| --- | --- |
| Purpose | Judging whether this is the component you're looking for. Its "Owns" and "Does not own" lists — each item saying who owns it instead — tell you where a requirement belongs |
| Before you deploy | What has to exist before `brickkit up` (a database with its schema and role, an account, a certificate) and who prepares it; check these before telling the user it's ready |
| Dependencies | What it uses each dependency for, and how it behaves when an optional one is missing |
| Configuration | What to fill in for each item when writing `config/<component>.yaml` — especially what the default can't say |
| Contracts | The interface files to write calling code from, and the events it publishes and consumes |
| Shell declaration | Whether it's a shell, and which members it compiles in |

When `BRICKKIT.md` and `component.yaml` disagree, `component.yaml` wins (it's all the CLI reads); tell the user about the
disagreement.

### The developer's `AGENTS.md`

Read it when you are about to **change** the component, not when you only call it:

| Section | Use it for |
| --- | --- |
| Code map | Where each thing lives, and for each feature where to start and what to read next — start from it instead of scanning the tree |
| Build and test | The exact commands, and what success looks like; run those, don't invent your own |
| Design decisions | Why it is the way it is, and what was rejected; don't undo a decision without reading why it was made |
| Pitfalls | Never / Symptom / Why, for this component |
| Before changing code | A short checklist to go through first |

The block at its end holds the component rules every BrickKit component follows. Rules that hold for every component in
one project are in that project's `AGENTS.md`, not repeated here.

## When there's no `BRICKKIT.md`

The Docs column in the project's component table is "—": the component carries no docs. Fall back to:

1. `component.yaml`: `metadata.description` for a one-line purpose; `dependencies`; the `description`, `default` and
   `required` of `configSchema`.
2. The contract files (`artifacts`): the interface.
3. When that's still not enough, tell the user the component lacks docs, rather than reading its source to guess — what's
   guessed there isn't a promise.

## Writing them

Sources of information, by reliability:

1. `component.yaml`: purpose, dependencies, config items, whether it's a shell — facts, restated directly.
2. The contract files: the list of endpoints and events.
3. The component's code: what the code actually does when an optional dependency is missing; how a config item is actually
   used; for `AGENTS.md`, where things really are and how it really builds.
4. The user: the business "why" — what problem the component solves, what it deliberately doesn't do, how to choose a
   config value, why a design was chosen.

When writing:

- **Don't repeat what `component.yaml` already says** (types, the defaults themselves); write what it can't: how to choose
  a value, what changing it does, what happens when a dependency is missing.
- **Change them in the same commit as the code.** `BRICKKIT.md` when what users see changes, `AGENTS.md` when the code
  map or a decision changes. Change behaviour without changing the docs, and the next AI reading them writes code against
  a wrong description.
- **Write `BRICKKIT.md` for someone meeting the component for the first time, without the repository.** It is read alone
  in other projects' caches, so it has no relative links: name a file as inline code (`api/openapi.yaml`), and use an
  absolute URL for anything outside. Leave out implementation details — they aren't promises.
- **A translation follows its primary.** It is a sibling file (`BRICKKIT.zh.md`), with the same sections, changed when
  the primary changes. `AGENTS.md` isn't translated.
- **Finish with `brickkit lint`.** It checks the sections, the code-map paths, the links, whether the docs still say what
  `component.yaml` says, the placeholders left, and the translations — as warnings, each naming the file and line.
