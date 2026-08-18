# 05 · Docker 启动全流程

🎯 **这一节验证什么：** 从"改配置"到"四个组件跑起来"的完整链路：生成部署文件 → 建库 → 迁移 → 启动 → 查状态 → 停。

前置：**02 做完**（配置里有组件）；Docker 可用，且四个组件的镜像已构建（见 [00-准备](00-准备.md)）。

> 03 与 04 **不是这一篇的前提**——它们讲的是「平台怎么算出该启动谁」，属于理解性内容。
> 想先看到东西跑起来的话，从 02 直接过来即可；跑完再回头看那两篇会更容易，
> 因为那时你有真容器可以对照。

---

## 5.1 声明基础资源

组件的 `component.yaml` 里只说"我需要一个 postgresql"，**具体连哪台库由项目决定**。在 `brickkit.yaml` 里补上：

```yaml
resources:
  - kind: database
    engine: postgresql
    id: pg-main
    host: pg                    # ← 不含点 = 服务名 = 由 CLI 起一个容器
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
```

再在**项目根目录**放一个 `.env`（`.gitignore` 里已经忽略了它）：

```bash
echo "PG_PASSWORD=devpass" > .env
```

### `host` 决定了谁来起这个数据库

| `host` 写法 | 含义 | CLI 行为 |
| --- | --- | --- |
| `pg`（不含点） | 容器网络内的服务名 | **CLI 起一个 postgres 容器** |
| `db.example.com`、`10.0.0.5`、`localhost` | 外部地址 | 假设运维已部署，一行不碰 |

判据就是"含不含点"。生产环境写真实地址，本地开发写服务名让 CLI 代劳。

> **`host.docker.internal` 是第三种情形**：指向**宿主机**。用它连本机上已经跑着的
> 数据库，或者连另一个 BrickKit 项目共享出来的资源（见
> [18-多项目与共享.md](18-多项目与共享.md)）。平台会自动为绑定它的组件补
> `extra_hosts`，你不用手工建网络。

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

## 5.2 先看看会生成什么（不启动）

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
- `POSTGRES_PASSWORD=${PG_PASSWORD}`：**密码没有被写进文件**，是 compose 运行时从 `.env` 读的

---

## 5.3 第一次 up：它会告诉你还差什么

### ▶️ 操作

```bash
brickkit up
```

### ✅ 预期

CLI 会先把要建的库列出来：

```
📌 以下数据库需要预先创建（平台不代建，见 006 §9.5）：
   caller  （pg:5432，供 demo/caller 使用）
      CREATE DATABASE "caller";
   department  （pg:5432，供 department/tree 使用）
      CREATE DATABASE "department";
   people  （pg:5432，供 people/basic 使用）
      CREATE DATABASE "people";
   已经建过就无需再执行，建库是一次性操作
```

然后迁移会**失败**，因为库还不存在：

```
❌ 错误：docker 执行失败
   输出：... service "department-tree-1-0-0-migration" didn't complete successfully: exit 1
```

**这是正确行为，不是 bug。** 平台不建库（建库要 `CREATEDB` 权限，云数据库上应用账号通常没有；建了会造成"开发能跑、生产必炸"）。它的责任是**把要建的库说清楚**。

### 把库建出来

```bash
for db in caller department people; do
  docker exec brickkit-demo-shop-pg-1 psql -U brickkit -d postgres -c "CREATE DATABASE \"$db\""
done
```

---

## 5.4 再来一次：全绿

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
   people-basic-1-0-0           running（healthy）
✅ 全部组件已启动（4 个）
```

注意 `up` 是**等到健康才返回**的（compose 的 `--wait`）。它返回时，四个组件是真的可用，不是"启动命令发出去了"。

### 迁移是怎么跑的

由部署文件里的**一次性容器**执行，用组件**自己的镜像**（迁移脚本与业务代码永远同版本），跑完就退出。主服务 `depends_on` 它 `service_completed_successfully`——**迁移失败，主服务根本不会启动**，不会出现"新代码撞上旧表结构"。

---

## 5.5 查状态

### ▶️ 操作

```bash
brickkit status
```

### ✅ 预期

```
📊 项目状态：demo-shop（deploy.target: docker）

