# 05 · Docker 启动全流程

🎯 **这一节验证什么：** 从"改配置"到"五个组件跑起来"的完整链路：生成部署文件 → 建库 → 迁移 → 启动 → 查状态 → 停。

前置：**五组件基准 + 镜像**（见 [00](00-准备.md)）；Docker 可用；概念上接着 [02](02-添加与移除组件.md)。

```bash
./试用指南/准备.sh --images      # 十个组件镜像，只需一次
./试用指南/准备.sh --baseline    # 五组件基准
```

> 03 与 04 **不是这一篇的前提**——它们讲的是「平台怎么算出该启动谁」，属于理解性内容。
> 想先看到东西跑起来的话，从 02 直接过来即可；跑完再回头看那两篇会更容易，
> 因为那时你有真容器可以对照。

---

## 5.1 起一套基础资源

**平台不部署基础资源**（006 §10.1）。数据库、Redis 这类东西是通用的、跨项目共享的、
有自己的运维节奏——谁把它跑起来，不该由某个项目的 `brickkit up` 决定。

本地想快速起一套？仓库里有现成的：

```bash
docker compose -f deploy/dev-resources/docker-compose.yaml up -d
```

一个 postgres（5432）+ 一个 redis（6379），端口发布到宿主机。
**这台机器上 5432 已经被占了**的话，换个端口即可，后面的声明跟着改：

```bash
PG_PORT=15432 REDIS_PORT=16379 docker compose -f deploy/dev-resources/docker-compose.yaml up -d
```

> 这份 compose 是**手写**的，不是平台生成的——它是个样例，你可以随手改
> （换 postgres 版本、加 extension、再加一个 kafka）。平台生成的话，
> 这些都得变成平台的配置项，而平台对它们一无所知。
> 已经有自己的数据库就直接用自己的，这一步跳过。

## 5.2 把资源声明进 brickkit.yaml

组件的 `component.yaml` 里只说"我需要一个 postgresql"，**具体连哪台库由项目决定**。
在 `brickkit.yaml` 里补上：

```yaml
resources:
  - kind: database
    engine: postgresql
    id: pg-main
    host: host.docker.internal  # ← 资源跑在本机时就写这个
    port: 5432
    username: brickkit
    password: ${PG_PASSWORD}    # ← 密码只写引用，绝不写明文
    bindings:
      - componentId: demo/caller
        database: caller
      - componentId: department/tree
        database: department
      - componentId: people/basic
        database: people

  # infra/redis-event-bus 要一个 redis（它的 component.yaml 里声明的）
  - kind: cache
    engine: redis
    id: redis-main
    host: host.docker.internal
    port: 6379
    bindings:
      - componentId: infra/redis-event-bus
```

再在**项目根目录**放一个 `.env`（`.gitignore` 里已经忽略了它）：

```bash
echo "PG_PASSWORD=devpass" > .env
```

### `host` 该写什么

| 资源跑在哪 | `host` 写法 | 平台做什么 |
| --- | --- | --- |
| **本机**（最常见） | `host.docker.internal` | 为绑定它的容器（含迁移容器）自动补 `extra_hosts`；`local: true` 组件的 env 文件里换成 `localhost` |
| 别的机器 / 云数据库 | IP 或域名 | 原样注入 |
| 你自己接进了本项目网络的容器 | 裸服务名（`pg`） | 原样注入，并**警告一次**——平台不会创建叫这个名字的 service |

⚠️ **不要写 `localhost`。** 容器里的 `localhost` 是容器自己，不是你的机器。

> **早先的规则已经取消。** 平台曾经在 `host` 不含点时自己起一个 postgres 容器
> （旧的 006 §10.4）：一个点决定平台要不要替你部署一个数据库。它只覆盖 6 种资源
> 类型里的 2 种、在 K8s 目标下从来不存在、而且托管出来的实例还没法跨项目共享。
> 完整理由见 006 §10.5。

### 一个组件要连两个同类资源：`envPrefix`

默认情况下，一个组件绑一个数据库，注入的变量是 `DATABASE_HOST` / `DATABASE_NAME`……
如果同一个组件要连**两个** PostgreSQL（比如主库 + 归档库），
变量名就撞了——这时用 `envPrefix` 区分：

```yaml
resources:
  - kind: database
    engine: postgresql
    id: pg-primary
    host: primary-db
    port: 5432
    bindings:
      - componentId: people/basic
        database: people
        envPrefix: PRIMARY          # ← 加前缀

  - kind: database
    engine: postgresql
    id: pg-archive
    host: archive-db
    port: 5432
    bindings:
      - componentId: people/basic
        database: people_archive
        envPrefix: ARCHIVE
```

