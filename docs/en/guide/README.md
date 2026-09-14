# Hands-on Guides

A sequence of tutorials, each one run for real against the actual CLI — not a described mechanism, a followed one. Every command and every output block in an existing article was actually executed while writing it, the same standard the old (now archived) 23-article series held itself to.

This series is being written incrementally; articles not yet linked below are planned but don't exist yet — see [`docs/superpowers/specs/2026-09-13-bilingual-docs-restructure-design.md`](../../../docs/superpowers/specs/2026-09-13-bilingual-docs-restructure-design.md) §7 for the rollout plan. In the meantime, the old (frozen, Chinese-only, may not reflect current CLI behavior) 23-article series is still readable at [`docs/archive/guide/`](../../archive/guide/).

1. [Get a project running](01-first-project.md) — init, add, up, talk to it over HTTP, change config, down
2. [How the platform decides what runs](02-what-runs.md) — dependencies, the `enabled` cascade, `--dry-run`
3. [Debug a component locally](03-local-debugging.md) — `local: true`
4. [Deploy to Kubernetes](04-kubernetes.md) — plus a real gotcha with `brickkit down` and shared namespaces
5. [Upgrade and run multiple versions side by side](05-upgrades-and-versions.md)
6. [Assemble a real system, then break it on purpose](06-assemble-and-break.md) — a real database this time, and two different real failure modes
7. [Consume someone else's component](07-consuming-artifacts.md) — artifacts and API docs
8. [Publish and install from a marketplace](08-marketplace.md) — plus version immutability and private visibility
9. [Sign and verify components](09-signing.md)
10. [Build your first component from scratch](10-build-your-own.md)
11. Network policy and least privilege
12. Multi-project sharing

(Article 4 already covered Kubernetes deployment directly with real components, so this series doesn't repeat it as a separate "same system on K8s" step the way the original 23-article series did.)

Not a tutorial step, but referenced throughout: a troubleshooting lookup, once enough of the series above exists to populate it with real, recurring failure modes rather than guesses.
