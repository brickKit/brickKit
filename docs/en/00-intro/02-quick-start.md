# Quick start (five minutes)

Start from an empty directory: create a project, put a component in it, build its image, start it, reach it with
`curl`, change a setting and watch it take effect, then stop everything. Every command and every piece of output below
comes from a real run.

The component is the repository's own test fixture `demo/hello`: a minimal HTTP service that answers with a greeting.
It isn't a business component — and because it is so small, it's the right one for seeing clearly what the platform
does.

## Before you begin

- `brickkit` is installed (see [Install in the README](../../../README.md#install)) and `brickkit version` prints a version;
- Docker 20.10+ (with Compose V2) is running;
- port 8080 on your machine is free (if it isn't, step 4 says how to use another port).

Also clone the BrickKit repository — the fixture components live in its `tests/components/`:

```bash
git clone https://github.com/brickKit/brickKit.git
```

## Step 1: create a project

```bash
brickkit init my-shop
```

```text
✅ Project initialized: my-shop
   📄 brickkit.yaml        Project config
   📄 deploy.yaml          How it is deployed (team file, committed)
   📁 config/              Component configuration and shared vars
   📁 components/          Component source (configured as the local install source local-dev)
   📁 shell/               Shells (kind: shell), project code (the local install source local-shells)
   📁 .brickkit/           CLI working directory
   📄 BRICKKIT.md          Project map: components and where their docs are
   📁 .claude/skills/      AI assistant skills (4)
   📄 AGENTS.md            AI assistant project guide
   💡 If component source goes into Git with the project: brickkit init --hooks installs the pre-commit check

Next steps:
  cd my-shop
  brickkit add --local                     add every component under components/
  brickkit add <scope>/<name>@<version>    add a component from an install source (enable one under sources: in brickkit.yaml first)
  brickkit up                              start everything in one go
```

`init <name>` creates a directory of that name holding the skeleton of the three layers:

```text
my-shop/
├── brickkit.yaml     which components (empty for now)
├── deploy.yaml       how to deploy: target: docker
├── config/
│   └── vars.yaml     shared variables (empty for now)
├── components/       a local install source: your own components' source goes here
├── shell/            a local install source: shell components
├── BRICKKIT.md       the project map
├── .brickkit/        the CLI's caches and generated files (not in Git)
└── .gitignore
```

`brickkit.yaml` already declares two **local install sources**, `./components` and `./shell`. An install source is
"where to look for components"; a local source is a directory on your machine, and the other kinds are Git repositories
and a component market.

## Step 2: add a component

Copy the fixture into the project's local source (components in a local source sit at
`<scope>/<name>/component.yaml`), then add it:

```bash
cd my-shop
mkdir -p components/demo
cp -r ../brickKit/tests/components/demo-hello components/demo/hello
brickkit add demo/hello
```

```text
🔎 No version given for demo/hello; the latest is 1.0.0 (install source local-dev)
➕ Adding demo/hello@1.0.0
   ✅ demo/hello@1.0.0
📝 Written: brickkit.yaml, deploy.yaml
📝 Config skeletons: config/demo-hello.yaml
📦 Artifacts: 1 file, in .brickkit/artifacts/
```

One `add` changed all three layers:

- `brickkit.yaml` gained a line with an exact version — that is the lock:

  ```yaml
  components:
    - id: demo/hello
      version: 1.0.0
  ```

- `deploy.yaml` gained a deployment entry for the component (nothing in it yet, meaning "run it the default way"):

  ```yaml
  components:
    - id: demo/hello
  ```

- `config/demo-hello.yaml` is a config skeleton generated from the component's `configSchema`. Optional items with a
  default are written as comments:

  ```yaml
  # === Optional: commented keys use the component's default; uncomment to override ===
  # GREETING: Hello  # string | The greeting the component answers with (default)
  ```

The component's API contract (an `openapi.json` here) was downloaded to `.brickkit/artifacts/` too; it's what you'd
read when writing a caller.

## Step 3: build the image

Try starting it straight away:

```bash
brickkit up
```

```text
🚀 Starting project my-shop (target: docker)
📋 Component state calculation:
   ✅ demo/hello@1.0.0  starting (top-level)

📋 Start order (topological sort):
   1. demo-hello-1-0-0  no dependencies

Can start on their own: demo-hello-1-0-0 (no dependencies)
📄 Generated: .brickkit/generated/compose.yaml
❌ Error: these images are built locally and have not been built yet
   demo/hello@1.0.0: brickkit-demo/hello:1.0.0
   Suggestion: up never builds on its own (building and deploying are separate): run brickkit build demo/hello@1.0.0 first
```

It fails, and it says exactly why: a component in a local source is **code you're developing**, so its image has to be
built from it — and `brickkit up` **never builds**. Building is your explicit step; deploying is the platform's
mechanical one. That way `up` can never quietly bake code you didn't mean to ship into an image.

```bash
brickkit build
```

```text
🔨 Building demo/hello@1.0.0 → brickkit-demo/hello:1.0.0
✅ Built demo/hello@1.0.0 → brickkit-demo/hello:1.0.0
```

When you need `build`: the component comes from a local source, or its `component.yaml` doesn't name a
`deployment.image` (it only says how to build one). A component added from a Git repository or a market that does
name an `image` is pulled, not built. An image's tag always equals the component's version.

## Step 4: start it

By default a component **doesn't open a port on the host** — what isn't declared isn't reachable. To `curl` it from
your machine, open it in `deploy.yaml`:

```yaml
components:
  - id: demo/hello
    expose: true        # map it to port 8080 on the host
```

If 8080 is taken, add `exposePort: 18080` as well and use 18080 in the `curl` below.

```bash
brickkit up
```

```text
🚀 Starting project my-shop (target: docker)
📋 Component state calculation:
   ✅ demo/hello@1.0.0  starting (top-level)

📋 Start order (topological sort):
   1. demo-hello-1-0-0  no dependencies

Can start on their own: demo-hello-1-0-0 (no dependencies)
📄 Generated: .brickkit/generated/compose.yaml

🐳 Starting (docker)...
   demo-hello-1-0-0             running (healthy)
✅ All components started (1)

💡 View the status: brickkit status
   View the logs: docker compose -p brickkit-my-shop logs -f
```

`demo-hello-1-0-0` is the component's **versioned service name**: the component ID with `/` and `.` turned into `-`,
followed by the exact version. Another component calling it gets the address `http://demo-hello-1-0-0:8080` — exactly
the same on local Docker and on Kubernetes.

## Step 5: check it

```bash
curl http://localhost:8080/api/v1/hello
```

```json
{"component":"demo/hello","greeting":"Hello","message":"Hello, I'm demo/hello@1.0.0","version":"1.0.0"}
```

```bash
brickkit status
```

```text
📊 Project status: my-shop (target: docker)

✅ Running (1 component)
 ┌────────────┬─────────┬───────────────────┬─────────────────────────────────────────────┐
 │ Component  │ Version │ Status            │ Port                                        │
 ├────────────┼─────────┼───────────────────┼─────────────────────────────────────────────┤
 │ demo/hello │ 1.0.0   │ running (healthy) │ 0.0.0.0:8080->8080/tcp, [::]:8080->8080/tcp │
 └────────────┴─────────┴───────────────────┴─────────────────────────────────────────────┘
```

## Step 6: change a setting and watch it take effect

In `config/demo-hello.yaml`, uncomment that line and give it another value:

```yaml
GREETING: Howdy
```

A key in the config file is the name of the environment variable the component receives, injected as-is. `up` again:

```bash
brickkit up
```

```text
🐳 Starting (docker)...
   demo-hello-1-0-0             running (healthy)
✅ All components started (1)
```

(The earlier sections are the same as last time and are left out here.)

```bash
curl http://localhost:8080/api/v1/hello
```

```json
{"component":"demo/hello","greeting":"Howdy","message":"Howdy, I'm demo/hello@1.0.0","version":"1.0.0"}
```

No config server, no hot reload: change the file, `up` again, and the container is recreated with the new environment.

## Step 7: stop it

```bash
brickkit down
```

```text
🛑 Stopping project my-shop
✅ All components stopped

💡 Data volumes were not deleted; database data is still there
   For a full cleanup, run by hand: docker volume rm <volume-name>
   Start again with: brickkit up
```

`down` never deletes data volumes: deleting data by mistake costs too much, so a real cleanup is yours to do by hand.

## Where next

- What each of these files owns and what else you can write in it: [The three layers](../01-three-layers/README.md)
- More components, local debugging, several environments: [Running a project](../02-project-guide/README.md)
- Writing your own component: [Writing components](../03-component-guide/README.md)
- Every flag of every command: [CLI reference](../07-cli-reference/README.md)
