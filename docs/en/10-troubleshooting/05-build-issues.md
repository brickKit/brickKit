# Build problems

`brickkit build` builds the images that have to be built on this machine; `brickkit up` never builds. Background in
[Building and images](../02-project-guide/11-build-and-images.md).

## Code changed, but the old behaviour still runs

**Symptom**

You changed a component's code in a local source, ran `brickkit build` and then `up`, and nothing changed. `build`
printed:

```text
⏭️  shop/order@0.1.0: image shop-order:0.1.0 already exists, skipped (--force rebuilds)
```

**Cause**

An image's tag is the component's version. The version didn't change, so neither did the tag, and `build` took the image to
be up to date and skipped it.

**Fix**

During development, having changed code but not the version, add `--force`:

```bash
brickkit build shop/order@0.1.0 --force
```

Then `brickkit up` — `up` sees the image changed and recreates the container. For changes you'll release, raise the
version (`metadata.version`) and build, with no need for `--force`.

## `up` says the images haven't been built yet

**Symptom**

```text
❌ Error: these images are built locally and have not been built yet
   demo/hello@1.0.0: demo-hello:1.0.0
   demo/caller@1.0.0: demo-caller:1.0.0
   Suggestions:
   1. up never builds on its own (building and deploying are separate): run brickkit build first, or build one of them:
   2. brickkit build demo/hello@1.0.0
   3. brickkit build demo/caller@1.0.0
```

**Cause**

These components come from a local source, or their `component.yaml` writes only `deployment.build` and no `image` — the
image has to be built on this machine, and this machine doesn't have it yet. Common right after cloning a project, right
after `upgrade` to a new version, or right after `add --repo` cloned a component's source into `components/` (from then on
it's provided by the local source, and its image is built from your workspace instead of pulling the one the author
published).

**Fix**

`brickkit build` (everything), or build just one as suggested. `up` doesn't build on the side, so that half-changed code in
your workspace is never quietly baked into an image and run.

## Images can't be pulled

**Symptom**

`up` reports `Error: image pull not authorized` or `Error: image not found`.

**Cause**

The component writes `deployment.image`, and the image is in a registry you're not logged in to or have no permission on,
or the address is wrong, or that tag was never pushed.

**Fix**

- Not logged in: `docker login <registry>`.
- The image really doesn't exist: confirm the image address and tag with the component's author; when the component also
  writes `build`, you can clone its source into a local source (`add --repo`) and build it on this machine instead.

## The Dockerfile has an error

**Symptom**

`brickkit build` fails, with `docker build`'s own last output in the error. Here a `COPY` names a file that doesn't exist
(an excerpt of the end of "Output"):

```text
🔨 Building shop/order@0.1.0 → shop-order:0.1.0
❌ Error: docker failed to run
   Command: docker build -t shop-order:0.1.0 -f components/shop/order/Dockerfile --label io.brickkit.build=local --label io.brickkit.component=shop/order --label io.brickkit.version=0.1.0 components/shop/order
   Output: #6 DONE 0.0s
      …
      Dockerfile:3
      --------------------
      1 |     FROM golang:1.22-alpine AS build
      2 |     WORKDIR /src
      3 | >>> COPY missing.go go.mod *.go ./
      4 |     RUN CGO_ENABLED=0 go build -o /out/order .
      5 |
      --------------------
      ERROR: failed to build: failed to solve: failed to compute cache key: failed to calculate checksum of ref nw5giryoc7te33iudojbosk8l::0paygf81hvsil7g696wl0ypwg: "/missing.go": not found
   Component: shop/order@0.1.0
```

`>>>` points at the failing line. The "Command" line is the `docker build` BrickKit actually ran, which you can copy and run
on its own as it is.

**Cause**

Building is done by `docker build`; BrickKit only passes it the build context and the Dockerfile path from
`deployment.build` in `component.yaml`. Both are relative to **the component repository's root**: `context` defaults to
`.`, `dockerfile` to `Dockerfile`. Common mistakes: `dockerfile` written relative to `context`; a `COPY` referencing a file
outside the build context.

**Fix**

Fix the Dockerfile following `docker build`'s own words. To reproduce it alone, run the same `docker build` in the
component repository's root, without BrickKit.

## The image is too big

**Symptom**

The image is hundreds of MB or even over a GB, and building, pushing and pulling are all slow.

**Cause and fix**

Nothing to do with BrickKit — it's how the Dockerfile is written. The usual measures:

- **Multi-stage builds**: compile in a stage with the full toolchain, and copy only the output into the final image. A Go
  component's final image can be a dozen or so MB.
- **Pick a small base image**: `alpine`, `*-slim`.
- **`.dockerignore`** to leave out `.git`, `node_modules`, test data.

Only one thing to watch: with a `type: http` health check, the health-check command runs **inside** the container, so the
image needs `wget` or `curl`. After switching to a base image with no tools at all (`scratch`, distroless), the container
stays judged unhealthy forever, while the component's own logs look fine. Such an image either keeps a `wget` (based on
`busybox` or `alpine`, say), or switches the health check to `type: tcp`. See
[up / down problems](01-up-down-issues.md#the-components-logs-look-fine-yet-the-platform-says-its-unhealthy).
