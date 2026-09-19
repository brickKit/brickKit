# Hands-on Guides

A sequence of tutorials, each one run for real against the actual CLI — not a described mechanism, a followed one. Every command and every output block was actually executed while writing it. This series is complete: 12 articles, deliberately fewer and more tightly scoped than the old 23-article series (see the note after the list for why).

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
11. [Network policy and least privilege](11-network-policy.md)
12. [Multi-project sharing](12-multi-project-sharing.md)

Two deliberate departures from the old 23-article series, both explained where they happen rather than just here: Article 4 already deploys real components to Kubernetes, so there's no separate "same system on K8s" repeat later the way the old series had one; and every article after the first few reuses the same two or three minimal fixture components (`demo/hello`, `demo/caller`, `infra/redis-event-bus`) rather than building out realistic, larger ones — the point of each article is a platform mechanism, not a business scenario, so the fixtures stay deliberately small.

Not a tutorial step, but the natural companion once something goes wrong partway through one of these: [Troubleshooting](../troubleshooting.md) — the common `up`/`down` and signature-verification failures, symptom → real cause → fix.
