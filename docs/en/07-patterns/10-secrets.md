# Secrets: where they live, where they end up, and how a secret manager plugs in

## What a secret is, and why it's kept apart

A **secret** is a value that lets whoever holds it act as you: a database password, an API key, a login token. It's the platform's equivalent of a house key — copy it, and the copy opens the same door, no more questions asked, until someone changes the lock. An **address** — a component's `PEOPLE_BASIC_ENDPOINT`, a database's `host`, a public API's URL — is the equivalent of the building's street number: painted on the wall, printed on a business card, safe for a stranger to see. Knowing the address gets you to the door. It doesn't get you through it.

That distinction is the whole reason BrickKit treats the two kinds of value so differently. `brickkit.yaml` is a file the platform explicitly wants committed to Git and reviewed like code — addresses, component IDs, resource `host`/`port` fields, all sit in it in plain sight, and that's fine, because none of them are keys. A real secret value sitting in that same file is a different kind of mistake, and a much more expensive one to undo: deleting the line in the next commit does not delete the secret. Every clone that ever fetched the earlier commit still has it in `.git` history, and so does every fork, every CI runner's cache, every backup — until someone notices, rotates the credential at the source, and only then (separately, and far more painfully) rewrites history everywhere it might have spread. A leaked door number costs nothing to "fix," because it was never a secret. A leaked key means changing the lock.

**Keeping secrets apart** is BrickKit's answer to that asymmetry: a secret's real value is allowed to exist in exactly one place at a time — the environment of whatever process is actually running it — and everywhere else a project would normally write a value (the file you commit, `brickkit.yaml`; the deployment file the CLI generates; the terminal output a warning prints) holds only a *reference* to where the value lives, never the value itself.

## Where a secret lives in BrickKit

| Declared where | Always a secret? | Ends up in a generated deployment file? |
| --- | --- | --- |
| `resources[].password` | Yes, unconditionally — a resource password is always treated as a credential, no matter the resource `kind` | Yes (see below) |
| A `configSchema` property with `secret: true` | Only if the component author declared it — the platform never guesses from a key's name (a config item just named `apiKey` with no `secret: true` is an ordinary plain value) | Yes (see below) |
| `sources[].authToken` | Yes — it's the CLI's own credential for logging into a Market or cloning a private Git source | No. It's read by the CLI process itself, to authenticate a `brickkit add`/`login`/`publish` call, and never becomes an environment variable inside any component's container |

`brickkit.yaml` never holds the real value for either of the first two rows — only a `${VAR}` reference (or, on K8s, optionally an `existingSecret` reference instead — see "`existingSecret`" below). The real value lives in one of two places the CLI looks, in this order: the process environment `brickkit up` (or `add`, `login`) was actually run in, checked first; the project root's `.env` file, checked second. `.env` is already in `.gitignore` from the moment `brickkit init` creates a project — real, unedited content from a freshly initialized project:

```
# 环境变量文件（包含密码）
.env
```

(`brickkit init`'s own comment translates to "environment variable file (contains passwords)" — the same file backs both rows above, and `sources[].authToken`.)

## Where it ends up

| Target | Where the real value lands | Who resolves the reference, and when |
| --- | --- | --- |
| Docker (`deploy.target: docker`) | Never in a file the CLI writes. `docker-compose.yaml` keeps the `${VAR}` placeholder exactly as written | `docker compose` itself, when a container actually starts (process environment first, `.env` second) |
| K8s (`deploy.target: k8s`) | A generated `Secret` under `.brickkit/generated/k8s/secrets/` (file mode `0600`) — the Deployment's `env` entry holds only a `secretKeyRef` pointing at it | The CLI, at generation time — same order, process environment first, `.env` second |
| `local: true` (Docker only) | `local-debug.<versioned-service-name>.env` (also `0600`, also under `.brickkit/generated/`) — deliberately plaintext, because it's what the IDE actually loads to run the process | The CLI, at generation time |

All three of those `.brickkit/generated/` paths are already covered by the default `.gitignore` `brickkit init` writes — nothing above asks a project to remember a new rule.

The rest of this section is real, unedited output — not a description of what should happen — from one small demo component, `acme/hello`, declaring a `database` resource and one `secret: true` config property, `apiKey`:

```yaml
# component.yaml (excerpt)
dependencies:
  resources:
    - kind: database
      engine: postgresql
configSchema:
  type: object
  properties:
    apiKey:
      type: string
      secret: true
```

```yaml
# brickkit.yaml (excerpt)
components:
  - id: acme/hello
    version: 0.1.0
    config:
      apiKey: ${THIRD_PARTY_KEY}
resources:
  - kind: database
    engine: postgresql
    id: main-db
    password: ${PG_PASSWORD}
    bindings:
      - componentId: acme/hello
        database: hello
```

**Docker, values set in the process environment:**

```
$ PG_PASSWORD=pw-from-env THIRD_PARTY_KEY=key-from-env brickkit up --dry-run
...
📄 已生成：.brickkit/generated/docker-compose.yaml

$ grep -n "PASSWORD\|API_KEY" .brickkit/generated/docker-compose.yaml
22:      - API_KEY=${THIRD_PARTY_KEY}
27:      - DATABASE_PASSWORD=${PG_PASSWORD}
```

The placeholders survive untouched — `key-from-env` and `pw-from-env` appear nowhere in the generated file, even though they were genuinely set in the environment `up` ran in. This is the behavior AGENTS.md §5.2 describes: the CLI's own parsing step never resolves a `${VAR}` in `config` or `resources[].password` (internally, `internal/config`'s `deferredRefs` marks exactly these fields), on purpose, so the file it writes stays safe to open and diff.

