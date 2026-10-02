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
   📄 AGENTS.md            the project's AI guide; the component table at its end is maintained by brickkit
   📄 CLAUDE.md            @AGENTS.md: Claude Code reads AGENTS.md through it
   📁 .claude/skills/      AI assistant skills (5)
   💡 If component source goes into Git with the project: brickkit init --hooks installs the pre-commit check

Next steps:
  cd my-shop
  brickkit add --local                     add every component under components/
  brickkit add <scope>/<name>@<version>    add a component from an install source (enable one under sources: in brickkit.yaml first)
  brickkit up                              start everything in one go
```

A project name may contain only lowercase letters, digits and hyphens, starting and ending with a letter or digit, at
most 54 characters — it names the Docker network (`brickkit-my-shop-net`) and the default Kubernetes namespace
(`brickkit-my-shop`).

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

By default `init` installs skill files for AI assistants to read (`.claude/skills/brickkit-*`, five of them); they're
committed with the project so every teammate's AI assistant understands it. Don't want them? Add `--no-skills`; to get
them later, run `brickkit skills update`. `AGENTS.md` and `CLAUDE.md` are written either way: they are the project's own
guide, not skills (see [The project's `AGENTS.md`](#the-projects-agentsmd)).

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
   ✅ create  AGENTS.md
   ✅ create  CLAUDE.md
   ⚠️  .gitignore is missing: .brickkit/, deploy.local.yaml, deploy.local.yaml.bak, .secrets/, .env, config/.archive/ (not edited — add them yourself)
Continue? [y/N] 
```

Worth noticing:

- **An existing `.gitignore` is checked, never modified.** Every missing entry gets a loud warning: without them,
  personal deploy files and secrets get committed. It doesn't edit the file because the file is yours, and may hold
  rules it doesn't understand.
- **`--yes` skips the question** (the plan is still printed), for CI.
- **The project name** is `--name`, otherwise the existing `brickkit.yaml`'s `project`, otherwise the directory name.
  A `--name` that contradicts an existing `brickkit.yaml` is refused (completion never changes an existing file); a
  directory name that isn't a valid project name is turned into one when it can be (`My_Shop` → `my-shop`), and
  otherwise `init` stops and asks for `--name`.
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
   ✅ create  AGENTS.md
   ✅ create  CLAUDE.md
   ⚠️  .gitignore is missing: .brickkit/, deploy.local.yaml, deploy.local.yaml.bak, .secrets/, .env, config/.archive/ (not edited — add them yourself)
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
   📁 .claude/skills/      AI assistant skills (5)
   🪝 .git/hooks/pre-commit Check the component layout before committing
   ✅ closing check passed: the project loads
```

When the project root is the Git repository root, `init` also installs the pre-commit check (see
[Managing source](11-sync-and-restore.md#the-pre-commit-check)).

## Cloning an existing project

A teammate already created the project and pushed it to Git. You **don't** need `init`:

```bash
git clone https://git.example.com/projects/my-shop.git
cd my-shop
brickkit build
brickkit up
```

`.brickkit/` isn't in the repository, but it's only a cache: on the first run the CLI fetches each component's
`component.yaml` (and its `BRICKKIT.md`) from the install sources again, following `brickkit.yaml`. Contract files
(OpenAPI and the like) aren't fetched by `up`; they're only needed when you write a caller, and
`brickkit fetch <id>@<version>` downloads them into `.brickkit/artifacts/`. `components/` isn't in the
repository either — `up` needs only the components' `component.yaml`, not their source; to change a component's code,
clone it with `brickkit add <id>@<version> --repo` (see [Adding components](02-add-and-component-install.md#cloning-source---repo---repo-all)).
The `build` step only has work to do when a component's image is built locally; see [Building and images](12-build-and-images.md).

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
├── AGENTS.md             the project's AI guide, with the component table at its end (committed)
├── CLAUDE.md             @AGENTS.md, so Claude Code reads AGENTS.md (committed)
├── .claude/skills/       the AI assistant skills (committed)
└── .brickkit/            the CLI's caches and generated files (not committed)
```

