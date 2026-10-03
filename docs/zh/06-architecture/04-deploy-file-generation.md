# 部署文件生成

组件仓库里从不带部署文件。每次 `up`（和 `up --dry-run`），CLI 按部署文件的 `target`，从三层文件与各组件的 `component.yaml` 重新生成：

| `target` | 生成 | 位置 |
| --- | --- | --- |
| `docker`、`podman` | 一份 `compose.yaml` | `.brickkit/generated/compose.yaml` |
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
    healthcheck:
      interval: 10s
      retries: 3
      start_period: 60s
      test:
        - CMD-SHELL
        - wget -q --spider http://127.0.0.1:8080/healthz || curl -fsS http://127.0.0.1:8080/healthz || exit 1
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
        - wget -q --spider http://127.0.0.1:8080/healthz || curl -fsS http://127.0.0.1:8080/healthz || exit 1
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
- **迁移容器**：同一个镜像、同一份环境、`restart: "no"`，入口换成迁移命令。
- **健康检查**：节奏由平台固定（10 秒一次、3 秒超时、连续 3 次失败），`start_period` 来自 `startPeriodSeconds`（缺省 60）。
  `wget` 与 `curl` 都试，所以镜像里有其中一个就行。
- **端口**：只有 `expose: true` 的组件映射宿主机端口；其余只在项目网络里可达。
- **资源**：`requests` 写成 `reservations`；没人写 `limits` 时不生成上限。
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

**三段探针**：`startupProbe` 给冷启动留出宽限期（12 次 × 5 秒 = 60 秒，来自 `startPeriodSeconds`），它通过之前另外两个探针不开始；
`readinessProbe` 决定流量来不来；`livenessProbe` 决定要不要重启。三者打的都是组件的健康检查路径——它只该检查本进程还活着。

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

## 两边唯一相同的东西

Docker 的 Compose 文件和 Kubernetes 的清单几乎没有一行相同——但**组件看到的东西完全相同**：同样的环境变量名、同样的值、同样格式的依赖地址
`http://<版本化服务名>:<端口>`。所以换部署目标只是换一份部署文件，组件代码一行不改。
