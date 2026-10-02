# Removing and archiving

## What `brickkit remove` checks first

**With several versions, you must name the one to remove:**

```text
❌ Error: demo/hello has several versions (1.1.0, 1.0.0); name the one to remove
   Suggestion: For example brickkit remove demo/hello@1.1.0
```

**While another component still requires it, it isn't removed:**

```text
❌ Error: demo/hello@1.0.0 is still required by: demo/caller@1.0.0
   Suggestion: Remove (or upgrade) the components that depend on it first, then remove it
```

Remove it, and the component depending on it can't start — better to stop now than to find out at the next `up`.

**When only optional dependencies point at it, it goes, and you're told the effect:**

```bash
brickkit remove demo/bus
```

```text
➖ Removed demo/bus@1.0.0
ℹ️  demo/caller@1.0.0 optionally depends on demo/bus@1.0.0: it keeps running, without the address of demo/bus@1.0.0
🗄️  Config archived: config/demo-bus.yaml → config/.archive/demo-bus@1.0.0.yaml
📝 Written: brickkit.yaml, deploy.yaml, deploy.local.yaml
```

## What removing does

- the declaration in `brickkit.yaml` goes;
- its deploy entries go (`deploy.yaml`, and `deploy.local.yaml` when it exists; other deploy files used with `-f` are
  named in the output for you to bring along);
- its config is **not** deleted but moved to `config/.archive/<component>@<version>.yaml`;
- compatibility versions kept only for it (those whose `requiredBy` names only it) go with it;
- when it's a shell, the members it hosted move back to the top level of the deploy file and run on their own.

**Its data is not touched.** `remove` changes the three layers and nothing else: the component's database schema, its
buckets, its queues, whatever it stored, all stay exactly where they are — the platform doesn't know where they are (a
database address is only a string in `config/`). How long that data is kept and when it is destroyed is the project's
decision; the component's `BRICKKIT.md` says what it stores (see "Before you deploy" there).

**A version is promoted to default.** When the default version is removed and one version of the component is left,
that one becomes the new default, and its config file goes back to the name without a version. When several versions
would be left, `remove` can't decide which one becomes the default: it stops and asks you to remove the others first,
or to make one of them the default with `brickkit upgrade`. With exactly one left:

```bash
brickkit remove demo/hello@1.1.0
```

```text
➖ Removed demo/hello@1.1.0
   ⭐ demo/hello@1.0.0 is now the default version
🗄️  Config archived: config/demo-hello.yaml → config/.archive/demo-hello@1.1.0.yaml
📝 Written: brickkit.yaml, deploy.yaml, deploy.local.yaml
```

## Archiving and restoring

`config/.archive/` is this machine's undo button (not in Git). When you later `add` the same component again, its config
comes back from the archive instead of a blank skeleton:

```bash
brickkit add demo/bus
```

```text
🔎 No version given for demo/bus; the latest is 1.0.0 (install source company-git)
➕ Adding demo/bus@1.0.0
   ✅ demo/bus@1.0.0
📝 Written: brickkit.yaml, deploy.yaml, deploy.local.yaml
♻️  config/demo-bus.yaml restored from the archive (config/.archive/demo-bus@1.0.0.yaml, migrated to this version)
📝 config/demo-bus.yaml
   Kept as written: GREETING
📦 Artifacts: 1 file, in .brickkit/artifacts/
```

Restoring uses the same [migration rules](07-upgrade-and-migration.md#how-config-is-migrated) as an upgrade rather than a
plain copy: what you add back may be another version, and items the new version dropped, added or gave a new default are
handled by that table. When the archive holds several versions, the one for the same version is taken; failing that, the
highest one not above this version; failing that, the highest one.

## The source directory

When a component's **last version** is removed, its source directory under `components/` (and any archived copy under
`components/.archived/`) is deleted too — otherwise it would become a directory nobody claims and `sync` no longer
manages.

But first it checks that what's deleted could be recovered:

```text
❌ Error: the source directory could not be recovered once deleted, so nothing was removed
   Component: demo/hello
   Directory: components/demo/hello/
   Reason: It has uncommitted changes (untracked files included)
   Suggestions:
   1. Save it first: commit and push to a remote, or move the directory elsewhere
   2. If you are sure you do not need it: brickkit remove demo/hello --force
```

Uncommitted changes, commits not pushed to a remote, or a directory that isn't a Git repository at all (its files
have no other copy) stop it. A directory registered as a git submodule is never deleted, not even with `--force`:
deleting the directory would leave `.gitmodules` and the index out of step, so the error lists the git commands
(`git submodule deinit`, `git rm`) to remove it properly yourself. Otherwise, if you're sure you don't need them, add
`--force`:

```text
➖ Removed demo/hello@1.0.0
🗄️  Config archived: config/demo-hello.yaml → config/.archive/demo-hello@1.0.0.yaml
📝 Written: brickkit.yaml, deploy.yaml, deploy.local.yaml
🗑️  Source directory deleted: components/demo/hello/
```

`remove` means "remove completely": the declaration, the deploy entries and the source all go; only the config stays in
the archive, because it may hold values you spent time tuning.
