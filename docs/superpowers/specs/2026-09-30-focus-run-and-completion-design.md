# Focus run, one `components/`, and shell completion — design

**Status: design for review.** Agreed with the user in conversation on 2026-09-30; the points marked
*decided here* are choices this document makes on details the conversation did not settle — say so if
any should go the other way. An implementation plan follows once this is approved.

## 1. Problem

A large project is often where its components are actually developed: every component's source sits in
the project's `components/`, and a change that touches several components is made there, with versions
moved forward together (an AI assistant makes that fast: it finds the files by the directory layout,
bumps versions and adapts components one by one until the whole project starts). Today that workflow
has five problems:

1. **You can't work from inside a component.** The CLI reads only the current directory; `brickkit up`
   in `components/erp/api` says there is no project.
2. **Workbenches copy the project's facts.** `add --local --init` gives each local component a workbench
   (its own `brickkit.yaml`, `deploy.yaml`, `config/`) that finds its siblings through copied install
   sources (`path: ../..`). But every workbench pins its own versions and holds its own config: after the
   project upgrades a component, each workbench needs its own `upgrade`, and dependency config (database
   addresses…) is filled in again per workbench. One fact, N+1 places.
3. **Components nest.** `add --repo` inside a workbench clones into that component's own `components/`,
   so one component's source can exist twice.
4. **A nested copy is silently ignored** (a defect today). The local source scans exactly
   `components/<scope>/<name>/`; a copy one level deeper is never seen, and edits made there never run,
   without any message.
5. **A build can silently miss code** (a risk today). `brickkit build` exports a git component's source
   with `git archive`; a git submodule arrives as an empty directory. A build that needs it fails in a way
   that doesn't say why — or produces an incomplete image.

Separately: component IDs are long, and users remember parts of them. The CLI completes command and flag
names in a shell, but not component IDs, and nothing installs the completion for the user.

## 2. Goals and non-goals

**Goals**

- Run and debug one component from inside its directory in a large project, using the project's own three
  layers — no copied versions or config.
- Each component's source exists in exactly one place: the top project's `components/`.
- Say clearly when a submodule's code is missing.
- TAB completion of component IDs, deploy files and languages; installed automatically where the shell
  allows it; a "did you mean" suggestion when an ID is mistyped.

**Non-goals**

- **No second switch for "what starts".** `--only` was removed (commit `f2dc54c`) because it answered that
  question outside the files, so `up` and `sync` disagreed and running components were invisible in the
  files. Focus is written into the personal deploy file (§4).
- **No implicit dependency lookup.** A component's dependencies still come only from install sources written
  in a `brickkit.yaml`; §3 finds *the project*, not extra sources.
- **No automatic moving of nested directories** — they may be repositories with unpushed work, and the top
  copy may differ; which one to keep is the user's call (§5).
- **No fetching of submodules**, and no option for it.
- **No fuzzy execution.** An ID that doesn't match exactly is an error with suggestions, never a guess.
- **No editing of the user's shell rc files.**

## 3. Finding the project from a subdirectory

A command that works on a project, run in a directory without a `brickkit.yaml`, walks up the parent
directories (like git looking for `.git`) to the nearest directory with a `brickkit.yaml`, and runs there.
It does not stop at a `.git` boundary — components under `components/` are usually repositories of their
own. If no ancestor has one, the error is today's `PROJECT_MISSING`.

**Nearest wins.** A directory with its own `brickkit.yaml` (a workbench) is a project; nothing above it is
consulted. Standalone component repositories keep working as today.

**Never silent.** When the project was found above the current directory, the command's first line says
so: `📁 Project: ../../.. (my-shop)`.

| Command | Looks upward | In a component's directory |
| --- | --- | --- |
| `up` | yes | focuses on this component (§4) |
| `down`, `status`, `sync`, `lint`, `graph`, `restore`, `local` | yes | acts on the whole project |
| `add`, `remove`, `upgrade`, `fetch` | yes | — (`add --repo` therefore clones into the top `components/`) |
| `build` | yes | without arguments, builds only this component |
| `deps` | yes | without arguments, shows this component's tree |
| `new` | yes | writes the new component into the top `components/` (`--path` unchanged) |
| `login`, `logout` | yes | — (credentials are kept per project) |
| `release`, `publish` | no | they act on the component in the current directory, as today |
| `init` | no | creates files here; inside a project's component it adds one note: a focus run needs no workbench, and once this directory has a `brickkit.yaml` nothing above it is used |
| `skills` | no | manages files in this directory |
| `lang`, `version`, `completion` | — | global |

