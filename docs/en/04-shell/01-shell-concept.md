# The idea

## One process, several modules

A shell isn't "several processes in one container". It is **one process** that loads several modules, each module being
one member component's code.

```text
Deployed on their own                      Deployed in a shell
┌──────────────┐ ┌──────────────┐          ┌──────────────────────────────┐
│ shop/cart    │ │ shop/stock   │          │ shop/shell (one process)     │
│ process :8081│ │ process :8082│    →     │   cart module   on :8081     │
└──────────────┘ └──────────────┘          │   stock module  on :8082     │
  two containers, two runtimes              └──────────────────────────────┘
                                             one container, one runtime
```

The shell listens for each member on **the member's own port**. So nothing changes for callers: `shop/order` still calls
`SHOP_STOCK_ENDPOINT`, only that address now points at the shell (see
[Writing a shell](05-shell-development.md#how-addresses-are-pointed-at-it)).

## A shell hosts several members; a member is in one shell at a time

In the deploy file, member entries are **nested under the shell's entry**:

```yaml
components:
  - id: shop/shell
    members:
      - id: shop/stock
      - id: shop/cart
  - id: shop/order
```

Each component version appears once in a deploy file, so a member can't belong to two shells at once — "where does it run
this time" has one answer. Another version of the same component can run on its own elsewhere (see
[Upgrading a shell](07-shell-upgrade.md)).

## Where it fits

- **A limited CPU or memory budget.** Each component running in its own process has runtime, background-task, and other
  overhead. If the device cannot support that overhead, suitable components can share one runtime in a shell, reducing
  repeated CPU and memory overhead. You can choose a shell while planning the deployment, without deploying all components
  separately first. Measure the savings with the actual components and device.
- **Saving network round trips.** When members call each other very often, compiled into one process the shell can have
  them call each other in-process.

Other ways to save resources are worth assessing too: GraalVM native images for the JVM, or `mode: disable` in the deploy
file for components you do not need. The choice of a shell depends on the required components' total overhead and the
device's budget, rather than the programming language alone.

## Where it doesn't

- **Scaling separately.** Members in a shell can only scale with the shell.
- **Isolating failures separately.** One module that brings the process down takes every member in the shell with it.
- **Different languages.** The shell compiles members' code in, so members usually share the shell's language and runtime.

## What the platform does, what the shell's author does

| The platform | The shell's author |
| --- | --- |
| Works out which members this shell hosts this time, and encodes their config and ports into one JSON value for the shell | Reads the JSON, initialises the matching modules, listens on the members' ports |
| Points the addresses of members at the shell | Hands each request to the right module inside the process |
| Runs members' migrations separately, with each member's own image, before starting the shell | Doesn't run members' migrations inside the shell |
| Checks that the member versions compiled into the shell's image match the ones it hosts this time | States truthfully in `component.yaml` which members, at which versions, are compiled in |

The platform doesn't understand how a shell loads modules or routes requests inside — that's the shell's own code. The
platform only makes clear "which members, with what config, at which address".
