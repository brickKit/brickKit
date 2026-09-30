# deploy.local.yaml

## What it is

Your personal deploy file: exactly the same structure as `deploy.yaml`, and **never committed**. It holds the way of
deploying that belongs only to you and only to this moment — "I'm debugging this component in my IDE"
(`mode: debug`), "port 8080 is taken on my machine", "I use my own database", "I run Podman on this machine".

## Whole-file replacement, not field overrides

With local mode on, every command reads **only** `deploy.local.yaml` and doesn't look at `deploy.yaml` at all. It is
not "a few fields layered on top of the team file".

Why: layering (an overlay) means merging two files in your head to know what is actually in effect; when something
goes wrong, you have to read both files plus a set of merge rules. With whole-file replacement, the file you open is
everything that takes effect — what you see is what runs. The cost is keeping up when the team changes `deploy.yaml`,
which the consistency check and `refresh` below take care of.

## `brickkit local`

```bash
brickkit local on
```

```text
✅ Local mode is on: commands now read deploy.local.yaml
   deploy.local.yaml was copied from deploy.yaml — change it as you like, it is not committed
```

The first `on` copies `deploy.yaml` over word for word; the one exception is the header comment `init` wrote ("team
file, please commit it"), replaced with a note for a personal file — that sentence would be false in a file that is
never committed:

```yaml
# deploy.local.yaml — your personal deploy file: while local mode is on, commands read it instead of deploy.yaml.
# Copied from deploy.yaml and never committed; after the team changes deploy.yaml, brickkit local refresh copies it again.
target: docker # docker | podman | k8s
```

After that, change it freely, for example:

```yaml
components:
  - id: demo/hello
    mode: debug
    localPort: 18080
```

| Subcommand | What it does |
| --- | --- |
| `brickkit local on` | Turn local mode on; if there's no `deploy.local.yaml`, copy one from `deploy.yaml`, otherwise keep using the existing one |
| `brickkit local off` | Turn local mode off; the file is **not** deleted, the next `on` picks it up again |
| `brickkit local status` | The switch, whether the file exists, whether it matches `brickkit.yaml` |
| `brickkit local refresh` | Regenerate it from the current `deploy.yaml`, keep the old one as `deploy.local.yaml.bak`, and list every local change in the old file |

The switch is recorded in `.brickkit/` and belongs to this machine only.

```bash
brickkit local status
```

```text
Local mode: on
deploy.local.yaml: present, in use
✅ deploy.local.yaml matches brickkit.yaml
```

## Strict consistency

The rule: the components `deploy.local.yaml` lists must match those declared in `brickkit.yaml` one for one, exactly as
required of `deploy.yaml`.

It usually trips after a `git pull`: a teammate added a component, so `brickkit.yaml` and `deploy.yaml` changed and your
personal file didn't. Every command that reads a deploy file then stops:

```bash
brickkit up --dry-run
```

```text
❌ Error: deploy.local.yaml is out of date: it does not match the components in brickkit.yaml
   File: deploy.local.yaml
   No entry for: demo/caller
   Reason: Local mode is on and brickkit.yaml changed, but your deploy.local.yaml was not updated
   Suggestions:
   1. Option A (recommended): brickkit local refresh regenerates deploy.local.yaml from deploy.yaml and keeps the old one as deploy.local.yaml.bak
   2. Option B: add or remove the listed entries in deploy.local.yaml by hand
   3. Option C: brickkit local off switches back to the team's deploy.yaml
```

Why it doesn't just fill the gap: that would mean deciding for you how the new component runs on your machine, when you
might need it on `mode: debug`. Stopping loudly with three ways forward beats a quiet guess. `lint` checks
`deploy.local.yaml` (if it exists) whether local mode is on or not.

When you run `add` / `remove` / `upgrade` yourself, the CLI changes `deploy.yaml` and `deploy.local.yaml` together, so
your personal file doesn't fall behind that way.

## `refresh` and merging your local changes

```bash
brickkit local refresh
```

```text
✅ Generated a fresh deploy.local.yaml from deploy.yaml. The old one is backed up at deploy.local.yaml.bak.
ℹ️ The old file had 2 local changes; merge the ones you still need into the new deploy.local.yaml by hand:
   - [demo/hello] mode: debug (now unset)
   - [demo/hello] localPort: 18080 (now unset)
```

`refresh` doesn't merge for you: it lists every place the old file differs from the team file, and you decide which
ones you still want. The old file is in `.bak`.

What counts as "a local change" is decided against **the copy your file was made from**: `local on` and `refresh` keep
that team file as `.brickkit/deploy.local.base.yaml`. A value you changed or added, and a field you **deleted**, are local
changes; a value you never touched isn't, even if the team has changed it since — after the refresh it simply follows the
team's new value. Only changes that differ from the team file as it is now are listed. Say you swapped the team's
`expose` for local debugging:

```text
ℹ️ The old file had 4 local changes; merge the ones you still need into the new deploy.local.yaml by hand:
   - [demo/hello] mode: debug (now unset)
   - [demo/hello] localPort: 18080 (now unset)
   - [demo/hello] expose: removed locally (now `true`)
   - [demo/hello] exposePort: removed locally (now `18080`)
```

When there's no such record (a `deploy.local.yaml` you wrote by hand), `refresh` can only compare the old file with the
new one: it lists values the old file sets differently or that the new file doesn't have, and a field you deleted can't be
told apart from one the team added — check `deploy.local.yaml.bak` against the new file yourself.

## With `-f` / `--no-local`

| Written | Which deploy file is read |
| --- | --- |
| default | `deploy.local.yaml` when local mode is on, otherwise `deploy.yaml` |
| `--no-local` | `deploy.yaml` this time; the local-mode switch stays as it is |
| `-f deploy.prod.yaml` | Only the named file this time, ignoring `deploy.local.yaml` and the local-mode switch entirely |

Both `--no-local` and `-f` apply to that one command only. Which commands take them is in the
[CLI reference](../07-cli-reference/README.md).

## Common uses

- **Debug a component in your IDE:** `mode: debug` plus `localPort`; the other components keep running in containers.
  See [Local debugging](../02-project-guide/03-local-debug-workflow.md).
- **A port is taken on your machine:** change that component's `exposePort`.
- **Use your own database:** override the matching shared variable under `vars:`, for example `PG_HOST: localhost`.
- **Use another engine:** `target: podman`; when the team file says `k8s`, a personal file saying `docker` runs the same
  components on your machine.
