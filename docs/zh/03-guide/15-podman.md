# 15. 用 Podman 代替 Docker 部署

同一份 Manifest、同一份 `brickkit.yaml`——唯一变的是谁来跑它，而且跟 Kubernetes 不一样，这次连 `brickkit.yaml` 里的字段都不用改：`target: podman` 写在 `override.yaml` 里（AGENTS.zh.md §5.10），是个人的、被 gitignore 掉的、按开发者各自选的东西，从来不需要让审 `brickkit.yaml` 的同事看到。这篇把[第 2 篇](02-what-runs.md)里的 `demo/hello` + `demo/caller` 这一对重新部署一遍——带上 `demo/caller` 从那篇起就一直缺的那个真实数据库，绑法跟[第 7 篇](07-assemble-and-break.md)给 Docker 绑的一模一样——这次换成真的 Podman，证明同一次跨容器调用、同一次弱依赖降级、同一份生成文件，全都照样成立。

**前置条件：** 一套能用的 rootless Podman。具体到 Ubuntu/Debian，`down` 需要先满足一个环境前提——[Podman 环境检查清单](../07-patterns/11-podman-environment-checklist.md)给了一行就能自查的方法和修法。用 `podman build` 重新构建这个系列一直在用的两个镜像——Podman 有自己独立的镜像存储，跟 Docker 是分开的：

```bash
podman build -t brickkit-demo/hello:1.0.0 tests/components/demo-hello
podman build -t brickkit-demo/caller:1.0.0 tests/components/demo-caller
```

## 部署一个真实数据库——跟第 7 篇给 Docker 用的配方完全一样

```bash
podman run -d --name guide-pg -p 15432:5432 \
  -e POSTGRES_PASSWORD=devpass -e POSTGRES_DB=callerdb \
  postgres:16-alpine
```

## 项目配置

跟第 7 篇一样的两个组件、一样的资源绑定——`brickkit.yaml` 完全不用改，连 `host.docker.internal` 都原样保留：

```yaml
components:
  - id: demo/hello
    version: 1.0.0
    expose: true
    exposePort: 18098
  - id: demo/caller
    version: 1.0.0
    expose: true
    exposePort: 18099

resources:
  - kind: database
    engine: postgresql
    id: caller-db
    host: host.docker.internal
    port: 15432
    username: postgres
    password: ${DB_PASSWORD}
    bindings:
      - componentId: demo/caller
        database: callerdb
```

`host.docker.internal` 这个名字不管换什么引擎都不用改，不是疏漏——它是平台唯一认得的那个字面量，认出它就会自动补一条 `extra_hosts: host.docker.internal:host-gateway`（AGENTS.zh.md §5.2），而 `host-gateway` 是个 Podman 跟 Docker 认得完全一样的魔法值。真正选引擎的是 `override.yaml`：

```yaml
target: podman

components:
  - id: demo/hello
  - id: demo/caller
```

## 真的部署一次

```bash
export DB_PASSWORD=devpass
brickkit up
```

```
🚀 启动项目 hello-world（deploy.target: podman）
⚠️ 警告：弱依赖缺失：demo/bus@1.0.0
   影响组件：demo/caller@1.0.0
   原因：该组件在所有安装源中均未找到
   影响：该组件的环境变量 DEMO_BUS_ENDPOINT 不会被注入
   💡 弱依赖降级由组件自行处理；如需启用，请确认该组件已发布并可从安装源获取
📋 组件状态计算：
   ✅ demo/hello@1.0.0   启动（demo/caller 需要）
   ✅ demo/caller@1.0.0  启动（顶层）

📋 启动顺序（拓扑排序）：
   1. demo-hello-1-0-0   无依赖
   2. demo-caller-1-0-0  ← 依赖 1

可独立启动：demo-hello-1-0-0（无依赖）
最长依赖链（2 层）：demo-hello-1-0-0 → demo-caller-1-0-0

依赖图：
   demo/caller@1.0.0 → demo/hello@1.0.0
                     → demo/bus@1.0.0（弱，未安装）
📄 已生成：.brickkit/generated/compose.yaml

📌 以下基础资源需要先跑起来（平台不代为部署）：
   caller-db    postgresql   host.docker.internal:15432  供 demo/caller 使用
      需要库 callerdb（供 demo/caller 使用）：CREATE DATABASE "callerdb";
   库也要预先建好；已经建过就无需再执行，建库是一次性操作
   本地开发想快速起一套：docker compose -f deploy/dev-resources/docker-compose.yaml up -d

🔧 启动前会执行的数据库迁移（失败则该组件不会启动）：
   demo/caller@1.0.0  /app/caller migrate

🔍 检测镜像拉取权限... ✅ 全部通过

🐳 正在启动（podman）...
   demo-hello-1-0-0             running（healthy）
   demo-caller-1-0-0            running（healthy）
✅ 全部组件已启动（2 个）

💡 查看状态：brickkit status
   查看日志：podman compose -p brickkit-hello-world logs -f
```

