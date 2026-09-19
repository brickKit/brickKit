# BrickKit documentation

The **numbers in front of the folder and file names are the suggested reading order**: start at 00 and go on — earlier ones are the foundation for later ones. If you'd rather not start from the top, see "Three routes" below.

## In numbered order

| No. | Read | You'll come away able to | When to read it |
| --- | --- | --- | --- |
| 00 | [Quick Start](00-quick-start.md) | Get a component running in five minutes and reach it over HTTP | Your first contact — hands first |
| 01 | [Core concepts](01-concepts.md) | Recognize the terms in the docs and in error messages, and remember the one naming rule that runs through everything | Once it's running, and you want to know what just happened |
| 02 | [Comparison with existing tools](02-comparison.md) | Tell what it and Docker Compose, Helm and the like each solve, and when to pick which | You're still deciding whether to use it |
| 03 | [Hands-on guide](03-guide/README.md) | Walk it through yourself: dependencies, upgrades, Kubernetes, the marketplace, signing, network policy… | You want to learn it systematically |
| 04 | [Writing a Go component](04-go-component-template.md) | Read a real component with a database and see why it's written that way | You're developing components |
| 05 | [AI-assisted development](05-ai-development.md) | Know how to have an AI help write a component, and what it can't do for you | You plan to have an AI write code |
| 06 | [Architecture](06-architecture/README.md) | Understand why the platform is shaped this way and how each mechanism works — and look up fields, commands and error codes | You want the reasoning, or need to look something up |
| 07 | [Patterns](07-patterns/README.md) | Get practices from real deployments: component design, testing, merged deployment… | You're on a real project |
| 08 | [Troubleshooting](08-troubleshooting.md) | Find the cause and the fix from a symptom | Something has gone wrong |
| 09 | [Market API](09-market-api.md) | Look up every marketplace HTTP endpoint | You're writing a client against the marketplace |

## Three routes

- **Your first time:** 00 → 01 → 03 (the first few tutorials)
- **Developing components:** tutorial 10 in 03 → 04 → 05 → 00–04 in 07
- **Just looking up a command:** the [command overview](06-architecture/09-cli-reference.md#all-the-commands-at-a-glance) — one line and "when to use it" per command, and each links to its full flags and real output

Rather find things by "what I want to do" than in order? See the [overall navigation](../README.md).
