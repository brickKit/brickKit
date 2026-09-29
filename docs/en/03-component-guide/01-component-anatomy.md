# A component repository

## What's in one

The repository of `demo/quote` looks like this:

```text
demo-quote/
├── component.yaml      the component describing itself: who I am, what I depend on, what config I need, how to deploy me, how to check I'm healthy
├── BRICKKIT.md         documentation for the people (and AIs) who use it
├── api/
│   └── openapi.yaml    the contract: how others call me
├── main.go             the source
├── go.mod
└── Dockerfile          how the source becomes an image
```

| File | Required? | Who reads it |
| --- | --- | --- |
| `component.yaml` | Yes | The CLI: installing, generating deployment files and injecting environment variables all rest on it |
| `Dockerfile` | When the component has no prebuilt image | `brickkit build` |
| `BRICKKIT.md` | It should be there | The people and AI assistants using the component; see [The component's BRICKKIT.md](08-component-doc-spec.md) |
| Contract files | When it has an external interface, they should be there | Callers; see [Contracts and artifacts](06-artifacts-and-contracts.md) |
| Database migration scripts | When the component has a database | The component's own migration command (`migration.command`) |
| Source | Depends | The platform never reads source |

The platform asks only three things of a component: a `component.yaml`, being a container (an image that can be built or
pulled), and a health check. Language, framework, directory layout and log format are the component's own business.

A component repository may also hold `brickkit.yaml`, `deploy.yaml` and `config/` — that's the author's own local
workbench for integration (see [Developing inside a component](05-local-dev-fractal.md)) and has nothing to do with
releases: users read only `component.yaml`.

## One component = one Git repository

A component is an independent **unit of release** (it has its own versions), **unit of movement** (`sync` moves the whole
directory, `.git` included, when archiving) and **unit of permission** (who can see it, who can change it). So a
component lives in a repository of its own. The parts of one piece of business (contract, backend code, migration
scripts) belong to **the same component**; they don't need splitting.

| | Rule | Example |
| --- | --- | --- |
| Component ID | `<scope>/<name>`, lowercase | `demo/quote` |
| Repository name | `<scope>-<name>` | `demo-quote` |
| Repository address | The install source's `baseUrl` + the repository name | `https://git.example.com/components/demo-quote` |
| Version | An exact `major.minor.patch` | `0.1.0` |
| Version tag | The version itself, without `v` | `0.1.0` |

When the address can't be derived (the repository is in another organisation, the name doesn't match), users can name
the repository for that one component; see [Distributing through Git](10-git-distribution.md). Several components in
subdirectories of one repository (a monorepo) work too: tags carry a namespace, `<scope>-<name>/<version>`, one per
component.

## The smallest usable component

Two files at least: `component.yaml`, and whatever makes the image exist.

- A component with its own code: `component.yaml` + `Dockerfile` + source.
- A component wrapping an existing image (an open-source service, say): `deployment.image` in `component.yaml` is enough —
  no code, no Dockerfile.

A frontend component is a container too: a web server container such as nginx serves the static files, with `port: 80`.
The platform has no such thing as "a component that isn't a container".
