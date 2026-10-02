# Developing inside the project

In a big project you rarely work on one component in isolation. You change `demo/caller`, you want to see it run
against the real `demo/hello` it calls, and the other forty components can stay off. This page is about doing that
**inside the project**: you stand in a component's directory, type `brickkit up`, and the project runs just that
component — from its source — plus what it needs.

That is a **focus run**. The component you are working on is the *focus*; everything the focus doesn't need stays off.

## Focus run or workbench

There are two ways to run a component while you develop it. They answer different questions:

| | Focus run (this page) | Workbench ([Developing inside a component](../03-component-guide/05-local-dev-fractal.md)) |
| --- | --- | --- |
| Where you work | Inside the project, in `components/<scope>/<name>/` | In the component's own repository, which has its own `brickkit.yaml` |
| What runs | The focus and what it needs, **at the versions the project uses** | Whatever the workbench declares — often the component plus stand-ins |
| Setup | None: the project already knows everything | A small project of its own, written once |
| Good for | "Does my change work in *this* system?" | Developing a component many projects use, on its own terms |

If the component lives in a project's `components/`, start with a focus run. A workbench is for components that have
a life outside one project.

## `up` in a component's directory

```bash
cd components/demo/caller
brickkit up
```

```text
📁 Project: ../../.. (my-shop)
✅ Local mode is on: commands now read deploy.local.yaml
   deploy.local.yaml was copied from deploy.yaml — change it as you like, it is not committed
🎯 Focus: demo/caller (written to deploy.local.yaml; brickkit up --all runs every component again)
Local mode is on: using deploy.local.yaml (brickkit local off switches back to deploy.yaml)
🚀 Starting project my-shop (target: docker)
📋 Component state calculation:
   ✅ demo/bus@1.0.0     starting (demo/caller needs it)
   ✅ demo/hello@1.0.0   starting (demo/caller needs it)
   ✅ demo/caller@1.0.0  starting (focus)
```

Line by line:

- **`📁 Project`** — there is no `brickkit.yaml` here, so `brickkit` looked upward, like `git` does, and found the
  project three levels up. Every project command works from any subdirectory; paths it prints are relative to where
  you are.
- **Local mode** — the focus is a personal setting, so it lives in your personal deploy file. If local mode was off,
  `up` turns it on first (the same as `brickkit local on`).
- **`🎯 Focus`** — the directory you are in belongs to `demo/caller`, so that is the focus.
- **The reasons** — each component says why it starts: `(focus)`, or `(demo/caller needs it)`.

The focus runs **from its source**, as a process on your machine (as if it were `mode: local`); what it needs runs in
containers, each given a port on your machine so your process can reach it:

```text
🐳 Starting (docker)...
   demo-bus-1-0-0               running (healthy)
   demo-hello-1-0-0             running (healthy)
✅ All components started (2)
```

```text
Starting 1 local component(s) — press Ctrl+C to stop
demo-caller-1-0-0  listening on port 8080
```

The process runs in the foreground; Ctrl+C stops it, and the containers keep running. To use breakpoints, write
`mode: debug` on the focus in `deploy.local.yaml` and start it from your IDE — see
[Local debugging](03-local-debug-workflow.md). The focus keeps a `mode: debug` you wrote.

## Where the focus lives

One line in `deploy.local.yaml`:

```yaml
# deploy.local.yaml
target: docker # docker | podman | k8s
focus: demo/caller

components:
  - id: demo/bus
  - id: demo/hello
  - id: demo/caller
```

