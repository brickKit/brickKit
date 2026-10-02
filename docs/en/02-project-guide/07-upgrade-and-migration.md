# Upgrading and config migration

A component's author released a new version. Upgrading is more than changing a version number: the new version may add
config items, drop some, or change defaults, and the values you wrote in `config/` have to move with it.
`brickkit upgrade` does all of that in one go.

## Three ways to write it

| Written | What it does |
| --- | --- |
| `brickkit upgrade` | Every component with a newer version moves to its latest — all of them, or none |
| `brickkit upgrade demo/hello` | Only this one, to its latest |
| `brickkit upgrade demo/hello@2.0.0` | Up (or down) to a given version |

Without a version it only moves forward: if the latest in the install source is older than the project's, the project
counts as up to date — never a quiet downgrade.

## Preview first: `--dry-run`

```bash
brickkit upgrade --dry-run
```

```text
📋 upgrade would do this:
   ⬆️  demo/hello: 1.0.0 → 1.1.0
   ✅ demo/hello@1.0.0 (requiredBy: demo/caller)
📝 config/demo-hello.yaml
   ⚠️  Conflict GREETING: your value Howdy, new default Hi
📝 Would write: brickkit.yaml, deploy.yaml, deploy.local.yaml

⚠️  These config files would get conflict blocks: config/demo-hello.yaml. A real upgrade asks you about each one in the terminal; with --yes or no terminal input it writes the blocks, and up refuses to start until they are resolved

💡 --dry-run: no file was changed
```

The preview really does the whole thing on a copy of the project's files, so an upgrade that would fail fails in the
preview too. Three things can be read from it:

1. `demo/hello`'s default version moves from 1.0.0 to 1.1.0;
2. `demo/caller@1.0.0` still depends on `demo/hello@1.0.0`, so 1.0.0 **stays**, marked with who needs it;
3. you changed `GREETING` to "Howdy", and 1.1.0 changed its default from Hello to Hi — that's a conflict.

## What changed: release notes

