# Judging and planning a requirement

A requirement arrives — "customers should get a credit limit", "orders need an approval step". Before any code is
written, three questions have answers in the project's files: which component owns it, whether it is sound, and in what
order the change goes. This page is how an AI answers them from what is written, instead of from a guess. The skill
`brickkit-plan-change`, installed in every project, carries the same steps.

## Understand the project first

Read in this order, and stop as soon as you know enough:

1. **The project's `AGENTS.md`** (already loaded — Claude Code reads it every session through `CLAUDE.md`): the
   project's conventions and pitfalls, then, at the end, the component table with what each component does.
2. **For each component the question touches, its `BRICKKIT.md`**: what it owns and doesn't, what it needs before
   deploying, its dependencies, configuration and contracts. It is in `.brickkit/manifests/<id>/<version>/`, or in the
   component's source directory when a local source holds that version.
3. **To change a component, its own `AGENTS.md`**: the code map says which file to start in; its pitfalls and
   "Before changing code" say what not to break.
4. **The structure**: `brickkit deps <id>` and `brickkit graph` for who depends on whom; the contract files under
   `.brickkit/artifacts/`.

Never load every component's documentation: only the ones the task touches. What each file holds is in
[A component's documentation](../03-component-guide/08-component-doc-spec.md).

## Find the owner

The component table's "What it does" column narrows the candidates. Each candidate's `BRICKKIT.md` decides: its
`Purpose` section lists what the component **owns** and what it **does not own** — and every "does not own" names who
does instead. Follow that pointer; it is there exactly so nobody has to guess.

## Check it against what is written

| Check | Where it is written | It fails when |
| --- | --- | --- |
| The owner's boundary | Its `BRICKKIT.md`, `Purpose` | The requirement needs it to own something it lists under "does not own" |
| Project conventions | `Conventions` in the project's `AGENTS.md` | The requirement breaks one |
| Recorded decisions | The project's `docs/decisions/`; the component's `Design decisions` and `docs/` | The requirement reverses one |
| Dependency direction | `brickkit deps`, `brickkit graph` | A new dependency would make a cycle, or point against the direction the design allows |
| Contract compatibility | The contract files listed under `artifacts` | It removes or changes the meaning of something callers use — a major version, and every caller moves |

A failed check is not a reason to say no. It is a decision that belongs to a person: changing a boundary, a convention
or a recorded decision is a choice about the system, not a coding detail.

## The outcome is one of four

| Outcome | What it means | Next |
| --- | --- | --- |
| Fits one component | Its owner owns it, nothing conflicts | Plan and change that component |
| Needs a provider first | The owner needs something a dependency doesn't offer yet | Change the provider's contract first, then the consumer |
| Needs a new component | No component owns it, and none should be stretched to | `brickkit new <scope>/<name>` |
| Conflicts | A check above failed | Stop, and put it to the person, quoting the file and section that says so |

## Plan and change

- **Order:** providers before consumers — the order `brickkit deps` prints.
- **Per component, one commit:** the contract, the code, and its `BRICKKIT.md` / `AGENTS.md` together. A doc that lags
  behind the code is how the next AI writes against a wrong description.
- **The version:** raise `metadata.version` — the patch for an implementation-only change, the minor for an addition, the
  major for a breaking change — then release, and `brickkit upgrade <id>@<version>` in the project.
- **Test inside the project with a focus run:** `brickkit up` in the component's directory runs it from its source plus
  what it needs (see [Developing inside the project](../02-project-guide/04-focus-run.md)).

## Done means

- The tests the component's `AGENTS.md` names pass.
- `brickkit lint` shows no documentation warnings for what changed. `DOC_OUT_OF_STEP` in particular says a new
  dependency, required key or contract file isn't mentioned in the docs yet.

## What goes to a person

- A boundary change: a component taking on something it lists as "does not own".
- Breaking a convention or reversing a recorded decision.
- A breaking contract change, and the order consumers move in.
- Whether a new component is warranted, and its boundary.
