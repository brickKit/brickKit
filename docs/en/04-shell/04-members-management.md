# Managing members

## Joining a shell

When you `add` a shell, the members compiled into it are added to the project at the versions the shell declares, and in
the deploy file they are **nested straight under the shell's entry**:

```bash
brickkit add --local
```

```text
➕ Adding shop/cart@0.1.0, shop/shell@0.1.0, shop/stock@0.1.0
   ✅ shop/stock@0.1.0 (in shell shop/shell)
   ✅ shop/cart@0.1.0 (in shell shop/shell)
   ✅ shop/shell@0.1.0
📝 Written: brickkit.yaml, deploy.yaml
📝 Config skeletons: config/shop-stock.yaml
```

```yaml
# deploy.yaml
components:
  - id: shop/shell
    members:
      - id: shop/stock
      - id: shop/cart
```

A member that was already in the project, at exactly the version the shell compiles in, is **moved under the shell too**,
and the output says so:

```text
   🔗 shop/stock moved into shell shop/shell
```

Adding the shell is the decision to merge its members, and the result is the same whichever came first. Only `add`ing
the shell itself moves anything: once the shell is in the project, `add` never moves an entry under it again, so a member
you moved out to run on its own stays out (a component added later that the shell compiles in is nested from the
start). `add` writes `deploy.yaml` (and `deploy.local.yaml`); other deploy files you pick with `-f` are listed as not
changed. To keep a member running on its own in an environment, move its entry back to the top level in that file
([Moving out of a shell](#moving-out-of-a-shell)).

**When two shells compile in the same member version**, a version can be in only one shell, and these rules decide
which one hosts it:

- The member is already nested under another shell in the project: it stays there, the shell you are adding doesn't
  host it, and the output has a note:

  ```text
  ℹ️  shop/stock@0.1.0 is nested under shell shop/shell, so shell shop/shell2 does not host it (a version can be in only one shell)
  ```

- The member is at the top level (you moved it out of the older shell), or it comes into the project with the new
  shell: it is nested under **the shell you are adding**.
- One `add` brings in two such shells: the first one hosts it, and the other gets the same note.

To have the other shell host it instead, move the member's entry under that shell's `members` in the deploy file.

## Moving out of a shell

Move the member's entry from under `members` to the top level:

```yaml
components:
  - id: shop/shell
    members:
      - id: shop/cart
  - id: shop/stock          # runs on its own
```

On the next `up`, `shop/stock` runs in its own container from its own image, the shell hosts only `shop/cart`, and the
addresses pointing at `shop/stock` go back to its own service name. **Not a word of its config changes**:
`config/shop-stock.yaml` says which values it needs, which has nothing to do with where it runs. The deploy file's `members`
decides only "how it runs"; it doesn't change the config itself.

By the same reasoning, writing a member's entry as `mode: disable` in a deploy file means the shell doesn't host it this
time; writing it as `mode: debug` or `mode: local` in `deploy.local.yaml` runs it as a process on your machine this time,
and the shell doesn't host it either (see [Local debugging](../02-project-guide/03-local-debug-workflow.md#debugging-a-shell-member)).

## When the shell doesn't run

When the shell is turned off with `mode: disable`, or doesn't start this time, the members under it don't disappear with
it — they **fall back to standalone components**, each deployed by its own entry.

## Removing a shell

```bash
brickkit remove shop/shell
```

The shell's entry is deleted, the members it hosted move back to the top level of the deploy file and run on their own,
and their config is kept.

## Fields that do nothing for a member in a shell

A member entry is a full deploy entry, but while a shell hosts it, the fields describing "how its own container is
deployed" **do nothing, without a warning**: `replicas`, `resources`, `serviceAccountName`, `labels`. It has no container
of its own this time, so there's nowhere for them to apply. Keeping them on the entry is useful: when the shell doesn't
run and the member falls back to a standalone component, they take effect.

Likewise, the `healthCheck` in a member's `component.yaml` isn't used inside a shell — what's health-checked is the shell's
process, with the shell's own `healthCheck`.

**Exposure is the exception: the shell opens it for the member.** The shell's process listens on each member's declared
port (checked when the files are generated; a member's `*_ENDPOINT` relies on it too), so `expose`, `exposePort`,
`hostname` and `tlsSecret` on a member entry take effect as usual, on the shell:

| Target | When a member says `expose: true` |
| --- | --- |
| Docker / Podman | The shell's container gets one more port mapping: the host's `exposePort` (the member's main port when there is none) → the member's main port inside the container. A clash with another `expose` is an error when the files are generated |
| Kubernetes | The member gets its own Ingress (with its `hostname` and `tlsSecret`), pointing at the member's own Service — which selects the shell's Pod. With `networkPolicy` on, the shell's policy lets the ingress controller in on the member's port |

So a small component facing the outside (a BFF, an adapter receiving webhooks) can go into a shell to save memory too.
When the shell runs as a process (`mode: local` / `debug`), the member's port is already open on the host, and `expose`
only gets a warning that says where.

## Start cycles caused by merging, and `skipWaitFor`

There's no cycle between the components, yet putting them in a shell can create one. `shop/cart` (in the shell) depends on
`shop/order` (outside), and `shop/order` depends on `shop/stock` (in the shell):

```text
shop/cart ──→ shop/order ──→ shop/stock
(in shell)    (outside)      (in shell)
```

The shell starts as one unit: it inherits every member's dependencies, so the shell waits for `shop/order`; whatever
depends on a member depends on the shell, so `shop/order` waits for the shell. On Docker Compose that's a wait cycle that
can never start, and `up` stops it before generating anything:

```text
❌ Error: running the members inside shell shop/shell@0.1.0 makes it wait for a component that in turn waits for the shell
   Dependency: shop/order@0.1.0 → shop/stock@0.1.0 (in shell shop/shell@0.1.0)
   Dependency: shop/cart@0.1.0 (in shell shop/shell@0.1.0) → shop/order@0.1.0
   Reason: No component requires another in a loop, but the shell container starts as one unit: it inherits every member's dependencies, and whatever depends on a member depends on the shell. With Docker Compose this becomes a depends_on cycle that can never start
   Suggestions:
   1. Put the component in the middle into the same shell too (nest its entry under the shell), or take one of these members out of the shell (move its entry to the top level)
   2. Or make one of these dependencies optional (optional: true) in its component.yaml: an optional dependency doesn't constrain start order
   3. Or accept the cost: in the deploy file, write skipWaitFor: [shop/order] on the entry of shop/cart@0.1.0. It then starts without waiting for shop/order and must retry until it is ready
```

The first two ways out change the structure; the third keeps the structure and removes one wait:

```yaml
components:
  - id: shop/shell
    members:
      - id: shop/stock
      - id: shop/cart
        skipWaitFor: [shop/order]
  - id: shop/order
```

```text
📋 Start order (topological sort):
   1. shop-shell-0-1-0  no dependencies  (hosts shop/stock@0.1.0, shop/cart@0.1.0; does not wait for shop/order@0.1.0)
   2. shop-order-0-1-0  ← depends on 1
```

`skipWaitFor` removes only **the wait at start-up**: the shell starts without waiting for `shop/order`. Connections are
unaffected — `shop/cart` still gets `SHOP_ORDER_ENDPOINT` and still reaches it.

**Whoever writes it pays the cost.** When the shell starts, `shop/order` may not be ready yet, so `shop/cart`'s code must
retry: code that exits when it can't connect during initialisation keeps restarting here. Make sure your member can do
that before writing it.

The rules:

- What's listed must be a real **required dependency** of this component version — a misspelled name, an optional
  dependency (never waited for anyway), or a component it doesn't depend on at all is an error, so that you don't believe a
  wait was removed when it wasn't.
- When the skipped dependency is in the same shell, the call never leaves the process and there's no wait to begin with;
  on Docker / Podman writing it gets a warning.
- On Kubernetes, Pods don't wait for each other at start, so there is no wait to skip. `up` warns for each entry that
  has it — `shop/cart@0.1.0 has skipWaitFor, but Pods on Kubernetes don't wait for each other at start: it has no
  effect here` — and its start order has no "does not wait for" notes. The entry isn't refused: the same deploy file
  pointed back at Docker / Podman makes it work again.
- A component running as a process on this machine (or inside a shell that runs as one) has no `depends_on` either;
  `up` warns that its `skipWaitFor` has no effect this time.
- In both of those cases a cycle caused by merging isn't stopped: with no waits at start, nothing deadlocks.
