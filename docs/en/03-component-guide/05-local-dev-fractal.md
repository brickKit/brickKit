# Developing inside a component

## A component can be a project too

`demo/quote` depends on `demo/hello`. While writing the code, you want it running against a real `demo/hello`, not
imagining one from the docs. BrickKit's way is **fractal**: while a component is being developed, its own repository is a
complete BrickKit project — with a `brickkit.yaml`, a deploy file and `config/` listing its dependencies. That project
belongs to the author alone and is called the **local workbench**.

Users never see it: when someone runs `add demo/quote`, the CLI reads only the `component.yaml` and `BRICKKIT.md` in the tag
you released, and resolves the dependency tree from `component.yaml`'s `dependencies` **in their own project**.

## Creating the workbench in the component repository

Run `init` without arguments at the component repository root (completion mode: `component.yaml`, the source and every
other existing file stay untouched; only what's missing is added):

```bash
cd demo-quote
brickkit init
```

```text
This directory already has files; brickkit init will:
   ✅ create  brickkit.yaml
   ✅ create  deploy.yaml
   ✅ create  config/vars.yaml
   ✅ create  config/.gitkeep
   ✅ create  .gitignore
```

In a component repository it creates neither `components/` nor `shell/`, which only assembling projects have, and it
declares no install source for you — where dependencies come from is your call. With the Git source written in:

```yaml
# brickkit.yaml — this component's local workbench: the components to run while developing it (its dependencies, and it may list itself too).
# It plays no part in releasing the component: brickkit release reads only component.yaml.
project: demo-quote

# where this component's dependencies come from — add the install sources it needs
sources:
  - name: company-git
    type: git
    baseUrl: https://git.example.com/components/

components: []
```

## Adding the dependencies

```bash
brickkit add demo/hello@1.1.0
```

```text
➕ Adding demo/hello@1.1.0
   ✅ demo/hello@1.1.0
📝 Written: brickkit.yaml, deploy.yaml
📝 Config skeletons: config/demo-hello.yaml
📦 Artifacts: 1 file, in .brickkit/artifacts/
```

Now `brickkit up` can bring the dependencies up locally.

## Adding the component itself

With the dependencies running in containers, how does your component get their addresses? Write the component itself
into the workbench — its source pointing at the repository root (`source.type: local`, `path: .`) — and have it run as a
process on your machine:

```yaml
# brickkit.yaml
components:
  - id: demo/hello
    version: 1.1.0
  - id: demo/quote
    version: 0.1.0          # the same as metadata.version in component.yaml
    source:
      type: local
      path: .
```

```yaml
# deploy.yaml
components:
  - id: demo/hello
  - id: demo/quote
    mode: local
```

`mode: local` has BrickKit recognise the start command from the source directory, start the process itself and watch it;
dependency addresses are injected in a form reachable from your machine:

```bash
brickkit up
```

```text
📋 Component state calculation:
   ✅ demo/hello@1.1.0  starting (demo/quote needs it)
   ✅ demo/quote@0.1.0  starting (top-level)
```

```text
🐳 Starting (docker)...
   demo-hello-1-1-0             running (healthy)
✅ All components started (1)

💡 View the status: brickkit status
   View the logs: docker compose -p brickkit-demo-quote logs -f

Starting 1 local component(s) — press Ctrl+C to stop
demo-quote-0-1-0 | 2026/09/30 00:47:45 demo/quote listening on :8080
demo-quote-0-1-0  listening on port 8080
```

```bash
curl http://localhost:8080/api/v1/quote
```

```json
{"greeting":"Hi","quote":"Quote of the day: Simple is better than complex"}
```

The greeting "Hi" comes from `demo/hello@1.1.0` in its container. After changing the code, `Ctrl+C` and `brickkit up`
again runs the new code.

A few notes:

- **For breakpoints, use `mode: debug`.** The platform doesn't start the process; it generates an environment file that you
  load in your IDE and start from there (see [Local debugging](../02-project-guide/03-local-debug-workflow.md)).
  `mode: debug` is your personal business and goes in `deploy.local.yaml` (`brickkit local on`).
- **Under `mode: local` the platform injects `PORT`**: the port on your machine it chose for this process (preferring
  `deployment.port`). Code that reads `PORT`, falling back to its default, never fights another process for a port.
- **Whether to commit this workbench into the component repository** is up to you: commit it, and collaborators who clone
  the repository are one `brickkit up` away from running it; it doesn't affect releases, and users never see it.

## Workbench or focus run

A workbench is not the only way to run a component while you develop it. When the component already lives in a
project's `components/`, `brickkit up` in its directory runs a **focus run**: that component from its source, plus what
it needs, at the versions the project uses — no workbench to write. See
[Developing inside the project](../02-project-guide/04-focus-run.md).

| Use | When |
| --- | --- |
| A focus run | The component is part of one project, and the question is "does my change work in this system?" |
| A workbench | The component has a life outside any single project: many projects use it, and it's developed on its own terms |

The two don't mix. Inside a workbench, commands use the workbench — the nearest `brickkit.yaml` wins, and nothing above
it is consulted. And a workbench inside a project must not carry a `components/` of its own full of copies: component
source lives in one place, the project's `components/`. `up`, `lint` and `sync` refuse a component nested inside another
component's directory, and `add --repo` refuses to clone into a workbench that sits inside another project.

## One repository, two identities

When a component repository has both `component.yaml` and `brickkit.yaml`, the CLI treats it like this:

| Command | Treats the directory as | Reads |
| --- | --- | --- |
| `up`, `add`, `down`, `status`… | A project (your workbench) | `brickkit.yaml`, the deploy file, `config/` |
| `release` | A component | Only `component.yaml`; nothing in the workbench takes part |
| `lint` | A project | The workbench's three layers, plus the `component.yaml` at the repository root |

That keeps releases clean: components you added to the workbench and settings you tuned there never end up in the version
you release.

## Many local components in a project: `add --local --init`

When a project's `components/` and `shell/` hold several components of your own, you don't need to `init` each one:

```bash
brickkit add --local --init
```

```text
   🧰 Workbench created in components/demo/widget (0 dependencies added)
   🧰 Workbench created in shell/erp/shell (0 dependencies added)
   💡 Commit the new workbench files in each component repository — brickkit release refuses a component directory with uncommitted files
➕ Adding demo/widget@0.1.0, erp/shell@0.1.0
   ✅ demo/widget@0.1.0 (in shell erp/shell)
   ✅ erp/shell@0.1.0
📝 Written: brickkit.yaml, deploy.yaml
```

It scans every local source and gives each component without a `brickkit.yaml` a workbench, adding the dependencies from
**its own** `component.yaml` to **its own** workbench, with the install sources inherited from the enclosing project
(paths rewritten relative to that directory); then it adds those components to the enclosing project as usual. After
that, `cd components/demo/widget && brickkit up` brings up that one component's dependency tree on its own.
