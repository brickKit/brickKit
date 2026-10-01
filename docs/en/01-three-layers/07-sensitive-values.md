# Secrets

`config/` goes into Git, so secrets (passwords, tokens, private keys) can't be written into it directly. A secret written
there goes into version control with it, and can't be removed from the history.

## Three ways to reference a secret

| Written | Where the real value is | Good for |
| --- | --- | --- |
| `${API_TOKEN}` | The process environment, then `.env` at the project root (`.env` isn't committed) | Passwords, tokens |
| `file://.secrets/tls.key` | A file (the path is relative to the project root; `.secrets/` is in `.gitignore` by default) | Multi-line content such as certificates and private keys |
| `{ existingSecret: name, key: key }` | A Kubernetes Secret that already exists in the cluster (managed by your operators, Vault, External Secrets and the like) | Kubernetes only, when another system in the cluster manages the secret |

`$var:` works indirectly too: write `PG_PASSWORD: ${PG_PASSWORD}` in `config/vars.yaml`, and `$var:PG_PASSWORD` in the
component's config.

```yaml
# config/demo-hello.yaml
GREETING: Howdy
API_TOKEN: ${API_TOKEN}
TLS_KEY: file://.secrets/tls.key
```

## Which values count as secrets

An item the component marks `secret: true` in its `configSchema` is a secret — the component's author decides, and the
platform never guesses from a name. Secrets take a separate path when deployment files are generated (see below) and are
never written in plain text into `compose.yaml` or a Deployment.

`up` also takes a look at the files you commit: an item declared `secret: true`, or with a secret-looking name, written in
plain text in `config/` gets a warning:

```text
⚠️ Files under config/ may contain plaintext secrets
   Config items: demo/hello@1.0.0 → API_TOKEN
   Why it matters: config/*.yaml and config/vars.yaml are meant to be committed to Git, so a secret written there goes into version control with it and can't be removed from the history
   Suggestions:
   1. Change it to a reference such as ${MY_TOKEN} (real value in .env) or file://.secrets/token
   2. .env must be listed in .gitignore
   3. Items that declare secret: true are ones the component author identified as credentials; the ones that merely have a suspicious name are judged by name only, not by value, so you can ignore them if they really aren't secrets
```

It never prints the value itself. Your `deploy.local.yaml` isn't committed, so a password for your machine under its
`vars:` gets no warning; but if the variable it shadows is plain text in `deploy.yaml` or `config/vars.yaml`, you're still
warned — those are the files that leak.

## No implicit environment overrides

If `config/` says `DB_HOST: pg.internal`, the component gets `pg.internal` even when your environment has
`DB_HOST=localhost`. The environment only takes part where you **explicitly** wrote `${…}`. Otherwise the same config
would quietly turn into different values for different people and different CI runs — and what you see would no longer
be what runs. The same goes for a `mode: local` process, which otherwise starts from your terminal's environment: the
component's `configSchema` keys are removed from that environment before it starts (see
[what a `mode: local` process inherits](../06-architecture/03-env-injection-contract.md#what-a-mode-local-process-inherits)).

## Docker / Podman: where values end up

| Value | Goes to |
| --- | --- |
| A plain value | `environment` in `compose.yaml` |
| `${VAR}` inside a plain value | Written into `compose.yaml` as is; `docker compose` expands it at start from the process environment or `.env` |
| A `secret: true` value, the content of a `file://` | `.brickkit/generated/env/<service>.env`, file mode 0600, referenced by `env_file`; a `${VAR}` there is also left for compose to expand at start, while a `file://`'s content is read and written by the CLI |
| `existingSecret` | Docker has no such concept: the item isn't injected, and you're told so |

Although `${VAR}` is left for compose to expand, the CLI checks at generation time, with the same order (process
environment, then `.env`), that it **is defined**. If it isn't, generation stops rather than handing it to compose:
compose would replace it with an empty string and warn only in its own output, and the component would run with a
broken value.

```text
❌ Error: environment variables referenced in config/ or the deploy file are not defined
   Missing variables: API_TOKEN
   Reason: docker compose would replace them with an empty string at start, warning only in its own output: the component would get a broken value and no error at all
   Suggestions:
   1. Add these variables to .env in the project root, or export them in the current shell
   2. You can also write a default: ${POSTGRES_PASSWORD:-dev}
```

On Kubernetes the same config fails at generation for the same reason — both targets agree on what an undefined
reference means. For a value that may legitimately be empty, write `${VAR:-}`.

The example above generates this:

```yaml
    env_file:
      - path: .brickkit/generated/env/demo-hello-1-0-0.env
    environment:
      - COMPONENT_ID=demo/hello
      - COMPONENT_VERSION=1.0.0
      - GREETING=Howdy
```

```bash
stat -c '%A  %n' .brickkit/generated/env/*.env
```

```text
-rw-------  .brickkit/generated/env/demo-hello-1-0-0.env
```

```text
API_TOKEN="${API_TOKEN}"
TLS_KEY="-----BEGIN PRIVATE KEY-----\nMIIBVQ...\n-----END PRIVATE KEY-----\n"
```

Multi-line PEM, `$`, quotes and backslashes are all escaped, and reach the container byte for byte.

## Kubernetes: where values end up

`kubectl` does no variable substitution, so on Kubernetes the CLI evaluates `${VAR}` **when it generates the manifests**
(the process environment first, then `.env`). One missing and it stops, rather than generating a manifest with an empty
value:

```text
❌ Error: environment variables referenced in config/ or the deploy file are not defined
   Missing variables: API_TOKEN
   Reason: They must be evaluated when the K8s manifests are generated — kubectl does no variable substitution
   Suggestions:
   1. Add these variables to .env in the project root, or export them in the current shell
   2. You can also write a default: ${POSTGRES_PASSWORD:-dev}
```

| Value | Goes to |
| --- | --- |
| A plain value (including an expanded `${VAR}`) | The Deployment's `env.value` |
| A `secret: true` value, the content of a `file://` | A Secret generated by the platform (`.brickkit/generated/k8s/secrets/`, file mode 0600), referenced from the Deployment with `secretKeyRef` |
| `existingSecret` | A `secretKeyRef` straight to that existing Secret; the platform neither reads nor writes its value |

```yaml
            - name: API_TOKEN
              valueFrom:
                secretKeyRef:
                  key: API_TOKEN
                  name: demo-hello-1-0-0-config-secret
```

`existingSecret` can only be used on an item declared `secret: true`; on an ordinary item it's warned about and ignored —
that item's value would be treated as plain text anyway.

## Shells

A shell receives its members' config packed into one JSON value (`BRICKKIT_SERVED_MEMBERS_CONFIG`). The CLI evaluates
that JSON in advance, so a member's `${VAR}` and `file://` are expanded and JSON-encoded at generation time — a multi-line
PEM becomes one string with `\n` in the JSON and parses back exactly; a value that isn't valid UTF-8 (binary) fails
loudly. This JSON also takes the secret path (the 0600 env file on Docker, a Secret on Kubernetes); see
[Special characters](../04-shell/06-special-characters.md).

## Offline checks

`brickkit lint --strict` reports a `${VAR}` found in neither the process environment nor `.env`, and a `file://` whose
file doesn't exist, as warnings — and under `--strict` a warning is a failure, which suits CI.
