# 4. 部署到 Kubernetes

同一份 Manifest、同一套 `brickkit.yaml` 结构、同一张依赖图——唯一变的是 `deploy.target: k8s`（AGENTS.zh.md §5.5）。这一篇把[第 2 篇](02-what-runs.md)里的 `demo/hello` + `demo/caller` 组合真实部署到一个真集群（minikube）上，从头走到尾，顺便把 `demo/caller` 从那篇开始就一直缺的那个真实数据库补上——以及一个真跑 `brickkit down` 之后才发现的、真实且有代价的坑，不是读代码读出来的。

**前置条件：** 一个跑着的集群和 `kubectl`——`minikube start`，如果没有独立的 `kubectl` 就用 `minikube kubectl --`（或者按它首次启动自己给的提示，把它缓存的那个 `kubectl` 软链到 `PATH` 里）。把前面几篇构建的两个镜像加载进集群自己的镜像库，因为 minikube 不共用你主机的 Docker 守护进程：

```bash
minikube image load brickkit-demo/hello:1.0.0
minikube image load brickkit-demo/caller:1.0.0
```

## 部署一个真实数据库——放在它自己的命名空间里

`demo/caller` 从第 2 篇开始就一直需要一个 `kind: database` 资源。资源是运维部署的，不是 BrickKit 部署的（AGENTS.zh.md §2.1）——这里用几条 `kubectl` 命令代替真实运维团队会跑的那些：

```bash
kubectl create namespace guide-resources
kubectl -n guide-resources create deployment guide-pg --image=postgres:16-alpine --port=5432
kubectl -n guide-resources set env deployment/guide-pg POSTGRES_PASSWORD=devpass POSTGRES_DB=callerdb
kubectl -n guide-resources expose deployment guide-pg --port=5432 --target-port=5432
```

把它放进自己的命名空间（`guide-resources`，不是 BrickKit 马上要接管的那个）不是风格选择——下面讲 `brickkit down` 的那一节会解释这为什么很重要。

## 项目配置

跟第 2 篇一样的两个组件，`deploy.target` 换掉，`demo/hello` 通过 Ingress 暴露出去，加一条指向刚部署好的数据库的资源绑定：

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

`host` 就是一个普通的 Kubernetes 跨命名空间 Service DNS 名，跟 BrickKit 没有任何特殊关系。

## 真正部署它

```bash
export DB_PASSWORD=devpass
brickkit up
```

```
📌 以下基础资源需要先跑起来（平台不代为部署）：
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

迁移作为独立的 `Job` 跑完，完成之后两个长期运行的 Deployment 才起来——顺序跟[部署文件是怎么生成出来的](../architecture/deployment-generation.md)里描述的完全一样，只是这次面对的是一个真实的 API server，不是磁盘上生成的一份文件。

## 证明整条链路真的能跑通

通过 Ingress，带上它配置的那个 hostname：

```bash
curl -H "Host: hello.local" http://$(minikube ip)/api/v1/hello
```
```json
{"component":"demo/hello","greeting":"你好","message":"你好，我是 demo/hello@1.0.0","version":"1.0.0"}
```

`demo/caller` 调用 `demo/hello`——一次真实的 Pod 到 Pod 调用，不只是一个看起来对的环境变量：

```bash
kubectl -n brickkit-hello-world port-forward svc/demo-caller-1-0-0 18080:8080 &
curl http://localhost:18080/api/v1/call
```
```json
{"component":"demo/caller","endpoint":"http://demo-hello-1-0-0:8080","upstream":{"component":"demo/hello","greeting":"你好","message":"你好，我是 demo/hello@1.0.0","version":"1.0.0"},"version":"1.0.0"}
```

`"upstream"` 是 `demo/hello` 的真实响应，由 `demo/caller` 在自己的 Pod 里实时拉取的——证明了 BrickKit 注入的地址不只是看起来说得通，而是真的连得上。第 2 篇里那个缺失的弱依赖，在这里也照样可见：

```bash
curl http://localhost:18080/api/v1/status
```
```json
{"component":"demo/caller","eventBus":"degraded","hello":"http://demo-hello-1-0-0:8080","version":"1.0.0"}
```

`eventBus: degraded` 是 `demo/caller` 自己的代码在对"`DEMO_BUS_ENDPOINT` 从来没被注入过"这件事做出反应——平台在这里的贡献仅仅是拒绝给一个没装的东西注入值；"降级"到底意味着什么，从头到尾都是组件自己的决定（AGENTS.zh.md §9.7）。

## 一个真实的坑：`brickkit down` 会删掉整个命名空间

```bash
brickkit down
```

```
🛑 停止项目 hello-world
✅ 已停止全部组件

💡 基础资源（数据库等）由运维部署，不受 brickkit down 影响
   重新启动：brickkit up
```

这句话是真的——BrickKit 从来不会对资源本身发出删除指令。它没提醒你的、而真跑一遍立刻就会暴露出来的是：**默认情况下，这个项目的命名空间是 BrickKit 自己创建的，`down` 会删掉它创建的整个命名空间**——不管里面的东西是不是 BrickKit 放进去的，全都一起删。如果 `guide-pg` 图省事（少打一条命令）被部署进了 `brickkit-hello-world`，而不是它自己的 `guide-resources`，这次 `brickkit down` 会把数据库一起带走，直接跟刚才那条控制台消息说的相反——不是因为那条消息说错了，而是"资源本身从没被针对性删除过"和"资源恰好活在一个刚被整体删掉的命名空间里"是两个不同的保证。

```mermaid
graph TB
    subgraph 安全：命名空间分开
        NS1["brickkit-hello-world<br/>（BrickKit 拥有，down 会删）"]
        NS2["guide-resources<br/>（运维管理，不受影响）"]
        PG1[("PostgreSQL")] -.->|活在| NS2
    end
    subgraph 不安全：同一个命名空间
        NS3["brickkit-hello-world<br/>（down 会把里面全删掉）"]
        PG2[("PostgreSQL")] -.->|活在| NS3
    end
```

解法就是这篇文章已经在做的两件事之一——把资源放在 BrickKit 不拥有的命名空间里——或者，如果一个项目的命名空间本来就该由别人管理，在 `deploy:` 里设 `createNamespace: false`（AGENTS.zh.md §7）：这样 BrickKit 就永远不会创建、也永远不会删除这个命名空间，`down` 只会动它自己在里面生成的那些对象。

## 清理

```bash
kubectl delete namespace guide-resources
minikube image rm brickkit-demo/hello:1.0.0 brickkit-demo/caller:1.0.0
```

---

下一篇：[升级，以及让多个版本并存](05-upgrades-and-versions.md)——把一个跑着的组件升级到新版本，以及故意让两个版本并存。