It stays there until you change it: the next `up` runs the same focus, unless you run it in another component's
directory — that moves the focus there. `deploy.yaml`, the team's file, is never touched. `up`, `status`, `down`,
`lint` and `build` remind you (`sync` ignores the focus, see [below](#what-the-other-commands-do-with-it)):

```text
🎯 Focus: demo/caller
```

## `--focus` and `--all`

From anywhere in the project, `--focus` sets the focus without changing directory:

```bash
brickkit up --focus demo/hello
```

```text
🎯 Focus: demo/hello (written to deploy.local.yaml; brickkit up --all runs every component again)
Local mode is on: using deploy.local.yaml (brickkit local off switches back to deploy.yaml)
🚀 Starting project my-shop (target: docker)
📋 Component state calculation:
   ⬜ demo/bus@1.0.0     not starting (outside the focus)
   ✅ demo/hello@1.0.0   starting (focus)
   ⬜ demo/caller@1.0.0  not starting (outside the focus)
```

`demo/hello` needs nothing, so it runs alone. `--all` removes the focus and runs every component again:

```bash
brickkit up --all
```

```text
📋 Component state calculation:
   ✅ demo/bus@1.0.0     starting (demo/caller needs it)
   ✅ demo/hello@1.0.0   starting (demo/caller needs it)
   ✅ demo/caller@1.0.0  starting (top-level)
```

`--focus` and `--all` only exist on `up`, and they can't be combined with `-f` or `--no-local`: those two mean "don't
read my personal file", and the focus is in it.

Which components start is worked out the same way as always, only from a different starting point: without a focus,
every top-level component starts; with one, only the focus and the components whose `mode` says they always run
(`enabled`, `local`, `debug`). What they need follows, as usual — required and optional dependencies, and the components
whose address their config refers to with `$endpoint:`. Shells count too: a focused shell runs with its members (`starting (hosted by …)`), and
when the focus needs a component a shell hosts, that shell starts to host it (`starting (hosts …)`) rather than the
member running on its own. A focus can't be a component that is `mode: disable` — that is a contradiction, and `up`
refuses it without changing any file.

## What the other commands do with it

| Command | With a focus |
| --- | --- |
| `status`, `down`, `lint`, `build` | Show the `🎯 Focus` line. `down` stops the whole project, focus or not |
| `lint` | Also checks that the focus has source to run from |
| `sync` | **Ignores the focus**: it keeps the source of every component the project runs without one, so switching focus never moves directories around |
| `local refresh` | Lists the focus as one of your local changes, so you can put it back in the new file |
| `remove` | Removing the focused component removes the focus with it, and says so |
| `graph`, `deps` | Unaffected — they read `deploy.yaml` |
| `build`, `deps`, `lint` without an argument | Use the component whose directory you are in (`lint --all` checks the whole project) |

`sync` with a focus on `demo/hello` still keeps all three:

```text
📂 Workspace tidying:
   ✅ components/demo/bus/                 active
   ✅ components/demo/caller/              active
   ✅ components/demo/hello/               active
✅ Workspace tidied (3 active, 0 archived, 0 activated)
```

`local refresh` copies `deploy.yaml` again, and the focus is one of the things it tells you to carry over:

```text
✅ Generated a fresh deploy.local.yaml from deploy.yaml. The old one is backed up at deploy.local.yaml.bak.
ℹ️ The old file had 1 local change; merge the ones you still need into the new deploy.local.yaml by hand:
   - [deploy] focus: demo/hello (now unset)
```

A focus run needs a process on your machine that the other components reach, so it works with `target: docker` and
`podman`. With `target: k8s`, nothing is written and `up` stops:

```text
❌ Error: deploy.local.yaml failed validation
   File: deploy.local.yaml
   focus: a focus run starts a process on your machine, which a cluster cannot reach; use target docker or podman
   Suggestion: Full field reference: brickkit docs 11-reference/03-deploy-yaml-schema (online: https://github.com/brickKit/brickKit/blob/v1.1.0/docs/en/11-reference/03-deploy-yaml-schema.md)
```

## Moving versions forward

The focus runs the code in its directory, so that code has to be the version the project declares. When you bump
`demo/hello` to `1.1.0` in its `component.yaml` while `brickkit.yaml` still says `1.0.0`, `up` stops rather than run a
version the project doesn't know about:

```text
❌ Code that runs from a local repository does not match this run
   File: deploy.local.yaml
   components[1]: demo/hello@1.0.0 runs from the local repository components/demo/hello, which holds 1.1.0
   Suggestions:
   1. To run the version in the repository: brickkit upgrade demo/hello@1.1.0
   2. To run the version in the project: git -C components/demo/hello checkout 1.0.0
```

`brickkit upgrade demo/hello@1.1.0` moves the whole project to the new version — config migration included (see
[Upgrading and config migration](07-upgrade-and-migration.md)) — and a component that still requires `1.0.0` keeps that
version alongside. The project moves forward one explicit `upgrade` at a time; nothing moves because a directory
changed.

That is also how a change is tested before it is released. Bump the version in the component's `component.yaml`,
`brickkit upgrade` the project to it, then run it: from source with a focus run (or `mode: local`), or as an image with
`brickkit build <id>` and `brickkit up`. Edit the unreleased version as often as you like while you test. Until
`brickkit release`, the version exists only in your `components/`: a teammate who pulls a `brickkit.yaml` naming it
gets `COMPONENT_NOT_FOUND`. So release the component first, then commit the project's upgrade. Once a version is
released its content is fixed — the next change, code or documents, is the next version.

## One `components/`

Component source lives in exactly one place: the project's `components/`. When a component's repository is itself a
workbench, it can have a `components/` of its own — and if that ends up inside the project, the same component can
exist twice, and nobody can tell which copy runs. `up`, `lint` and `sync` refuse:

```text
❌ Error: component source is nested inside another component's directory
   components/demo/caller/components/demo/hello: demo/hello, inside demo/caller; the project's components/ has demo/hello too
   Before you move or delete it: It is not a Git repository — these files have no other copy
   Suggestions:
   1. The project's components/ already has demo/hello at components/demo/hello: carry over the changes you still need, then rm -rf components/demo/caller/components/demo/hello
   2. A component's source lives in one place, the project's components/; move or delete the nested copy yourself — BrickKit moves nothing
```

BrickKit never moves or deletes the nested copy for you: it may hold changes that exist nowhere else — the
"Before you move or delete it" line says whether it does. The suggestion gives the command for each copy: delete it
when the project already has the component (after carrying over what you still need), or move it into the project's
`components/` when the project doesn't. For the same reason, `brickkit add --repo` refuses to run inside a
workbench that sits in another project's `components/`: it would clone a second copy.

## Git submodules

BrickKit never fetches git submodules. `add --repo` clones without them and names the ones it skipped; `build` warns
when a component's source has submodules that are empty directories. Fetch them yourself with
`git submodule update --init` when you really need them — or, better, have the component publish an image so building
it needs nothing extra.

Every flag of `up` is in the [CLI reference](../07-cli-reference/README.md#brickkit-up).
