# Troubleshooting

The failure modes people actually hit around `brickkit up`/`down`, local debugging, signature verification and the offline checks (`brickkit lint` and the editor schemas). If your problem isn't here, check the relevant command's `--help` first, or the [architecture docs](06-architecture/00-overview.md) for how that mechanism is actually designed.

If you're looking at a `❌` block rather than a symptom, the `error_code` in the JSON log line printed right after it is the index into [Error codes](06-architecture/10-error-codes.md), which covers every code and the situations behind each.

**How to use this page:** find your symptom in the tables below and click its name to jump to the numbered write-up. Every entry is "symptom → cause → fix".

### A. `brickkit up` fails

| Symptom | Cause in one line | The fix |
| --- | --- | --- |
| [1. Image not found](#1-image-not-found)<br>`No such image` / `pull access denied` | The image isn't built yet, or the registry isn't reachable | `docker build` it; check `deployment.image`; `docker login` for a private registry |
| [2. Two components claim the same host port](#2-two-components-claim-the-same-host-port)<br>Fails at generation time | Two `expose: true` components ended up on one host port | Give one a different `exposePort`, or drop its `expose` |
| [3. Migration fails](#3-migration-fails)<br>The main service never starts | The database is unreachable, or the migration script has a bug | Read the migration container / Job logs, fix, re-run `up` |
| [4. Can't reach a dependency](#4-cant-reach-a-dependency)<br>`connection refused` / `no such host` | The dependency isn't running, or was never declared | `brickkit status` to check it; read a weak dependency with `.get()` |
| [5. A required component setting has no value](#5-a-required-component-setting-has-no-value)<br>`up` is stopped | A component marks a setting required, with no default, and the project doesn't set it | Set it under that component's `config` in `brickkit.yaml` |
| [6. An environment variable referenced in brickkit.yaml isn't defined](#6-an-environment-variable-referenced-in-brickkityaml-isnt-defined)<br>`up` is stopped | A `${VAR}` can't be resolved to a value | Define it in `.env`, `export` it, or give a default `${VAR:-dev}` |

### B. `up` worked, but a component misbehaves

| Symptom | Cause in one line | The fix |
| --- | --- | --- |
| [7. Stays unhealthy while the component's log says it's ready](#7-stays-unhealthy-while-the-components-log-says-its-ready) | The image lacks the probe command, or startup outlasts the grace period | Install `wget` / `curl`; or raise `healthCheck.startPeriodSeconds` |
| [8. You changed config and nothing happened](#8-you-changed-config-and-nothing-happened) | A misspelled key wasn't injected, so the component used its default | Read the warning from `up`, use the key in `configSchema`; restart to apply |
| [9. `docker compose logs` shows nothing](#9-docker-compose-logs-shows-nothing) | No project name, so Compose looks at a different project | Add `-p brickkit-<project>` |

### C. Kubernetes

| Symptom | Cause in one line | The fix |
| --- | --- | --- |
| [10. The network policy was generated but traffic isn't blocked](#10-the-network-policy-was-generated-but-traffic-isnt-blocked) | The cluster's network plugin (CNI) doesn't enforce NetworkPolicy | Switch to a CNI that does, then check what is blocked and what isn't |

### D. After `brickkit down`

| Symptom | Cause in one line | The fix |
| --- | --- | --- |
| [11. Volumes are still there](#11-volumes-are-still-there) | `down` never deletes volumes, to protect your data | `docker volume rm` when you actually want them gone |
| [12. K8s namespace is still there](#12-k8s-namespace-is-still-there) | `createNamespace: false`: BrickKit didn't create it, so it won't delete it | Set it back to `true`, or `kubectl delete` it yourself |

### E. Local debug mode (`local: true`)

| Symptom | Cause in one line | The fix |
| --- | --- | --- |
| [13. `extra_hosts` doesn't resolve](#13-extra_hosts-doesnt-resolve) | Docker is too old to support `host-gateway` | Upgrade to 20.10+ |
| [14. Port mismatch](#14-port-mismatch) | The local process listens on a different port than `localPort` | Make the two agree |
| [15. Migrations never ran](#15-migrations-never-ran)<br>`relation does not exist` | A `local: true` component gets no migration container | Run the migration yourself once, before the first run |

### F. Signature verification fails

| Symptom | Cause in one line | The fix |
| --- | --- | --- |
| [16. Wrong public key](#16-wrong-public-key)<br>`brickkit add` reports a signature failure | The key in `installer.publicKeys` doesn't match the publisher's private key | Confirm the right key with the publisher, update the path |

### G. `brickkit lint` and editor schemas

| Symptom | Cause in one line | The fix |
| --- | --- | --- |
| [17. `brickkit lint` says everything is fine, but `up` fails](#17-brickkit-lint-says-everything-is-fine-but-up-fails)<br>`错误：强依赖缺失` | `lint` checks each file on its own; whether a dependency or a `servedBy` target exists needs the whole graph | `brickkit up --dry-run` (or `brickkit graph`) resolves the graph and names what's missing |
| [18. The editor underlines almost every field of a valid `component.yaml`](#18-the-editor-underlines-almost-every-field-of-a-valid-componentyaml)<br>`Property apiVersion is not allowed.` | No BrickKit schema is attached, so the editor applies another tool's schema to that file name | Attach the schema with a `$schema` comment or the `yaml.schemas` setting |
| [19. The editor underlines something the CLI accepts](#19-the-editor-underlines-something-the-cli-accepts) | The schemas are stricter than the CLI in three deliberate places | Write the literal value, quote the number, or fix the key |

---

### 1. Image not found

- **Symptom:** `Error: No such image: xxx` or `Error: pull access denied`
- **Cause:** the component's image hasn't been built yet, or the registry `deployment.image` points at isn't reachable from here.
- **Fix:**
  - For local test components, build the image yourself first (a published component's image is always already in a registry — the CLI never builds anything for you).
  - Double-check `deployment.image`'s spelling and tag.
  - Private registries need `docker login` first.
- **Code:** `IMAGE_UNAUTHORIZED` — the CLI usually translates Docker's raw text into `错误：镜像不存在` or `错误：镜像拉取未授权`; see [Error codes](06-architecture/10-error-codes.md#image_unauthorized).

---

### 2. Two components claim the same host port

- **Symptom:** `brickkit up` (or `--dry-run`) fails immediately at generation time. What it prints depends on whether the components write `exposePort` themselves.
- **Cause:** two `expose: true` components ended up on the same host port. Either way it is caught while the CLI is still generating, before Docker ever starts a second container on the same port — which is why you won't actually see Docker's own "port is already allocated".
- **Fix:** give one of the components an explicit `exposePort: <different port>`, or drop `expose: true` from one of them (components reach each other over the container network without needing exposure at all).
- **Code:** `PORT_CONFLICT` if neither writes `exposePort`, `CONFIG_INVALID` if both write the same explicit one; see [Error codes](06-architecture/10-error-codes.md).

If neither writes `exposePort`, both default to their own `deployment.port`:

```
❌ Error: host port 8080 is claimed by more than one component
   Component: demo/caller@1.0.0
   Component: demo/hello@1.0.0
   Host port: 8080
   Suggestions:
   1. In brickkit.yaml, give one of the components a different exposePort
   2. Or remove expose: true from one of them (components reach each other inside the container network without expose)
```

If both write the same explicit `exposePort`:

```
❌ Error: brickkit.yaml failed validation
   File: brickkit.yaml
   components[1].exposePort: conflicts with components[0].exposePort (host port 9000 is already taken)
```

If you're seeing Docker's raw `Error: port is already allocated` instead of a BrickKit error like the two above, something outside BrickKit's own components is holding that host port — check with `lsof -i :<port>` or `docker ps`.

---

### 3. Migration fails

- **Symptom:** the migration Job/container fails and the main service never starts.
- **Cause:** a database connection problem, or a bug in the migration script itself.
- **Fix:**
  - Docker: `docker logs <migration-container>`.
  - K8s: `kubectl logs job/<migration-job>`.
  - Fix the script and re-run `brickkit up`.
  - On K8s you don't need to delete the old Job by hand first — the CLI already does that for you, the equivalent of `kubectl delete job --ignore-not-found` before every migration.
- **Code:** `MIGRATION_FAILED` on Kubernetes; on Docker the same failure surfaces as `ENGINE_FAILED` — see [Error codes](06-architecture/10-error-codes.md#migration_failed).

---

### 4. Can't reach a dependency

- **Symptom:** `connection refused` or `no such host` when a component calls one of its dependencies.
- **Cause:** the dependency isn't running, or it was never declared in `brickkit.yaml` / `component.yaml`.
- **Fix:**
  - `brickkit status` to check whether the dependency is `healthy`.
  - Verify the dependency declaration.
  - If it's an optional dependency: the component's code must read it with `os.environ.get()`, never `os.environ["X"]` — a missing optional dependency means that environment variable is never injected at all, and the bracket form crashes with a `KeyError`. This is deliberate, not a bug (see AGENTS.md §9.13). [Guide 2](03-guide/02-what-runs.md) shows the real warning `brickkit up` prints for a missing optional dependency.

---

### 5. A required component setting has no value

- **Symptom:** `brickkit up` refuses to generate anything and reports `错误：必填的组件配置没有值`.
- **Cause:** a component lists a setting in `configSchema.required` with no default, and the project doesn't set it. The platform can't derive it (the address of a service in another project, say), so the project has to supply it. Letting it through would mean the component starts, looks completely healthy, and has one call path that quietly never works — so the platform stops here instead.
- **Fix:** set it under that component's `config` in `brickkit.yaml`; the value may be `${ENV_VAR}`, with the real value in `.env`.
- **Code:** `CONFIG_INVALID`; see [Error codes](06-architecture/10-error-codes.md#config_invalid).

The real error block (the missing setting is `pricingServiceUrl` of `shop/pricing`):

```
❌ Error: a required component config item has no value
   Missing config: shop/pricing@1.0.0 → pricingServiceUrl (injected as PRICING_SERVICE_URL)
   Reason: The component declares it in configSchema.required without a default — the platform can't derive this one, so the project has to supply it
   Suggestions:
   1. Give it a value in brickkit.yaml:
    components:
      - id: shop/pricing
        config:
          pricingServiceUrl: <值>
   2. The value may be ${ENV_VAR}; keep the real value in .env
```

---

### 6. An environment variable referenced in brickkit.yaml isn't defined

- **Symptom:** `brickkit up` is stopped with `错误：brickkit.yaml 里引用的环境变量没有定义`.
- **Cause:** some `${VAR}` in `brickkit.yaml` can't be resolved to a value. Kubernetes manifests can't defer the substitution to runtime, so it has to be resolved at generation time.
- **Fix:** define it in the project root's `.env`, `export` it, or give a default: `${VAR:-dev}`.
- **Code:** `CONFIG_INVALID`; see [Error codes](06-architecture/10-error-codes.md#config_invalid).

---

### 7. Stays unhealthy while the component's log says it's ready

- **Symptom:** `brickkit up` reports `错误：部分组件没有正常启动` (under Docker, `up -d --wait` fails as soon as it sees `unhealthy`), or `brickkit status` keeps showing `unhealthy` / `starting`. Yet the component's own log is fine all the way, and its last line is often exactly "service started".
- **Cause:** usually one of two things:
  - **The image lacks the probe command.** The health check runs **inside the container**: an HTTP check runs `wget -q --spider <url> || curl -fsS <url>`, and a TCP check runs `nc -z localhost <port>`. A slim image such as `python:slim` or any distroless one often has none of them, so the probe fails forever even though the component is fine.
  - **Startup outlasts the grace period.** `healthCheck.startPeriodSeconds` defaults to 60 seconds. A Spring Boot cold start, a Django with heavy preloading, or a .NET first JIT can exceed it. Under Docker, `up -d --wait` fails outright; under Kubernetes the livenessProbe kills the Pod, it restarts and spends the same time again — a permanent CrashLoopBackOff — while the container log looks normal throughout.
- **Fix:**
  - Start with `brickkit status` to see the state.
  - Then read the container log and the output of the last few probes: `docker inspect --format '{{json .State.Health}}' <container>`.
  - Missing command: put `wget` or `curl` into the image (HTTP check), or `nc` (TCP check).
  - Slow start: raise `healthCheck.startPeriodSeconds` above the real cold start. The grace period only delays "declaring it dead", never "declaring it alive" — a component that's ready in two seconds still turns healthy in two seconds — so being generous costs nothing.
  - `/healthz` must check only the process itself, never the database or another dependency: otherwise one dependency wobbling gets every upstream marked unhealthy and restarted together.
  - The field's full constraints are in the [component.yaml field reference](06-architecture/07-component-yaml-reference.md); how each engine generates its probes is in [Deployment generation](06-architecture/03-deployment-generation.md).

---

### 8. You changed config and nothing happened

- **Symptom:** you changed a component's `config` in `brickkit.yaml`, re-ran `up`, and the component behaves exactly as before.
- **Cause:** most often a **misspelled key**: the component's `configSchema` has no such key, so nothing is injected for it, and the component runs on its own default with no error at all. The CLI warns and guesses which key you meant. If the component declares no `configSchema` at all, the whole `config` block has no effect, and that warns too.
- **Fix:**
  - Read the warning from `brickkit up` (or `--dry-run`) and change the key to the one in `configSchema`.
  - After the change, `brickkit up` restarts the component so it takes effect: configuration is injected as environment variables, and there is no hot reload.
  - The full set of warnings is in section 5 of the [environment variable contract](06-architecture/04-environment-variables.md).

The CLI's real warning when `greeting` is written `greetting`:

```
⚠️ A config item won't take effect: greetting on component demo/hello
   Config item: greetting
   Reason: The component's configSchema has no such item; did you mean greeting?
   Impact: This item is not injected as any environment variable; the component uses its own default
```

---

### 9. `docker compose logs` shows nothing

- **Symptom:** the components are running, yet `docker compose logs` prints nothing.
- **Cause:** the Compose project BrickKit starts is named `brickkit-<project>`. Without `-p`, Compose is looking at a different (the default) project.
- **Fix:** add the project name: `docker compose -p brickkit-<project> logs` (the project name is `project` in `brickkit.yaml`). You can also use `docker logs <container>` directly.

---

### 10. The network policy was generated but traffic isn't blocked

- **Symptom:** after `deploy.networkPolicy.enabled: true`, `brickkit up` reports "generated N NetworkPolicy" with a warning, and `kubectl get networkpolicy` shows them — yet an unauthorized connection still gets through, with no error.
- **Cause:** the cluster's network plugin (CNI) doesn't enforce NetworkPolicy. Many clusters accept the objects and enforce none of them; the **default** CNI of minikube and kind is one of these. Kubernetes has no API to ask, so the platform can't tell, and `up` warns you every time to check it yourself once.
- **Fix:**
  - Switch to a CNI that enforces NetworkPolicy ([guide 12](03-guide/12-network-policy.md) uses `minikube start --cni=calico`).
  - Then verify as the guide does: an authorized path still works, and an unauthorized one really is blocked.

The warning `brickkit up` prints:

```
🔒 Generated 2 NetworkPolicy manifests (deploy.networkPolicy.enabled: true)
   ⚠️ They only take effect when the cluster's CNI enforces them. When it doesn't: apply succeeds,
      kubectl get networkpolicy shows them, yet traffic is not restricted at all — with no error whatsoever.
      The **default** CNI of minikube / kind is exactly this kind.
   平台测不出来（K8s 没有这个 API），只能你自己验一次
```

---

### 11. Volumes are still there

- **Symptom:** database data survives across a `brickkit down` + `brickkit up`.
- **Cause:** `down` never deletes volumes on its own — that's protecting your data, not an oversight.
- **Fix:** `docker volume rm <volume>` when you actually want it gone, or `docker compose -p brickkit-<project> down -v`.

---

### 12. K8s namespace is still there

- **Symptom:** the namespace survives a `brickkit down`.
- **Cause:** most likely `deploy.createNamespace` is `false` in `brickkit.yaml` — that means the namespace was created by ops, not by BrickKit, so `down` deliberately leaves it alone (not having created it is reason enough not to delete it). This isn't a failure, it's the designed behavior.
- **Fix:** set `createNamespace: true` (the default) if you actually want BrickKit managing that namespace's lifecycle. A manually-created namespace you're done with needs `kubectl delete namespace <name>` yourself.

---

### 13. `extra_hosts` doesn't resolve

- **Symptom:** a container can't resolve the local component's service name.
- **Cause:** Docker is too old to support `host-gateway`.
- **Fix:** upgrade Docker to 20.10+.

---

### 14. Port mismatch

- **Symptom:** other containers call `demo-hello-1-0-0:8080`, but your local process is actually listening on a different port, and callers keep getting a 503.
- **Cause:** the component's `localPort` in `brickkit.yaml` doesn't match what the local process is really bound to.
- **Fix:** make the two agree.

---

### 15. Migrations never ran

- **Symptom:** a `local: true` component errors with something like `relation does not exist`.
- **Cause:** `local: true` components don't get a migration container/Job at all — it runs in your own IDE, outside anything the CLI manages.
- **Fix:** run the migration script yourself once, before the first run.

---

### 16. Wrong public key

- **Symptom:** `brickkit add` reports a signature verification failure.
- **Cause:** the public key `installer.publicKeys` points at doesn't match the private key the publisher actually signed with.
- **Fix:** confirm the correct public key with the publisher (usually a file like `<component>-release.pub`) and update the path in `installer.publicKeys`.
- **Worth knowing:** `publicKeys` is the only field that actually makes verification take effect — with zero keys configured, `requireSignature: true` does nothing at all (the CLI warns once, but it won't supply a trust anchor for you).
- **Code:** `SIGNATURE_INVALID` — see [Error codes](06-architecture/10-error-codes.md#signature_invalid).

---

### 17. `brickkit lint` says everything is fine, but `up` fails

- **Symptom:** `brickkit lint` prints a `✅` for every file and exits `0`, then `brickkit up` (or `--dry-run`) stops with `错误：强依赖缺失` — or with `错误：servedBy 指向的组件不存在`.
- **Cause:** `lint` checks each file's own structure and nothing else. Whether a dependency can be found in some source, or a `servedBy` target exists, depends on the rest of the project's Manifests (over the network, for market and Git components), so `lint` never looks — it would stop being offline. A clean `lint` means every YAML file is well-formed, not that the project will start.
- **Fix:** run `brickkit up --dry-run`. It resolves the dependency graph and names the component, the dependency it can't find and the sources it tried. Add the missing component with `brickkit add`, or correct the ID or version in the declaration. (`brickkit graph` resolves the same graph and stops on a missing dependency too; for a missing `servedBy` target it draws the group as declared and leaves the report to `up`.)
- **Code:** `DEPENDENCY_MISSING` (`CONFIG_INVALID` for a missing `servedBy` target); see [Error codes](06-architecture/10-error-codes.md#dependency_missing).

Here `department/tree`, which `people/basic` needs, is in no source:

```
$ brickkit lint
✅ brickkit.yaml
✅ components/people/basic/component.yaml

📋 Checked 2 files: 0 with errors, 0 warnings
$ brickkit up --dry-run
🚀 Starting project demo-shop (deploy.target: docker)
❌ Error: required dependency missing
   Component: people/basic@1.0.0
   Missing dependency: department/tree@1.0.0
   Reason: The component was not found in any install source
   已尝试的安装源：local-dev（local）
   Suggestions:
   1. Check the install source configuration (brickkit.yaml → sources)
   2. Confirm the component has been published to the Market
   3. Confirm the version number is correct
```

---

### 18. The editor underlines almost every field of a valid `component.yaml`

- **Symptom:** you open a `component.yaml` that `brickkit lint` accepts, and the editor draws red lines nearly everywhere: `Property apiVersion is not allowed.` on `apiVersion`, `Missing property "implementation".` at the top of the file.
- **Cause:** no BrickKit schema is attached, so the YAML language server falls back to SchemaStore, a public catalog of schemas. As of yaml-language-server 1.24.0 and the catalog at the time of writing, it maps the file name `component.yaml` to Kubeflow Pipelines' schema (both are third-party and can change). Those messages are about Kubeflow's fields, not BrickKit's. (The same check found nothing in the catalog for `brickkit.yaml`, so that file simply gets no checking until you attach the schema.)
- **Fix:** attach the BrickKit schema — a `# yaml-language-server: $schema=…` comment on the file's first line, or a `yaml.schemas` entry mapping `component.yaml` to it. Either replaces the catalog's guess; both are in [Wire up your editor](00-quick-start.md#wire-up-your-editor).

---

### 19. The editor underlines something the CLI accepts

- **Symptom:** a red line in `brickkit.yaml` or `component.yaml`, yet `brickkit lint` and `brickkit up` run without complaint.
- **Cause:** the schemas are stricter than the CLI in three deliberate places:
  - a `${VAR}` in a field with a closed set of values (`deploy.target: ${TARGET}`; also `sources[].type` and `resources[].kind`) — the CLI expands the variable first, the schema checks the literal text;
  - YAML the CLI reads loosely — an unquoted number in a string field (`project: 2024`), `yes` or `on` for a boolean, a `null` list item or map value, a fraction in an integer field (`port: 5432.5`);
  - an extra key inside a `configSchema` property (`defualt` for `default`) — the CLI ignores it, and `brickkit lint` warns that it won't take effect.
- **Fix:** treat the red line as a hint, not a bug: write the literal value, quote the number, use `true` / `false`, or correct the key. Each case is explained, with its reason, in the last list of [Wire up your editor](00-quick-start.md#wire-up-your-editor).

---

## Read further

- [Error codes](06-architecture/10-error-codes.md)
- [Signing and trust](06-architecture/06-signing-and-trust.md)
- [Dependency resolution](06-architecture/02-dependency-resolution.md)
- [Deployment generation](06-architecture/03-deployment-generation.md)