"This component" means: the current directory is inside the source directory of a component that one of the
project's local install sources provides (`components/<scope>/<name>/…`). Deeper subdirectories count.

## 4. Focus run

### 4.1 What it is

You are in `components/erp/api`, changing its code. You want `erp/api` running from its source, the
components it needs running in containers, and the other forty components not started. A focus run is
exactly that: **only the focused component and what it needs start; the focused component runs from its
source** (`mode: local`, or `mode: debug` if its entry says so).

### 4.2 Where it is written

In the personal deploy file, as a new top-level field that is accepted **only** in `deploy.local.yaml`
(the same rule as `mode: debug` — it is a personal fact):

```yaml
# deploy.local.yaml
target: docker
focus: erp/api
components:
  - id: erp/api
  …
```

Because it is in the file every command reads, `up`, `status`, `lint`, `build` and `down` agree, and
opening the file shows what runs. `deploy.yaml` or a file given with `-f` (other than `deploy.local.yaml`)
that contains `focus:` is rejected, with the same kind of message `mode: debug` gets there.

### 4.3 Setting and clearing it

| You run | Effect |
| --- | --- |
| `brickkit up` in a component's directory | sets `focus:` to this component, then starts |
| `brickkit up --focus erp/api` anywhere in the project | sets `focus: erp/api`, then starts |
| `brickkit up` at the project root, or in a non-component subdirectory | starts whatever the file says (a focus already written stays) |
| `brickkit up --all` | removes `focus:`, then starts every component as usual |
| deleting the `focus:` line | the same as `--all`, on the next command |

*Decided here:* the flag names `--focus <id>` and `--all`.

Setting a focus when local mode is off turns it on first (copying `deploy.yaml` exactly as `local on` does,
or reusing an existing `deploy.local.yaml`), and says so. Editing the file changes only the `focus:` line;
every other byte, comment and personal change is kept. Each change is announced in one line:
`🎯 Focus: erp/api (written to deploy.local.yaml; brickkit up --all runs everything again)`. Every later
command that reads the file repeats `🎯 Focus: erp/api` next to the existing local-mode reminder.

### 4.4 What starts

The existing rule is "a top-level component runs; below it, a component runs while anything running needs
it; explicit `mode` pins win". A focus replaces only the first part — which components are the starting
points:

- **Starting points:** the focused component, plus every entry with an explicit `mode: enabled`, `local` or
  `debug` (those are intents you wrote, and they still hold).
- **Everything else** runs only if a starting point needs it, required or optional, exactly as today
  ("follows the layer above"). A component outside that set is reported as
  `not starting (outside the focus)`; the focused one as `starting (focus)`.
- **The focused component** runs from source: as `mode: local`, or as `mode: debug` when its entry says
  `mode: debug`. Its entry saying `mode: disable` is an error (two intents contradict).
- **Existing rules are unchanged:** the local repository's `metadata.version` must equal the default version
  in `brickkit.yaml`; a `requiredBy` version can't run from source; shells host whichever members run; a
  member that is focused leaves its shell and runs as a bare process.

### 4.5 One project, not a second deployment

A focus narrows this project's own deployment; it is not a separate stack. It uses the same compose project
(`brickkit-<project>`), network and volumes. Containers of components outside the focus are removed as any
component that stops running is today (`--remove-orphans`); **data volumes are kept**, so the database a
full run used is still there. Switching the focus, or `--all`, reuses what is already running. There are no
duplicate containers to fight over ports.

### 4.6 How each command treats a focus

| Command | With `focus:` set |
| --- | --- |
| `up` | starts the focus set (§4.4) |
| `status` | shows the whole project as today, plus the focus line |
| `down` | stops the project, as today |
| `lint` | also checks the field: the component is in `brickkit.yaml`, has a local source, the target is not `k8s` |
| `sync` | **ignores the focus**: it keeps the source of every component the project would run without a focus. A focus is a temporary narrowing, not a statement about which source you need; archiving forty directories each time you focus would be wrong. It errs toward keeping source, so it never archives something that is running |
| `graph` | reads `deploy.yaml` as today (never local mode), so no focus |
| `deps` | unaffected (it prints dependencies, not what runs) |
| `build` | unaffected by the field (a focused component runs from source and needs no image) |
| `local refresh` | the focus line is one of your local changes: the fresh copy has none, and the list shows `focus: erp/api (now unset)` |
| `local off` | commands read `deploy.yaml` again, which has no focus |

