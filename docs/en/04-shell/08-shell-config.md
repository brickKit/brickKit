# The shell's own config

## A shell is a component too

A shell has config of its own: a log level, its own connection pool, its own external port. Like any component, it
declares a `configSchema` in `component.yaml`, and the project fills in values in `config/<shell>.yaml`:

```yaml
# shell/shop/shell/component.yaml
configSchema:
  type: object
  properties:
    SHELL_LOG_LEVEL:
      type: string
      default: info
      description: The shell's own log level
```

## Two kinds of config, two channels

| | For | How it arrives |
| --- | --- | --- |
| The shell's own config | The shell's process | Ordinary environment variables, as for any component |
| The members' config | The modules inside the shell | Packed into `BRICKKIT_SERVED_MEMBERS_CONFIG` (see [Config as JSON](02-json-injection.md)) |

Both sit side by side on the generated shell container:

```yaml
    env_file:
      - path: .brickkit/generated/env/shop-shell-0-2-0.env
    environment:
      - BRICKKIT_SERVED_MEMBERS=shop-cart-0-1-0,shop-stock-0-2-0
      - COMPONENT_ID=shop/shell
      - COMPONENT_VERSION=0.2.0
      - SHELL_LOG_LEVEL=info
```

`SHELL_LOG_LEVEL` is the shell's own; the members' config is in the JSON in the env file and doesn't collide with it. The
shell's `COMPONENT_ID` / `COMPONENT_VERSION` are the shell's own — members' IDs and versions are in each JSON item's
`componentId` / `version`.

A shell's `configSchema` can't use the platform's reserved names either, `BRICKKIT_SERVED_MEMBERS` and
`BRICKKIT_SERVED_MEMBERS_CONFIG` among them.

## Communication inside the shell

How members call each other inside the shell — through their own ports, through in-process function calls, or over an
internal message bus — the platform stays out of it. The address the platform gives
(`SHOP_STOCK_ENDPOINT=http://shop-shell-0-1-0:8082`) always works: the shell really does listen on that port for
`shop/stock`. A shell author who wants to save that network hop can have the shell recognise "this address points at one
of my own modules" and make an in-process call instead — that's the shell's own optimisation, which the platform doesn't
understand and doesn't need to.
