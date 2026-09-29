# Architecture and mechanisms in depth

**For whom**: people who want to know how BrickKit works inside — tracking down a behaviour that makes no sense,
weighing a design proposal, or writing code for BrickKit itself. To simply use it, the
[Project guide](../02-project-guide/README.md) and the [Component guide](../03-component-guide/README.md) are enough.

**How to read it**: start with [01 The pipeline](01-pipeline-overview.md) to see which stations one `up` passes through,
then jump to the station you need. For "why is it this way and not that way", read
[05 Design principles and trade-offs](05-design-principles.md).

| Page | Covers |
| --- | --- |
| [01 The pipeline](01-pipeline-overview.md) | From the three declaration layers to running containers: every station of the pipeline |
| [02 Dependency resolution](02-dependency-resolution.md) | Topological sort, diamond dependencies, cycles, several versions |
| [03 The environment variable contract](03-env-injection-contract.md) | Every variable the platform may inject, how names are formed, when values are worked out |
| [04 Generating deployment files](04-deploy-file-generation.md) | What the generated `compose.yaml` and Kubernetes manifests look like |
| [05 Design principles and trade-offs](05-design-principles.md) | The idea running through everything, fifteen engineering ideas, the ten principles |
| [06 The Git repository cache](06-bare-repo-mechanism.md) | Bare repositories, the read order, working offline |
| [07 Cache design](07-cache-design.md) | What's in `.brickkit/`, what can be deleted |
| [08 Security and signing](08-security-and-signing.md) | Signing and verification, where public keys live, network policies and their limits |
| [09 Error codes](09-error-codes.md) | Every error code, the situations behind it, whether to retry |
