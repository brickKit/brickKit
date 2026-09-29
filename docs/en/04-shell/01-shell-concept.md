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

- **Saving memory.** A process's memory floor is mostly decided by its language runtime: Go takes a dozen or so MB,
  Python / Node a few dozen, a JVM two or three hundred. Twenty Spring Boot components, each its own process, take four to
  nine GB idle; compiled into three or four shells, that's three or four runtimes.
- **Saving network round trips.** When members call each other very often, compiled into one process the shell can have
  them call each other in-process.

Consider the alternatives first: components written in Go or Rust are frugal already, and merging isn't worth it; for the
JVM, try GraalVM native images; components you don't need can be `mode: disable` in the deploy file and not run at all.

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