**K8s, same component, values available only through `.env` (not the process environment):**

```
$ env -u PG_PASSWORD -u THIRD_PARTY_KEY brickkit up --dry-run
...
📄 已生成 5 份清单：.brickkit/generated/k8s/

$ ls -l .brickkit/generated/k8s/secrets
-rw------- 1 you you 548 ... config-secrets.yaml
-rw------- 1 you you 534 ... resource-secrets.yaml

$ cat .brickkit/generated/k8s/secrets/resource-secrets.yaml
apiVersion: v1
kind: Secret
metadata:
  name: main-db-secret
  namespace: brickkit-demo
stringData:
  password: pw-from-dotenv
type: Opaque

$ cat .brickkit/generated/k8s/secrets/config-secrets.yaml
apiVersion: v1
kind: Secret
metadata:
  name: acme-hello-0-1-0-config-secret
  namespace: brickkit-demo
stringData:
  API_KEY: key-from-dotenv
type: Opaque

$ grep -n -B1 -A4 "API_KEY" .brickkit/generated/k8s/deployments/acme-hello-0-1-0.yaml
            - name: API_KEY
              valueFrom:
                secretKeyRef:
                  key: API_KEY
                  name: acme-hello-0-1-0-config-secret
```

Two things worth noticing: the resource password and the `secret: true` config value each get their *own* generated Secret file (`resource-secrets.yaml` vs. `config-secrets.yaml`) — never mixed into one, so a project with only resource passwords never even sees a `config-secrets.yaml` on disk — and, with the process environment cleared, the value genuinely came from `.env` (`pw-from-dotenv`, `key-from-dotenv`), proving the fallback order is real, not just documented.

**A caveat worth knowing before it costs you a debugging session:** `docker compose config` — the standard way to sanity-check a generated compose file — *does* expand `${VAR}` placeholders, using the exact same process-env-then-`.env` order, and prints the real value:

```
$ docker compose -f .brickkit/generated/docker-compose.yaml --project-directory . config | grep -n "API_KEY\|PASSWORD"
10:      API_KEY: key-from-dotenv
15:      DATABASE_PASSWORD: pw-from-dotenv
```

The file the CLI wrote never had the real value in it. Running `docker compose config` against it does. Don't paste that command's output into a ticket, a chat message, or a CI log — it defeats the entire point of keeping the placeholder in the file in the first place.

## Two ways to plug in a secret manager

### Put the value in the process environment, then `brickkit up`

The mechanism is one sentence: **put the value in the process environment, then run `brickkit up`.** BrickKit doesn't care how the value got there:

```
PG_PASSWORD="$(cat /run/secrets/pg-password)" brickkit up
```

That line reads a value from wherever it's mounted (a Docker/Swarm secret file, a Kubernetes-mounted Secret volume, a CI system's secret-file export, a systemd credential — anything that ends up as a file or an environment variable on the machine running the CLI) and hands it to `brickkit up` as a normal shell-prefixed environment variable. The CLI's own lookup order (process environment first, `.env` second — verified above) picks it up exactly the same way it would pick up a value you typed by hand. Any tool that can put a value into a process's environment works this way; BrickKit doesn't recognize, and doesn't need to recognize, which one you used.

### `existingSecret`: reference a Secret something else already created

The first mechanism still asks *you* to hand `brickkit up` the value, once, on every run. Sometimes that's not what you want — the value already lives in a Kubernetes `Secret` some other system keeps in sync (a Vault Agent Injector sidecar, the External Secrets Operator, Sealed Secrets, or ops simply having run `kubectl create secret` by hand), and you'd rather point at it than feed it through the CLI at all.

That's `existingSecret`: a resource can write `existingSecret: <name>` instead of `password: ${VAR}`, and a `secret: true` config value can be written as `{ existingSecret: <name>, key: <key-in-secret> }` instead of a scalar. Both are K8s-only — Docker has no concept of "an already-existing Secret object" to reference. Real, unedited output from the same demo component, with both rewritten this way:

