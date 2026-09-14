# 4. Deploy to Kubernetes

Same Manifest, same `brickkit.yaml` structure, same dependency graph — the only thing that changes is `deploy.target: k8s` (AGENTS.md §5.5). This article deploys the `demo/hello` + `demo/caller` pair from [Article 2](02-what-runs.md) to a real cluster (minikube), start to finish, including finally binding the real database `demo/caller` has been asking for since that article — and a real, load-bearing gotcha discovered by actually running `brickkit down` afterward, not by reading the source.

**Prerequisites:** a running cluster and `kubectl` — `minikube start`, and `minikube kubectl --` if you don't have a standalone `kubectl` (or symlink minikube's cached one onto your `PATH`, as its own first-run message suggests). Load the two images built in earlier articles into the cluster's own image store, since minikube doesn't share your host's Docker daemon:

```bash
minikube image load brickkit-demo/hello:1.0.0
minikube image load brickkit-demo/caller:1.0.0
```

## Deploy a real database — in its own namespace

`demo/caller` has needed a `kind: database` resource since Article 2. Resources are deployed by ops, not by BrickKit (AGENTS.md §2.1) — here, that's a couple of `kubectl` commands standing in for whatever your real infrastructure team would run:

```bash
kubectl create namespace guide-resources
kubectl -n guide-resources create deployment guide-pg --image=postgres:16-alpine --port=5432
kubectl -n guide-resources set env deployment/guide-pg POSTGRES_PASSWORD=devpass POSTGRES_DB=callerdb
kubectl -n guide-resources expose deployment guide-pg --port=5432 --target-port=5432
```

Putting this in its own namespace (`guide-resources`, not the one BrickKit is about to manage) isn't a stylistic choice — the section on `brickkit down` below explains exactly why it matters.

## The project config

Same two components as Article 2, `deploy.target` flipped, `demo/hello` exposed through an Ingress, and a resource binding pointing at the database just deployed:

```yaml
deploy:
  target: k8s

components:
  - id: demo/hello
    version: 1.0.0
    expose: true
    hostname: hello.local
  - id: demo/caller
    version: 1.0.0

resources:
  - kind: database
    engine: postgresql
    id: caller-db
    host: guide-pg.guide-resources.svc.cluster.local
    port: 5432
    username: postgres
    password: ${DB_PASSWORD}
    bindings:
      - componentId: demo/caller
        database: callerdb
```

`host` is an ordinary Kubernetes cross-namespace Service DNS name — nothing BrickKit-specific about it.

## Deploy it for real

```bash
export DB_PASSWORD=devpass
brickkit up
```

```
📌 以下基础资源需要先跑起来（平台不代为部署，见 006 §9.1）：
   caller-db    postgresql   guide-pg.guide-resources.svc.cluster.local:5432  供 demo/caller 使用

🔧 启动前会执行的数据库迁移（失败则该组件不会启动）：
   demo/caller@1.0.0  /app/caller migrate

☸️  正在部署到 Kubernetes（命名空间 brickkit-hello-world）...
   先执行数据库迁移，完成后才启动主服务
   demo-hello-1-0-0             running（healthy）
   demo-caller-1-0-0            running（healthy）
✅ 全部组件已启动（2 个）
```

```bash
kubectl -n brickkit-hello-world get pods,job,ingress
```

```
NAME                                     READY   STATUS      RESTARTS
pod/demo-caller-1-0-0-86bf54446d-8pcnb   1/1     Running     0
pod/demo-caller-1-0-0-migration-k879v    0/1     Completed   0
pod/demo-hello-1-0-0-5f96bcd9b-jgxdm     1/1     Running     0

NAME                                    STATUS     COMPLETIONS
job.batch/demo-caller-1-0-0-migration   Complete   1/1

NAME                                         CLASS   HOSTS         ADDRESS
ingress.networking.k8s.io/demo-hello-1-0-0   nginx   hello.local   192.168.49.2
```

The migration ran as its own `Job`, completed, and only then did the two long-running Deployments come up — exactly the ordering [Deployment file generation](../architecture/deployment-generation.md) describes, now against a real API server instead of a generated file on disk.

## Proving the whole chain actually works

Through the Ingress, with the hostname it was configured for:

```bash
curl -H "Host: hello.local" http://$(minikube ip)/api/v1/hello
```
```json
{"component":"demo/hello","greeting":"你好","message":"你好，我是 demo/hello@1.0.0","version":"1.0.0"}
```

`demo/caller` calling `demo/hello` — a real Pod-to-Pod call through the injected address, not just an environment variable that looks right:

```bash
kubectl -n brickkit-hello-world port-forward svc/demo-caller-1-0-0 18080:8080 &
curl http://localhost:18080/api/v1/call
```
```json
{"component":"demo/caller","endpoint":"http://demo-hello-1-0-0:8080","upstream":{"component":"demo/hello","greeting":"你好","message":"你好，我是 demo/hello@1.0.0","version":"1.0.0"},"version":"1.0.0"}
```

`"upstream"` is `demo/hello`'s real response, fetched live by `demo/caller` from inside its own Pod — proof the address BrickKit injected doesn't just look plausible, it actually connects. And the same missing optional dependency from Article 2 is still visibly missing here too:

```bash
curl http://localhost:18080/api/v1/status
```
```json
{"component":"demo/caller","eventBus":"degraded","hello":"http://demo-hello-1-0-0:8080","version":"1.0.0"}
```

`eventBus: degraded` is `demo/caller`'s own code reacting to `DEMO_BUS_ENDPOINT` never having been injected — the platform's contribution here was refusing to inject a value for something that isn't installed; what "degraded" actually means was always the component's own decision (AGENTS.md §9.7).

## A real gotcha: `brickkit down` deletes the whole namespace

```bash
brickkit down
```

```
🛑 停止项目 hello-world
✅ 已停止全部组件

💡 基础资源（数据库等）由运维部署，不受 brickkit down 影响
   重新启动：brickkit up
```

That message is true — BrickKit never issues a delete against the resource itself. What it doesn't warn you about, and what running this for real surfaces immediately: **by default, BrickKit created this project's namespace, and `down` deletes the entire namespace it created** — every object in it, whether BrickKit put it there or not. Had `guide-pg` been deployed into `brickkit-hello-world` instead of its own `guide-resources` namespace (for convenience — one less command to type), this exact `brickkit down` would have taken the database with it, directly contradicting what the console message just told you, not because the message is wrong, but because "the resource itself was never targeted" and "the resource happened to live in a namespace that just got deleted" are two different guarantees.

The fix is either of the two things this article already does — keep resources in a namespace BrickKit doesn't own — or, if a project's namespace is meant to be managed by someone else entirely, set `createNamespace: false` in `deploy:` (AGENTS.md §7): BrickKit then never creates *or* deletes the namespace, and `down` only ever touches the objects it generated inside it.

## Cleanup

```bash
kubectl delete namespace guide-resources
minikube image rm brickkit-demo/hello:1.0.0 brickkit-demo/caller:1.0.0
```

---

Next in this series (see [the guide index](README.md)): upgrading a running component to a new version, and running two versions side by side on purpose.
