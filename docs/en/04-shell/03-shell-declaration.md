# Declaring a shell

Three files each say one thing about a shell:

| File | Says | Written by |
| --- | --- | --- |
| The shell's `component.yaml`: `shell.members` | **What the shell is**: which members were compiled in at build time, each at which exact version | The shell's author |
| `brickkit.yaml`: `kind: shell` | This component is a shell (a marker) | The CLI |
| The deploy file: `members` under the shell's entry | **What it hosts this time**: which members run inside the shell this run | The project |

The platform only checks that the three say the same thing; it never works out on your behalf how things should run.

## What the shell is: `shell.members`

```yaml
# shell/shop/shell/component.yaml
metadata:
  id: shop/shell
  name: Shop shell
  version: 0.1.0
  description: Compiles the cart and the stock into one process

shell:
  members:
    - shop/cart@0.1.0
    - shop/stock@0.1.0
```

Writing `shell.members` makes the component a shell. Each member is written with an **exact version**, the same way as in
`dependencies`: those versions' code is what's compiled into the shell's image, and it can't load an arbitrary version
when it starts — BrickKit supports only shells that compile their members in at build time.

When `brickkit build` builds the shell's image, it records the member versions compiled in as an image label:

```text
io.brickkit.shell.members = shop/cart@0.1.0,shop/stock@0.1.0
```

`up` checks it before using a shell image built on this machine; see
[Upgrading a shell](07-shell-upgrade.md#when-the-image-and-the-declaration-dont-match).

## What it hosts this time: `members` in the deploy file

```yaml
# deploy.yaml
components:
  - id: shop/shell
    members:
      - id: shop/stock
      - id: shop/cart
  - id: shop/order
```

Member entries are nested under the shell's entry, one level only. They are full deploy entries and can carry `mode`,
`skipWaitFor` and the other fields. A bare ID means the default version; `id@version` means that version.

**Membership has this one source only.** The shell's `component.yaml` says who it **can** host; the deploy file says who
it **does** host this time. A shell with five modules compiled in, in a project that uses two of them, gets just those two
under it — the other three aren't initialised this time.

## `kind: shell`

The shell's entry in `brickkit.yaml` carries `kind: shell`:

```yaml
components:
  - id: shop/shell
    version: 0.1.0
    kind: shell
```

The CLI writes it when it `add`s the shell. `lint`, `up` and `graph` all check that it agrees with the shell's
`component.yaml`, and that every member placed under the shell really is compiled into it. It's only a marker, so that
someone reading `brickkit.yaml` sees at a glance which component is a shell; it doesn't decide who is inside the shell.

## Why it's split this way

- **Being able to isn't the same as doing.** The same shell image can host different combinations of members in different
  projects; writing "who it hosts" into the shell's `component.yaml` would allow only one.
- **"Which version was compiled in" has to be said by the shell itself.** Only whoever built the image knows what code is
  in it; the project can only decide whether and how to use it.
- **Each fact is written in one place.** Membership only in the deploy file, compiled-in versions only in the shell's
  `component.yaml`. A fact written in two places sooner or later disagrees with itself.

When the three disagree, what gets generated is certainly wrong — the image doesn't contain the member's code at all, it
has another version compiled in, or the shell doesn't know it's a shell. So `up` fails loudly before generating anything,
instead of letting it show up only once the containers are running.
