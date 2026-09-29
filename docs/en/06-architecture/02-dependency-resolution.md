# Dependency resolution

`brickkit.yaml` only says "which components, at which versions", never who depends on whom — each component declares its
own dependencies in its `component.yaml`. On every run the CLI reads them and connects them into a graph: the nodes are
component versions, the edges are dependencies. Who runs, the start order, the injected addresses and the network policies
are all worked out from this graph.

## Required and optional dependencies

```yaml
dependencies:
  components:
    - shop/catalog@1.0.0          # required
    - id: shop/bus@1.0.0          # optional
      optional: true
```

| | When it's missing | Start order | Address variable |
| --- | --- | --- | --- |
| Required | An error; nothing starts | It starts before whoever depends on it (they wait for it to be healthy) | Always injected |
| Optional | A warning; start-up goes on | Doesn't constrain the start order | Injected while it runs; when it doesn't, the variable **doesn't exist at all** |

When deciding who runs, both kinds count the same: if something above needs it (required or optional), it runs along. They
differ only in what a missing one leads to, and in whether it's waited for.

## Topological sort

The start order is a topological sort of the dependency graph: each component comes after all of its required dependencies.
A diamond as an example — `shop/portal` depends on `shop/order` and `shop/stock`, and both of those depend on
`shop/catalog`:

```text
📋 Component state calculation:
   ✅ shop/catalog@1.0.0  starting (shop/order needs it)
   ✅ shop/order@1.0.0    starting (shop/portal needs it)
   ✅ shop/stock@1.0.0    starting (shop/portal needs it)
   ✅ shop/portal@1.0.0   starting (top-level)

📋 Start order (topological sort):
   1. shop-catalog-1-0-0  no dependencies
   2. shop-order-1-0-0    ← depends on 1
   3. shop-stock-1-0-0    ← depends on 1
   4. shop-portal-1-0-0   ← depends on 2, 3

Can start on their own: shop-catalog-1-0-0 (no dependencies)
Longest dependency chain (3 levels): shop-catalog-1-0-0 → shop-order-1-0-0 → shop-portal-1-0-0
   Components not on this chain start in parallel with it
```

## Diamond dependencies

`shop/catalog` is depended on along two paths, but it's **one** node and starts once: both dependants name the same exact
version, so it's the same component version.

```text
shop/portal@1.0.0
├── shop/order@1.0.0
│   └── shop/catalog@1.0.0
└── shop/stock@1.0.0
    └── shop/catalog@1.0.0 (shown above)
```

When the two paths want **different** versions, those are two nodes — see several versions below.

## Dependency cycles

When a few components **require** each other in a loop, each waits for the other to come up first, there's no start order,
and resolution fails:

```text
❌ Error: dependency cycle detected
   Cycle path: shop/order@1.0.0 → shop/stock@1.0.0 → shop/order@1.0.0
   Reason: These components all require each other; each is waiting for the others to start first, so there is no valid startup order
   Suggestions:
   1. Check the dependency declarations in the Manifest (dependencies.components)
   2. Make one side an optional dependency (optional: true) instead — an optional edge doesn't constrain startup order, and one optional edge in the cycle breaks the deadlock
```

One optional edge in the loop and it's no deadlock: optional edges don't constrain the start order, and with it left out
the sort has an answer. `shop/order` requires `shop/stock`, and `shop/stock` optionally depends on `shop/order`:

```text
📋 Start order (topological sort):
   1. shop-stock-1-0-0  no dependencies
   2. shop-order-1-0-0  ← depends on 1

Dependency graph:
   shop/order@1.0.0 → shop/stock@1.0.0
   shop/stock@1.0.0 → shop/order@1.0.0 (optional)
```

Both components run: deciding who runs computes the least fixed point of "who **doesn't** run", and cycles need no special
handling — nothing turns them off, so they both run.

A wait cycle that only appears once components are merged into a shell is another matter; see
[Managing members](../04-shell/04-members-management.md#start-cycles-caused-by-merging-and-skipwaitfor).

## Several versions

Dependencies name exact versions, so two components can depend on two versions of the same component: they are two nodes
in the graph with different service names (`demo-hello-1-0-0`, `demo-hello-1-1-0`), running at the same time. When `add`
meets this it adds the second version to `brickkit.yaml` too, marked with `requiredBy` (see
[Several versions side by side](../03-component-guide/09-multi-version-coexistence.md)).

The other way round, a component ID can appear only once in one component's `dependencies`: the address variable's name
carries no version, so two versions would collide on the same variable.

## The longest chain decides start-up time

More components doesn't mean more serial steps. Components without a dependency between them start in parallel; what
really decides how long an `up` waits is **the longest chain of required dependencies** in the graph — each one on it
waits for the one before to be healthy. `up` prints it:

```text
Longest dependency chain (3 levels): shop-catalog-1-0-0 → shop-order-1-0-0 → shop-portal-1-0-0
   Components not on this chain start in parallel with it
```

To start faster, look at this chain: turn a required dependency on it that needn't be waited for into an optional one (the
component must then retry on its own while the other isn't up yet), and the chain gets shorter.

## Where Manifests come from

Resolution needs every component version's `component.yaml`. For components from a local source it's read from the
directory every time; for ones from Git or a market, the project's permanent cache is checked first, and the install
source is asked only when it isn't there (see [The Git repository cache](06-bare-repo-mechanism.md)). So `deps`, `graph`
and `up --dry-run` may need the network the first time; after that the same versions come from the cache. `lint` doesn't
build this graph, so it never goes to the network.
