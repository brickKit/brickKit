# Managing source

`components/` holds component source you cloned or are writing. As a project grows, most of that source is code you're
not touching right now: your IDE indexes it, and global search, `grep` and an AI reading code for you all scan it anyway.
The two commands on this page manage that directory.

## `brickkit sync`

Moves the source of components that **won't start** this time into `components/.archived/`, and moves the source of
components that **will start** but sit in the archive back out.

The criterion is exactly `up`'s. So to narrow what you work on, turn off top-level components in the deploy file — the
components below that ran only for them stop too, and `sync` follows:

```yaml
# deploy.yaml
components:
  - id: demo/hello
    mode: disable
  - id: demo/bus
  - id: demo/caller
    mode: disable
  - id: demo/hello@1.0.0
```

```bash
brickkit sync
```

```text
📂 Workspace tidying:
   📦 components/demo/hello/               → components/.archived/demo/hello
      Reason: disabled explicitly (mode: disable)
✅ Workspace tidied (0 active, 1 archived, 0 activated)
```

The rules:

- **Both directions.** Archive what should be archived, bring back what should be active.
- **Directories only.** It doesn't affect running containers or change what `up` starts.
- **Per component.** As long as any version of a component starts this time, its source stays active (had only
  `demo/hello`'s default version been turned off above, `demo/hello@1.0.0` would still run for `demo/caller`, and the
  source wouldn't be archived).
- **Only components declared in `brickkit.yaml` whose source is present.**
- **The whole directory moves, `.git` included.** Git keeps working inside it after archiving.
- **No `--dry-run`.** If it got it wrong, running it again brings things back.
- **The focus doesn't count.** A [focus run](04-focus-run.md) is a temporary narrowing, not a statement of what source
  you need: `sync` keeps the source of everything the project runs *without* the focus, so switching focus never moves
  directories back and forth.
- **One `components/`.** When a component's source sits nested inside another component's directory, `sync` stops
  before moving anything and lists each copy — moving directories around a copy nobody can place would only make it
  worse. See [Developing inside the project](04-focus-run.md#one-components).

`sync` is a command of its own, not folded into `up`: `up` manages what runs, `sync` manages directories. If `up` moved
your files as a side effect, you'd wonder why they suddenly disappeared. `up` never needs component source either — it
reads only `component.yaml`, which it finds in the archive too.

## `brickkit restore`

Puts each component's `mode` in `deploy.yaml` back to its value in the last commit, then has the source directories
follow:

```bash
brickkit restore
```

```text
📄 deploy.yaml: mode restored from the last commit (other changes untouched)
   demo/hello                 mode: disable → remove the field (the commit doesn't set it)
   demo/caller                mode: disable → remove the field (the commit doesn't set it)
📂 Workspace tidying:
   📂 components/.archived/demo/hello      → components/demo/hello/
      Reason: re-enabled
✅ Workspace tidied (0 active, 0 archived, 1 activated)
```

It touches only the `mode` field in `deploy.yaml`, entry by entry:

| Entry | Treatment |
| --- | --- |
| In both the working tree and the last commit | `mode` goes back to the committed value; removed when the commit doesn't set it |
| New in the working tree (just `add`ed) | Left exactly as it is |
| In the commit but not in the working tree | Never added back — this isn't `git revert` |

Every other change is left alone, and the values it overwrites are printed before it acts. `deploy.local.yaml` is your
personal file and isn't committed, so there's no baseline to go back to; it stays as it is.

## The pre-commit check

By default `components/` is in `.gitignore`: component source doesn't go into the project repository, because each
component is its own repository. Some teams want the source committed with the project — just take `components/` out of
`.gitignore`. A new project's `.gitignore` lists it, but it isn't one of the entries `brickkit init` requires: those
keep personal files and secrets out of Git, and must stay.

Then `sync`'s directory moves show up in the project's diff, and sooner or later this happens:

> Turn off a few top-level components → `sync` archives → forget to put things back → commit.

In that commit, a component's source sits in `components/.archived/` while `deploy.yaml` says it should start. A teammate
pulls it, and nothing lines up.

The pre-commit check stops exactly that. When the project root is the Git repository root, `init` already installed it;
when the project sits inside someone else's repository, install it explicitly:

```bash
brickkit init --hooks
```

```text
   🪝 .git/hooks/pre-commit Check the component layout before committing
```

On every `git commit` the hook calls `brickkit restore --check`: it only checks and changes nothing — are the `deploy.yaml`
and the directory layout about to be committed consistent with each other? If not, it exits non-zero, the commit is
stopped, and it tells you whether to change `mode` back or to commit the directory move along with it. If `brickkit`
can't be found, the hook lets the commit through rather than blocking you.
