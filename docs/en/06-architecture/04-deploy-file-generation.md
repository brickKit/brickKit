# Generating deployment files

A component's repository never carries deployment files. On every `up` (and `up --dry-run`) the CLI regenerates them,
following the deploy file's `target`, from the three layers and each component's `component.yaml`:

| `target` | Generates | Where |
| --- | --- | --- |
| `docker`, `podman` | One `compose.yaml` | `.brickkit/generated/compose.yaml` |
| `k8s` | A set of Kubernetes manifests | `.brickkit/generated/k8s/` |

They are generated files: don't edit them by hand, the next `up` overwrites them. To change something, change the three
layers.

Both examples below come from the same project: `demo/caller` requires `demo/hello`, optionally depends on `demo/bus`,
declares a database migration of its own, and is exposed to the outside.

## Docker Compose

```yaml
# deploy.yaml
target: docker

components:
  - id: demo/hello
  - id: demo/bus
  - id: demo/caller
    expose: true
    exposePort: 18080
```

The generated `compose.yaml` (an excerpt: `demo/caller`, its migration, `demo/hello`):

```yaml
  demo-caller-1-0-0:
    depends_on:
      demo-caller-1-0-0-migration:
        condition: service_completed_successfully
      demo-hello-1-0-0:
        condition: service_healthy
    deploy:
      resources:
        reservations:
          cpus: "0.05"
          memory: 32M
    environment:
      - COMPONENT_ID=demo/caller
      - COMPONENT_VERSION=1.0.0
      - DATABASE_PORT=5432
      - DEMO_BUS_ENDPOINT=http://demo-bus-1-0-0:8080
      - DEMO_HELLO_ENDPOINT=http://demo-hello-1-0-0:8080
    healthcheck:
      interval: 10s
      retries: 3
      start_period: 60s
      test:
        - CMD-SHELL
        - wget -q --spider http://localhost:8080/healthz || curl -fsS http://localhost:8080/healthz || exit 1
      timeout: 3s
    image: demo-caller:1.0.0
    networks:
      - brickkit-net
    ports:
      - 18080:8080
    restart: unless-stopped
  demo-caller-1-0-0-migration:
    command:
      - migrate
    entrypoint:
      - /app/caller
    environment:
      - COMPONENT_ID=demo/caller
      - COMPONENT_VERSION=1.0.0
      - DATABASE_PORT=5432
      - DEMO_BUS_ENDPOINT=http://demo-bus-1-0-0:8080
      - DEMO_HELLO_ENDPOINT=http://demo-hello-1-0-0:8080
    image: demo-caller:1.0.0
    networks:
      - brickkit-net
    restart: "no"
  demo-hello-1-0-0:
    environment:
      - COMPONENT_ID=demo/hello
      - COMPONENT_VERSION=1.0.0
      - GREETING=Hello
    healthcheck:
      interval: 10s
      retries: 3
      start_period: 60s
      test:
        - CMD-SHELL
        - wget -q --spider http://localhost:8080/healthz || curl -fsS http://localhost:8080/healthz || exit 1
      timeout: 3s
    image: demo-hello:1.0.0
    networks:
      - brickkit-net
    restart: unless-stopped
```

Item by item:

- **The service name is the versioned service name**: `demo-caller-1-0-0`. Another version of the same component is
  another service, running alongside.
- **`depends_on`**: a required dependency is waited for until it's **healthy** (`service_healthy`), the component's own
  migration until it **finished successfully** (`service_completed_successfully`). The optional dependency `demo/bus`
  isn't in it — it may not start at all, and listing it would hold up the whole project.
- **Environment variables**: platform variables, dependency addresses, config values (`DATABASE_PORT` is the
  `configSchema` default, and so is `GREETING`). Secrets aren't here; they are in the 0600 file `env_file` references.
- **The migration container**: the same image, the same environment, `restart: "no"`, with the entry point replaced by
  the migration command.
- **The health check**: its rhythm is fixed by the platform (every 10 seconds, a 3-second timeout, 3 failures in a row);
  `start_period` comes from `startPeriodSeconds` (60 by default). Both `wget` and `curl` are tried, so having either in the
  image is enough.
