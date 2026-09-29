# Contracts and artifacts

## A component's boundary is its contract

To call your component, others need to know one thing: what interface it offers. A **contract** writes that down as a
file machines can read — OpenAPI, proto, a GraphQL schema, a description of an event format. With it, callers can
generate client code, write mocks and integrate, without reading your source.

This matters especially when an AI writes the code: ask an AI to write a caller and give it a contract, and it knows every
endpoint's path, parameters and response; give it a whole source repository, and it first has to guess what's public.

So **contract first, then implementation**: once the interface is settled in the contract, callers and implementers can
start at the same time; and whatever the implementation changes, comparing it with the contract tells you whether the
agreement broke.

## `artifacts`: handing the contract over

```yaml
artifacts:
  - type: api-contract
    format: openapi
    description: HTTP interface
    files:
      - api/openapi.yaml
```

| Field | Required | Meaning |
| --- | --- | --- |
| `type` | ✅ | What this set of files is: `api-contract`, `sdk`, `docs`… a free string |
| `files` | ✅ | File paths, relative to the repository root |
| `format` | | `openapi`, `protobuf`, `json`… a free string |
| `description` | | A description for people |

The platform doesn't understand `type` or `format`, nor does it parse the contract's content — it only delivers `files`
to users as they are. So you can hand over a contract in any format.

When a user `add`s your component, these files land in their project under `.brickkit/artifacts/<versioned-service-name>/<type>/`,
and the project map `BRICKKIT.md` lists the path:

```text
| demo/quote | 0.1.0 | `.brickkit/manifests/demo/quote/0.1.0/BRICKKIT.md` | `.brickkit/artifacts/demo-quote-0-1-0/` |
```

The directory name carries the version: which version of the contract a caller's client was written against is visible at
a glance.

## What goes into a contract

`demo/quote`'s contract:

```yaml
openapi: 3.0.3
info:
  title: demo/quote
  version: 0.1.0
  description: Returns a quotation each time, prefixed with demo/hello's greeting
paths:
  /api/v1/quote:
    get:
      summary: Get a quotation
      responses:
        "200":
          description: The greeting and the quotation
          content:
            application/json:
              schema:
                type: object
                properties:
                  greeting: { type: string }
                  quote: { type: string }
```

- `info.version` follows the component version.
- Every external endpoint is in it; internal ones aren't — whatever is written there becomes an agreement you can't
  change freely later.
- Contract and implementation are released in the same repository, under the same tag: whichever version of the component
  you get, you get that version's contract.

**Closed-source components need a contract even more.** Users can't see the source; the contract is the only interface
description they have.

## `brickkit fetch`: the contract without the component

Another project wants to call `demo/quote`, but `demo/quote` is deployed by your project — they only need the contract to
write a client, and adding the component to their project would only deploy another copy on their side. That's what
`fetch` is for:

```bash
brickkit fetch demo/quote
```

```text
🔎 No version given; resolved to demo/quote@0.1.0 (from install source company-git, type git)
📦 Downloaded the artifacts of demo/quote@0.1.0 (not written to brickkit.yaml)
   .brickkit/artifacts/demo-quote-0-1-0/
     api-contract/api/openapi.yaml

💡 This component won't be deployed by this project.
   Another project runs it, so no *_ENDPOINT is injected for it: make its address one of the caller's own settings — declared in configSchema, filled in under config/
```

It downloads only `component.yaml` and the contract files: no change to `brickkit.yaml`, no deployment files, no image
pulls, nothing started.

For calls across projects, the platform injects no `*_ENDPOINT` for the other side's component — it isn't in your
dependency graph. The caller declares the other side's address as one of its own config items (a required key without a
default in `configSchema`), which users fill in under `config/` (see
[Designing a configSchema](03-config-schema-design.md#good-designs)).

`.brickkit/` isn't committed: teammates run the same `fetch` on their machines; to commit the contract with the project,
copy the files you need into a directory of your own.
