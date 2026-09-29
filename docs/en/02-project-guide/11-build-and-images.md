# Building and images

## Building is separate from deploying

`brickkit up` **never builds images**. When an image is missing, it says so, tells you to run `brickkit build`, and stops.

Why so strict: building turns a piece of code into something that will run — a step you should decide on. Is this code
the one you mean to ship? Deploying, on the other hand, is mechanical: run images that already exist, the way the deploy
file says. If `up` built whenever an image was missing, half-finished code in your working copy could end up in an image
and running when you never meant it to. With the two apart, what `up` does is always predictable: it only runs what
already exists.

## Which components are built locally

| Component | Where the image comes from |
| --- | --- |
| Its `component.yaml` names a `deployment.image`, and it comes from a Git source or a market | Pulled: the author already pushed the image to a registry, so you build nothing |
| Its `component.yaml` names only `deployment.build` (no `image`) | Built locally: from an export of this version's Git tag |
| From a local source (code being developed in `components/` or `shell/`) | Built locally: from your working copy — whether or not an `image` is named |

The last row is worth noting: once `add --repo` has cloned a component's source into `components/`, the component is
provided by the local source, and its image is built from your working copy rather than pulled from what the author
published — because you cloned it to run the code in your hands.

## `brickkit build`

```bash
brickkit build
```

```text
⏭️  demo/hello@1.1.0: image demo-hello:1.1.0 already exists, skipped (--force rebuilds)
⏭️  demo/bus@1.0.0: image demo-bus:1.0.0 already exists, skipped (--force rebuilds)
🔨 Building demo/hello@1.0.0 → demo-hello:1.0.0
✅ Built demo/hello@1.0.0 → demo-hello:1.0.0
🔨 Building demo/caller@1.0.0 → demo-caller:1.0.0
✅ Built demo/caller@1.0.0 → demo-caller:1.0.0
```

Without arguments, it builds every component in the project that's built locally, skipping images that already exist.
To build only one:

```bash
brickkit build demo/bus
```

```text
🔨 Building demo/bus@1.0.0 → demo-bus:1.0.0
✅ Built demo/bus@1.0.0 → demo-bus:1.0.0
```

`brickkit build demo/hello` builds every version of that component in the project; `brickkit build demo/hello@1.1.0`
only that one.

**An image that already exists is skipped:**

```text
⏭️  demo/hello@1.1.0: image demo-hello:1.1.0 already exists, skipped (--force rebuilds)
⏭️  demo/hello@1.0.0: image demo-hello:1.0.0 already exists, skipped (--force rebuilds)
```

An image's tag is the component's version, and the tag doesn't change while the version doesn't. So **when you changed
the code but not the version number, add `--force`**, or `build` considers the image up to date:

```bash
brickkit build demo/hello@1.1.0 --force
```

```text
🔨 Building demo/hello@1.1.0 → demo-hello:1.1.0
✅ Built demo/hello@1.1.0 → demo-hello:1.1.0
```

**Where the source comes from.** When the local source holds exactly this version, your working copy is used; otherwise a
clean export of this version's Git tag is built — so when a project has both 1.0.0 and 1.1.0, each image matches its own
tag, and nothing gets mixed up.

**Tag rules.** With an `image` named, its name is used; without one, the name is derived from the component ID
(`demo/hello` → `demo-hello`); the tag always equals `metadata.version`. A shell's image also records the member versions
compiled into it; when `up` finds they don't match the members the shell declares (a member version changed without a
rebuild), it stops — see [Upgrading a shell](../04-shell/07-shell-upgrade.md).

## A prebuilt image: `image`

The component's author pushes an image to a registry and names it in `component.yaml`:

```yaml
deployment:
  type: container
  image: registry.example.com/demo/hello:1.0.0
  port: 8080
```

Users build nothing: `up` checks the image is on the machine or can be pulled from the registry, then starts it. When it
can't be pulled (not logged in, no permission, no network), `up` stops with an error and points out that you can build
locally instead.

## Building locally: `build`

When a component has no published image, or you want to build from source anyway:

```yaml
deployment:
  type: container
  build:
    context: .
    dockerfile: Dockerfile
  port: 8080
```

`context` is the build context and `dockerfile` the path of the Dockerfile, both relative to the component repository
root; unwritten, they're `.` and `Dockerfile`. With both `image` and `build`, a component in a local source is built from
`build`, and one from Git or a market is pulled by `image`. How to write a Dockerfile: [Writing components](../03-component-guide/README.md).

## Cloning a project for the first time

```bash
git clone https://git.example.com/projects/my-shop.git
cd my-shop
brickkit build
brickkit up
```

`build` exports each locally built component's Git tag and builds it; components with a prebuilt image are skipped. After
that you only need `build` again when a component version changes (after `upgrade`) or when you change code in a local
source.
