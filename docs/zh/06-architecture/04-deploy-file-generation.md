# 部署文件生成

组件仓库里从不带部署文件。每次 `up`（和 `up --dry-run`），CLI 按部署文件的 `target`，从三层文件与各组件的 `component.yaml` 重新生成：

| `target` | 生成 | 位置 |
| --- | --- | --- |
| `docker`、`podman` | 一份 `compose.yaml` | `.brickkit/generated/compose.yaml` |
| `podman` 加 `podman.units: quadlet` | Podman 的 systemd 单元（Quadlet） | `.brickkit/generated/quadlet/` |
| `k8s` | 一组 Kubernetes 清单 | `.brickkit/generated/k8s/` |

它们是生成物：不要手改，下次 `up` 会覆盖。要改什么，改三层文件。

下面两份都来自同一个项目：`demo/caller` 强依赖 `demo/hello`、弱依赖 `demo/bus`，自己声明了数据库迁移，对外开放。

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

生成的 `compose.yaml`（节选 `demo/caller`、它的迁移、`demo/hello`）：

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
      - PORT=8080
    healthcheck:
      interval: 10s
      retries: 3
      start_interval: 1s
      start_period: 60s
      test:
        - CMD-SHELL
        - wget -q -O /dev/null http://127.0.0.1:8080/healthz || curl -fsS -o /dev/null http://127.0.0.1:8080/healthz || exit 1
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
      - PORT=8080
    image: demo-caller:1.0.0
    networks:
      - brickkit-net
    restart: "no"
  demo-hello-1-0-0:
    environment:
      - COMPONENT_ID=demo/hello
      - COMPONENT_VERSION=1.0.0
      - GREETING=Hello
      - PORT=8080
    healthcheck:
      interval: 10s
      retries: 3
      start_interval: 1s
      start_period: 60s
      test:
        - CMD-SHELL
        - wget -q -O /dev/null http://127.0.0.1:8080/healthz || curl -fsS -o /dev/null http://127.0.0.1:8080/healthz || exit 1
      timeout: 3s
    image: demo-hello:1.0.0
    networks:
      - brickkit-net
    restart: unless-stopped
```

逐项看：

- **服务名就是版本化服务名**：`demo-caller-1-0-0`。同一个组件的另一个版本是另一个服务，并排运行。
- **`depends_on`**：强依赖等它**健康**（`service_healthy`），自己的迁移等它**成功结束**（`service_completed_successfully`）。弱依赖 `demo/bus` 不在里面——
  它可能根本不启动，写进去会把整个项目卡住。
- **环境变量**：平台变量、依赖地址、配置值（`DATABASE_PORT` 是 `configSchema` 的默认值，`GREETING` 同理）。密钥不在这里，在 `env_file` 引用的 0600 文件里。
  声明了 `mount: file` 的密钥连 env 文件也不进：它是 `.brickkit/generated/secrets/<服务名>/` 下的一个文件，这个目录经 `volumes`
  只读挂到容器的 `/run/brickkit/secrets/<服务名>/`，环境变量里只有路径（见[以文件交付](../01-three-layers/07-sensitive-values.md#以文件交付mount-file)）。
- **`restart`**：容器崩溃后由引擎拉起，开机时再启动。`target: docker` 写 `unless-stopped`（Docker 守护进程开机时拉起它们，手动停掉的保持停止）；
  `target: podman` 写 `always`——在 Podman 里运行时与前者完全相同，但 Podman 的开机单元 `podman-restart.service` 只启动策略是 `always` 的容器。
  开机能回来，还要引擎自己设成开机启动（见[重启机器之后](../09-patterns/06-deployment-selection.md#重启机器之后)）。
- **迁移容器**：同一个镜像、同一份环境、`restart: "no"`，入口换成迁移命令。
- **健康检查**：节奏由平台固定（10 秒一次、3 秒超时、连续 3 次失败），`start_period` 来自 `startPeriodSeconds`（缺省 60）。
  第一次成功之前每秒查一次（`start_interval`），依赖方等 `service_healthy`、`up --wait` 都在等这一次，所以组件一答复就往下走，成功之后才改回 10 秒一次。
  `start_interval` 要 Docker Engine 25 及以上；Podman 的兼容接口与更早的 Docker 不认这个字段（不报错），启动阶段照 10 秒一次查，一个一启动就答复的组件要 10 秒左右才转 healthy。
  `wget` 与 `curl` 都试，所以镜像里有其中一个就行；两者发的都是 `GET`，与 Kubernetes 的探针一样。
- **端口**：只有 `expose: true` 的组件映射宿主机端口；其余只在项目网络里可达。
- **资源**：`requests` 写成 `reservations`；没人写 `limits` 时不生成上限。
- **副本**：`replicas` 大于 1 时写成 `deploy.replicas`，服务名在项目网络里解析到每个副本；迁移容器仍只有一个。
  多个副本的服务不能映射宿主机端口（一个宿主机端口只能给一个容器），生成阶段就报错（见[多个副本](../01-three-layers/03-deploy-yaml.md#多个副本)）。
- **网络**：一个项目一个网络，名字是 `brickkit-<项目名>-net`；Compose 项目名是 `brickkit-<项目名>`。
  项目之外的容器（自己的数据库、IdP、网关的 compose）要和组件在同一个网络里，比如 IdP 的 webhook 要按服务名回调组件时，
  别去模仿这个网络的内部标签，而是让网络归项目：先建好它，在部署文件里写 `network: <名字>`。生成的 compose 把它标成 `external`，
  只加入、不创建也不删除；`up` 之前核对它存在，不在就停下并给出 `docker network create` 命令。外部的 compose 同样把它声明成
  `external`，于是谁先起、谁先 `down` 都不冲突：

  ```bash
  docker network create shop-net
  ```

  ```yaml
  # deploy.yaml
  network: shop-net
  ```

  ```yaml
  # 你自己的 infra/compose.yaml
  networks:
    default:
      name: shop-net
      external: true
  ```

外壳在 Compose 里的样子（网络别名、`BRICKKIT_SERVED_MEMBERS`、成员迁移）见 [开发一个外壳](../04-shell/05-shell-development.md#地址怎么指过来)。

## Podman Quadlet 单元

`podman.units: quadlet` 时，同一份计划变成 Podman 的 Quadlet 格式的 systemd 单元：每个组件一个 `.container`、每个迁移一个，加上项目的 `.network`
（部署文件写了自己的 `network:` 时没有）。`up` 把它们复制进 `~/.config/containers/systemd/brickkit-<项目名>/`（无根）或
`/etc/containers/systemd/brickkit-<项目名>/`（有根），分两步：先是网络与迁移的单元，重新加载 systemd，跑完每个迁移；全部成功了，才是组件的单元，
再重新加载一次，启动组件。systemd 让它们一直跑着、开机拉起。示例里的 `demo/caller`：

```ini
[Unit]
Description=demo/caller@1.0.0 (BrickKit project qtest)
Wants=brickkit-qtest_demo-hello-1-0-0.service
After=brickkit-qtest_demo-hello-1-0-0.service

