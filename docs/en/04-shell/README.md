# Shells

A **shell** is a special kind of component: at build time it compiles the code of a few other components (its
**members**) into its own process, and when deployed, one container serves all of them. Other components keep calling the
members at their addresses, without knowing whether a container or a shell answers.

**When to use one:** if the device's CPU or memory budget cannot support the required components running in separate
processes, consider compiling suitable components into a shell. Sharing one runtime can reduce CPU and memory overhead.
You can make this choice while planning the deployment; you do not have to run all components separately first.
For members that call each other very often, a shell can also reduce network round trips through in-process calls;
the actual savings depend on the shell's implementation.

**When not to:** members that need to scale separately, isolate failures separately, or are written in different
languages — all of that requires separate processes. Choose the deployment layout according to the device's resource
budget and the components' runtime requirements. Shell members scale together and share the process's failure scope.

This module uses a real example: the shell `shop/shell` compiles in `shop/cart` (the cart) and `shop/stock` (the stock),
with `shop/order` running on its own outside.

| Page | What it covers |
| --- | --- |
| [01 The idea](01-shell-concept.md) | One process loading several modules; when it fits |
| [02 JSON config injection](02-json-injection.md) | How a shell gets each member's config and port |
| [03 Declaring a shell](03-shell-declaration.md) | What each of the three files says: "what it compiles in" vs. "what it hosts this time" |
| [04 Members](04-members-management.md) | Moving members in and out; start cycles created by merging, and `skipWaitFor`; which member fields don't apply |
| [05 Writing a shell](05-shell-development.md) | The shell's start-up code, how addresses are pointed at it, local integration |
| [06 Special characters](06-special-characters.md) | How multi-line secrets, quotes and `$` reach a member byte for byte |
| [07 Upgrading a shell](07-shell-upgrade.md) | How member versions line up with the shell's version; three ways out when they don't |
| [08 A shell's own config](08-shell-config.md) | The shell's own config next to its members' |
