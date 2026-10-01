# Writing components

**Who it's for**: whoever writes and releases components. A component is BrickKit's smallest unit you install and run — a
service that runs on its own, with a `component.yaml` describing itself.

**Read first**: [What BrickKit is](../00-intro/01-what-is-brickkit.md) and [Core concepts](../00-intro/04-core-concepts.md).
Assembling components into a system and deploying it is another matter; see [Running a project](../02-project-guide/README.md).

This module follows a real component, `demo/quote`, from nothing to its release: every time it's called it returns a
quotation, prefixed with the greeting of another component, `demo/hello`. Every command and output comes from a real run.

| Page | What it covers |
| --- | --- |
| [01 A component repository](01-component-anatomy.md) | What a component repository holds; how component ID, repository name and version tag relate |
| [02 component.yaml fields](02-component-yaml-reference.md) | What each block is for and how to write it; the exact field rules are in the reference |
| [03 Designing a configSchema](03-config-schema-design.md) | How to split and name config items; what the platform checks and what it doesn't |
| [04 Generating a skeleton](04-new-and-skeleton.md) | `brickkit new`, and from a skeleton to a component that runs |
| [05 Developing inside a component](05-local-dev-fractal.md) | A local workbench in the component repository, running yourself against your dependencies |
| [06 Contracts and artifacts](06-artifacts-and-contracts.md) | `artifacts`, contract first, `brickkit fetch` |
| [07 Releasing](07-release-workflow.md) | `brickkit release`: checks, tag, push |
| [08 A component's documentation](08-component-doc-spec.md) | The documents a component repository carries, what goes in each, and how `BRICKKIT.md` travels into the projects that use it |
| [09 Several versions side by side](09-multi-version-coexistence.md) | How two versions of one component run at once, and who talks to which |
| [10 Distributing through Git](10-git-distribution.md) | How repository addresses are derived, how the cache works, authentication for private repositories |
