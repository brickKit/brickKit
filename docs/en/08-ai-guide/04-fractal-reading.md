# Reading fractally

BrickKit is fractal: platform, project and component are three levels, each with an entry file written for its reader,
and each covering only its own level. An AI reads from the top down, and each level, once read, says which file to read at
the next — there's no need to load everything into context at once.

## Three entry points

| Level | Entry point | What you know after reading it |
| --- | --- | --- |
| The rules | The project's `AGENTS.md` (and `.claude/skills/`) | What each of the three layers owns, the hard rules, which skill covers what |
| The project | `BRICKKIT.md` at the project root | Which components exist, where each one's docs and contracts are |
| A component | The component's `BRICKKIT.md` | What this component is, how to configure it, how to call it |

Each level points at the next: `AGENTS.md` tells you to read `BRICKKIT.md`, and the project's `BRICKKIT.md` lists the path
to each component's docs.

A component under development is itself a project too (its own workbench), so the same three levels repeat inside the
component repository — but **when using a component, read only its component level** (`BRICKKIT.md`, `component.yaml`,
contracts), never the workbench files in its repository.

## The reading order

1. `AGENTS.md`: once per session is enough.
2. The project root's `BRICKKIT.md`: which components exist.
3. The `BRICKKIT.md` of the components the current task involves: usually only one or two.
4. When needed, that component's `component.yaml` and contracts.
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