### 4.7 Errors

All reuse existing error codes; each gets a title, a reason and hints like any other error.

| Situation | Code |
| --- | --- |
| `focus:` names a component that isn't in `brickkit.yaml` (with "did you mean", §7) | `COMPONENT_NOT_FOUND` |
| The focused component has no local source to run from | `CONFIG_INVALID`, hint: `brickkit add <id> --repo` |
| `focus:` with `target: k8s` | `CONFIG_INVALID` (a cluster can't reach a process on your machine — as for `mode: local`) |
| `focus:` in `deploy.yaml` or another non-personal file | `CONFIG_INVALID` |
| The focused entry says `mode: disable` | `COMPONENT_DISABLED` |
| `--focus` and `--all` together | `INVALID_ARGUMENT` |

## 5. One `components/`: only the top project has one

**Rule:** a component's source exists in exactly one place, the top project's `components/`. A component
directory never contains another component's source.

**Prevention.**
- Commands run from a component's directory act on the top project (§3), so `add --repo` there clones into
  the top `components/`.
- In a workbench that itself sits inside another project's local source, `add --repo` is refused
  (`CONFIG_CONFLICT`): it would clone a second copy. The hint names the outer project and suggests running
  from there, or a focus run instead of the workbench.

**Detection.** `up`, `lint` and `sync` look inside every local component's source directory for another
component (a `components/<scope>/<name>/component.yaml` beneath it). If one exists, they fail
(`CONFIG_CONFLICT`) and list, for each copy: its path, whether the top project has the same ID, and whether
the copy's git repository has uncommitted or unpushed changes. The hints give the commands to move or delete
it; the CLI does neither. Only a directory with a `component.yaml` counts — the empty directories a git
submodule leaves behind (§6) are not a copy. This also fixes problem 4: the ignored copy is now reported.

## 6. Submodules in other people's components

BrickKit already never fetches a submodule (verified in code and by experiment):

| Path | How | A submodule becomes |
| --- | --- | --- |
| Reading `component.yaml` / `BRICKKIT.md` from a git source | bare cache, `git cat-file` of the tag | nothing — never downloaded |
| `add --repo` | `git clone` without `--recurse-submodules` | an empty directory |
| `brickkit build` of a git component | `git archive` of the tag | an empty directory |

Unchanged. Two messages are added:

- **`build`**: when the exported source has a `.gitmodules` with entries, a warning lists the empty
  directories and says BrickKit doesn't fetch submodules; if the build needs them, the component should
  publish an image (`deployment.image`) or build without them.
- **`add --repo`**: when the cloned repository has submodules, a note says they were not fetched and that
  `git submodule update --init` fetches them if you need them.

## 7. Completion and "did you mean"

### 7.1 Candidates the CLI provides

Cobra already generates `brickkit completion bash|zsh|fish|powershell`; the generated script calls back
into `brickkit` for candidates at each TAB. Candidates are added for:

| Where | Candidates |
| --- | --- |
| component argument of `remove`, `upgrade`, `deps`, `build`; `up --focus` | components in the project's `brickkit.yaml` (`id`, and `id@version` for non-default versions) |
| component argument of `add` | components the local install sources provide, plus components in the project's manifest cache; after `id@`, versions from the local source, the manifest cache, and the tags of the local bare-repository cache |
| `-f` / `--file` | `deploy*.yaml` at the project root |
| `lang set`, `skills update --lang` | registered languages |

Rules: completion reads only local files — no network, no market — so a TAB is instant and works offline; it
finds the project from a subdirectory (§3); outside a project it offers nothing and prints nothing.

### 7.2 "Did you mean"

Commands that take a component ID and don't find it (`remove`, `upgrade`, `deps`, `build`, `add`,
`--focus`) keep their error and code, and add a hint with at most three close IDs: IDs that contain what was
typed, share its name part (`api` → `erp/api`), or are within a small edit distance. Nothing runs on a guess.

### 7.3 Installing the completion

Completion is a feature of the shell: the shell must load a function registered for `brickkit`, and a
program can't change a shell that is already running. What can be automatic is putting the file where the
shell loads it from — as Homebrew and apt packages do. `install.sh` does that after installing the binary,
for each of bash, zsh and fish that exists on the machine (*decided here*: all present shells, not only the
login shell — the files are harmless):