A component's author can write **release notes** when releasing a version — a few lines of Markdown on what changed and
what you have to do when you upgrade (see [Releasing](../03-component-guide/07-release-workflow.md#release-notes)).
Before `upgrade` changes anything, with or without `--dry-run`, it prints the notes of every version it moves across:
each version after the one you have, up to and including the target. Versions without notes are left out, and when no
version has any, the section doesn't appear at all:

```bash
brickkit upgrade demo/quote --dry-run
```

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

Read them before the real upgrade: a note like "`QUOTE_TTL` is now in seconds" is a change of meaning, and the config
migration below can't see it — the key keeps its name, so to the migration it is the same item, and your value in
minutes would be carried over untouched.

Where the notes come from depends on the install source that provides the target version: a Git source reads them from
the version's annotated tag, a market from the version's changelog (only versions that can be installed); a local source
has only the working copy, no version history, so it has no notes. If the notes can't be read (Git fails, the market
can't be reached), a ⚠️ line says why and the upgrade goes on — notes are for people to read and never block an upgrade.

## The real upgrade

```bash
brickkit upgrade
```

```text
GREETING of demo/hello: your value Howdy (1.0.0), new default Hi (1.1.0) — [m] keep yours / [n] take the new one / Enter keeps both as a conflict to resolve later: 

   ⬆️  demo/hello: 1.0.0 → 1.1.0
   ✅ demo/hello@1.0.0 (requiredBy: demo/caller)
📝 config/demo-hello.yaml
   ⚠️  Conflict GREETING: your value Howdy, new default Hi
📝 Written: brickkit.yaml, deploy.yaml, deploy.local.yaml

⚠️  These config files contain conflict blocks, and up refuses to start until they are resolved: config/demo-hello.yaml. Open them in a plain text editor, keep one line and delete the other and the comment
```

Each conflict is asked about in the terminal: `m` keeps your value, `n` takes the new default, and plain Enter keeps both
lines for you to choose later (that's what happened above). With `--yes` or no terminal input (scripts, CI), both lines
are always kept.

The upgrade changed these files:

```yaml
# brickkit.yaml
components:
  - id: demo/hello
    version: 1.1.0
  - id: demo/bus
    version: 1.0.0
  - id: demo/caller
    version: 1.0.0
  - id: demo/hello
    version: 1.0.0
    requiredBy: [demo/caller]
```

The deploy files (`deploy.yaml`, and `deploy.local.yaml` when it exists) gained an entry for the version that stayed:

```yaml
  - id: demo/hello@1.0.0
```

`config/demo-hello.yaml` belongs to the default version (1.1.0) and was migrated; the 1.0.0 that stayed gets a config
file with the version in its name, `config/demo-hello@1.0.0.yaml`, holding your original values, not a character changed:

```yaml
# Component: demo/hello@1.0.0
# Environment variables for this component; every key is injected as-is.
# Shared variable: $var:NAME (config/vars.yaml) · environment variable: ${NAME} · local file: file://path
# Another component's address: $endpoint:<scope>/<name>

# === Optional: commented keys use the component's default; uncomment to override ===
GREETING: Howdy
```

When no component depends on the old version any more, it doesn't stay, and its config moves into `config/.archive/`.

## How config is migrated

Each config item is decided mechanically by the table below — no rename guessing, no type conversion:

| Situation | Result |
| --- | --- |
| You didn't write it (still a comment in the skeleton) | Written as the new version's skeleton line, following the new default |
| You wrote it, and the new version's default didn't change | Your text copied as is — `$var:`, `${VAR}`, `file://`, quotes, the comment above it, not a character touched |
| You wrote it, and your value happens to equal the old default | Evidently you never changed it: it follows the new default |
| You wrote a value other than the old default, and the new version changed the default | **A conflict**: you changed it, the author changed it, and only you know which is right |
| The new version dropped the item | Not written into the new file; the value stays in the old version's file (archived under `config/.archive/`, or renamed to `config/<component>@<old version>.yaml` when the old version stays), and the report lists it |
| An item the new version added | Written as a skeleton line; a required one without a default becomes `KEY: ""`, and `up` stops until you fill it in |

Comments you wrote move along with what they're about: those at the top of the file stay at the top, those above an item
stay above it (whether you wrote a value for the item or it's still a commented-out skeleton line), and those after the
last item stay at the end. A comment above an item the new version dropped goes with it. The skeleton's own notes and
section headings are regenerated for the new version, so they don't appear twice. Block scalars (`|`, `|+`, `>` and other
multi-line forms) are carried over as written, parsing back to exactly the same value.

When the old version doesn't stay, its whole config file is kept in `config/.archive/<component>@<old version>.yaml`, to
compare against whenever you like.

## Resolving a conflict

A conflict is written as two lines with the same key:

```yaml
# ⚠️ Config conflict: upgrading to 1.1.0 changed the suggested value of this key.
# Keep one line, delete the other and this comment; until then brickkit refuses to start.
GREETING: Howdy  # brickkit:conflict current 1.0.0
GREETING: Hi  # brickkit:conflict proposed 1.1.0
```

Until then, `up` refuses to start:

```text
❌ Error: unresolved configuration conflicts
   File: config/demo-hello.yaml
   Config item: GREETING
   Line 8: Howdy (your previous value, 1.0.0)
   Line 9: Hi (suggested by 1.1.0)
   Suggestions:
   1. Open the file in a plain text editor, keep the line you want, delete the other one and the comment above them, then run the command again
   2. Do not run yq or your editor's Format Document on this file: they silently drop one of the duplicate keys, and the conflict disappears without being resolved
   💡 Your editor may mark this file as invalid YAML. That is expected: BrickKit wrote the duplicate key on purpose so the conflict cannot be missed
```

Why use "broken YAML" with duplicate keys: it makes a conflict **impossible to overlook**. A comment would leave a file
that still runs — possibly with the wrong value. In a plain text editor, keep the line you want (the
`# brickkit:conflict …` marker at its end can go too), and delete the other line and the two lines of explanation:

```yaml
GREETING: Howdy
```

Then `build` (the new version's image) and `up` as usual. Both versions run at once:

```text
📋 Component state calculation:
   ✅ demo/hello@1.1.0   starting (top-level)
   ✅ demo/bus@1.0.0     starting (demo/caller needs it)
   ✅ demo/hello@1.0.0   starting (demo/caller needs it)
   ✅ demo/caller@1.0.0  starting (top-level)
```

## When a component comes from a local source

After `add --repo` clones a component's source into `components/`, the component is provided by the local source — the
local source comes first, and the first source that has a component decides. Its "latest version" is then **the version
in the working copy's `component.yaml`**; newer tags on Git don't count:

```text
✅ Every component is up to date
ℹ️ These components come from a local source, so "latest" is the version in their working copy's component.yaml — newer tags on a remote don't count:
   demo/hello@1.0.0 (local source local-dev)
   💡 To move one: check out that version in its source directory (git checkout <version>), then brickkit upgrade
```

That's on purpose: you cloned the source because you want to run the code in your hands. To upgrade, check out the new
version in the source directory:

```bash
git -C components/demo/hello checkout 1.1.0
brickkit upgrade
```

## Shells

Upgrading a shell switches its members to **the set of versions the new shell declares** — the shell's image compiles in
exactly those versions, so members can't be upgraded on their own. How member compatibility is checked after an upgrade,
and the ways out when member versions don't match: [Upgrading a shell](../04-shell/07-shell-upgrade.md).

## After upgrading

- Components built locally need `brickkit build` again (the new version's image doesn't exist yet; `up` will say so).
- Deploy files for other environments used with `-f` aren't changed by `upgrade` — the end of its output names them;
  bring the new entries over yourself.
- Commit the changes to `brickkit.yaml`, the deploy files and `config/` together: those files, together, describe this
  upgrade.
