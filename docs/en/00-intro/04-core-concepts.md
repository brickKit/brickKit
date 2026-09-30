# Core concepts

If you only want a few minutes on BrickKit's skeleton — enough not to trip over the terms in the docs and in error
messages — this page is enough. Every term points to the page that explains it fully.

## The one-page glossary

| Term | What it is |
| --- | --- |
| **Component** | The basic unit you install and run: a program that runs on its own, **always a container** — a frontend (nginx serving static files) included |
| **Manifest** (`component.yaml`) | A component describing itself: what it depends on, which port it listens on, what config it needs, how to tell it's alive, where its image comes from |
| **Project** | A set of components described by the three layers — the system you're assembling |
| **The three layers** | `brickkit.yaml` (what there is), `deploy.yaml` / `deploy.local.yaml` (how it runs), `config/` (what config each component gets); see [The three layers at a glance](../01-three-layers/01-overview.md) |
| **Lock file** | The role of `brickkit.yaml`: every component in use is locked to an exact version, and a component not written there doesn't exist |
| **Install source** | Where to look for components: a Git repository (the default; a version is a Git tag), a directory on your machine (a local source), a component market (optional) |
| **Local source** | A directory on your machine holding component source at `<scope>/<name>/component.yaml`; their images are built by `brickkit build` |
| **Required / optional dependency** | A missing required dependency is an error and nothing starts; a missing optional one (`optional: true`) only means its address variable is **not injected** — not injected as an empty string |
| **Contract** (artifacts) | The API description a component publishes (OpenAPI, Protobuf, …), declared under `artifacts` in `component.yaml` and downloaded by `add` / `fetch` |
| **Shell** | A component that compiles several components into **one process**, to save memory and CPU; see [Shells](../04-shell/README.md) |
| **Member** | A component hosted by a shell; it has no container of its own but still has its own config and migrations |
| **Fractal structure** | A component is a project while you develop it and a black box when someone uses it; see [The fractal structure](06-fractal-architecture.md) |
| **`BRICKKIT.md`** | Documentation for people and AIs: the one in a component repository explains how to use that component; the one at the project root lists the project's components and where their docs are |
| **Local mode** | After `brickkit local on`, every command reads the personal `deploy.local.yaml` instead; see [Local debugging](../02-project-guide/03-local-debug-workflow.md) |

## The naming rules everything builds on

Once you know these rules, you can work out every name you meet in the docs, in errors and in generated files.

| Name | Rule | Example |
| --- | --- | --- |
| Component ID | `scope/name`, all lowercase | `people/basic` |
| Version | An exact `major.minor.patch`; ranges like `^1.0.0` are not accepted | `1.0.0` |
| Versioned service name | The component ID and version with `/` and `.` turned into `-` | `people-basic-1-0-0` |
| Dependency address variable | The component ID uppercased, `/` and `-` turned into `_`, plus `_ENDPOINT` | `PEOPLE_BASIC_ENDPOINT=http://people-basic-1-0-0:8080` |
| Config file name | The component ID with `/` turned into `-`; one meant for a single version adds `@version` | `config/people-basic.yaml`, `config/people-basic@2.0.0.yaml` |
| Config item | A key in `configSchema` **is** the environment variable name, injected as-is | `DB_HOST` |
| Image tag | Always exactly the component's `metadata.version` | `registry.example.com/people/basic:1.0.0` |
| Release tag | The version when the component sits at the repository root; prefixed with the component when it sits in a subdirectory | `1.0.0`, `people-basic/1.0.0` |

A variable's **name** is derived from the component ID alone and never carries a version; its **value** is what points
at a specific version. So two versions of the same component can run side by side (`people-basic-1-0-0` and
`people-basic-2-0-0` are two DNS names that don't clash), while a caller's code only ever reads the one name
`PEOPLE_BASIC_ENDPOINT`.

## What runs: follow the ones above

Each component in a deploy file can have a `mode`. Without one, it **follows the components above it**: a top-level
component (nothing depends on it) runs by default, and a component others depend on runs as long as at least one of
them does.

| Written | Meaning |
| --- | --- |
| nothing | Follow the ones above |
| `mode: enabled` | Always runs, whatever is above it; an error if one of its required dependencies is turned off (two conflicting intents) |
| `mode: disable` | Never runs; whatever depends on it stops too |
| `mode: local` | Always runs, but not in a container: BrickKit works out the start command, launches the process on your machine and watches it |
| `mode: debug` | Always runs, as a process you start yourself in your IDE; **written only in the personal `deploy.local.yaml`** |

`local` and `debug` only make sense on the Docker / Podman targets: a Pod in a cluster can't reach a process on your
laptop.

## The key verbs

| Command | What it does |
| --- | --- |
| `brickkit add` | Fetch a component and its dependencies, write them into the three layers, generate config skeletons, download contracts |
| `brickkit build` | Build the images that are built locally |
| `brickkit up` | Generate deployment files → run database migrations → start containers (never builds) |
| `brickkit upgrade` | Change a version and migrate your config between the old and new `configSchema` |
| `brickkit local` | Turn the personal local mode on or off |
| `brickkit release` | Check the component → tag it in Git → push, leaving no tag behind if anything fails |

Every command is in the [CLI reference](../07-cli-reference/README.md).
