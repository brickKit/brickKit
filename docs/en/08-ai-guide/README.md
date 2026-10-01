# Guide for AI assistants

**For whom**: AI assistants working in a BrickKit project, and people who want their AI assistants to work better.

BrickKit's design keeps AI in mind throughout: components are small enough to read in one go, boundaries are written in
files, addresses and variable names are computed by rule rather than guessed, every component carries a `BRICKKIT.md`
written for the people using it, and every project's `AGENTS.md` ends with a table of its components. This module
covers how an AI makes use of all that: what to read, in what order, what it needn't read, and the workflow for writing
and changing components.

## How it relates to AGENTS.md

There are three things written for AI, each covering one level:

| File | Where | Covers |
| --- | --- | --- |
| The BrickKit repository's `AGENTS.md` / `AGENTS.zh.md` | This repository's root | What the BrickKit platform is and its principles — read it when discussing the platform itself |
| A project's `AGENTS.md` and `.claude/skills/` | Every project (written by `brickkit init`) | How to work in this project: the team's conventions, a summary of the platform rules and the component table, five skills split by task |
| This module | The docs | The "why" behind the two above, and the complete practice |

The one in the project is the one used every day: `brickkit init` installs `.claude/skills/` (`brickkit-assemble`,
`brickkit-component`, `brickkit-deploy`, `brickkit-troubleshoot`, `brickkit-plan-change`) and writes `AGENTS.md` (the
team's own guide, with a block at its end that the CLI keeps up to date) and `CLAUDE.md`; they're committed with the
project, and after the CLI is upgraded `brickkit skills update` refreshes the skills and the block at the end of
`AGENTS.md` (creating `AGENTS.md` / `CLAUDE.md` if they're missing). They deliberately don't copy command flags — for
flags, ask `brickkit <command> --help`.

A component developed in its own repository (a `component.yaml` and no `brickkit.yaml`) gets the part that makes sense
there: `brickkit skills update` run in that repository installs just the `brickkit-component` skill, plus the
component's `AGENTS.md` (its block holds the component rules) and `CLAUDE.md`. `brickkit new` doesn't do this for you.

| Page | Covers |
| --- | --- |
| [01 The routing table](01-ai-routing-table.md) | What to read for each question; what not to read |
| [02 The development workflow](02-ai-dev-workflow.md) | The steps for writing a new component and changing an existing one |
| [03 Judging and planning a requirement](03-judging-a-requirement.md) | Which component owns a requirement, whether it is sound, and the order to change things in |
| [04 Component docs, from an AI's side](04-component-doc-spec.md) | How to read a component's `BRICKKIT.md` and `AGENTS.md`, and how to write them |
| [05 Reading fractally](05-fractal-reading.md) | The reading order from platform to project to component, and managing the context window |
| [06 Reading BrickKit itself](06-reading-brickkit.md) | Three routes into this repository: the bundles, `llms.txt` routing, the `AGENTS.md` maps |
