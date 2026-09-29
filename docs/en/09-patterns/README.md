# Recommended practices

The earlier modules cover "how to use it": what commands do, how files are written. This module covers "how to use it
well" — how to draw component boundaries, how to test, how to build data, how to write calls that hold up, what shape a
project should run in.

These are all **recommended practices, not platform rules**. The platform stays out of how components are designed or
tested inside; most of what's below comes from reviews of real deployments, and each page says which parts are backed by
practice and which are only a recommended order. Whether to use them, and how to adapt them, is your call.

For developers who have already run a project with BrickKit and are starting to ask "how do I do this right".

| Page | Answers |
| --- | --- |
| [Component design](01-component-design.md) | How big a component should be; component or config switch; how DDD concepts map; failure modes to avoid |
| [Testing strategy](02-testing-strategy.md) | Four layers for backends, four for frontends; a component's own tests and cross-component integration tests; spec first, then implementation |
| [Seed data](03-seed-data.md) | Two paths for demo data and test data; why to isolate them physically; why seed mechanisms must never reach production |
| [Calling dependencies reliably](04-service-calling.md) | What Go / Python / Node clients each do when a dependency's container is replaced; how to set timeouts and retries |
| [Choosing a deployment shape](05-deployment-selection.md) | Standalone / shell / mixed × Docker / K8s / processes on this machine: what each of the 12 combinations suits |
