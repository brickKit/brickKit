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

Naming a component whose image is normally pulled (`brickkit build demo/x`) builds a local image anyway — a way out when
the registry can't be reached — and says so:

```text
ℹ️  demo/x@1.0.0 normally pulls its image (registry.example.com/demo/x:1.0.0); building a local one because it was named
```

## `brickkit build`

Name a component to build only that one:

```bash
brickkit build demo/bus
```

```text
🔨 Building demo/bus@1.0.0 → demo-bus:1.0.0
✅ Built demo/bus@1.0.0 → demo-bus:1.0.0
```

Without arguments, it builds every component in the project that's built locally, skipping images that already exist —
here `demo/hello@1.1.0`, built earlier, and the `demo/bus` image just built:

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

Naming a component doesn't bypass the check: an image that already exists is skipped either way, and only `--force`
rebuilds it (see below).

`brickkit build demo/hello` builds every version of that component in the project; `brickkit build demo/hello@1.1.0`
only that one. In a component's directory, `brickkit build` without an argument builds just that component — here
`demo/lib`, an extra component of `my-shop` whose source has a git submodule:

```bash
cd components/demo/lib
brickkit build
```

```text
📁 Project: ../../.. (my-shop)
🔨 Building demo/lib@1.0.0 → demo-lib:1.0.0
⚠️ Warning: the source of demo/lib@1.0.0 has git submodules, and they are empty directories here
   Directory: third_party/sdk
   Suggestion: BrickKit never fetches submodules; if the build needs them, the component should publish an image (deployment.image) or build without them
✅ Built demo/lib@1.0.0 → demo-lib:1.0.0
```

The warning in the middle is about **git submodules**: BrickKit never fetches them, so in the source it builds from they
are empty directories. When the Dockerfile copies nothing from them, the image is fine; when it needs them, the build
fails or — worse — succeeds without their contents. The lasting fix is for the component to publish an image
(`deployment.image`), so nobody has to build it; in a cloned repository you can also run `git submodule update --init`
yourself.

**An image that already exists is skipped:**

```text
⏭️  demo/hello@1.1.0: image demo-hello:1.1.0 already exists, skipped (--force rebuilds)
⏭️  demo/hello@1.0.0: image demo-hello:1.0.0 already exists, skipped (--force rebuilds)
```

For a component from a local source, "exists" means an image `brickkit build` built from local code; one with the same
tag that came from anywhere else (pulled, or built by hand) is rebuilt. So is a shell's image whose recorded member
versions differ from the members the shell declares.

An image's tag follows the component's version (see the tag rules below), and the tag doesn't change while the version
doesn't. So **when you changed the code but not the version number, add `--force`**, or `build` considers the image up
to date:

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

**Tag rules.** Without an `image`, the image is `<scope>-<name>:<metadata.version>` (`demo/hello` →
`demo-hello:1.0.0`). With an `image` that has no tag, `:<metadata.version>` is appended. An `image` that already has a
tag or a digest is used exactly as written — nothing checks it against the version, so keep its tag equal to the
version yourself. A shell's image also records the member versions
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