✅ 运行中（4 个组件）
 ┌─────────────────┬───────┬───────────────────┬────────────────────┐
 │ 组件            │ 版本  │ 状态              │ 端口               │
 ├─────────────────┼───────┼───────────────────┼────────────────────┤
 │ demo/hello      │ 1.0.0 │ 运行中（healthy） │ 8080/tcp           │
 │ demo/caller     │ 1.0.0 │ 运行中（healthy） │ 8080/tcp           │
 │ department/tree │ 1.0.0 │ 运行中（healthy） │ 8080/tcp           │
 │ people/basic    │ 1.0.0 │ 运行中（healthy） │ 8080/tcp, 9090/tcp │
 └─────────────────┴───────┴───────────────────┴────────────────────┘

📦 资源状态
 ┌─────────┬──────────┬────────────────────────┐
 │ 资源    │ 类型     │ 状态                   │
 ├─────────┼──────────┼────────────────────────┤
 │ pg-main │ database │ 可达（容器 pg 运行中） │
 └─────────┴──────────┴────────────────────────┘
```

`people/basic` 有两个端口——它的 `extraPorts` 声明了一个独立的 gRPC 端口（Python 的 grpcio 不能和 HTTP 共用端口）。

**CLI 自己不存运行状态**，`status` 每次都是现问引擎的。

---

## 5.6 看日志、进容器

```bash
# 看某个组件的日志（--project-directory 与 -p 都不能省）
docker compose --project-directory . -p brickkit-demo-shop \
  -f .brickkit/generated/docker-compose.yaml logs -f demo-hello-1-0-0

# 看迁移容器干了什么
docker compose --project-directory . -p brickkit-demo-shop \
  -f .brickkit/generated/docker-compose.yaml logs department-tree-1-0-0-migration
```

> `-p` 少了会**静默返回空**（compose 会拿部署文件所在目录名 `generated` 当项目名）；`--project-directory` 少了会刷一串 "variable is not set" 警告。`brickkit up` 的输出里给的就是完整命令，直接复制。

## 5.7 试一下 `--check-resources`

```bash
brickkit up --check-resources
```

启动前体检：外部资源拨号探一次、要占的宿主机端口有没有被**别的进程**占着。全部只警告不阻断。

---

## ⏹️ 怎么停

```bash
brickkit down                # 停掉全部组件；数据卷保留
brickkit down --only demo/caller   # 只停一个（依赖方先停）
```

`down` 之后再 `brickkit status`，会告诉你"没有正在运行的组件"。

**彻底清理（连数据库数据一起删）：**

```bash
docker compose --project-directory . -p brickkit-demo-shop \
  -f .brickkit/generated/docker-compose.yaml down -v
```

**`brickkit up` 中途 Ctrl-C 了：** 直接 `brickkit down` 收尾，不会有残留。

---

## 📁 代码在哪

| 能力 | 位置 |
| --- | --- |
| `up` 的完整流程 | [internal/cli/up.go](../internal/cli/up.go) |
| 升级检查、`--only`、`--check-resources` | [internal/cli/up_checks.go](../internal/cli/up_checks.go) |
| `down` / `status` | [internal/cli/down.go](../internal/cli/down.go)、[status.go](../internal/cli/status.go) |
| 生成 docker-compose.yaml | [internal/compose/compose.go](../internal/compose/compose.go) |
| 环境变量注入、配额合并 | [internal/inject/inject.go](../internal/inject/inject.go) |
| 调 docker compose | [internal/engine/compose.go](../internal/engine/compose.go) |
| 状态表格渲染 | [internal/cli/table.go](../internal/cli/table.go) |

设计书：005 §3（Docker 部署）、005 §6（迁移）、006 §9（建库责任）、004 §3.5–3.7。

---

➡️ 下一节：[06-本地调试.md](06-本地调试.md)（或直接跳 [07-K8s部署.md](07-K8s部署.md)）
