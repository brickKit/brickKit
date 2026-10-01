# The AI-assisted development workflow

When the task starts as a requirement rather than a component — "users should be able to …" — work out first which
component owns it, whether it is sound, and the order to change things in: see
[Judging and planning a requirement](03-judging-a-requirement.md). The steps below begin once you know which component
you are writing or changing.

## Writing a new component

1. **Read the context.** The project root's `AGENTS.md` — the team's conventions, and the component table at its end — and
   `brickkit.yaml`: what the project already has, the rules a new component must follow, and where it goes.
2. **Read the dependencies.** For every component the new one will call: its `BRICKKIT.md`, `configSchema` and contracts.
   Only direct dependencies.
3. **Generate the skeleton.** `brickkit new <scope>/<name>` (`--path` for a repository of its own, `--contract
   openapi|proto` for a contract placeholder).
4. **Write `component.yaml` first.** `dependencies` (exact versions), `configSchema` (the keys are the environment
   variable names; mark secrets `secret: true`; what the platform can't work out is written as required), `deployment`,
   `healthCheck`, and `migration` if needed. Then `brickkit lint`.
5. **Write the contract.** Put the interface in the contract file first — callers can start at the same time, and you have
   your acceptance criteria.
6. **Write the code.** Read dependency addresses from `*_ENDPOINT`; an optional dependency's variable may not exist, so
   read it with `.get()` and write down the degradation; the health check checks only this process; the entry program
   exits with an error at once on an argument it doesn't know.
7. **Write the Dockerfile.** The image needs `wget` or `curl` (for the HTTP health check), and doesn't run as root.
8. **Write the component's documents.** `BRICKKIT.md` for the people using it — six sections, holding what
   `component.yaml` can't say; `AGENTS.md` for whoever develops it next — the code map, how to build and test, the
   design decisions; and the first line of `README.md`. `brickkit lint` lists every placeholder left. See
   [A component's documentation](../03-component-guide/08-component-doc-spec.md) and
   [Component docs, from an AI's side](04-component-doc-spec.md).
9. **Run it.** `brickkit add --local`, `brickkit build`, `brickkit up`; or build a workbench in the component repository for
   integration work (see [Developing inside a component](../03-component-guide/05-local-dev-fractal.md)).
10. **Test.** Write the tests first and watch them go red, then make the implementation turn them green (see the
    [testing strategy](../09-patterns/02-testing-strategy.md)).

## Changing an existing component

1. **Read its `BRICKKIT.md`, `AGENTS.md` and `component.yaml`.** Especially `configSchema` and `dependencies`, and the
   code map and pitfalls in `AGENTS.md`.
2. **Judge whether it's a breaking change.** Removing an endpoint, changing a field's meaning, changing a config item's name
   or meaning, changing a default — all affect the people using it.
3. **Change the code, the contract and the documents.** In the same commit: `BRICKKIT.md` when what users see changes,
   `AGENTS.md` when the code map or a decision changes. Don't let the docs fall behind the implementation —
   `brickkit lint` says when a document no longer matches `component.yaml` (`DOC_OUT_OF_STEP`) or points at code that
   moved (`DOC_PATH_MISSING`).
4. **Raise the version.** Change `metadata.version` (the version's only source): the patch for implementation-only
   changes, the minor for additions, the major for breaking changes.
5. **Release.** Commit, push, `brickkit release`.
6. **Upgrade in the project.** `brickkit upgrade <component>`; when there are config conflicts, a person decides which value
   to keep.

**Change defaults with care**: keys the project never wrote follow the new default automatically — which is what you want;
keys it did write become a conflict, and it has to decide each one.

## Working in a large project

When the project has dozens of components and the task is about one of them, don't start them all.

1. **Run a focus run.** `brickkit up` in the component's directory (or `brickkit up --focus <id>` anywhere in the
   project) runs that component from its source plus what it needs; the reasons printed next to each component say why
   it starts or doesn't. `brickkit up --all` goes back to the whole project. See
   [Developing inside the project](../02-project-guide/04-focus-run.md).
2. **Run commands from where you are.** Every project command finds the project upward; `build` and `deps` without an
   argument mean "this component".
3. **Move versions forward explicitly.** When you raise `metadata.version` in a component under `components/`, `up`
   stops until the project follows: run the `brickkit upgrade <id>@<version>` it suggests, and read the config migration
   it reports. Nothing moves because a directory changed.
4. **Keep one `components/`.** Never clone or copy a component into another component's directory; `up`, `lint` and
   `sync` refuse nested copies, and BrickKit never moves them for you — ask the person before deleting one: it may hold
   the only copy of their changes.

## What you don't need to do

- **Read the whole codebase.** Component boundaries are written in files; the current component and its direct dependencies
  are enough.
- **Guess service addresses.** It's `http://<versioned service name>:<port>`, injected by the platform.
- **Guess variable names.** Dependency addresses are derived from the component ID; config items are named by the keys of
  `configSchema`.
- **Write deployment scripts.** Deployment files are generated by the CLI; you change only the three layers.
- **Remember command flags.** Use `brickkit <command> --help`.

## What to hand to a person

- How component boundaries are drawn (a domain judgment).
- Which value to keep in a config conflict after an upgrade.
- Bringing in a new third-party component, or adding a trusted public key to `installer.publicKeys`.
- Whether to turn on network policies, ServiceAccount isolation, `podSecurity: restricted`.
- Changes to production deploy files.
