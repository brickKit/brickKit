# Shells

A **shell** is a special kind of component: at build time it compiles the code of a few other components (its
**members**) into its own process, and when deployed, one container serves all of them. Other components keep calling the
members at their addresses, without knowing whether a container or a shell answers.

**When to use one:** there are many components and running each as its own process costs too much memory (JVMs especially:
an idle Spring Boot takes two or three hundred MB), or a few components call each other so often that you want to save the
network round trips.

**When not to:** members that need to scale separately, isolate failures separately, or are written in different
languages — all of that requires separate processes. A shell is a way of deploying, not a default: let components run on
their own first, and merge when there's a real need.

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
