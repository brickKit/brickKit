# Hands-on guides

A sequence of tutorials, each one run for real against the actual CLI — not a described mechanism, a followed one. Every command and every output block was actually executed while writing it.

**The numbers in the file names are the reading order:** one after the next, each using what the earlier ones taught. Every tutorial assumes you've built the BrickKit CLI and Docker is running (the prerequisite of tutorial 1); the last column below lists what one tutorial needs **on top of that**.

| No. | Tutorial | What you'll learn | Also needs |
| --- | --- | --- | --- |
| 01 | [Get a project running](01-first-project.md) | `init`, `add`, `up`, talk to it over HTTP, change config, `down` | — |
| 02 | [How the platform decides what runs](02-what-runs.md) | Dependencies, the `mode` cascade, `--dry-run` | — |
| 03 | [Debug a component locally](03-local-debugging.md) | `mode: debug`: one component runs in your IDE while the rest stay in containers | — |
| 04 | [Run a component locally, hands-off](04-local-execution.md) | `mode: local`: BrickKit detects the start command, launches the process, and supervises it itself — `status`/`graph`/`down` all learn to recognize it | — |
| 05 | [Deploy to Kubernetes](05-kubernetes.md) | The same declaration, only `deploy.target` changes; plus a real gotcha with `brickkit down` and shared namespaces | minikube and `kubectl` |
| 06 | [Upgrade and run multiple versions side by side](06-upgrades-and-versions.md) | Changing the version number is the upgrade; two versions running together on purpose | — |
| 07 | [Assemble a real system, then break it on purpose](07-assemble-and-break.md) | Binding a real database, and meeting two different real failure modes | A PostgreSQL (the tutorial starts one with `docker run`) |
| 08 | [Consume someone else's component](08-consuming-artifacts.md) | Artifacts and API docs; getting them without installing anything (`fetch`); standing in a stub for an upstream that isn't built yet (`brickkit new --contract` plus `mode: debug`) | Python 3 (only for the mock in the stub section) |
| 09 | [Manage component source](09-component-source.md) | Cloning source with `add --repo`; keeping only what you're working on with `sync`; the guards on `remove`; `restore` and the commit hook when source is committed with the project | `git` (no Docker needed) |
| 10 | [Publish and install from a marketplace](10-marketplace.md) | Publishing, installing and logging out; version immutability; private visibility | A marketplace (the tutorial starts one with `docker compose up`) |
| 11 | [Sign and verify components](11-signing.md) | Generating a key pair, signing, verifying, and what a failed verification looks like | cosign (only the publisher needs it) |
| 12 | [Build your first component from scratch](12-build-your-own.md) | Four files, from an empty directory to running | — |
| 13 | [Network policy and least privilege](13-network-policy.md) | Generating a NetworkPolicy from the dependency graph and proving what it really blocks | A minikube that enforces NetworkPolicy (`--cni=calico`) |
| 14 | [Multi-project sharing](14-multi-project-sharing.md) | Shared resources and isolated resources; treating another project's component as someone else's API | A Redis (the tutorial starts one with `docker run`) |

**Why every tutorial uses such small components:** after the first, every tutorial reuses the same two or three minimal fixture components (`demo/hello`, `demo/caller`, `infra/redis-event-bus`) rather than building out a larger, more realistic one. The point of each tutorial is a platform mechanism, not a business scenario, so the fixtures stay deliberately small.

**Something went wrong partway through:** [Troubleshooting](../08-troubleshooting.md) lists the most common `up`/`down`, local-debug and signature failures, plus the offline checks (`lint` and the editor schemas), by symptom, symptom → real cause → fix.
