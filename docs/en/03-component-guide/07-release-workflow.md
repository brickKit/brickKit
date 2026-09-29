# Releasing

On a Git source, **releasing a version is pushing a tag to the component's own repository**. Users' CLIs find versions by
tag: tag `0.1.0` is version 0.1.0. `brickkit release` does this for you, and before acting it stops every situation that
would go wrong.

(Publishing to a component market is another command, `brickkit publish`; see the
[CLI reference](../07-cli-reference/README.md#brickkit-publish).)

## The flow

```bash
cd demo-quote
brickkit release
```

```text
✅ Released demo/quote@0.1.0: tag 0.1.0 pushed
```

What it did:

1. **Read the version.** It takes `metadata.version` from `component.yaml` — the version has exactly one source.
2. **Validate `component.yaml`.** It has to parse and its fields have to be valid before there's anything to release.
3. **Check the working tree.** The component directory must have no uncommitted changes.
4. **Check the branch.** The current branch must have an upstream and no unpushed commits: the commit the tag points at must
   already be in the remote's history.
5. **Check the tag.** It must not exist yet, locally or on the remote.
6. **Tag and push.**

The first five steps are read-only checks; nothing is written until all of them pass.

## What gets stopped

**Uncommitted changes:** a tag holds a commit, not your working tree — changes you haven't committed wouldn't be released.

```text
❌ Error: demo/quote@0.1.0 has uncommitted changes
   Directory: .
   Changed: M main.go
   Suggestion: Commit (or discard) them first — a tag holds a commit, never your working tree
```

**A branch that was never pushed:** users take the tag from the remote; for a commit that exists only on your machine,
the tag they get would point at something they can't fetch.

```text
❌ Error: the branch demo/quote@0.1.0 is released from has no upstream
   Branch: main
   Suggestion: Push the branch and set its upstream: git push -u origin main
```

**This version was already released:**

```text
❌ Error: demo/quote@0.1.0 is already released (the tag 0.1.0 is on the current commit)
   Suggestion: A released version never changes: raise metadata.version in component.yaml, commit, push, then release
```

A released version never changes: someone may already have installed 0.1.0, and with one version number pointing at two
different pieces of code, nobody can tell which one anyone runs. Changed something? Raise the version.

## All of it, or no trace

If the tag was created but the push fails (the network dropped, no push permission), `release` deletes the tag it just
created locally. Otherwise the next attempt would be stopped by "the tag already exists", for a tag that never reached
the remote.

## Releasing a new version

```bash
# 1. change component.yaml: version: 0.1.0 → 0.2.0 (and the contract's info.version with it)
# 2. commit and push
git commit -am "demo/quote 0.2.0: …"
git push
# 3. release
brickkit release
```

Which number to raise: only the internals changed and the interface didn't — the patch (0.1.0 → 0.1.1); endpoints or
config items were added and old callers still work — the minor; endpoints were removed or an existing field's meaning
changed — the major, and users have to change their code when they upgrade.

## Many at once: `--local`

When a project holds several components you wrote yourselves (all under the local source `components/`, each its own
repository):

```bash
brickkit release --local
```

It checks every one of them first, and only when all pass does it tag and push them one by one; ones already released
(the tag is on the current commit) are skipped; the first failed push stops it — those released before it stay, the rest
aren't attempted.

## A component in a repository subdirectory

When several components live in subdirectories of one repository (a monorepo), point `--path` at the component directory.
Tags then carry a namespace, one per component:

```bash
brickkit release --path svc/api        # component erp/api, tag erp-api/1.2.0
```

The working-tree check looks only at that component's own directory: changes in a neighbouring component don't concern it.

## The workbench has nothing to do with releasing

The `brickkit.yaml`, deploy files and `config/` in a component repository (your local workbench; see
[Developing inside a component](05-local-dev-fractal.md)) take no part in releasing: `release` reads only
`component.yaml`. If they have uncommitted changes, though, step 3 stops those too — commit or discard them, then release.
