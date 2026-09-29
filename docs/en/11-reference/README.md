# Reference

The exact specification: every field's type, whether it's required, its default, the rules the validator really applies.
For looking up one specific question, not for reading from the top.

How it differs from the [Field quick reference](../01-three-layers/09-field-reference.md): the quick reference shows every
field on one page, one sentence each; here every field carries its full rules, and matches the structures in the code one
by one — a struct gaining a field that the docs don't list makes a test fail.

| Page | Covers |
| --- | --- |
| [01 component.yaml](01-component-yaml-schema.md) | Every field of a component's Manifest |
| [02 brickkit.yaml](02-brickkit-yaml-schema.md) | Every field of the project declaration |
| [03 Deploy files](03-deploy-yaml-schema.md) | Every field of `deploy.yaml` / `deploy.local.yaml` / `deploy.<environment>.yaml` |
| [04 The configSchema spec](04-config-schema-spec.md) | How the config spec sheet is written; the platform checks key names, not values |
| [05 JSON Schemas](05-json-schemas.md) | The three JSON Schemas under `schemas/`, and how to wire them into an editor |
| [06 Market API](06-market-api.md) | The component market's HTTP interface (optional infrastructure) |