- **Ports**: only `expose: true` components map a host port; the rest are reachable only inside the project's network.
- **Resources**: `requests` become `reservations`; when nobody writes `limits`, no ceiling is generated.
- **The network**: one network per project, named `brickkit-<project name>-net`; the Compose project name is
  `brickkit-<project name>`.
  When containers outside the project (your own database, an IdP, a gateway's compose) must share a network with the
  components — an IdP calling a component back by service name, say — don't imitate this network's internal labels; let
  the project own the network instead: create it first and write `network: <name>` in the deploy file. The generated
  compose marks it `external`, joining it without creating or removing it; `up` checks it exists first and, if it
  doesn't, stops with the `docker network create` command. The outside compose declares it `external` as well, so it
  doesn't matter which side starts first or goes `down` first:

  ```bash
  docker network create shop-net
  ```

  ```yaml
  # deploy.yaml
  network: shop-net
  ```

  ```yaml
  # your own infra/compose.yaml
  networks:
    default:
      name: shop-net
      external: true
  ```

What a shell looks like in Compose (network aliases, `BRICKKIT_SERVED_MEMBERS`, members' migrations) is in
[Writing a shell](../04-shell/05-shell-development.md#how-addresses-are-pointed-at-it).

## Kubernetes

```yaml
# deploy.k8s.yaml
target: k8s
components:
  - id: demo/hello
    replicas: 2
  - id: demo/bus
  - id: demo/caller
    expose: true
    hostname: shop.example.com
```

```text
📄 Generated 10 manifests: .brickkit/generated/k8s/
   Namespace: brickkit-my-shop
```

```text
k8s/
├── namespace.yaml
├── deployments/      demo-bus-1-0-0.yaml  demo-caller-1-0-0.yaml  demo-hello-1-0-0.yaml
├── services/         demo-bus-1-0-0.yaml  demo-caller-1-0-0.yaml  demo-hello-1-0-0.yaml
├── ingress/          demo-caller-1-0-0.yaml
├── migrations/       demo-caller-1-0-0-migration.yaml
└── poddisruptionbudgets/  demo-hello-1-0-0.yaml
```

**Deployment** (`demo/hello`, an excerpt of the container part):

```yaml
      containers:
        - env:
            - name: COMPONENT_ID
              value: demo/hello
            - name: COMPONENT_VERSION
              value: 1.0.0
            - name: GREETING
              value: Hello
          image: demo-hello:1.0.0
          livenessProbe:
            failureThreshold: 3
            httpGet:
              path: /healthz
              port: 8080
            initialDelaySeconds: 10
            periodSeconds: 10
            timeoutSeconds: 3
          name: demo-hello
          ports:
            - containerPort: 8080
              name: http
          readinessProbe:
            failureThreshold: 3
            httpGet:
              path: /healthz
              port: 8080
            initialDelaySeconds: 5
            periodSeconds: 5
            timeoutSeconds: 3
          resources:
            requests:
              cpu: 50m
              memory: 32Mi
          startupProbe:
            failureThreshold: 12
            httpGet:
              path: /healthz
              port: 8080
            periodSeconds: 5
            timeoutSeconds: 3
```

**Three probes**: `startupProbe` leaves a grace period for a cold start (12 × 5 seconds = 60 seconds, from
`startPeriodSeconds`), and the other two probes don't begin until it passes; `readinessProbe` decides whether traffic
comes; `livenessProbe` decides whether to restart. All three hit the component's health check path — which should only
check that this process is alive.

**Service**: its name is the versioned service name, so `http://demo-hello-1-0-0:8080` is the same string on Kubernetes
as on Docker.

```yaml
kind: Service
metadata:
  name: demo-hello-1-0-0
spec:
  ports:
    - name: http
      port: 8080
      targetPort: 8080
  selector:
    app: demo-hello-1-0-0
  type: ClusterIP
```

**Ingress**: only for `expose: true` components, and `hostname` is required. When the entry has `paths`, the rules
carry those prefixes instead of `/` — that is how several components
[share one domain](../01-three-layers/03-deploy-yaml.md#several-components-on-one-domain).

```yaml
kind: Ingress
spec:
  rules:
    - host: shop.example.com
      http:
        paths:
          - backend:
              service:
                name: demo-caller-1-0-0
                port:
                  number: 8080
            path: /
            pathType: Prefix
```

**Migration Job**: `backoffLimit: 0`, no retry on failure; `up` first deletes the same-named Job from last time, then
creates it and waits for it to complete, and only then `apply`s the main services.

```yaml
kind: Job
spec:
  backoffLimit: 0
  template:
    spec:
      containers:
        - command:
            - /app/caller
            - migrate
          image: demo-caller:1.0.0
          name: demo-caller-migration
      restartPolicy: Never
```

**PodDisruptionBudget**: generated automatically when `replicas` is above 1 (`maxUnavailable: 1`), so node maintenance
never evicts every replica at once.

A few more things are generated depending on the deploy file's settings:

| Setting | Generates |
| --- | --- |
| `secret: true` config, the contents of `file://` | A Secret (file mode 0600), referenced by the Deployment through `secretKeyRef` (see [Sensitive values](../01-three-layers/07-sensitive-values.md)) |
| `k8s.networkPolicy.enabled: true` | One NetworkPolicy per component, letting through what the dependency graph allows (see [Security and signing](08-security-and-signing.md)) |
| `k8s.serviceAccount.enabled: true` | One ServiceAccount per component, with no token mounted |
| `k8s.podSecurity: restricted` | A `securityContext` on every container |

## The one thing both sides share

Docker's Compose file and Kubernetes' manifests have hardly a line in common — but **what components see is exactly the
same**: the same environment variable names, the same values, dependency addresses in the same format
`http://<versioned service name>:<port>`. So switching the deploy target is switching one deploy file, without a line of
component code changing.
