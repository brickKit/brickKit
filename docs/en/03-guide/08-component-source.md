# 8. Manage Component Source

Until now, `components/` has been either empty or a folder of Manifests copied in from `tests/components/`. One obvious question has gone unanswered: **you want to read another team's component source — maybe change it. What then?** This walks through the whole life of component source: cloning it, keeping only what you're working on, pushing your changes back, deleting it cleanly, and keeping things consistent when the source is committed along with the project. Everything below ran against the real CLI, using the same two tiny fixtures ([`demo/hello`](../../../tests/components/demo-hello/) and [`demo/caller`](../../../tests/components/demo-caller/)) — this time turned into two Git repositories.

**Prerequisites:** the BrickKit CLI built, and `git` installed. **No Docker needed** — nothing here starts a container, only `up --dry-run` to see the decision. Also, do this in an empty directory **outside** the BrickKit repository: later on the project gets its own `git init`, and nesting it inside another repo would tangle the two Git states together.

**One thing to get straight first: `up` needs a component's Manifest, never its code.** For a component from the marketplace or a Git source, the Manifest comes from the cache in `.brickkit/manifests/`, and `up` doesn't care whether the source is in `components/` at all; `components/` only serves development — reading source, changing it, debugging in an IDE ([article 3](03-local-debugging.md)). There's one exception, and we'll run into it below: a component provided by a **local** install source (`init`'s default `local-dev` points at exactly `components/`) skips the cache, and its `component.yaml` is re-read from that directory every time (AGENTS.md §2.3). So most operations here don't affect whether the project runs, only whether you have the source at hand — and we'll see the exception with our own eyes once the clone is done.

## Setup: two "remote" repositories

`--repo` clones a component's own Git repository, so there has to be one to clone. In real use it's a repository on GitHub, or the `gitUrl` the component registered when it was published to a marketplace. With no network and no marketplace here, local directories stand in for the remotes: each fixture becomes a **bare repository** (history only, no working tree — pushing to it is unrestricted), plus one empty one to play the fork you'd make on GitHub. None of this is BrickKit; it's just building the stage:

```bash
export BRICKKIT_REPO=~/brickKit      # wherever you cloned this repository
mkdir workspace-lab && cd workspace-lab
mkdir remotes && cd remotes
for name in hello caller; do
  cp -r $BRICKKIT_REPO/tests/components/demo-$name $name
  git -C $name init -q -b main
  git -C $name add .
  git -C $name commit -q -m "demo/$name 1.0.0"
  git clone -q --bare $name $name.git
  rm -rf $name
done
git init -q --bare -b main hello-fork.git
cd ..
```

(If Git complains it doesn't know who you are, set `git config --global user.name` and `user.email` first.) `demo/caller` strongly depends on `demo/hello` — you met that edge in article 2.

Now create the project and configure the two repositories as `git`-type install sources:

```bash
mkdir workspace-demo && cd workspace-demo
brickkit init workspace-demo
```

The output is the same as in article 1, so it's skipped here. Edit `brickkit.yaml` so `sources:` reads as follows — the two `git` sources go **after** the `local-dev` that `init` generated, and that order matters later:

```yaml
sources:
  - id: local-dev
    type: local
    path: ./components
  - id: hello-remote
    type: git
    url: ../remotes/hello.git
  - id: caller-remote
    type: git
    url: ../remotes/caller.git
```

A `git`-type source is "a Git repository whose root holds a `component.yaml`" (a repository can also hold several components laid out as `<scope>/<name>/component.yaml`) — exactly the "one component, one repository" shape (AGENTS.md §9.16). When installing, the CLI asks the sources one by one in declared order, and the first that has it wins.

## `add` doesn't clone source by default

```bash
brickkit add demo/caller@1.0.0
```

```
📦 Adding demo/caller@1.0.0
   ├── Manifest ✅
   ├── dependency demo/hello@1.0.0 ✅ pulled (artifacts: 1 file)
   └── artifacts ✅ (1 file)
⚠️ Warning: optional dependency missing: demo/bus@1.0.0
   Affected component: demo/caller@1.0.0
   Reason: The component was not found in any install source
   Impact: This component's environment variable DEMO_BUS_ENDPOINT will not be injected
   💡 Degrading gracefully for a missing optional dependency is the component's own responsibility; to enable it, confirm it has been published and is available from an install source
✅ Written to brickkit.yaml (2 components)
📁 Downloaded artifacts into .brickkit/artifacts/ (2 files)
```

(That weak-dependency warning is from article 2: `demo/caller` declares an optional dependency that isn't installed here.)

Both components are in, and the dependency was pulled in automatically — yet `components/` is empty:

```bash
ls components              # nothing at all (just one hidden, empty directory, .archived/)
ls .brickkit/manifests
```

```
demo-caller-1.0.0.sig.json
demo-caller-1.0.0.yaml
demo-hello-1.0.0.sig.json
demo-hello-1.0.0.yaml
```

`add` pulls only the Manifest and artifacts — which is all `up` needs. Source is a **development-time** need, not an **install-time** one (AGENTS.md §9.18): one `add` can pull five or six components down the dependency tree, and cloning all their repositories would be slow and take a lot of disk, when most of the time you just want to *use* them.

## `--repo`: clone one component's full repository

Now you want to read `demo/hello`'s source:

```bash
brickkit add demo/hello@1.0.0 --repo --yes
```

```
ℹ️ demo/hello@1.0.0 already exists in brickkit.yaml, and --yes was given: refreshing the cache directly
📦 Adding demo/hello@1.0.0
   ├── Manifest ✅
   └── artifacts ✅ (1 file)
✅ Refreshed the Manifest and artifacts cache of demo/hello@1.0.0
📁 Downloaded artifacts into .brickkit/artifacts/ (1 file)
📁 Cloned the source into components/demo/hello/
💡 For how to push source changes back and how to manage this source afterwards, see docs/en/03-guide/08-component-source.md (swap en for zh for the Chinese version)
```

The `--yes` is there because `demo/hello` is already in `brickkit.yaml`: when `add` meets a component that's already been added, it first asks "refresh the Manifest and artifacts cache? [y/N]", and `--yes` answers y for you. Where there's no terminal to answer (a script, CI), leaving `--yes` off is taken as N — nothing happens, and nothing is cloned. **A component that isn't in `brickkit.yaml` yet gets no such question**, so `brickkit add demo/hello@1.0.0 --repo` alone is enough.

What you get is a **complete** Git repository:

```bash
ls -A components/demo/hello
```

```
.dockerignore
.git
Dockerfile
component.yaml
go.mod
main.go
main_test.go
openapi.json
```

There's a `.git`, all the history, no `--depth` truncation — because you need to be able to commit, branch and push in there. BrickKit does the clone and nothing more; how you use Git afterwards (permissions, forks, branching policy) is your business: people are on GitHub, GitLab, Gitee or a self-hosted Gitea, and the CLI can't support every one's fork API (AGENTS.md §9.19).

`--repo` has two preconditions: **the component must have a Git address** (from a `git`-type source, or an open-source component whose repository the marketplace has on record; a closed-source component has no repository and is refused outright, with a note that it installs and runs fine without `--repo`), **and its source must not already be on disk**. The second one is easy to see — run it on the same component again:

```bash
brickkit add demo/hello@1.0.0 --repo --yes
```

```
ℹ️ demo/hello@1.0.0 already exists in brickkit.yaml, and --yes was given: refreshing the cache directly
❌ Clone failed: directory already exists
   Component: demo/hello@1.0.0
   Directory: components/demo/hello/
   Reason: This directory already exists and may hold source you're actively working on
   Suggestions:
   1. If this happened by mistake, delete or rename the directory first
   2. If the source is already there, there's no need to clone it again
```

It **never overwrites**: a directory that already exists is most likely source you're working on, and the platform doesn't decide for you whether to delete it. A component ID has exactly one source directory under `components/`, shared by every version of that component.

One more thing worth knowing: once cloned, the component is claimed by `local-dev`, the first source in the list — `local-dev` scans `components/`, and the source is now sitting there. That isn't a problem; it's the effect you want: from this moment `components/demo/hello/` is your working copy, and its `component.yaml` no longer comes from the cache but is re-read from here every time. See it for yourself: change `deployment.port` in `components/demo/hello/component.yaml` from 8080 to 9090, then look at the dependency address `up --dry-run` generates:

```bash
brickkit up --dry-run > /dev/null
grep -m1 DEMO_HELLO_ENDPOINT .brickkit/generated/docker-compose.yaml
```

```
      - DEMO_HELLO_ENDPOINT=http://demo-hello-1-0-0:9090
```

The change took effect on the next `up`, with no need to `add` again. (When you're done, put the port back to 8080 — the component's code still listens on 8080.)

## `--repo-all`: the whole dependency tree

To get the source of `demo/caller` and every open-source component it depends on:

```bash
brickkit add demo/caller@1.0.0 --repo-all --yes
```

```
ℹ️ demo/caller@1.0.0 already exists in brickkit.yaml, and --yes was given: refreshing the cache directly
📦 Adding demo/caller@1.0.0
   ├── Manifest ✅
   ├── dependency demo/hello@1.0.0 ✅ pulled (artifacts: 1 file)
   └── artifacts ✅ (1 file)
⚠️ Warning: optional dependency missing: demo/bus@1.0.0
   Affected component: demo/caller@1.0.0
   Reason: The component was not found in any install source
   Impact: This component's environment variable DEMO_BUS_ENDPOINT will not be injected
   💡 Degrading gracefully for a missing optional dependency is the component's own responsibility; to enable it, confirm it has been published and is available from an install source
✅ Refreshed the Manifest and artifacts cache of demo/caller@1.0.0
📁 Downloaded artifacts into .brickkit/artifacts/ (2 files)
   ⏭️ demo/hello             → the source directory already exists; skipping clone
   ✅ demo/caller            → clone finished (components/demo/caller/)
📁 Cloned 1 open-source component repository (1 skipped, see the reasons above)
```

The last three lines are the point. `--repo-all` is a batch operation: for each component in the tree it clones what it can and **skips the rest, stating the reason for each one**, rather than failing the whole batch over a single component — so `demo/hello` is skipped (you just cloned it) and `demo/caller` is cloned. There are three possible reasons, and each ⏭️ line says which: the component is closed-source (no repository), its source has no Git address on record, or its source is already on disk. The summary line doesn't assert one particular reason precisely because the lines above it have already said each one.

## Changed the source — how to push it back

`demo/hello` is now an ordinary Git repository, and changing it is your usual Git workflow. Which path you take depends on whether you have push access to the original repository.

**You have push access** (say, a company-internal repository): branch, change, commit, push:

```bash
git -C components/demo/hello checkout -b feature/greeting
echo '// a different greeting' >> components/demo/hello/main.go
git -C components/demo/hello commit -am "Adjust the greeting"
git -C components/demo/hello push origin feature/greeting
```

**You don't** (say, a public open-source repository): fork it first on GitHub / GitLab (a click in the browser — the empty `hello-fork.git` plays that role here), then add your fork as a **second** remote and push to that. The original `origin` stays, so you can still pull upstream updates:

```bash
git -C components/demo/hello remote add myfork "$PWD/../remotes/hello-fork.git"
git -C components/demo/hello push myfork feature/greeting
```

(If you'd rather move over to your fork entirely and stop caring about upstream, replace `origin` instead: `git remote set-url origin <your fork's address>`.)

To get your change into use — for other people, or for your other projects — is a matter of publishing a new version: bump the version in `component.yaml`, build a new image, `brickkit publish` ([article 9](09-marketplace.md)); how projects already on the old version upgrade is [article 5](05-upgrades-and-versions.md).

## Keep only what you're working on: `enabled` and `sync`

`components/` only grows. In a 50-component project, the ones you're actually looking at right now might be two or three. `brickkit sync` keeps `components/` to just those: it moves the source of components that **won't start** this time into `components/.archived/`, and moves back what's needed. The "who starts this time" decision is exactly the one `up` makes, so you steer it the same way — by changing `enabled` ([article 2](02-what-runs.md)).

Turn `demo/caller` off by giving it an `enabled: false` line:

```yaml
components:
  - id: demo/hello
    version: 1.0.0
  - id: demo/caller
    version: 1.0.0
    enabled: false
```

```bash
brickkit up --dry-run
```

Only the decision part is shown:

```
📋 Component state calculation:
   ⬜ demo/hello@1.0.0   not starting (nothing above it is starting)
   ⬜ demo/caller@1.0.0  disabled explicitly (enabled: false)
```

`demo/hello` isn't starting either — only `demo/caller` needs it, and it "follows the top". Now let `sync` put both of their sources away:

```bash
brickkit sync
```

```
📂 Workspace tidying:
   📦 components/demo/caller/              → components/.archived/demo/caller
      Reason: disabled explicitly (enabled: false)
   📦 components/demo/hello/               → components/.archived/demo/hello
      Reason: not starting (nothing above it is starting)
✅ Workspace tidied (0 active, 2 archived, 0 activated)
```

```bash
ls components                  # empty: .archived starts with a dot, so it's hidden by default
ls -A components
ls components/.archived/demo
```

```
.archived
caller
hello
```

A few things worth knowing:

- **The whole directory moves, `.git` included.** An archived component still works with Git (`git -C components/.archived/demo/hello log`), and IDEs open it as usual; `up` still reads its `component.yaml` too. Archiving only changes whether you can *see* it, not whether it can be *found*.
- **`sync` never touches running containers and never changes what `up` starts** — it only moves directories.
- **There's no `--dry-run`.** If it moved the wrong thing, change `enabled` back and run it again; that swaps them back.
- **It's a separate command, deliberately not folded into `up`.** `up` manages runtime, `sync` manages the source directory; if `up` moved files as a side effect, you'd wonder why your files had moved on their own (AGENTS.md §9.17).
- The three numbers in the parentheses mean, in order: already in place, put away this time, brought back this time.

Now say you only want to read `demo/hello`'s source. Pin it with `enabled: true` — "run this regardless of what's above it":

```yaml
components:
  - id: demo/hello
    version: 1.0.0
    enabled: true
  - id: demo/caller
    version: 1.0.0
    enabled: false
```

```bash
brickkit up --dry-run
```

```
📋 Component state calculation:
   ✅ demo/hello@1.0.0   starting (enabled: true)
   ⬜ demo/caller@1.0.0  disabled explicitly (enabled: false)
```

```bash
brickkit sync
```

```
📂 Workspace tidying:
   📂 components/.archived/demo/hello      → components/demo/hello/
      Reason: re-enabled
✅ Workspace tidied (0 active, 0 archived, 1 activated)
```

`demo/hello` came back; `demo/caller` stays archived. When you want everything back, delete both `enabled` lines (back to "unwritten", i.e. follow the top) and run it once more:

```bash
brickkit sync
```

```
📂 Workspace tidying:
   📂 components/.archived/demo/caller     → components/demo/caller/
      Reason: re-enabled
   ✅ components/demo/hello/               active
✅ Workspace tidied (1 active, 0 archived, 1 activated)
```

## `remove`: clean removal, and no accidents

`brickkit remove` means "remove completely": drop it from `brickkit.yaml`, clear the caches, and **delete the source directory too** (AGENTS.md §9.20). Leaving source on disk that nothing claims is worse than deleting it cleanly. But deleting source can't be undone, so before it acts there are two checks to pass.

**First: does anything still depend on it?** `demo/caller` strongly depends on `demo/hello`, so remove `demo/hello` directly:

```bash
brickkit remove demo/hello
```

```
❌ Cannot remove demo/hello
   Version: 1.0.0
   These components depend on it as a required dependency: demo/caller@1.0.0
   Suggestion: Remove the dependents first
```

**Second: once the source is deleted, can it still be found?** It asks only one thing — do these bytes exist anywhere else? Three situations stop it: not a Git repository (no other copy), uncommitted changes, or commits not pushed to any remote. Change something in `demo/caller`'s source without committing, then try to remove it:

```bash
echo '// I am editing this' >> components/demo/caller/main.go
brickkit remove demo/caller
```

```
❌ Error: the source can't be recovered once it is deleted
   Component: demo/caller@1.0.0
   Directory: components/demo/caller/
   Reason: It has uncommitted changes (untracked files included)
   Suggestions:
   1. First keep it safe: commit and push it to a remote, or copy the directory away / rename it
   2. If you are sure you don't need it, add --force: brickkit remove demo/caller --force
   3. If you only don't need it for now, write enabled: false for it and run brickkit sync — that moves the source into the archive directory instead of deleting it
```

Committed, but not pushed:

```bash
git -C components/demo/caller commit -am "wip: adjust caller"
brickkit remove demo/caller
```

```
❌ Error: the source can't be recovered once it is deleted
   Component: demo/caller@1.0.0
   Directory: components/demo/caller/
   Reason: It has commits that haven't been pushed to any remote
   ...
```

(The suggestions after that are the same as above.) Push it, then remove:

```bash
git -C components/demo/caller push origin main
brickkit remove demo/caller
```

```
✅ Removed demo/caller@1.0.0
   🗑️ Deleted source directory components/demo/caller/
   🗑️ Cleaned the Manifest cache
   🗑️ Cleaned the artifacts cache
```

Nothing stopped it this time — a clean clone with everything pushed loses nothing when deleted. The three ways out listed in that error are the point of this section: **save it** (commit and push), **confirm you don't want it** (`--force`), or **you just don't need it right now** — in which case don't delete it; `enabled: false` plus `sync` moves the source into the archive and loses nothing.

The archived copy is deleted too. Give `demo/hello` an `enabled: false`, run `brickkit sync` to put it in the archive (the output is the same as before, so it's skipped), then remove it:

```bash
brickkit remove demo/hello
```

```
✅ Removed demo/hello@1.0.0
   🗑️ Deleted archived source directory components/.archived/demo/hello
   🗑️ Cleaned the Manifest cache
   🗑️ Cleaned the artifacts cache
```

`remove` has to clear the archive as well: the component is no longer in `brickkit.yaml`, so `sync` would no longer recognize it, and that archived source would be an orphan nobody ever reclaims.

One last kind of stop: if the source is a **git submodule** registered in the project repository (a common way for teams to hang component source off a project repo), `remove` also refuses to delete it directly — removing only the working directory would leave `.gitmodules`, the index entry and the data under `.git/modules/` dangling, and Git would keep referring to something that no longer exists. It doesn't do that for you; it tells you the equivalent manual steps, verbatim:

```bash
brickkit remove demo/hello
```

```
❌ Error: can't remove this component's source — it's a registered git submodule
   Component: demo/hello
   Path: components/demo/hello/
   Reason: Deleting the working directory directly doesn't clean up .gitmodules, the gitlink entry in the superproject index, or the internal repo data under .git/modules/ — git's state would end up referencing something that no longer exists
   Suggestions:
   1. Run by hand: git submodule deinit -f -- components/demo/hello/
   2. Then run: git rm -f components/demo/hello/
   3. For a full cleanup: rm -rf .git/modules/components/demo/hello/
```

This one is **not affected by `--force`**: `--force` is the way out of "will data be lost" risks, while this stop is about "will Git's books be corrupted", and forcing past it would only make the books worse.

## Source committed with the project: `restore` and the commit hook

So far `workspace-demo` has been the "one repository per component" shape: the rule `init` appended to `.gitignore` ignores `components/` entirely, the project repository holds only `brickkit.yaml`, and the source lives in its own repositories. Some teams want the other shape: **component source committed along with the project**, code and config in one commit. Then `sync`'s directory moves show up right in `git status` (a wall of `D` and `??`), and a mistake becomes easy to make again and again: you turn a few components off locally, run `sync`, and commit only the source — the `enabled` change in `brickkit.yaml` doesn't come along. The repository is now contradictory: config says the component should run, its source sits in the archive directory, and a teammate who pulls it is lost. The commit hook and `restore` exist to close exactly that hole.

Start another project. This time `git init` **first**, then `brickkit init`:

```bash
cd ..
mkdir shared-src && cd shared-src
git init -q -b main
brickkit init shared-src
```

```
✅ Project initialized: shared-src
   📁 brickkit.yaml        Project config
   📁 components/          Component source (configured as the local install source local-dev)
   📁 .brickkit/           CLI working directory
   📁 .claude/skills/      AI assistant skills (4)
   📁 AGENTS.md            AI assistant project guide
   🪝 .git/hooks/pre-commit Check the component layout before committing
...
```

The extra 🪝 line is the new thing: `init` noticed the project root **is** the repository root and installed a pre-commit hook. The order helps here — with `git init` before `brickkit init`, the hook is installed automatically; the other way round works too, install it afterwards with `brickkit init --hooks`. A project nested inside someone else's repository has to use `--hooks` anyway: `init` won't write into someone else's repository on its own.

To do it the "source travels with the project" way, delete these two lines from `.gitignore`:

```
# Component source directory (each component is its own Git repository, not committed to the project repository)
components/
```

Then put the two fixtures' source into `components/` — as plain files this time, not clones — add them to the project, and commit:

```bash
mkdir -p components/demo
cp -r $BRICKKIT_REPO/tests/components/demo-hello components/demo/hello
cp -r $BRICKKIT_REPO/tests/components/demo-caller components/demo/caller
brickkit add --local
git add -A
git commit -m "Initial: hello and caller"
```

The hook ran on that commit too, but everything was consistent, so it said nothing.

**Now make the mistake.** You want to concentrate on `demo/hello`: turn `demo/caller` off, pin `demo/hello`, and run `sync`:

```yaml
components:
  - id: demo/hello
    version: 1.0.0
    enabled: true
  - id: demo/caller
    version: 1.0.0
    enabled: false
```

```bash
brickkit sync
echo '// adjust the greeting in hello' >> components/demo/hello/main.go
git add components/          # you only meant to commit the source — and swept up sync's moves with it
git commit -m "Adjust hello's greeting"
```

```
❌ Commit blocked: component source is committed under the archive directory, but brickkit.yaml says it should start
   demo/caller: Location about to be committed: components/.archived/demo/caller
   Suggestions:
   1. To keep this archived layout → git add brickkit.yaml (the enabled: false in the yaml going into the commit is your declaration of intent)
   2. Don't want that → git reset components/ && brickkit restore, then git add again
```

The hook looks at **what's about to be committed** (the staging area), not your working tree: in the staging area `brickkit.yaml` hasn't changed (no `enabled: false`, so "it should run"), yet `demo/caller`'s source is in the archive directory — a contradiction, so it stops. It offers two ways out:

**Way out one: this is what you wanted.** `git add brickkit.yaml`, so `enabled: false` and the archived layout go into the commit together. The two agree, and the hook lets it through — you narrowed the scope on purpose, and the platform has no standing to change your mind.

**Way out two: it was a slip.** Unstage, then `brickkit restore`:

```bash
git reset -q components/
brickkit restore
```

```
📄 brickkit.yaml: enabled restored from the last commit (other changes untouched)
   demo/hello@1.0.0           enabled: true → remove the field (the commit doesn't set it)
   demo/caller@1.0.0          enabled: false → remove the field (the commit doesn't set it)
📂 Workspace tidying:
   📂 components/.archived/demo/caller     → components/demo/caller/
      Reason: re-enabled
   ✅ components/demo/hello/               active
✅ Workspace tidied (1 active, 0 archived, 1 activated)
```

`restore` puts each component's `enabled` back to its value in the last commit (if the commit didn't write one, the field is removed altogether), then lets the source layout follow, by the same rule `sync` uses. Remember its boundaries:

- **It touches only the `enabled` field.** The old values it's about to overwrite are printed first (the two lines right after the heading above).
- Entries you've just `add`ed, or whose version you've changed, are left **untouched**.
- Anything in the commit but not in your working tree is **never added back** — it isn't `git revert`.

Your changes to `demo/hello`'s source are intact, right there in `components/demo/hello/main.go`. `git add components/` again, and this time the hook lets it through:

```bash
git add components/
git commit -m "Adjust hello's greeting"
```

`brickkit restore --check` is the very command the hook calls: it only checks and changes nothing, and exits non-zero if things aren't consistent — so you can wire it into CI too, as a second gate before merging.

The hook blocks only **one direction**: source in the archive directory while the yaml says it should run. The opposite — the yaml says don't run, yet the source is still in the active directory — merely means "`sync` hasn't been run", and `sync` is optional by design; blocking it would force everyone to run `sync`. To uninstall the hook, delete `.git/hooks/pre-commit`; to refresh it after upgrading the CLI (it has the `brickkit` binary's path and version written into it), run `brickkit init --hooks` again.

## Common snags

| What you see | What's going on | What to do |
| --- | --- | --- |
| `add … --repo` prints "Cancelled; brickkit.yaml was not modified" and clones nothing | The component is already in `brickkit.yaml`; `add` asked "refresh the cache?" and, with no terminal, took it as N | Add `--yes` |
| `--repo` says "Clone failed: directory already exists" | `components/<scope>/<name>/` already holds source: cloned earlier, or hand-written by you | Use it. If you really want to clone again, move that directory away first |
| `--repo` says "Clone failed: the source is already there, just archived" | `sync` put the source into `.archived/` | Bring it back: change `enabled`, then `brickkit sync` |
| `--repo` says "Clone failed: this component is closed-source" | A closed-source component has no Git repository | Drop `--repo`; a plain `add` works |
| A wall of `D` and `??` in `git status` after `sync` | When `components/` is tracked by the project repository, archiving is a directory move and shows up in the diff | Expected. Commit `enabled` and the moves together, or undo with `brickkit restore` |
| The hook blocks a commit | Archived source went into the commit while `brickkit.yaml` says it should run | Follow the message: `git add brickkit.yaml`, or `git reset components/` then `brickkit restore` |
| `remove` says "the source can't be recovered once it is deleted" | The source isn't a Git repository, has uncommitted changes, or has commits not pushed to any remote | Commit and push; or copy it away; or `--force`; or, if you just don't need it for now, use `enabled: false` plus `sync` instead |
| `remove` says "it's a registered git submodule" | Deleting directly would leave `.gitmodules` and the index dangling | Run the git commands it lists by hand; `--force` doesn't apply |
| Want an archived component back | | Change `enabled`, then `brickkit sync`; or just work in `components/.archived/<scope>/<name>/` — Git commands and IDEs work as usual |

## At a glance

| You want to | Use |
| --- | --- |
| Read or change a component's source | `brickkit add <component> --repo` |
| Get the source of a whole dependency tree | `brickkit add <component> --repo-all` |
| Keep only what you're working on in `components/` | Change `enabled`, then `brickkit sync` |
| Get rid of a component for good | `brickkit remove <component>` |
| Undo a narrowing you haven't committed | `brickkit restore` |
| Check the structure is consistent before committing | `brickkit restore --check` (the hook calls it) |

---

Next: [Publish and install from a marketplace](09-marketplace.md) — publishing a component to a marketplace, and installing it from there instead of a local source.
