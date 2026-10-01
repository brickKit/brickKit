# Several environments

Docker on the development machines, Kubernetes for staging and production, a different database address in each.
BrickKit has exactly one way to do this: **one complete deploy file per environment**, chosen with `-f`.

## `-f` / `--file`

```bash
brickkit up -f deploy.prod.yaml
```

`-f` names the deploy file to read this time (relative to the project root). It's binding: even with local mode on,
`deploy.local.yaml` isn't read, and a missing file is an error rather than a fallback to the default. `up`, `down`,
`status`, `sync`, `lint` and `graph` all accept it.

```text
Using deploy.prod.yaml (--file)
🚀 Starting project my-shop (target: k8s)
```

When local mode is on, the first line adds a note, so you don't think your personal settings apply as well:

```text
Using deploy.prod.yaml (--file); local mode is ignored for this run
```

There's one `brickkit.yaml` and one `config/`, shared by every environment: which components, which versions, and what
the business config is are the same everywhere. Only "how it's deployed" differs.

## A production deploy file

```yaml
# deploy.prod.yaml
target: k8s

k8s:
  context: prod-cluster
  namespace: my-shop

vars:
  DB_HOST: pg.prod.internal

components:
  - id: demo/hello
    replicas: 2
  - id: demo/caller
    expose: true
    hostname: shop.example.com
  - id: demo/hello@1.0.0
  - id: demo/bus
```

Its entries must match every component version in `brickkit.yaml`, one for one, just like `deploy.yaml`; the difference
is that they describe how production runs: two replicas, opened to the outside through an Ingress with a host name,
deployed to a given namespace of a given cluster.

## `vars:` gives the same `config/` different values per environment

`demo/caller` connects to a database. On the development machine the address is `localhost`; in production it's
`pg.prod.internal`. The config file is written once, referencing a shared variable with `$var:`:

```yaml
# config/demo-caller.yaml
DATABASE_HOST: $var:DB_HOST
```

```yaml
# config/vars.yaml
DB_HOST: localhost
```

The production deploy file overrides it under `vars:` (see `deploy.prod.yaml` above). `$var:` looks in the current deploy
file's `vars:` first, then in `config/vars.yaml`:

```bash
brickkit up -f deploy.prod.yaml --dry-run
```

```text
📄 Generated 12 manifests: .brickkit/generated/k8s/
   Namespace: my-shop
```

In the generated Deployment:

```yaml
            - name: DATABASE_HOST
              value: pg.prod.internal
```

Without `-f` (on the development machine, reading `deploy.yaml`), the generated `compose.yaml` has:

```text
      - DATABASE_HOST=localhost
```

The production database password works the same way: write `PG_PASSWORD: ${PG_PASSWORD}` in `config/vars.yaml` and keep
the real value in the deploy machine's environment; on Kubernetes the CLI evaluates it when generating the manifests.
Where the value lands depends on the component, not on `${…}`: when its `configSchema` declares the key `secret: true`
(as a password should be), the value goes into the generated Secret; otherwise it is written in plain text into the
Deployment (see [Secrets](../01-three-layers/07-sensitive-values.md)).

On Kubernetes, `--dry-run` only generates manifests and doesn't connect to the cluster. For a real deployment, `up` first
confirms that `kubectl`'s current context is the deploy file's `k8s.context`, and only then acts — deploying to the wrong
cluster can't be undone.

## `-f` vs. `--no-local`

| | `--no-local` | `-f <file>` |
| --- | --- | --- |
| Reads | `deploy.yaml` | The file you name |
| Local mode | Ignored this time, the switch unchanged | Ignored this time, the switch unchanged |
| When | Local mode is on and you want one run with the team's config | Deploying to another environment |

## Recommendations

- **One complete, self-contained file per environment.** No inheritance, no "base file + overlay" merging: open
  `deploy.prod.yaml` and you see everything production runs. The cost is some repeated entries across files; in exchange,
  no file ever changes quietly because something elsewhere did.
- **Changes to the production file go through code review.** The Git diff is the review: which environment changed, and
  how, at a glance.
- **One file per cluster.** To deploy to another cluster, copy the file and change `k8s.context`; there is no flag for
  switching clusters on the command line.
- **Changes to the component list start in `brickkit.yaml`.** `add` / `remove` / `upgrade` maintain `deploy.yaml` (and
  `deploy.local.yaml` when it exists) as well; other deploy files are yours to bring along — the commands name the ones
  that haven't caught up at the end of their output, and `lint -f <file>` reports them too.
