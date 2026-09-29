# Config conflict problems

## Duplicate keys: `up` refuses to start

**Symptom**

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

Your editor may also mark the file red, complaining about a duplicate key.

**Cause**

During `upgrade`, a config item you had changed also had its default changed by the component's author in the new
version. Only you know which is right, so both values are written into the file, as two lines with the same key:

```yaml
# ⚠️ Config conflict: upgrading to 1.1.0 changed the suggested value of this key.
# Keep one line, delete the other and this comment; until then brickkit refuses to start.
GREETING: Howdy  # brickkit:conflict current 1.0.0
GREETING: Hi  # brickkit:conflict proposed 1.1.0
```

Two same-named keys written by hand by accident give the same error.

**Fix**

Open the file in a plain text editor, keep the line you want, and delete the other one and the two comment lines above.
The `# brickkit:conflict …` marker at the end of the line can go too:

```yaml
GREETING: Howdy
```

Then run the command again. Background in
[Upgrading and config migration](../02-project-guide/06-upgrade-and-migration.md#resolving-a-conflict).

## After yq or "Format Document", the conflict error is gone but the value is wrong

**Symptom**

You ran `yq` on the conflicted file, or clicked "Format Document" in your editor. Afterwards `up` no longer reports a
conflict and the component runs, but the value it reads isn't the one you want — often the new version's default.

**Cause**

The YAML specification doesn't define what to do with duplicate keys. Most third-party tools **silently drop one of them**
(usually the first) when reading, and write the rest back. The conflict block is thus "resolved", only with a value chosen
for you, and you don't know which. That's exactly why BrickKit deliberately uses duplicate keys for conflicts, and warns in
the error not to reformat.

**Fix**

1. Restore the file to before the reformat: `git checkout -- config/<component>.yaml` if it wasn't committed; if it was,
   recover that version from Git history. When the file from before the upgrade is still in `config/.archive/`, you can
   compare against it too.
2. In a **plain text editor** (with the YAML plugin's auto-formatting off), keep the line you want by hand.
3. From then on, don't use any tool that re-serialises YAML on files under `config/` with conflict blocks.

## The variable a `$var:` refers to doesn't exist

**Symptom**

```text
❌ Error: config files reference shared variables that are defined nowhere
   Undefined reference: config/demo-hello.yaml: GREETING → $var:NOPE
   Suggestion: Define the variable in config/vars.yaml, or in the deploy file's vars:
```

**Cause**

`$var:NAME` checks the current deploy file's `vars:` first, then `config/vars.yaml`, and neither has it. The usual
reasons: a misspelled variable name; the variable is written only in the `vars:` of `deploy.prod.yaml`, while this run
uses `deploy.yaml`; local mode is on, and `deploy.local.yaml` was copied long ago, without the `vars:` the team added later.

**Fix**

Define it in `config/vars.yaml` (shared by every environment) or in the current deploy file's `vars:` (this environment
only). Unsure which deploy file this run reads: `brickkit local status`. `brickkit lint` reports the same problem, handy for
catching it before committing.

## Config written but not taking effect

**Symptom**

You wrote an item under `config/`, and the component still runs with its default behaviour. `up`'s output has a warning:

```text
⚠️ demo/hello@1.0.0: GREETTING is not declared in the component's configSchema, so it has no effect
   File: config/demo-hello.yaml
   Declared keys: GREETING
   Suggestion: Did you mean GREETING?
```

**Cause**

The key name is misspelled, or the component's `configSchema` has no such item at all. A config item's name is the
environment variable name injected, and a key not in `configSchema` isn't injected — the component can't read it, takes
its own default branch, and runs normally. An error like this makes nothing crash, so the platform says so with a warning.

**Fix**

Rename it as the "did you mean" suggestion says. Which config items a component has: its `component.yaml`'s
`configSchema`, or the commented-out lines in the `config/<component>.yaml` skeleton.

## Can an environment variable be overridden quietly somewhere else?

**No.** Every config value a component gets can be read straight from the files: what `config/<component>.yaml` says is
what it gets; when it says `$var:NAME`, look in the current deploy file's `vars:` and in `config/vars.yaml`; when it says
`${VAR}`, it's the value from the process environment or `.env`. There's no global default layer, no inheritance, no other
file replacing it behind your back — what you see is what runs.

The one exception is a config item colliding with a name the platform reserves (`COMPONENT_ID`, `PORT`, names ending in
`_ENDPOINT` and so on): then the platform's value wins, with a warning; see
[The environment variable contract](../06-architecture/03-env-injection-contract.md#reserved-names).

To confirm what a component finally gets: `brickkit up --dry-run`, then that service's `environment` in
`.brickkit/generated/compose.yaml` (the generated Deployment on K8s).