组件收到的是：

```
PRIMARY_DATABASE_HOST=primary-db     ARCHIVE_DATABASE_HOST=archive-db
PRIMARY_DATABASE_NAME=people         ARCHIVE_DATABASE_NAME=people_archive
```

**什么时候需要它：** 只有"一个组件绑多个同类资源"这一种情况。
绑一个的时候别加——加了变量名就变了，而组件读的是不带前缀的那个名字。

组件那边要读哪些变量名，看它 `component.yaml` 的 `dependencies.resources`
与 README。前缀是**使用者**定的，组件作者无从预知，所以支持多资源的组件
会在文档里说明它认哪个前缀。

---

## 5.3 先看看会生成什么（不启动）

### ▶️ 操作

```bash
brickkit up --dry-run
cat .brickkit/generated/docker-compose.yaml
```

### ✅ 预期

生成的文件开头就写着"别手改"，里面每个组件一个 service，你会看到：

- `environment` 里注入好的地址：`DEMO_HELLO_ENDPOINT=http://demo-hello-1-0-0:8080`
- 弱依赖 `DEMO_BUS_ENDPOINT` **根本不存在**（不是空值）
- `depends_on` + `condition: service_healthy`：依赖方等被依赖方**健康**才起
- 有迁移的组件多一个 `xxx-migration` service，主服务 `depends_on` 它 `service_completed_successfully`
- **没有 postgres / redis 的 service**：平台不部署基础资源，文件里只有你的组件

### 🔍 顺便看一眼资源配额

```bash
sed -n '/demo-hello-1-0-0:/,/environment:/p' .brickkit/generated/docker-compose.yaml | grep -A6 deploy:
```

```
    deploy:
      resources:
        reservations:
          cpus: "0.05"
          memory: 32M
```

**只有 `reservations`，没有 `limits`。** `reservations` 是 compose 对 requests 的叫法，
这里的值来自 `demo/hello` 自己的 `component.yaml`（它声明了 `requests: 50m/32Mi`）。

上限一个都没有，是因为**没人写过**：组件没声明、项目也没写，平台就不生成。
平台只给 `requests` 兜底（`100m` / `128Mi`），**给 `limits` 不兜底**——猜一个上限的
后果是去 kill 一个跑得好好的组件：它真的需要 600Mi，而那个数字是平台编的。

起来之后可以直接问 docker 要真实值：

```bash
docker inspect brickkit-demo-shop-demo-hello-1-0-0-1 \
  --format 'NanoCpus={{.HostConfig.NanoCpus}}  Memory={{.HostConfig.Memory}}  MemoryReservation={{.HostConfig.MemoryReservation}}'
```

```
NanoCpus=0  Memory=0  MemoryReservation=33554432
```

`NanoCpus=0` 与 `Memory=0` 就是"不限"。（CPU 上限看的是 **`NanoCpus`**，
不是 `CpuQuota`——后者在 compose 起的容器上一直是 0，别看错。）

### 给它设一个内存上限

在 `brickkit.yaml` 里给这个组件加几行：

```yaml
  - id: demo/hello
    version: 1.0.0
    resources:
      requests: { cpu: "50m", memory: "64Mi" }
      limits:   { memory: "64Mi" }     # 内存 requests = limits；CPU 仍不设上限
```

`brickkit up` 之后再 inspect：

```
NanoCpus=0  Memory=67108864  MemoryReservation=67108864
```

内存被压到 64Mi，**CPU 依然不限**。这正是推荐的形状——两种资源的建议是**相反**的：

| | 建议 | 一句话理由 |
| --- | --- | --- |
| CPU | 设 `requests`，**不设** `limits` | CPU 上限走 CFS quota，**节点空闲时也照样限流**，表现成毫无来由的 p99 毛刺 |
| 内存 | `requests` = `limits` | 拿到 `Guaranteed` QoS，节点缺内存时**最后**才被驱逐 |

⚠️ **内存 `requests` 千万别为了"多塞几个组件"报低。** `requests` 是给调度器的承诺，
不是给自己的配额——写 `50Mi` 不会让它只用 50Mi，只会让调度器把机器塞爆。然后节点
内存耗尽时，kubelet 按「`BestEffort` → 超出 requests 最多的 → `Guaranteed`」的顺序
驱逐，**报低的组件排在处刑队列前面，而排最前的往往正是你最忙的那个**。

