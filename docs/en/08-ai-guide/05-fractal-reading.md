# Reading fractally

BrickKit is fractal: a project is made of components, and a component under development is a small project of its own.
Each level has an entry file written for its reader, and each covers only its own level. An AI reads from the top down,
and each level, once read, says which file to read at the next — there's no need to load everything into context at
once.

## Three entry points

| Level | Entry point | What you know after reading it |
| --- | --- | --- |
| The project | The project's `AGENTS.md` (and `.claude/skills/`) | The team's conventions and pitfalls; at its end, the platform rules in brief and the component table — which components exist, what each does, where their docs and contracts are |
| A component, to use it | The component's `BRICKKIT.md` | What it owns and doesn't, what to prepare, how to configure it, how to call it |
| A component, to change it | The component's own `AGENTS.md`, in its source directory | Where its code is, how to build and test it, why it is designed the way it is |

Each level points at the next: the project's `AGENTS.md` ends with the table and the rule for where each component's
`BRICKKIT.md` is; a component's `BRICKKIT.md` is all a caller needs, and its `AGENTS.md` takes over only when the code
itself is to change.

A component under development is itself a project too (its own workbench), so the same levels repeat inside the
component repository — but **when using a component, read only what it publishes** (`BRICKKIT.md`, `component.yaml`,
contracts), never the workbench files in its repository.

## The reading order

1. The project's `AGENTS.md`: once per session is enough.
2. The `BRICKKIT.md` of the components the current task involves: usually only one or two.
3. When needed, that component's `component.yaml` and contracts.
4. When the task changes a component's code, that component's own `AGENTS.md`.
5. When needed, the relevant one of the three layers (the deploy file or `config/`).

**When to stop**: as soon as you can answer the current task's question. To fix a component's config, reading its
`configSchema` and its file under `config/` is enough; there's no need to read the components it depends on.

## The context window

- **Don't read everything.** With many components in a project, reading every component's docs wastes context and drags
  irrelevant details into your judgment.
- **Read on demand.** The routing table ([The routing table](01-ai-routing-table.md)) says which file answers each question.
- **Let the CLI answer derived questions.** Who depends on whom (`brickkit deps`), who runs this time (`brickkit up
  --dry-run`), which variables a component gets (the generated deployment files) — these are all computed from the three
  layers, and working them out yourself from the three layers is slow and error-prone.
- **Don't look for a "merged view".** The platform deliberately offers no command that merges the three layers into one:
  read the layer that owns each question, where the information is densest.