生成的是 `compose.yaml`，不是 `docker-compose.yaml`——这份文件遵循的是引擎无关的 Compose 规范，Podman 消费的是跟 Docker 完全同一份文件（AGENTS.zh.md §5.10）。日志提示这次也已经说的是 `podman compose`，不是 `docker compose`——同一份文件，这次真正负责的引擎换了。

## 证明整条链路真的通

```bash
curl http://localhost:18098/api/v1/hello
```
```json
{"component":"demo/hello","greeting":"Hello","message":"Hello, I'm demo/hello@1.0.0","version":"1.0.0"}
```

`demo/caller` 调 `demo/hello`——一次走 Podman 自己网络和 DNS 的真实容器间调用，不只是一个看起来对的环境变量：

```bash
curl http://localhost:18099/api/v1/call
```
```json
{"component":"demo/caller","endpoint":"http://demo-hello-1-0-0:8080","upstream":{"component":"demo/hello","greeting":"Hello","message":"Hello, I'm demo/hello@1.0.0","version":"1.0.0"},"version":"1.0.0"}
```

而第 2 篇那个缺失的弱依赖，在这里同样能看得见：

```bash
curl http://localhost:18099/api/v1/status
```
```json
{"component":"demo/caller","eventBus":"degraded","hello":"http://demo-hello-1-0-0:8080","version":"1.0.0"}
```

`brickkit status` 从平台自己这一侧也能确认：

```bash
brickkit status
```
```
📊 项目状态：hello-world（deploy.target: podman）

✅ 运行中（2 个组件）
 ┌─────────────┬───────┬───────────────────┬──────────┐
 │ 组件        │ 版本  │ 状态              │ 端口     │
 ├─────────────┼───────┼───────────────────┼──────────┤
 │ demo/hello  │ 1.0.0 │ 运行中（healthy） │ 8080/tcp │
 │ demo/caller │ 1.0.0 │ 运行中（healthy） │ 8080/tcp │
 └─────────────┴───────┴───────────────────┴──────────┘

📦 资源状态
 ┌───────────┬──────────┬─────────────────────────┐
 │ 资源      │ 类型     │ 状态                    │
 ├───────────┼──────────┼─────────────────────────┤
 │ caller-db │ database │ 可达（localhost:15432） │
 └───────────┴──────────┴─────────────────────────┘
```

## 一个真实踩过的坑：`down` 曾经是唯一跑不通的那一步

```bash
brickkit down
```
```
🛑 停止项目 hello-world
✅ 已停止全部组件

💡 数据卷未删除，数据库数据仍然保留
   需要彻底清理时手动执行：podman volume rm <卷名>
   重新启动：brickkit up
```

这条命令能退出 `0`，正是这篇文章存在的全部意义。Rootless Podman 的网络拆卸辅助进程 `pasta`，需要收到 `podman` 发来的 `SIGTERM` 才能把这次部署的网络干净拆掉——而大多数 Ubuntu/Debian 默认的 AppArmor 策略恰好会拦下这一个信号，这是一个已确认的发行版打包缺口（[containers/podman#27372](https://github.com/containers/podman/issues/27372)），不是 BrickKit 或 Podman 的 bug。这正是当初 `target: podman` 被撤掉、直到最近才重新回来的原因（AGENTS.zh.md §5.10）：一个能起来但拆不掉的项目，比一个从没起来的更糟。如果你这里 `down` 报的不是上面那样干净的退出，而是 `rootless netns: kill network process: permission denied`，那就是这一个前提没满足——[环境检查清单](../07-patterns/11-podman-environment-checklist.md)给了修法，BrickKit 自己的报错也已经指回了那里。

而且上面那条清理提示说的是 `podman volume rm`，不是 `docker volume rm`——很小的一个细节，但跟 `compose.yaml` 不再以某个引擎命名是同一类事：Podman 一旦成了真正的部署目标，每一条提到具体命令的提示，就都得说对是哪个引擎的命令。

## 清理

```bash
podman rm -f guide-pg
podman rmi brickkit-demo/hello:1.0.0 brickkit-demo/caller:1.0.0
```

---

这是当前这个系列的最后一篇（还规划了哪些内容见[教程索引](README.md)）。这个系列动手走过的任何一个具体机制，想深挖细节，`docs/zh/06-architecture/` 和 `docs/zh/07-patterns/` 里都有配着真实代码和真实生成输出的完整讲解——AGENTS.zh.md §11.2 有完整的索引。