💡 **组件作者建议只写 `requests`，别写 `limits`**（002 §4.6）。配额是**逐字段合并**的，
所以组件一旦写了 `limits.cpu`，用它的项目就只能改那个数字、删不掉——而"不设 CPU 上限"
恰恰是推荐做法。本篇这些自测组件就是这么写的。

完整规则与三个可直接抄的场景（Go 组件群 / JVM / 突发型）见 005 §2.5。

---

## 5.4 第一次 up：它会告诉你还差什么

### ▶️ 操作

```bash
brickkit up
```

### ✅ 预期

CLI 会先把这次要用到的基础资源连同要建的库一起列出来：

```
📌 以下基础资源需要先跑起来（平台不代为部署，见 006 §9.1）：
   pg-main      postgresql   host.docker.internal:5432  供 demo/caller、department/tree、people/basic 使用
      需要库 caller（供 demo/caller 使用）：CREATE DATABASE "caller";
      需要库 department（供 department/tree 使用）：CREATE DATABASE "department";
      需要库 people（供 people/basic 使用）：CREATE DATABASE "people";
   redis-main   redis        host.docker.internal:6379  供 infra/redis-event-bus 使用
   库也要预先建好；已经建过就无需再执行，建库是一次性操作
   本地开发想快速起一套：docker compose -f deploy/dev-resources/docker-compose.yaml up -d
```

**每次 `up` 都会打印这一段**，不是只在出错时——建库是一次性动作，
而"资源得跑着"是每次启动都要满足的前提。

然后迁移会**失败**，因为库还不存在：

```
❌ 错误：docker 执行失败
   输出：... service "department-tree-1-0-0-migration" didn't complete successfully: exit 1
```

**这是正确行为，不是 bug。** 平台不建库（建库要 `CREATEDB` 权限，云数据库上应用账号通常没有；建了会造成"开发能跑、生产必炸"）。它的责任是**把要建的库说清楚**。

### 把库建出来

```bash
for db in caller department people; do
  docker exec brickkit-dev-resources-postgres-1 \
    psql -U brickkit -d postgres -c "CREATE DATABASE \"$db\""
done
```

用自己的数据库就换成你自己的连法——平台只负责告诉你要建哪些库。

---

## 5.5 再来一次：全绿

### ▶️ 操作

```bash
brickkit up
```

### ✅ 预期

```
🔍 检测镜像拉取权限... ✅ 全部通过

🔧 启动前会执行的数据库迁移（失败则该组件不会启动）：
   demo/caller@1.0.0  /app/caller migrate
   department/tree@1.0.0  /app/department-tree migrate
   people/basic@1.0.0  python -m app.main migrate

🐳 正在启动（docker）...
   demo-hello-1-0-0             running（healthy）
   demo-caller-1-0-0            running（healthy）
   department-tree-1-0-0        running（healthy）
   infra-redis-event-bus-1-0-0  running（healthy）
   people-basic-1-0-0           running（healthy）
✅ 全部组件已启动（5 个）
```

注意 `up` 是**等到健康才返回**的（compose 的 `--wait`）。它返回时，五个组件是真的可用，不是"启动命令发出去了"。

### 迁移是怎么跑的

由部署文件里的**一次性容器**执行，用组件**自己的镜像**（迁移脚本与业务代码永远同版本），跑完就退出。主服务 `depends_on` 它 `service_completed_successfully`——**迁移失败，主服务根本不会启动**，不会出现"新代码撞上旧表结构"。

---

## 5.6 查状态

### ▶️ 操作

```bash
brickkit status
```

### ✅ 预期

```
📊 项目状态：demo-shop（deploy.target: docker）

✅ 运行中（5 个组件）
 ┌───────────────────────┬───────┬───────────────────┬────────────────────┐
 │ 组件                  │ 版本  │ 状态              │ 端口               │
 ├───────────────────────┼───────┼───────────────────┼────────────────────┤
 │ demo/hello            │ 1.0.0 │ 运行中（healthy） │ 8080/tcp           │
 │ demo/caller           │ 1.0.0 │ 运行中（healthy） │ 8080/tcp           │
 │ department/tree       │ 1.0.0 │ 运行中（healthy） │ 8080/tcp           │
 │ infra/redis-event-bus │ 1.0.0 │ 运行中（healthy） │ 8080/tcp           │
 │ people/basic          │ 1.0.0 │ 运行中（healthy） │ 8080/tcp, 9090/tcp │
 └───────────────────────┴───────┴───────────────────┴────────────────────┘

📦 资源状态
 ┌────────────┬──────────┬────────────────────────┐
 │ 资源       │ 类型     │ 状态                   │
 ├────────────┼──────────┼────────────────────────┤
 │ pg-main    │ database │ 可达（localhost:5432） │
 │ redis-main │ cache    │ 可达（localhost:6379） │
 └────────────┴──────────┴────────────────────────┘
```