[Service]
Restart=always
TimeoutStartSec=120

[Container]
Image=brickkit-demo/caller:1.0.0
Network=brickkit-qtest.network
Environment="COMPONENT_ID=demo/caller"
Environment="DEMO_HELLO_ENDPOINT=http://demo-hello-1-0-0:8080"
Environment="PORT=8080"
ContainerName=brickkit-qtest_demo-caller-1-0-0
NetworkAlias=demo-caller-1-0-0
PublishPort=18555:8080
HealthCmd=wget -q -O /dev/null http://127.0.0.1:8080/healthz || curl -fsS -o /dev/null http://127.0.0.1:8080/healthz || exit 1
HealthInterval=10s
HealthTimeout=3s
HealthRetries=3
HealthStartPeriod=60s
Notify=healthy
HealthOnFailure=kill
Label="brickkit.io/project=qtest"
Label="brickkit.io/service=demo-caller-1-0-0"
LogDriver=journald

[Install]
WantedBy=default.target
```

和 Compose 不同的地方，以及为什么：

- **名字**：`brickkit-<项目名>_<服务名>`——`_` 标出项目名在哪里结束（项目名与服务名里都没有它），两个项目的单元、容器、secret 不会同名。
- **迁移只在 `up` 时跑**：迁移的单元没有 `[Install]`，也没有组件依赖它，所以开机与组件崩溃重启都不跑它。把关的是 `up`：它单独把每个迁移跑一遍
  （`systemctl restart`），全部成功了才装组件的单元、换组件的 Podman secret——迁移为此有自己的一份 secret。于是开机或组件重启时 systemd 起的，
  永远是上一次成功的 `up` 装的那些，不会是一个面对没迁移过的库的新版本。库结构在那次 `up` 时已经迁好；开机时再跑迁移，只会撞上与组件一起开机、
  还不接受连接的数据库而失败，每次开机都留下失败的单元。
- **依赖只管顺序**（`Wants=` + `After=`）：`Requires=` 会在依赖被停止或重启时连带停掉、重启这个组件，而组件自己会重试连接——与 Compose 的 `depends_on` 一样。
  依赖自己的 `Notify=healthy` 让它的启动在健康之后才算完成，所以 `After=` 等的是健康，相当于 `service_healthy`。
- **不健康就杀掉重启**（`HealthOnFailure=kill`），但只在探的是存活检查时：声明了 `readinessCheck` 的组件探的是就绪，暂时不接流量不会因此被杀，与 Kubernetes 一样。
- **不能让文件里看到的值**——密钥、`file://` 的内容、`${VAR}` 模板、跨好几行的值——在 `up` 时求值（开机时没有 shell 的环境），作为 Podman secret 交过去
  （`Secret=<名字>,type=env,target=<键>`）；单元带着 `Label="brickkit.io/secret-digest=…"`，换了值组件就会重启。普通的值写成 `Environment=`，`%` 与 `$` 双写：
  Quadlet 把它们放进 `ExecStart`，systemd 会在那里展开。
