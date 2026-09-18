# Troubleshooting

The failure modes people actually hit around `brickkit up`/`down` and signature verification. If your problem isn't here, check the relevant command's `--help` first, or the [architecture docs](architecture/overview.md) for how that mechanism is actually designed.

If you're looking at a `❌` block rather than a symptom, the `error_code` in the JSON log line printed right after it is the index into [Error codes](architecture/error-codes.md), which covers every code and the situations behind each.

## `brickkit up` fails

### Image not found

**Symptom:** `Error: No such image: xxx` or `Error: pull access denied`

**Cause:** the component's image hasn't been built yet, or the registry `deployment.image` points at isn't reachable from here.

**Fix:**
- For local test components, build the image yourself first (a published component's image is always already in a registry — the CLI never builds anything for you)
- Double-check `deployment.image`'s spelling and tag
- Private registries need `docker login` first

**Code:** `IMAGE_UNAUTHORIZED` — the CLI usually translates Docker's raw text into `错误：镜像不存在` or `错误：镜像拉取未授权`; see [Error codes](architecture/error-codes.md#image_unauthorized).

### Two components claim the same host port

**Symptom:** `brickkit up` (or `--dry-run`) fails immediately at generation time. What it prints depends on whether the components write `exposePort` themselves. If neither does, both default to their own `deployment.port`:

```
❌ 错误：宿主机端口 8080 被多个组件占用
   组件：demo/caller@1.0.0
   组件：demo/hello@1.0.0
   宿主机端口：8080
   建议：
   1. 在 brickkit.yaml 中给其中一个组件设置不同的 exposePort
   2. 或去掉其中一个组件的 expose: true（组件之间在容器网络内互访不需要 expose）
```

If both write the same explicit `exposePort`:

```
❌ 错误：brickkit.yaml 校验失败
   文件：brickkit.yaml
   components[1].exposePort：与 components[0].exposePort 冲突（宿主机端口 9000 已被占用）
```

**Cause:** two `expose: true` components ended up on the same host port. Either way it is caught while the CLI is still generating, before Docker ever starts a second container on the same port — which is why you won't actually see Docker's own "port is already allocated". The first form is `PORT_CONFLICT`, the second `CONFIG_INVALID`; see [Error codes](architecture/error-codes.md).

**Fix:** give one of the components an explicit `exposePort: <different port>`, or drop `expose: true` from one of them (components reach each other over the container network without needing exposure at all).

If you're seeing Docker's raw `Error: port is already allocated` instead of the BrickKit error above, something outside BrickKit's own components is holding that host port — check with `lsof -i :<port>` or `docker ps`.

### Migration fails

**Symptom:** the migration Job/container fails and the main service never starts

**Cause:** a database connection problem, or a bug in the migration script itself

**Fix:**
- Docker: `docker logs <migration-container>`
- K8s: `kubectl logs job/<migration-job>`
- Fix the script and re-run `brickkit up`
- On K8s you don't need to delete the old Job by hand first — the CLI already does that for you, equivalent to running `kubectl delete job --ignore-not-found` before every migration

**Code:** `MIGRATION_FAILED` on Kubernetes; on Docker the same failure surfaces as `ENGINE_FAILED` — see [Error codes](architecture/error-codes.md#migration_failed).

### Can't reach a dependency

**Symptom:** `connection refused` or `no such host` when a component calls one of its dependencies

**Cause:** the dependency isn't running, or it was never declared in `brickkit.yaml`/`component.yaml`

**Fix:**
- `brickkit status` to check whether the dependency is `healthy`
- Verify the dependency declaration
- If it's an optional dependency: the component's code must read it with `os.environ.get()`, never `os.environ["X"]` — a missing optional dependency means that environment variable is never injected at all, and the bracket form crashes with a `KeyError` (this is deliberate, not a bug — see AGENTS.md §9.13). [Guide 2](guide/02-what-runs.md) shows the real warning `brickkit up` prints for a missing optional dependency.

## After `brickkit down`

### Volumes are still there

**Symptom:** database data survives across a `brickkit down` + `brickkit up`

**Cause:** `down` never deletes volumes on its own — that's protecting your data, not an oversight

**Fix:** `docker volume rm <volume>` when you actually want it gone, or `docker compose -p brickkit-<project> down -v`

### K8s namespace is still there

**Symptom:** the namespace survives a `brickkit down`

**Cause:** most likely `deploy.createNamespace` is `false` in `brickkit.yaml` — that means the namespace was created by ops, not by BrickKit, so `down` deliberately leaves it alone (not having created it is reason enough not to delete it). This isn't a failure, it's the designed behavior.

**Fix:** set `createNamespace: true` (the default) if you actually want BrickKit managing that namespace's lifecycle. A manually-created namespace you're done with needs `kubectl delete namespace <name>` yourself.

## Local debug mode (`local: true`)

### `extra_hosts` doesn't resolve the local component's service name

**Cause:** Docker is too old to support `host-gateway`

**Fix:** upgrade Docker to 20.10+

### Port mismatch

**Symptom:** other containers call `demo-hello-1-0-0:8080`, but your local process is actually listening on a different port

**Fix:** check that the component's `localPort` in `brickkit.yaml` matches what the local process is really bound to

### Migrations never ran

**Symptom:** a `local: true` component errors with something like `relation does not exist`

**Cause:** `local: true` components don't get a migration container/Job at all — it runs in your own IDE, outside anything the CLI manages

**Fix:** run the migration script yourself once, before the first run

## Signature verification fails (`brickkit add`)

### Wrong public key

**Symptom:** `brickkit add` reports a signature verification failure

**Cause:** the public key `installer.publicKeys` points at doesn't match the private key the publisher actually signed with

**Fix:** confirm the correct public key with the publisher (usually a file like `<component>-release.pub`) and update the path in `installer.publicKeys`

**Worth knowing:** `publicKeys` is the only field that actually makes verification take effect — with zero keys configured, `requireSignature: true` does nothing at all (the CLI warns once, but it won't supply a trust anchor for you).

**Code:** `SIGNATURE_INVALID` — see [Error codes](architecture/error-codes.md#signature_invalid).

## Read further

- [Error codes](architecture/error-codes.md)
- [Signing and trust](architecture/signing-and-trust.md)
- [Dependency resolution](architecture/dependency-resolution.md)
- [Deployment generation](architecture/deployment-generation.md)