`people/basic` 有两个端口——它的 `extraPorts` 声明了一个独立的 gRPC 端口（Python 的 grpcio 不能和 HTTP 共用端口）。

**CLI 自己不存运行状态**，`status` 每次都是现问引擎的。

> 正因为如此，`status` 与 `down` 在**配置写坏的时候照样能用**——
> 前者退成一份说明白哪儿不准的降级视图，后者压根不读安装源。
> 见 [20 · 依赖图取不到时的 status 与 down](20-排障速查.md)。

---

## 5.7 看日志、进容器

```bash
# 看某个组件的日志（-p 不能省；不带 -f 时 compose 从容器标签认项目）
docker compose -p brickkit-demo-shop logs -f demo-hello-1-0-0

# 看迁移容器干了什么
docker compose -p brickkit-demo-shop logs department-tree-1-0-0-migration
```

> `-p` 少了会**静默返回空**：compose 会拿当前目录名当项目名，去找一个不存在的项目——不报错、也没有输出，让人以为组件根本没打日志。
>
> `-f` 反倒不需要：compose 从容器标签就认得出项目。不带 `-f` 也就没有文件要插值，
> 于是 `--project-directory`（指路去项目根找 `.env`）连同那串 "variable is not set" 警告一起消失了。
> `brickkit up` 的输出里给的就是这条短命令，直接复制。

## 5.8 资源没起来会怎样

试着把资源栈停掉再 `up`：

```bash
docker compose -f deploy/dev-resources/docker-compose.yaml stop
brickkit up
```

CLI **不会**替你先探一下——它只是照常把"要先跑起来什么"列出来，然后照常启动。
报错来自**迁移容器**：

```bash
docker compose -p brickkit-demo-shop logs demo-caller-1-0-0-migration
# dial tcp 172.17.0.1:5432: connect: connection refused
```

这是有意的：组件用的是**自己那套凭据**、从**容器网络里**连；CLI 只能从宿主机、
用另一套凭据试一下，连成功也说明不了组件连得上（006 §8.3）。**那条失败信息
比平台的任何探测结论都准确。**

记得把资源栈起回来：

```bash
docker compose -f deploy/dev-resources/docker-compose.yaml start
```

---

## ⏹️ 怎么停

```bash
brickkit down                # 停掉全部组件；数据卷保留
# 只想停其中几个？给它们写 enabled: false 再 brickkit up
# （生成的部署文件里没有它们，引擎会把对应容器一并移除，见 04）
```

`down` 之后再 `brickkit status`，会告诉你"没有正在运行的组件"。

**`down` 不会碰你的数据库**——平台没部署它，自然也不停它。资源要停另说：

```bash
docker compose -f deploy/dev-resources/docker-compose.yaml down       # 停，数据保留
docker compose -f deploy/dev-resources/docker-compose.yaml down -v    # 连数据一起删
```

**`brickkit up` 中途 Ctrl-C 了：** 直接 `brickkit down` 收尾，不会有残留。

---

## 📁 代码在哪

| 能力 | 位置 |
| --- | --- |
| `up` 的完整流程 | [internal/cli/up.go](../internal/cli/up.go) |
| 升级处理 | [internal/cli/up_upgrade.go](../internal/cli/up_upgrade.go) |
| `down` / `status` | [internal/cli/down.go](../internal/cli/down.go)、[status.go](../internal/cli/status.go) |
| 生成 docker-compose.yaml | [internal/compose/compose.go](../internal/compose/compose.go) |
| 环境变量注入、配额合并 | [internal/inject/inject.go](../internal/inject/inject.go) |
| 调 docker compose | [internal/engine/compose.go](../internal/engine/compose.go) |
| 状态表格渲染 | [internal/cli/table.go](../internal/cli/table.go) |

设计书：005 §3（Docker 部署）、005 §6（迁移）、006 §9（建库责任）、004 §3.5–3.7。

**为什么是这样** → [012 架构设计原理与考量](../design/012-架构设计原理与考量.md) §2.4（组件为什么不自带部署文件，非要现生成）。

---

➡️ 下一节：[06-K8s部署.md](06-K8s部署.md)（要 minikube；没有的话直接看 [07-本地调试.md](07-本地调试.md)）
