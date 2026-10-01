# Component docs, from an AI's side

Every component's `BRICKKIT.md` is a description written for the people (and AIs) using it. Its format and how to write it
are in [The component's BRICKKIT.md](../03-component-guide/08-component-doc-spec.md); this page is about how an AI uses one
and writes one.

## Reading it

**Where.** The component table in the project root's `BRICKKIT.md` has the path to each component's docs. Usually it's
`.brickkit/manifests/<scope>/<name>/<version>/BRICKKIT.md`; when the component's source is in a local source at exactly
this version, it's the `BRICKKIT.md` in that directory.

**What each of the five sections gives you:**

| Section | Use it for |
| --- | --- |
| Purpose | Judging whether this is the component you're looking for |
| Dependencies | Knowing how it behaves when an optional dependency is missing, and whether calling it needs degradation in mind |
| Configuration | What to fill in for each item when writing `config/<component>.yaml` — especially what the default can't say |
| Contracts | Finding the interface files and writing calling code from them |
| Shell declaration | Whether it's a shell, and what it compiles in |

When `BRICKKIT.md` and `component.yaml` disagree, `component.yaml` wins (it's all the CLI reads); tell the user about the
disagreement.

## When there's no `BRICKKIT.md`

The doc path in the project map is "—": the component carries no docs. Fall back to:

1. `component.yaml`: `metadata.description` for a one-line purpose; `dependencies`; the `description`, `default` and
   `required` of `configSchema`.
2. The contract files (`artifacts`): the interface.
3. When that's still not enough, tell the user the component lacks docs, rather than reading its source to guess — what's
   guessed there isn't a promise.

## Writing `BRICKKIT.md` for a component

Sources of information, by reliability:

1. `component.yaml`: purpose, dependencies, config items, whether it's a shell — facts, restated directly.
2. The contract files: the list of endpoints.
3. The component's code: what the code actually does when an optional dependency is missing; how a config item is actually
   used.
4. The user: the business "why" — what problem the component solves, how to choose a config value.

When writing:

- **Don't repeat what `component.yaml` already says** (types, the defaults themselves); write what it can't: how to choose
  a value, what changing it does, what happens when a dependency is missing.
- **Change it together with the code.** Change behaviour without changing the docs, and the next AI reading them writes
  code against a wrong description.
- **Write for someone meeting the component for the first time.** Leave out implementation details — they aren't promises.