| Shell | File | Loaded automatically when |
| --- | --- | --- |
| bash | `${XDG_DATA_HOME:-~/.local/share}/bash-completion/completions/brickkit` | the bash-completion package is installed (most Linux distributions; on macOS it must be installed) |
| zsh | `_brickkit` in the first writable of `$(brew --prefix)/share/zsh/site-functions`, `/usr/local/share/zsh/site-functions`; otherwise `~/.zsh/completions/_brickkit` | that directory is on `$fpath` and `compinit` runs (oh-my-zsh and most setups do) |
| fish | `${XDG_CONFIG_HOME:-~/.config}/fish/completions/brickkit.fish` | always |
| PowerShell | — | never: the script prints the line to add to `$PROFILE` |

- The script reports each file it wrote. When zsh's file had to go into `~/.zsh/completions`, it prints the
  two lines to add to `~/.zshrc`; it never edits `~/.zshrc` itself.
- `BRICKKIT_NO_COMPLETION=1` skips this step.
- It says that terminals already open need a new shell (or `exec $SHELL`).
- Re-running the script (an upgrade) rewrites the files.

## 8. Documentation

Every example and every output in the pages below comes from a real run (`BRICKKIT_LANG=en` for the English
pages). Both languages are written independently, not translated.

### 8.1 New pages and renumbering

**A. `02-project-guide/04-focus-run.md` — "Developing inside the project" / 「在项目里就地开发」.**
Inserted after `03-local-debug-workflow.md`: the reading order is "debug one component" then "work on
components inside the project". The eight pages after it move up one number, in both languages:

| Old | New |
| --- | --- |
| `04-multi-env-switch.md` | `05-multi-env-switch.md` |
| `05-up-and-down.md` | `06-up-and-down.md` |
| `06-upgrade-and-migration.md` | `07-upgrade-and-migration.md` |
| `07-remove-and-archive.md` | `08-remove-and-archive.md` |
| `08-status-and-graph.md` | `09-status-and-graph.md` |
| `09-lint-and-checks.md` | `10-lint-and-checks.md` |
| `10-sync-and-restore.md` | `11-sync-and-restore.md` |
| `11-build-and-images.md` | `12-build-and-images.md` |

Content: when to use a focus run and when a workbench; `up` from a component's directory; `--focus`, `--all`;
what starts and why (the reasons printed); how the focus sits in `deploy.local.yaml`; what `sync`, `status`,
`down`, `local refresh` do with it; moving versions forward across the project; the "one `components/`"
rule and what the nested-copy error means; submodules.

**B. `00-intro/03-shell-completion.md` — "Shell completion" / 「命令补全」.** Inserted after the quick start
(installing is where completion starts). The three pages after it move up one number:

| Old | New |
| --- | --- |
| `03-core-concepts.md` | `04-core-concepts.md` |
| `04-comparison.md` | `05-comparison.md` |
| `05-fractal-architecture.md` | `06-fractal-architecture.md` |

Content: what completion does and why the shell needs a file (in plain words); what `install.sh` already did
and how to check it; per shell — bash, zsh (including the `~/.zshrc` lines when needed), fish, PowerShell —
the manual steps for installs without `install.sh` (downloaded binary, `go install`); what gets completed;
"it doesn't complete" troubleshooting (new terminal, `compinit`, bash-completion missing, `$fpath`).

