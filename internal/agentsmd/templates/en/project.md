## BrickKit

This project is assembled with [BrickKit](https://github.com/brickKit/brickKit): each component describes itself in its own `component.yaml`; the `brickkit` CLI resolves the graph, generates the deployment files and exits. There is no registry, config server or gateway to look for.

- `brickkit.yaml` lists the components at exact versions (it is the lock file); `deploy.yaml` says how they run (`deploy.local.yaml` replaces it on your machine while local mode is on); `config/<scope>-<name>.yaml` holds each component's environment variables, `config/vars.yaml` the shared values.
- `brickkit add` / `remove` / `upgrade` keep the three layers in step: never hand-edit one and forget another.
- Config keys are the environment variable names the component's `configSchema` declares; secrets are `${VAR}` or `file://.secrets/…`, never plaintext.
- A health check checks only its own process. To work on one component, run `brickkit up` in its directory; `brickkit up --all` runs everything again.
- Task skills are in `.claude/skills/brickkit-*`; for flags ask `brickkit <command> --help`.

## Components

A component's documentation is `BRICKKIT.md` (translations `BRICKKIT.<lang>.md`) in its source directory when a local source holds this version, otherwise in `.brickkit/manifests/<id>/<version>/`; its contracts are in `.brickkit/artifacts/<service name>/` (the ID with `/` and `.` as `-`, then the version with `.` as `-`). To change a component, read its own `AGENTS.md`.
