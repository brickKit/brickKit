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
6. **Run your own checks**, when `component.yaml` declares them ([below](#your-own-checks-releasechecks)).
7. **Tag and push** — an annotated tag carrying the [release notes](#release-notes) when you give them.

Nothing is written until every step before the last has passed.

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

## Your own checks: `release.checks`

What a version has to pass before it goes out — the tests, a conformance suite run against the running service, a
check your project insists on — differs per component, and only you know it. Write it in `component.yaml`, and every
release runs it, whoever runs `brickkit release` and on whichever machine:

```yaml
release:
  checks:
    - [go, test, ./...]
    - [./scripts/conformance.sh]
```

Each check is one command written as an array: the program, then its arguments, one per item. It runs in the component
directory, without a shell (no pipes, no `&&`; put those in a script), with the environment of your terminal; a program
whose name contains a `/` is relative to the component directory. They run in order after the platform's own checks,
their output shown as it comes, and the first one that exits non-zero stops the release before anything is tagged:

```text
🧪 Release check of demo/quote@0.1.0: go test ./...
ok  	example.com/quote	0.004s
🧪 Release check of demo/quote@0.1.0: ./scripts/conformance.sh
conformance: 12 cases, 1 failed (GET /quotes/42 returned 500)
❌ Error: a release check of demo/quote@0.1.0 failed: ./scripts/conformance.sh exited with code 1
   Directory: .
   Suggestions:
   1. Its output is above. Nothing was tagged or uploaded; fix what it reports, commit, and release again
   2. To release without running the checks: --skip-checks (the output says they were skipped)
```

`--skip-checks` releases without running them, and says so:

```text
⚠️  Release checks of demo/quote@0.1.0 skipped (--skip-checks)
✅ Released demo/quote@0.1.0: tag 0.1.0 pushed
```

`brickkit publish` runs the same checks before anything reaches the market, and takes the same `--skip-checks`.

**Why in `component.yaml`, and why the platform doesn't decide.** A tag, once pushed, and a version, once on a market,
can't be taken back, and `release` / `publish` are the only steps every way of releasing goes through. A Git hook or a
`make` target guards one machine or one habit; a check written in `component.yaml` travels with the component. What to
check stays your decision: the platform runs the commands and reads their exit codes, nothing more. The checks run only
when you release from the component's own directory — `add` and `fetch` read the same `component.yaml` in a project and
never run anything from it.

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
# 3. release, with what changed (see "Release notes" below)
brickkit release --notes-file ../notes-0.2.0.md
```

Which number to raise: only the internals changed and the interface didn't — the patch (0.1.0 → 0.1.1); endpoints or
config items were added and old callers still work — the minor; endpoints were removed or an existing field's meaning
changed — the major, and users have to change their code when they upgrade.

## Release notes

A version number says *that* something changed; release notes say *what* — and above all what a project has to do when
it upgrades: a config key whose meaning changed, an endpoint that went away. They are a few lines of Markdown you hand to
`release`, and every project that later upgrades across this version reads them before anything is changed.

They are optional. Without them the version is released exactly the same, with a lightweight tag (a bare name pointing
at a commit). With them, `release` makes an annotated tag (a tag that carries a message of its own) and puts the notes
in it, kept as you wrote them: lines starting with `#` stay — Git would normally drop them as comments, and a Markdown
heading would go with them.

Write them in a file **outside the component directory**: an uncommitted file in it, even a new one, is stopped by the
working-tree check (step 3 above).

```markdown
## Breaking

- `QUOTE_TTL` is now in seconds (was minutes): multiply your value by 60.

## Added

- `GET /quotes/{id}/history`
```

```bash
brickkit release --notes-file ../notes-0.3.0.md     # a path relative to the current directory
brickkit release --notes "Quotes now carry a currency field. No config changes."   # short ones inline
```

```text
✅ Released demo/quote@0.3.0: tag 0.3.0 pushed
   📝 Release notes written into the tag: projects see them when they upgrade
```

`--notes` and `--notes-file` together is an error, as is a file that can't be read — in both cases nothing is
released. `--local` takes neither: one set of notes describes one component's version, so release each component with
its own (`brickkit release --path <dir> --notes-file <file>`). Like the version they belong to, the notes are written
once: to say more, release the next version.

**What a project sees.** `brickkit upgrade` — and `upgrade --dry-run` — prints, before it changes anything, the notes of
every version after the one the project has, up to and including the target; versions without notes are left out:

```text
📋 Release notes of demo/quote, 0.1.0 → 0.3.0:

   ── 0.2.0 ──
      Quotes now carry a currency field. No config changes.

   ── 0.3.0 ──
      ## Breaking

      - `QUOTE_TTL` is now in seconds (was minutes): multiply your value by 60.

      ## Added

      - `GET /quotes/{id}/history`

📋 upgrade would do this:
   ⬆️  demo/quote: 0.1.0 → 0.3.0
📝 Would write: brickkit.yaml, deploy.yaml

💡 --dry-run: no file was changed
```

Publishing to a market takes the same two flags — `brickkit publish --notes-file <file>` — and the market keeps them as
the version's changelog, which `upgrade` shows the same way. More on the project side is in
[Upgrade and migration](../02-project-guide/07-upgrade-and-migration.md).

## Many at once: `--local`

When a project holds several components you wrote yourselves (all under the local source `components/`, each its own
repository):

```bash
brickkit release --local
```

It checks every one of them first — the platform's checks for all of them, then each one's own `release.checks` — and
only when all pass does it tag and push them one by one; ones already released
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
