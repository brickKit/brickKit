# Upgrading a shell

## The shell's version decides the members' versions

A shell's image has the code of one specific version of each member compiled in. So "which version of a member does the
shell host this time" isn't something to pick freely: it has to be the version written in `shell.members` of the shell's
`component.yaml`.

The platform only **checks**; it never works it out for you. The deploy file and `brickkit.yaml` say which version is
hosted this time, the shell's `component.yaml` says which version is compiled in, and when the two disagree it's an error.

## Upgrading the shell: switch to the set the new shell compiles in

`shop/shell@0.2.0` compiles in `shop/stock@0.2.0`:

```yaml
metadata:
  id: shop/shell
  name: Shop shell
  version: 0.2.0
  description: Compiles the cart and the stock into one process

shell:
  members:
    - shop/cart@0.1.0
    - shop/stock@0.2.0
```

```bash
brickkit upgrade shop/shell
```

```text
   ⬆️  shop/shell: 0.1.0 → 0.2.0
   ⬆️  shop/stock: 0.1.0 → 0.2.0
   ✅ shop/stock@0.1.0 (requiredBy: shop/cart, shop/order)
📝 config/shop-stock.yaml
   Kept as written: STOCK_WAREHOUSE
📝 config/shop-shell.yaml
🗄️  Config archived: config/shop-shell.yaml → config/.archive/shop-shell@0.1.0.yaml
📝 Written: brickkit.yaml, deploy.yaml
```

Upgrading a shell is switching to the set of member versions the new shell compiles in. `shop/stock@0.1.0` is still
depended on by other components (`shop/cart` and `shop/order` both declare `shop/stock@0.1.0`), so it stays, and runs on
its own at the top level; an old version nobody needs any more would be removed. Each config file follows its version, as
in any upgrade: `config/shop-stock.yaml` is migrated to `shop/stock@0.2.0` and the kept old version gets its own
`config/shop-stock@0.1.0.yaml`; the shell's own config is migrated to 0.2.0, and the 0.1.0 file is archived into
`config/.archive/`, because no `shop/shell@0.1.0` is left. The deploy file changes with it:

```yaml
components:
  - id: shop/shell
    members:
      - id: shop/stock
      - id: shop/cart
        skipWaitFor: [shop/order]
  - id: shop/order
  - id: shop/stock@0.1.0
```

Fields you wrote on member entries (`mode`, `skipWaitFor`…) are kept. Build the new shell's image and `up`:

```text
📋 Start order (topological sort):
   1. shop-stock-0-1-0  no dependencies
   2. shop-order-0-1-0  ← depends on 1
   3. shop-shell-0-2-0  ← depends on 1  (hosts shop/stock@0.2.0, shop/cart@0.1.0; does not wait for shop/order@0.1.0)
```

A shell can have only one version in `brickkit.yaml`: with two versions of the same shell hosting members at once, who
hosts whom would be unclear.

## Upgrading only a member

Upgrading only a member (`brickkit upgrade shop/stock`) leaves the shell alone: the shell's image still holds the old
version's code. So the old version stays with a `requiredBy` (the shell is on the list of those who need it), the member
entry under the shell is pinned to the old version, and the new default version runs on its own at the top level:

```text
   ⬆️  shop/stock: 0.1.0 → 0.2.0
   ✅ shop/stock@0.1.0 (requiredBy: shop/cart, shop/order, shop/shell)
```

```yaml
components:
  - id: shop/shell
    members:
      - id: shop/stock@0.1.0
      - id: shop/cart
        skipWaitFor: [shop/order]
  - id: shop/order
  - id: shop/stock
```

When the two versions share one database, whether the data layer stays compatible is for the member to guarantee (see
[Several versions side by side](../03-component-guide/09-multi-version-coexistence.md)). **After upgrading, check
compatibility between the members yourself**: members in a shell call each other in-process, and the platform can't know
whether `shop/cart@0.1.0` works with a newer version of the stock module.

## Three ways out when versions don't match

Editing the deploy file by hand, you may have a shell host a version it doesn't compile in — for instance, by swapping the
two `shop/stock` entries above:

```text
❌ Error: the member versions this run hosts in a shell differ from the ones its component.yaml says are compiled in
   File: deploy.yaml
   components[0].members[0]: shell shop/shell@0.1.0 contains shop/stock@0.1.0, but this run hosts shop/stock@0.2.0
   Suggestions:
   1. Upgrade shop/shell to a version whose component.yaml lists shop/stock@0.2.0 under shell.members
   2. Move the entry of shop/stock out of the members of shop/shell to the top level of the deploy file, so it runs on its own
   3. Keep both versions: brickkit.yaml already keeps the version compiled into the shell, so swap the two deploy entries: change the member entry under shop/shell to `- id: shop/stock@0.1.0` and the entry at components[2] to `- id: shop/stock`; shop/stock@0.2.0 then runs on its own
```

| Way out | Fits when |
| --- | --- |
| Upgrade the shell | There's already a shell version that compiles in the new version |
| Move the member out of the shell | It's fine not to merge this member for now |
| Keep both versions | The old version keeps running in the shell, the new one runs on its own, until the shell catches up |

The third one spells out exactly what to change; make those edits and `up` passes.

## When the image and the declaration don't match

Change `shell.members` in the shell's `component.yaml` without rebuilding the shell's image, and the image on this machine
still holds the old members' code. `brickkit build` records the member versions compiled in as an image label when it
builds a shell's image, and `up` checks it before using a shell image built on this machine:

```text
❌ Error: the local image of shell shop/shell@0.2.0 contains other member versions than its component.yaml declares
   Image: shop-shell:0.2.0
   Members in the image: shop/cart@0.1.0,shop/stock@0.1.0
   Members declared in component.yaml: shop/cart@0.1.0,shop/stock@0.2.0
   Suggestion: Rebuild the shell image: brickkit build shop/shell@0.2.0 --force
```

Better still: when the members a shell compiles in change, raise the shell's version. For a released shell version,
`component.yaml`, tag and image agree by construction. A shell image without this label (built by hand, or a third-party
image) can't be checked, and `up` only warns.