- **`components/`** holds component source. Each component is its own Git repository, laid out as `<scope>/<name>/` —
  the layout a local source scans. To commit component source with the project, take `components/` out of
  `.gitignore` and install the pre-commit check with `brickkit init --hooks` (see
  [The pre-commit check](11-sync-and-restore.md#the-pre-commit-check)).
- **`shell/`** holds shells: components that compile several components into one process (see [Shells](../04-shell/README.md)).
  A shell is the project's own code and is committed with it.
- **`config/`** holds each component's config, one file per component (see [The config/ directory](../01-three-layers/05-config-directory.md)).

## The project's `AGENTS.md`

`AGENTS.md` is the file AI coding tools read first when they open a repository — a convention shared across tools, not
something BrickKit invented. Claude Code reads `CLAUDE.md` instead, so `init` also writes a `CLAUDE.md` holding exactly
one line, `@AGENTS.md`, which makes Claude Code read the same file. Both are created only when they are missing; in a
file that already exists `init` changes nothing outside the brickkit-maintained block at the end of `AGENTS.md` (see
[Which commands touch it](#which-commands-touch-it); the one exception is an untouched `AGENTS.md` left by an earlier
version, see [below](#a-project-from-an-earlier-version-the-old-brickkitmd)).

What it buys: whichever AI tool a teammate uses, it starts from the same page, and that page is in Git with the project.
What it costs: one more file the team has to keep true — an `AGENTS.md` that describes last year's project misleads
every AI that reads it.

### The sections you write

The file is yours. The skeleton has four sections, each with a `<!-- TODO: … -->` hint (`brickkit lint` lists every hint
still there):

| Section | What goes in it |
| --- | --- |
| Overview | What this project is, its domains, how its components group |
| Conventions | The rules every component here follows — stack, port and schema registries, naming, review rules — so that no component has to repeat them. One line each; a rule that needs more links its full text in `docs/` |
| Where to look | A table: what you are doing (the words you would search for) → the file to read first. Include where the detailed conventions, the decisions and the operations docs are (see [The project's other documents](#the-projects-other-documents)): skills look them up here |
| Pitfalls | A table: Never / Symptom / Why — mistakes that hold for every component in this project |

### The block at the end

At the end of the file is **one** block the CLI maintains, between `<!-- brickkit:managed:begin lang=<code> -->` and
`<!-- brickkit:managed:end -->`. Whatever is written between the markers is overwritten the next time the block is
rewritten; everything outside them is left alone. The block holds:

- a short summary of the platform rules: what each of the three layers holds, that `add` / `remove` / `upgrade` keep them
  in step, that config keys are the variable names from `configSchema`, where the skills are;
- one line saying where each component's documentation is — `BRICKKIT.md` (translations `BRICKKIT.<lang>.md`) in its
  source directory when a local source holds exactly this version, otherwise in `.brickkit/manifests/<id>/<version>/` —
  and where its contracts are, `.brickkit/artifacts/<service name>/`;
- the component table. After `brickkit add --local` with two components:

```text
| Component | Version | What it does | Docs | Home |
|---|---|---|---|---|
| demo/hello | 1.0.0 | The smallest HTTP component for BrickKit's own self-tests; serves a greeting and echoes its environment variables | BRICKKIT.md | — |
| demo/caller | 1.0.0 | The caller component for BrickKit's own self-tests; verifies dependency address injection and optional-dependency degradation | BRICKKIT.md | — |
```

| Column | Comes from |
| --- | --- |
| What it does | The component's `metadata.description` |
| Docs | `BRICKKIT.md`, plus each translation it carries (`BRICKKIT.md +zh`); `—` when it carries no docs |
| Home | The component's optional `metadata.repository`; `—` when it has none |

**Only facts that are the same on every machine go into the block.** The file is committed: a column such as "is the
source local here" would change with whoever ran the command last, and every teammate's `add` would produce a diff.
Whether a component's source is on this machine, an AI checks for itself under `components/`.

**The block records its language** (`lang=`). It is set when the block is first written, and every later rewrite uses
that language — never the language of whoever runs the command — so two teammates whose CLIs speak different languages
don't rewrite each other's file. `brickkit skills update --lang zh` switches it, together with the skills.

### Which commands touch it

| Command | What it does to `AGENTS.md` and `CLAUDE.md` |
| --- | --- |
| `init` | Creates either file when it is missing. In an existing `AGENTS.md` it rewrites the block if there is one; if there isn't, it only warns |
| `add`, `remove`, `upgrade` | Rewrite the existing block (the component table) and nothing else; they never create a block or append one to your file |
| `brickkit skills update` | An explicit request, so it also repairs: creates a missing file, appends the block to an `AGENTS.md` that lacks it, and appends `@AGENTS.md` to a `CLAUDE.md` that lacks it |

When `init` completes a directory that already has an `AGENTS.md` and a `CLAUDE.md` of its own, it changes neither and
says what is missing:

```text
⚠️ AGENTS.md has no usable brickkit-maintained block, so its component table is not kept up to date
   Reason: there is no block maintained by brickkit
   Suggestion: Run brickkit skills update to add it (an explicit request: no other command changes your file)
⚠️ CLAUDE.md does not contain @AGENTS.md, so Claude Code does not read AGENTS.md
   Suggestion: Run brickkit skills update to add it (an explicit request: no other command changes your file)
```

```bash
brickkit skills update
```

```text
✅ AI assistant skills updated
   ✅ AGENTS.md: appended the brickkit-maintained block at the end
   ✅ appended @AGENTS.md to CLAUDE.md
```

### A project from an earlier version: the old `BRICKKIT.md`

Earlier versions kept the component table in a `BRICKKIT.md` at the project root, the "project map". That file is gone:
the table lives at the end of `AGENTS.md` now, and `BRICKKIT.md` means only a component's own documentation. When the
old map (recognised by its markers) is still at the project root, `init`, `brickkit skills update` and `lint` report it
with the warning code `PROJECT_MAP_OBSOLETE`:

```text
⚠️ BRICKKIT.md here is the old project map: the component table now lives at the end of AGENTS.md
```

The suggestion under it says what to do: move any notes of your own into `AGENTS.md`, then delete `BRICKKIT.md`. It is
never deleted for you, because you may have written in it. An `AGENTS.md` that an earlier version installed as a
skill file, and that nobody edited since (recognised from the old `.brickkit/skills.lock` on this machine), is replaced
with the new skeleton by `init` and `brickkit skills update`.

## The project's other documents

`AGENTS.md` is loaded into every AI session, so it has to stay short: each convention in a line, the few pitfalls
everyone hits, and where to look. What doesn't fit goes into the project's `docs/`. BrickKit doesn't fix a layout for
it — a project of three components may need none — but most projects end up with the same three kinds of document,
and these places work:

| Kind | Where | Holds |
| --- | --- | --- |
| Conventions in detail | `docs/conventions/` | The full rule behind each line of `AGENTS.md`'s Conventions: the port registry, schema naming, the review checklist |
| Decisions | `docs/decisions/NNNN-<title>.md` | One decision per file, numbered ([below](#numbering-decisions)): the context, what was decided, why, and what was turned down |
| Operations | `docs/operations/` | Going live, backups, rotating secrets, what to do when production fails |

Whichever layout you choose, put it in the Where to look table of `AGENTS.md` — for example "a change that may go against
an earlier decision → `docs/decisions/`". That table is how an AI finds them, the `brickkit-plan-change` skill included.

### Numbering decisions

A decision's number is its name. Other files cite it — a pitfall in `AGENTS.md` ends with "(0203)", a commit message says
"see 0105" — so a number alone has to find exactly one file:

- **Unique across the project, never reused, never changed.** A decision that is overturned stays where it is, marked as
  replaced by the one that replaced it; the new decision gets a new number.
- **One folder: count up** (`0001`, `0002`, …). That is enough for most projects.
- **Folders by topic: a number range per folder** — `01-architecture/` 0101–0199, `02-permissions/` 0201–0299, and so on.
  The number stays unique and also says the topic; a search for `0203` lands on one file. Write the ranges into the
  folder's `README.md`, the place a person or an AI looks before adding one.
- A decision that later turns out to belong to another topic **keeps its number**, even outside the new folder's range:
  the citations to it are worth more than a tidy range.

This is different from the numbers in front of pages meant to be read in order (`01-…`, `02-…`): those are a reading
order, and change when a page is inserted. A decision number is a name and doesn't.

### One fact, one home

The way project documents go wrong is ordinary: one rule written in `AGENTS.md`'s Pitfalls, in a convention, in a
decision and in a skill of the project's own, each worded a little differently, and an AI trusting whichever it read
first. So, as for a component, every fact has one home and the other files link to it:

| Fact | Its home |
| --- | --- |
| A rule every component follows, in full | `docs/conventions/`; a line in `AGENTS.md`'s Conventions names it and links there |
| The few cross-component mistakes everyone makes | `AGENTS.md`'s Pitfalls, each linking the rule it breaks |
| Why the project chose something, and what it turned down | One file in `docs/decisions/`: the conclusion and the reason, not the rule's full text again |
| How to run it in production | `docs/operations/` |
| What one component does, needs and owns | That component's `BRICKKIT.md`, never repeated in the project's documents |
| Which components there are, at which versions | The component table at the end of `AGENTS.md`, maintained by brickkit |

### More than one language, and what lint checks

The language rules are a component's
([More than one language](../03-component-guide/08-component-doc-spec.md#more-than-one-language)): a translation next to
its file for a few files, a tree per language (`docs/en/`, `docs/zh/`) for a whole `docs/` in two languages. `AGENTS.md`
is usually written once; an `AGENTS.<lang>.md` for human reviewers is allowed and checked like any translation.

`brickkit lint` checks the project's documents as warnings, with a component's rules: the four sections and the block of
`AGENTS.md`, `CLAUDE.md`, the links in `AGENTS.md`, `README.md` and every file under `docs/` (a link may point anywhere in
the project, `components/` included — only its target must exist), placeholders, and translations. Whether a document
says the right thing is for review.
