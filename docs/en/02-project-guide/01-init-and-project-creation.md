# Creating a project

A BrickKit project is an ordinary directory holding the three layers of files plus a few agreed subdirectories.
`brickkit init` creates them all at once.

## `brickkit init <name>`: a new project

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
```

A project name may contain only lowercase letters, digits and hyphens, starting and ending with a letter or digit — it
becomes the name of the Docker network (`brickkit-my-shop`) and of the Kubernetes namespace.

The generated `brickkit.yaml` already declares two **local install sources**; the other two kinds are there as
comments — uncomment one and fill in the address to use it:

```yaml
# brickkit.yaml — what this project is made of: its sources and the exact component versions.
# Shared and reviewed like code: commit it.
project: my-shop

# Install sources, tried in declaration order
sources:
  - name: local-dev
    type: local
    path: ./components      # brickkit init already created this directory
  - name: local-shells
    type: local
    path: ./shell           # shells (kind: shell) are project code: brickkit new --shell writes here
  # More sources: uncomment and fill in the address
  # - name: company-git
  #   type: git
  #   baseUrl: https://git.example.com/components/
  # - name: market
  #   type: market
  #   url: https://market.example.com/api/v1

components: []
```

An "install source" is "where to look for components": a local source is a directory on your machine, a Git source is
a set of Git repositories (component `demo/hello`'s repository is `<baseUrl>demo-hello`, and a version is a tag), and a
market is a component market service. They're tried in declaration order, and **the first source that has the
component wins**.

`deploy.yaml` holds only the deploy target and `config/vars.yaml` is an empty shared-variables file — they fill up as
you add components.

### The generated `.gitignore`

```text
# BrickKit CLI working directory: caches, generated files, credentials
.brickkit/

# Your personal deploy file and its backup (brickkit local on)
deploy.local.yaml
deploy.local.yaml.bak

# Secrets referenced from config with file://, and the .env file
.secrets/
.env

# Config files of removed components (kept for a later re-add)
config/.archive/

# Component source cloned for development (each component is its own Git repository)
components/
```

Each line has a reason: `.brickkit/` is a cache that can be rebuilt at any time; `deploy.local.yaml` is yours alone;
`.secrets/` and `.env` hold real secrets; `config/.archive/` is this machine's undo button; every component under
`components/` is **its own** Git repository (one component per repository) and shouldn't be taken in again by the
project's.

### Without the AI assistant skills: `--no-skills`

By default `init` installs skill files for AI assistants to read (`.claude/skills/`, `AGENTS.md`); they're committed with
the project so every teammate's AI assistant understands it. Don't want them? Add `--no-skills`; to get them later, run
`brickkit skills update`. `init` never touches your own `CLAUDE.md`.

## Completion: `brickkit init` in an existing directory

Without a name, `init` adds whatever the **current directory** lacks. Two typical cases:

- an existing project repository is adopting BrickKit;
- a component repository wants its own local workbench for integration (see
  [Writing components](../03-component-guide/05-local-dev-fractal.md)).

The rule is "create what's missing, leave what exists". When the directory isn't empty, it prints the plan first and
waits for you:

```bash
cd legacy-shop
brickkit init
```

```text
This directory already has files; brickkit init will:
   ✅ create  brickkit.yaml
   ✅ create  deploy.yaml
   ✅ create  config/vars.yaml
   ✅ create  config/.gitkeep
   ✅ create  shell/.gitkeep
   ✅ create  components/
   ✅ create  BRICKKIT.md
   ⚠️  .gitignore is missing: .brickkit/, deploy.local.yaml, deploy.local.yaml.bak, .secrets/, .env, config/.archive/, components/ (not edited — add them yourself)
Continue? [y/N] 
```

Worth noticing:

- **An existing `.gitignore` is checked, never modified.** Every missing entry gets a loud warning: without them,
  personal deploy files and secrets get committed. It doesn't edit the file because the file is yours, and may hold
  rules it doesn't understand.
- **`--yes` skips the question** (the plan is still printed), for CI.
- **The project name** is `--name`, otherwise the existing `brickkit.yaml`'s `project`, otherwise the directory name.
- **At the end, the project is loaded the way `up` loads it**; if it can't be, the command fails — what completion
  produces has to be a usable project.

```bash
brickkit init --yes
```

```text
This directory already has files; brickkit init will:
   ✅ create  brickkit.yaml
   ✅ create  deploy.yaml
   ✅ create  config/vars.yaml
   ✅ create  config/.gitkeep
   ✅ create  shell/.gitkeep
   ✅ create  components/
   ✅ create  BRICKKIT.md
   ⚠️  .gitignore is missing: .brickkit/, deploy.local.yaml, deploy.local.yaml.bak, .secrets/, .env, config/.archive/, components/ (not edited — add them yourself)
✅ Project completed: legacy-shop
⚠️ Warning: .gitignore is missing required entries — personal deploy files and secrets can be committed
   File: .gitignore
   Missing: .brickkit/
   Missing: deploy.local.yaml
   Missing: deploy.local.yaml.bak
   Missing: .secrets/
   Missing: .env
   Missing: config/.archive/
   Missing: components/
   Suggestion: Add each missing line to .gitignore by hand (brickkit init never edits an existing .gitignore)
   📁 .claude/skills/      AI assistant skills (4)
   📄 AGENTS.md            AI assistant project guide
   🪝 .git/hooks/pre-commit Check the component layout before committing
   ✅ closing check passed: the project loads
```

When the project root is the Git repository root, `init` also installs the pre-commit check (see
[Managing source](10-sync-and-restore.md#the-pre-commit-check)).

## Cloning an existing project

A teammate already created the project and pushed it to Git. You **don't** need `init`:

```bash
git clone https://git.example.com/projects/my-shop.git
cd my-shop
brickkit build
brickkit up
```

`.brickkit/` isn't in the repository, but it's only a cache: on the first run the CLI fetches each component's
`component.yaml` and contract files from the install sources again, following `brickkit.yaml`. `components/` isn't in the
repository either — `up` needs only the components' `component.yaml`, not their source; to change a component's code,
clone it with `brickkit add <id>@<version> --repo` (see [Adding components](02-add-and-component-install.md#cloning-source---repo---repo-all)).
The `build` step only has work to do when a component's image is built locally; see [Building and images](11-build-and-images.md).

## The project layout

```text
my-shop/
├── brickkit.yaml         which components, which versions (committed)
├── deploy.yaml           how to deploy (committed)
├── deploy.local.yaml     your personal deploy file (not committed, created by brickkit local on)
├── config/               the components' business config and shared variables (committed)
│   └── .archive/         config of removed components (not committed)
├── components/           a local install source: component source you're developing, or cloned to change (not committed)
├── shell/                a local install source: shells, the project's own code (committed)
├── BRICKKIT.md           the project map
└── .brickkit/            the CLI's caches and generated files (not committed)
```

- **`components/`** holds component source. Each component is its own Git repository, laid out as `<scope>/<name>/` —
  the layout a local source scans. To commit component source with the project, take `components/` out of
  `.gitignore` and install the pre-commit check with `brickkit init --hooks`.
- **`shell/`** holds shells: components that compile several components into one process (see [Shells](../04-shell/README.md)).
  A shell is the project's own code and is committed with it.
- **`config/`** holds each component's config, one file per component (see [The config/ directory](../01-three-layers/05-config-directory.md)).
