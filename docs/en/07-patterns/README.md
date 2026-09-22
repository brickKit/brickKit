# Patterns

These are **recommended practices**, not hard requirements the platform enforces — `brickkit up` won't check whether you followed any of them. Each one's core comes from lessons learned in a real deployment; a few individual sections are a recommended working order or a concept table without the same field record, and say so at their start.

**The numbers in the file names are the suggested reading order, with one exception:** 00–04 first (component developers), then 05–08, 10 and 11 (deployers), and 09 last (operations) — `10-secrets.md` and `11-podman-environment-checklist.md` were both added after `09-deployment/` was already numbered, and read better grouped with the other deployer docs than wedged in front of the chapter that's deliberately always last.

## Find your problem first

| What you're doing | Read this |
| --- | --- |
| Just got a requirement, not sure how many components it should split into | [Component Design Guidelines](00-component-design.md) |
| Writing tests for a component, unsure how many layers or what each should cover, or want an AI to write the component | [Testing patterns for components built on BrickKit](01-testing.md) |
| Planning seed/test data, worried about polluting production | [Planning Seed Data and Test Data](02-data-construction.md) |
| A component needs to stay closed-source, worried about the image being reverse-engineered | [Protecting Closed-Source Components from Image-Based Extraction](04-closed-source-image-hardening.md) |
| A project needs dozens of components and memory/ports are running out | [Choosing a deployment shape](05-deployment-selection-guide.md) → may lead you to `servedBy` |
| Already decided to merge components with `servedBy` | [Declaring `servedBy`: A Deployment Checklist](06-servedby-deployment-checklist.md) |
| Building a "shell" component to host others | [Building a Qualified Shell](07-shell-implementers-guide.md) |
| Components merged into a shell need to share one database connection pool | [Sharing a Database Connection Pool Inside a Shell](08-shared-connection-pools.md) |
| A password or API key needs to reach a component without ever sitting in `brickkit.yaml` as plaintext, or you need to hook up Vault/ESO | [Secrets](10-secrets.md) |
| Want to run rootless Podman itself on Ubuntu/Debian and `down`/`rm` fails with a permission error | [Podman on Linux: an environment checklist](11-podman-environment-checklist.md) |
| Need to self-host a BrickKit Market instance | [Self-Hosting the BrickKit Market](09-deployment/self-hosted-market.md) |
| Calling a dependency's `*_ENDPOINT` address, wondering if your HTTP client needs special handling for a redeploy | [Calling a Dependency's Address Reliably](03-service-addressing.md) |

## By role

### Component developers

- **00** [Component Design Guidelines](00-component-design.md) — how to research a domain, when a feature needs a component family rather than a flag, and how component boundaries map onto DDD's vocabulary
- **01** [Testing patterns for components built on BrickKit](01-testing.md) — four backend layers (contract/business-rule/unit/integration), four frontend layers (unit/component/e2e/visual regression), and a recommended order for having an AI write a component
- **02** [Planning Seed Data and Test Data](02-data-construction.md) — two paths that must stay physically separate
- **03** [Calling a Dependency's Address Reliably](03-service-addressing.md) — real, measured behavior of Go/Python/Node HTTP clients across a dependency's container being replaced
- **04** [Protecting Closed-Source Components from Image-Based Extraction](04-closed-source-image-hardening.md) — pulling an image isn't the same guarantee as a private Git repo

### Deployers / platform admins

- **05** [Choosing a deployment shape](05-deployment-selection-guide.md) — how to pick among topology (independent / shell-merged / mixed) × `docker`/`k8s`
- **06** [Declaring `servedBy`: A Deployment Checklist](06-servedby-deployment-checklist.md) — a checklist for deciding whether and how to use it
- **07** [Building a Qualified Shell](07-shell-implementers-guide.md) — for whoever is building the shell component itself
- **08** [Sharing a Database Connection Pool Inside a Shell](08-shared-connection-pools.md) — how components merged into the same shell share a pool
- **10** [Secrets](10-secrets.md) — where a secret lives and ends up on each deploy target, and the two ways a secret manager plugs in
- **11** [Podman on Linux: an environment checklist](11-podman-environment-checklist.md) — not a BrickKit engine choice; an environment prerequisite for running rootless Podman itself

### Operations

- **09** [Self-Hosting the BrickKit Market](09-deployment/self-hosted-market.md) — deploying the marketplace itself, from local dev to production

## By topic

### Component design and quality

- [Component Design Guidelines](00-component-design.md)
- [Testing patterns for components built on BrickKit](01-testing.md)
- [Planning Seed Data and Test Data](02-data-construction.md)
- [Protecting Closed-Source Components from Image-Based Extraction](04-closed-source-image-hardening.md)
- [Calling a Dependency's Address Reliably](03-service-addressing.md)

### Deployment shape and merged deployment

- [Choosing a deployment shape](05-deployment-selection-guide.md)
- [Declaring `servedBy`: A Deployment Checklist](06-servedby-deployment-checklist.md)
- [Building a Qualified Shell](07-shell-implementers-guide.md)
- [Sharing a Database Connection Pool Inside a Shell](08-shared-connection-pools.md)
- [Secrets](10-secrets.md)
- [Podman on Linux: an environment checklist](11-podman-environment-checklist.md)
- [Self-Hosting the BrickKit Market](09-deployment/self-hosted-market.md)
