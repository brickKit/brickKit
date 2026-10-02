# Several versions side by side

## The service name carries the version

Every running component version has a **versioned service name**: the component ID with `/` and `.` turned into `-`,
followed by the exact version.

| Component version | Service name | The address others get |
| --- | --- | --- |
| `demo/hello@1.0.0` | `demo-hello-1-0-0` | `http://demo-hello-1-0-0:8080` |
| `demo/hello@1.1.0` | `demo-hello-1-1-0` | `http://demo-hello-1-1-0:8080` |

The address is exactly the same on Docker and on Kubernetes (it is both the Docker Compose service name and the Kubernetes
Service name). Two versions are two names, and DNS keeps them apart by nature: they run at the same time without clashing,
with no extra mechanism.

The address variable's **name** carries no version (`DEMO_HELLO_ENDPOINT`); its **value** does. A caller always knows
which version it talks to — there's no such thing as a quiet upgrade.

## When two versions appear

`demo/caller@1.0.0` declares a dependency on `demo/hello@1.0.0`, and `demo/quote@0.1.0` on `demo/hello@1.1.0`. With both
components in the same project, both versions of `demo/hello` are added — dependencies wanting different versions **isn't
an error**:

```yaml
# brickkit.yaml
components:
  - id: demo/hello
    version: 1.1.0
  - id: demo/quote
    version: 0.1.0
  - id: demo/hello
    version: 1.0.0
    requiredBy: [demo/caller]
```

The one without `requiredBy` is the **default version**; the one with `requiredBy` is a compatibility version "kept
because someone needs it", and it says who at a glance. `add` and `upgrade` maintain these markers: a compatibility version
nobody needs any more is cleared.

In the generated deployment file (an excerpt), each caller connects to the version it declared:

```text
  demo-caller-1-0-0:
      - DEMO_HELLO_ENDPOINT=http://demo-hello-1-0-0:8080
  demo-hello-1-0-0:
      - COMPONENT_VERSION=1.0.0
  demo-hello-1-1-0:
      - COMPONENT_VERSION=1.1.0
  demo-quote-0-1-0:
      - DEMO_HELLO_ENDPOINT=http://demo-hello-1-1-0:8080
```

## One config file per version

| File | Belongs to |
| --- | --- |
| `config/demo-hello.yaml` | The default version (1.1.0) |
| `config/demo-hello@1.0.0.yaml` | The compatibility version 1.0.0 |

The file without a version always follows the default version; when the default moves (`upgrade`), the config is carried
over by the migration rules, and an old version that is still depended on keeps its original config renamed with the
version (see [Upgrading and config migration](../02-project-guide/07-upgrade-and-migration.md)). Deploy files work the
same way: the entry with the bare ID (`- id: demo/hello`) is the default version, and every other version has its own
`id@version` entry.

## What a component author should know

- **Within one component's `dependencies`, a component ID can appear only once.** The address variable's name carries no
  version, so two versions would collide on the same variable. Versions side by side is a **project-level** capability:
  different components each depending on a different version.
- **Two versions may share one database.** Each runs its own migrations; whether the data layer stays compatible (does the
  old version accept the columns the new one added, can the new version read what the old one wrote) is the component
  author's responsibility. The migration state table's primary key should include a component identifier, or two
  components sharing a database overwrite each other's migration records.
- **Incompatible changes raise the major version.** Users decide whether they can upgrade by the version number; change
  the interface quietly in a patch release, and callers break without knowing why.

## When coexistence is needed, and when it isn't

Needed:

- **Different components depend on different versions**, and one of them hasn't caught up yet — coexistence is a buffer
  that gives it time to upgrade.
- **Gradual rollout**: the new version serves some callers first while the old one keeps serving the rest.

Not needed: once every caller has upgraded, the old version has no `requiredBy` left, and `upgrade` / `remove` clears it
naturally. Coexistence is a transition, not a goal — every extra version is another image, another config file, another
set of containers to look after.

In one case the old version doesn't go by itself: a dependent whose **version didn't change** now pins the new version
(common while developing in a local source). `upgrade` sees the dependent is already current and does nothing, so the
line with `requiredBy` stays. `brickkit lint` points it out, with the command to run:

```text
ℹ️ demo/hello@1.0.0 is in the project only for demo/caller, and by their current component.yaml none of them depends on it any more — brickkit remove demo/hello@1.0.0 takes it out
```
