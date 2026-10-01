---
name: brickkit-plan-change
description: Use when the user brings a new requirement, a feature or a change in a BrickKit project and you need to decide which component owns it, whether it is sound, and how to plan and make it across components. Covers finding the owner from the component table in AGENTS.md and each component's BRICKKIT.md, checking a requirement against boundaries, project conventions, recorded decisions, dependency direction and contract compatibility, the order to change components in, and what "done" means. Applies when the user says "add a feature", "new requirement", "where should this go", "plan this change", or asks whether something is a good idea.
---

# Judging and planning a change

## When to use this skill

- A new requirement or feature arrives and it isn't yet clear which component it belongs to
- A change touches more than one component
- You're asked whether a change is sound, or to write a plan before coding

## 1. Find the owner

1. The component table at the end of the project's `AGENTS.md`: its "What it does" column narrows the candidates.
2. Each candidate's `BRICKKIT.md`, section `Purpose`: what it **owns** and what it **does not own** — and every
   "does not own" names who does. Follow that pointer rather than guessing. The docs are under
   `.brickkit/manifests/<scope>/<name>/<version>/` (or in the component's source directory when a local source holds
   that version).
3. Read only the components the requirement touches; never load every component's docs.

## 2. Check it against what is written

| Check | Where it's written | When it fails |
| --- | --- | --- |
| The owner's boundary | Its `BRICKKIT.md` Purpose | It needs the component to own something listed under "does not own": that's a boundary change — the person decides |
| Project conventions | `Conventions` in the project's `AGENTS.md` | It breaks a convention: the person decides |
| Recorded decisions | The project's `docs/decisions/`, the component's `Design decisions` and `docs/` | It reverses a decision: quote it, the person decides |
| Dependency direction | `brickkit deps <id>` / `brickkit graph` | A new dependency would make a cycle, or point against the direction the design allows |
| Contract compatibility | The contract files under `artifacts` | Adding is a minor version; removing or changing meaning is a major version, and every consumer has to move |

## 3. Decide the outcome

- **Fits one component** — go ahead.
- **Needs a provider's contract first** — change the provider first, then the consumer.
- **Needs a new component** — no owner, and none should stretch to own it: `brickkit new <scope>/<name>`.
- **Conflicts** — stop, and put the conflict to the person, quoting the file and section that says so.

## 4. Plan and change

- **Order:** providers before consumers — the order `brickkit deps` prints.
- **Per component, one commit:** the contract, the code, and its `BRICKKIT.md` / `AGENTS.md` together. Then raise
  `metadata.version` (patch for implementation only, minor for additions, major for breaking changes), release, and
  `brickkit upgrade <id>@<version>` in the project.
- **Before changing a component's code:** read its `AGENTS.md` — the code map tells you where to start, `Pitfalls`
  and `Before changing code` what not to break.
- **Test inside the project with a focus run:** `brickkit up` in the component's directory runs it from its source
  plus what it needs.

## What the change touches in the project

- A new component, or a new version of one: `brickkit add` / `brickkit upgrade` write `brickkit.yaml` (the lock
  file), `deploy.yaml` and `config/` together — never hand-edit one layer and forget another.
- A new config key a component needs: the component declares it in `configSchema`; the project fills it in
  `config/<scope>-<name>.yaml` (shared values in `config/vars.yaml`).
- How it runs (ports, mode, replicas): `deploy.yaml` for the team; `deploy.local.yaml` only for your own machine
  (local mode on), never committed.

## 5. Done means

- The tests the component's `AGENTS.md` names pass.
- `brickkit lint` shows no documentation warnings for what you changed (`DOC_OUT_OF_STEP` in particular: a new
  dependency, required key or contract file the docs don't mention yet). The next AI reads what you left.

## Where to dig deeper

- Writing the component's documents: the `brickkit-component` skill
- Adding, upgrading and running components: the `brickkit-assemble` skill
- Flags: `brickkit <command> --help`