**Renumbering procedure** (from the repository's earlier renumbering, done by script, never by hand):

1. A map of old → new path for both languages; a check that no new name equals an old one (no chained
   renames).
2. Every Markdown link is resolved against the linking file's **old** location and rewritten relative to
   its **new** location, anchors kept; link *texts* that show a file name are updated too.
3. Whole-string replacement of paths in non-Markdown places: `llms.txt`, `llms.zh.txt`, code strings, tests,
   scripts, skill assets (none reference these files today — verified — but the script checks again).
4. `git mv`; then `git grep` for every old file name must return nothing outside `docs/superpowers` and
   `archive/`.
5. Index tables that carry the numbers are edited explicitly (the script asserts each edit hit):
   `02-project-guide/README.md` (its page table and the "life of a project" sentence),
   `00-intro/README.md`, `docs/en/README.md` / `docs/zh/README.md`, `llms.txt` / `llms.zh.txt` (entries in
   number order), and `AGENTS.md` / `AGENTS.zh.md` §9.
6. Sentences that state a count or an order ("eleven pages", "the last page") are searched for and fixed.
7. `make lint` (links, bilingual mirror, CLI docs, doc fields, doc tree) and `make test-all`.

### 8.2 Pages that change

| Page (both languages) | Change |
| --- | --- |
| `README.md`, `README.zh.md` | install section: completion is installed, link to B; "where next" rows if they list these pages |
| `00-intro/02-quick-start.md` | one line after installing: TAB completes component IDs, link to B |
| `01-three-layers/04-deploy-local-yaml.md` | the `focus:` field: what it means, that it's personal-only |
| `01-three-layers/09-field-reference.md`, `11-reference/03-deploy-yaml-schema.md` | the `focus` field row (the field-coverage guard requires it) |
| `02-project-guide/02-add-and-component-install.md` | `add --repo` from a component directory clones into the top `components/`; submodules are not fetched |
| `02-project-guide/03-local-debug-workflow.md` | a pointer to A for debugging inside a large project |
| `02-project-guide/11-sync-and-restore.md` (new number) | `sync` ignores the focus; the nested-copy check |
| `02-project-guide/12-build-and-images.md` (new number) | `build` without arguments in a component directory; the submodule warning |
| `03-component-guide/05-local-dev-fractal.md` | workbench or focus run — which to use when; the one-`components/` rule |
| `06-architecture/06-bare-repo-mechanism.md` | why submodules are never fetched, in each path |
| `06-architecture/09-error-codes.md` | the new titles from §4.7, §5 (the doc-field guard checks every title exists in source) |
| `07-cli-reference/README.md` | "Running from a subdirectory" (the §3 table); `up --focus` / `--all`; `build` and `deps` defaults; the `completion` command with the candidates of §7.1 (the CLI-docs guard requires every new flag) |
| `08-ai-guide/02-ai-dev-workflow.md` | how an assistant works in a large project: focus run, versions moved together |
| `10-troubleshooting/02-local-debug-issues.md` | focus errors and "why didn't X start" under a focus |
| `10-troubleshooting/05-build-issues.md` | empty submodule directories |
| `AGENTS.md`, `AGENTS.zh.md` | §3.2 (focus is personal-only; commands find the project upward), §3.3 (workbench vs focus), §5 (`--focus`, `--all`, running from a subdirectory) |
| `llms.txt`, `llms.zh.txt` | entries for A and B; renumbered entries |
| skill assets (en, zh) | `AGENTS.md`, `brickkit-assemble` (focus run, one `components/`), `brickkit-deploy` (the `focus:` field), `brickkit-troubleshoot` (focus errors, nested-copy error, submodule warning) |

Generated or guarded alongside: `schemas/deploy.schema.json` (`make generate-schemas`), the message
catalogs (every new message in both languages), `tests/checklist` rows for the new boundary and error
cases. Projects that already have the skills installed see them as out of date; `brickkit skills update`
refreshes them.

## 9. Testing

- Project discovery: nearest `brickkit.yaml` wins; a workbench stops the walk; crossing a nested `.git`;
  no project → `PROJECT_MISSING`; the "Project:" line appears exactly when found above.
- Focus: setting from a component directory and with `--focus`; `--all`; the file keeps every other byte
  (comments, personal changes); local mode turned on with the copy rules of `local on`; the start set of
  §4.4 including pins and optional dependencies; `mode: debug` on the focused entry; each error of §4.7;
  `sync` unaffected; `local refresh` lists the focus; `graph` unaffected; compose output keeps volumes.
- One `components/`: `add --repo` from a component directory lands in the top `components/`; refused in a
  nested workbench; a nested copy detected with dirty/unpushed status; an empty submodule directory is not a
  copy.
- Submodules: the build warning and the `add --repo` note, with a real repository that has a submodule.
- Completion: candidates for each row of §7.1 through cobra's `__complete`; offline (no network calls);
  from a subdirectory; silent outside a project; "did you mean" suggestions.
- `install.sh`: extended in `scripts/check-install-sh.sh` with a fake `$HOME`: files written per present
  shell, the zsh fallback prints the `.zshrc` lines and doesn't touch `.zshrc`, `BRICKKIT_NO_COMPLETION=1`.
- The whole suite, `make lint` and `make test-all` stay green; every doc example comes from a real run.