- **多副本**是一个模板单元（`…@.container`）加每个实例一个链接；每个实例挂同一个 `NetworkAlias`。
- 文件里**不写生成时间，也不跟语言走**：`up` 正好重启单元变了的那些组件。写进去了还没来得及重启的单元（重启失败、`up` 被打断）记在单元的目录里，
  下一次 `up` 重启它。
- **镜像由 `up` 先拉好**，再启动任何东西，第一次拉一个大镜像不算进单元的启动时限。会成为空 Podman secret 的值改为写进单元（Podman 不收空的 secret）。

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
📄 已生成 10 份清单：.brickkit/generated/k8s/
   命名空间：brickkit-my-shop
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

**Deployment**（`demo/hello`，节选容器部分）：

```yaml
      containers:
        - env:
            - name: COMPONENT_ID
              value: demo/hello
            - name: COMPONENT_VERSION
              value: 1.0.0
            - name: GREETING
              value: Hello
            - name: PORT
              value: "8080"
          image: demo-hello:1.0.0
          livenessProbe:
            failureThreshold: 3
            httpGet:
              path: /healthz
              port: 8080
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
            periodSeconds: 5
            timeoutSeconds: 3
          resources:
            requests:
              cpu: 50m
              memory: 32Mi
          startupProbe:
            failureThreshold: 60
            httpGet:
              path: /healthz
              port: 8080
            periodSeconds: 1
            timeoutSeconds: 3
```

**三段探针**：`startupProbe` 给冷启动留出宽限期（60 次 × 1 秒 = 60 秒，来自 `startPeriodSeconds`），它通过之前另外两个探针不开始；
`readinessProbe` 决定流量来不来；`livenessProbe` 决定要不要重启。三者打的都是组件的健康检查路径——它只该检查本进程还活着。

启动探针每秒一次：它只在通过之前运行，查得多密，就多快发现组件已经起来（包括崩溃后重启）；另外两个不设首次延迟，因为启动探针已经挡在前面。
启动探针一通过，kubelet 立刻补跑一次就绪探针，所以崩溃的组件一答复，一秒左右就重新收流量——就绪探针要是有首次延迟，这次补跑会被跳过，要等下一个周期。
就绪探针（每 5 秒）与存活探针（每 10 秒）在 Pod 的整个生命里一直在跑，所以疏一些：它们的周期决定跑着的实例多快被摘掉流量或重启，不决定重启的实例多快回来。
这些值由平台固定，组件只声明 `startPeriodSeconds`。

**Service**：名字就是版本化服务名，所以 `http://demo-hello-1-0-0:8080` 在 Kubernetes 上与 Docker 上是同一个字符串。

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

**Ingress**：只有 `expose: true` 的组件才有，`hostname` 必填。条目写了 `paths` 时，规则里是那几条前缀而不是 `/`——
几个组件[共用一个域名](../01-three-layers/03-deploy-yaml.md#几个组件共用一个域名)靠它分流。

```yaml
kind: Ingress
metadata:
  name: demo-caller
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

Ingress 的名字是组件 ID，**不带版本号**（只因别的组件依赖才在项目里的兼容版本用版本化服务名）。升级一个对外的组件时：
新版本的 Deployment 与 Service 先起来，和旧版本并排；等它就绪，`up` 才原地改这份 Ingress 的后端，把流量一次切过去；
然后删掉旧版本。所以升级过程中域名一直有人接，滚动更新失败时路由也还指着旧版本。同一个域名的同一条路径不能出现在两份
Ingress 里（nginx-ingress 会拒绝第二份），所以 `up` 在下发 Ingress 之前先删掉集群里不再属于这次部署的那些。

**迁移 Job**：`backoffLimit: 0`，失败不重试；`up` 先删掉上一次的同名 Job，再创建、等它完成，然后才 `apply` 主服务。

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

**PodDisruptionBudget**：`replicas` 大于 1 时自动生成（`maxUnavailable: 1`），节点维护时不会一次把副本全部赶走。

另外几样按部署文件的设置生成：

| 设置 | 生成 |
| --- | --- |
| `secret: true` 的配置、`file://` 的内容 | 一个 Secret（文件权限 0600），Deployment 用 `secretKeyRef` 引用（见 [敏感值](../01-three-layers/07-sensitive-values.md)） |
| `k8s.networkPolicy.enabled: true` | 每个组件一条 NetworkPolicy，按依赖图放行（见 [安全与签名](08-security-and-signing.md)） |
| `k8s.serviceAccount.enabled: true` | 每个组件一个 ServiceAccount，不挂载令牌 |
| `k8s.podSecurity: restricted` | 每个容器的 `securityContext` |

## 大家唯一相同的东西

Docker 的 Compose 文件、Podman 的单元和 Kubernetes 的清单几乎没有一行相同——但**组件看到的东西完全相同**：同样的环境变量名、同样的值、同样格式的依赖地址
`http://<版本化服务名>:<端口>`。所以换部署目标只是换一份部署文件，组件代码一行不改。
