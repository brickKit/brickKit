# Architecture

This layer answers two questions: **how the platform works**, and **why it is shaped this way**. The numbers are the suggested reading order.

## Read these two first

| No. | Read | What it covers |
| --- | --- | --- |
| 00 | [Architecture overview](00-overview.md) | How one `brickkit up` goes from a declaration to running containers — the whole pipeline — and what the platform deliberately doesn't do |
| 01 | [Design principles and trade-offs](01-design-principles.md) | The one idea underneath everything; the engineering ideas BrickKit uses and deliberately leaves alone; the twelve design principles |

## Mechanisms: how each step is done

| No. | Read | What it covers |
| --- | --- | --- |
| 02 | [Dependency resolution and start order](02-dependency-resolution.md) | A real diamond dependency, a real cycle, and why the longest dependency chain — not the component count — decides how long `up` takes |
| 03 | [How deployment files are generated](03-deployment-generation.md) | The same project generated for Docker and for Kubernetes, side by side |
| 04 | [The environment variable contract](04-environment-variables.md) | Which environment variables the platform puts in a container, and where their names and values come from |
| 05 | [Resource binding](05-resource-binding.md) | What happens when a binding is wrong or two bindings collide, and how quotas merge |
| 06 | [Signing and the trust model](06-signing-and-trust.md) | What actually gets signed, and why the public key can never come from the marketplace |

## For looking things up (no need to read in order)

| No. | Read | What it covers |
| --- | --- | --- |
| 07 | [component.yaml field reference](07-component-yaml-reference.md) | Each field's type, whether it's required, its default, and the validation rule |
| 08 | [brickkit.yaml field reference](08-brickkit-yaml-reference.md) | The same, for the project-config side |
| 09 | [CLI command reference](09-cli-reference.md) | Every command and flag, with real output |
| 10 | [Error codes](10-error-codes.md) | Each error code's situations, cause and fix; which ones are worth retrying |
