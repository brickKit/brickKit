# Hands-on Guides

A sequence of tutorials, each one run for real against the actual CLI — not a described mechanism, a followed one. Every command and every output block in an existing article was actually executed while writing it, the same standard the old (now archived) 23-article series held itself to.

This series is being written incrementally; articles not yet linked below are planned but don't exist yet — see [`docs/superpowers/specs/2026-09-13-bilingual-docs-restructure-design.md`](../../../docs/superpowers/specs/2026-09-13-bilingual-docs-restructure-design.md) §7 for the rollout plan. In the meantime, the old (frozen, Chinese-only, may not reflect current CLI behavior) 23-article series is still readable at [`docs/archive/guide/`](../../archive/guide/).

1. [Get a project running](01-first-project.md) — init, add, up, talk to it over HTTP, change config, down
2. [How the platform decides what runs](02-what-runs.md) — dependencies, the `enabled` cascade, `--dry-run`
3. Debug a component locally — `local: true`
4. Deploy to Kubernetes — plus replicas and PodDisruptionBudgets
5. Upgrade and run multiple versions side by side
6. Assemble a real multi-component system, then break it on purpose
7. Consume someone else's component — artifacts and API docs
8. The same system, deployed to Kubernetes
9. Publish and install from a marketplace — plus visibility and org scoping
10. Sign and verify components
11. Build your first component from scratch
12. Network policy and least privilege
13. Multi-project sharing

Not a tutorial step, but referenced throughout: a troubleshooting lookup, once enough of the series above exists to populate it with real, recurring failure modes rather than guesses.