```yaml
# brickkit.yaml (excerpt)
components:
  - id: acme/hello
    version: 0.1.0
    config:
      apiKey: { existingSecret: acme-thirdparty-vault-synced, key: api-key }
resources:
  - kind: database
    engine: postgresql
    id: main-db
    existingSecret: acme-db-vault-synced
    bindings:
      - componentId: acme/hello
        database: hello
```

```
$ brickkit up --dry-run
...
📄 已生成 3 份清单：.brickkit/generated/k8s/

$ find .brickkit/generated/k8s -type f
.brickkit/generated/k8s/namespace.yaml
.brickkit/generated/k8s/services/acme-hello-0-1-0.yaml
.brickkit/generated/k8s/deployments/acme-hello-0-1-0.yaml

$ grep -rn "vault-synced\|api-key" .brickkit/generated/k8s/secrets/
(no such directory — neither generated file exists at all)

$ grep -n -B1 -A4 "API_KEY\|DATABASE_PASSWORD" .brickkit/generated/k8s/deployments/acme-hello-0-1-0.yaml
            - name: API_KEY
              valueFrom:
                secretKeyRef:
                  key: api-key
                  name: acme-thirdparty-vault-synced
...
            - name: DATABASE_PASSWORD
              valueFrom:
                secretKeyRef:
                  key: password
                  name: acme-db-vault-synced
```

Compare the file count to the previous section: five manifests there (because two Secret files existed), three here — the `secrets/` directory doesn't even get created, because there's nothing left for the platform to generate. The `secretKeyRef.name` in the Deployment points straight at the name you wrote, not at anything the CLI computed.

The difference between the two mechanisms is exactly this: the first hands **the value** to `brickkit up`, once, every run; the second hands **the whole Secret object** to Kubernetes, and the CLI never touches the value at either end — not at generation time, not ever. Neither one asks BrickKit to know what Vault, AWS Secrets Manager, or any other product even is.

That's deliberate, and it's why BrickKit doesn't call a secret-manager SDK directly on your behalf, either — it belongs on the same [rejection list](../06-architecture/00-overview.md#what-the-platform-deliberately-doesnt-do-and-why) as [config centers](../06-architecture/00-overview.md#6-config-center-and-hot-reload) (AGENTS.md §4.1), for almost the same reasons. A real secret manager SDK integration would mean one client library per backend — Vault, AWS, GCP, Azure all have different APIs, and a project only ever needs one of them, so the other three would be dead weight the CLI still has to maintain. It would mean `brickkit up --dry-run` — a command whose entire point is "only generate files, touch nothing real" — suddenly needing network access and live credentials just to print a file that won't be used. And it would mean the platform holding a decrypted secret value in its own memory for the length of a run, which is exactly the kind of thing that shows up in a security review's "who has touched this value" list. Handing the CLI a value it can put straight into a generated file, or a name it can put straight into a `secretKeyRef`, needs none of that.

## Honest boundaries

- **The K8s Secret files the CLI generates are plaintext on disk** — `0600`, yes, and gitignored by default, but plaintext, and regenerated by every `up`. Don't upload `.brickkit/generated/` as a CI artifact, and don't assume file permissions alone are a substitute for the access control a real secret store gives you.
- **`existingSecret` doesn't check that the Secret it names actually exists in the cluster, or that it actually has the key you named.** That's genuinely Kubernetes' job, not the CLI's — it happens the moment the Pod tries to start, and a missing Secret or key surfaces there, as a Pod stuck unable to start, not as anything `brickkit up` could have caught at generation time (the CLI never talks to a live cluster during generation).
- **A literal value gets a warning, and `secret: true` makes that unconditional.** Write a plain string where a `secret: true` property expects a reference, and `brickkit up` warns regardless of what the key is named — the name-based heuristic (`password`, `token`, `apiKey`, …) that catches an *undeclared* secret-looking key only ever runs on the name; a declared one is always checked.
- **Under Docker, neither `secret: true` nor `existingSecret` changes anything real.** `secret: true` changes nothing because the `${VAR}` placeholder was already never written into the compose file in the first place — there's no separate "Docker secret delivery" to opt into. `existingSecret` under Docker gets a warning and is treated as if nothing was configured at all — Docker simply has no equivalent concept to route it to.
- **There's no "load this whole `config` block from one Secret" shortcut** (the `envFrom`-style bulk import Kubernetes itself supports). Every reference is written one property at a time, on purpose: it's the same design choice AGENTS.md §9.23 makes for dependency names — a variable name that's computed from something (here, the config key) stays traceable back to it; a bulk import would trade that traceability for a few lines of typing.
