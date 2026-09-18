# Patterns

These are **recommended practices**, not hard requirements the platform enforces — `brickkit up` won't check whether you followed any of them. Each one's core comes from lessons learned in a real deployment; a few individual sections are a recommended working order or a concept table without the same field record, and say so at their start.

## Find your problem first

| What you're doing | Read this |
| --- | --- |
| Just got a requirement, not sure how many components it should split into | [Component Design Guidelines](component-design.md) |
| Writing tests for a component, unsure how many layers or what each should cover, or want an AI to write the component | [Testing patterns for components built on BrickKit](testing.md) |
| Planning seed/test data, worried about polluting production | [Planning Seed Data and Test Data](data-construction.md) |
| A component needs to stay closed-source, worried about the image being reverse-engineered | [Protecting Closed-Source Components from Image-Based Extraction](closed-source-image-hardening.md) |
| A project needs dozens of components and memory/ports are running out | [Choosing a deployment shape](deployment-selection-guide.md) → may lead you to `servedBy` |
| Already decided to merge components with `servedBy` | [Declaring `servedBy`: A Deployment Checklist](servedby-deployment-checklist.md) |
| Building a "shell" component to host others | [Building a Qualified Shell](shell-implementers-guide.md) |
| Components merged into a shell need to share one database connection pool | [Sharing a Database Connection Pool Inside a Shell](shared-connection-pools.md) |
| Need to self-host a BrickKit Market instance | [Self-Hosting the BrickKit Market](deployment/self-hosted-market.md) |
| Calling a dependency's `*_ENDPOINT` address, wondering if your HTTP client needs special handling for a redeploy | [Calling a Dependency's Address Reliably](service-addressing.md) |

## By role

### Component developers

- [Component Design Guidelines](component-design.md) — how to research a domain, when a feature needs a component family rather than a flag, and how component boundaries map onto DDD's vocabulary
- [Testing patterns for components built on BrickKit](testing.md) — four backend layers (contract/business-rule/unit/integration), four frontend layers (unit/component/e2e/visual regression), and a recommended order for having an AI write a component
- [Planning Seed Data and Test Data](data-construction.md) — two paths that must stay physically separate
- [Protecting Closed-Source Components from Image-Based Extraction](closed-source-image-hardening.md) — pulling an image isn't the same guarantee as a private Git repo
- [Calling a Dependency's Address Reliably](service-addressing.md) — real, measured behavior of Go/Python/Node HTTP clients across a dependency's container being replaced

### Deployers / platform admins

- [Choosing a deployment shape](deployment-selection-guide.md) — how to pick among topology (independent / shell-merged / mixed) × `docker`/`k8s`
- [Declaring `servedBy`: A Deployment Checklist](servedby-deployment-checklist.md) — a checklist for deciding whether and how to use it
- [Building a Qualified Shell](shell-implementers-guide.md) — for whoever is building the shell component itself
- [Sharing a Database Connection Pool Inside a Shell](shared-connection-pools.md) — how components merged into the same shell share a pool

### Operations

- [Self-Hosting the BrickKit Market](deployment/self-hosted-market.md) — deploying the marketplace itself, from local dev to production

## By topic

### Component design and quality

- [Component Design Guidelines](component-design.md)
- [Testing patterns for components built on BrickKit](testing.md)
- [Planning Seed Data and Test Data](data-construction.md)
- [Protecting Closed-Source Components from Image-Based Extraction](closed-source-image-hardening.md)
- [Calling a Dependency's Address Reliably](service-addressing.md)

### Deployment shape and merged deployment

- [Choosing a deployment shape](deployment-selection-guide.md)
- [Declaring `servedBy`: A Deployment Checklist](servedby-deployment-checklist.md)
- [Building a Qualified Shell](shell-implementers-guide.md)
- [Sharing a Database Connection Pool Inside a Shell](shared-connection-pools.md)
- [Self-Hosting the BrickKit Market](deployment/self-hosted-market.md)
