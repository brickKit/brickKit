# demo/caller

## 组件定位

BrickKit 平台自己的自测组件，不是业务组件：一个会调用别人的 HTTP 服务，让平台的四条承诺能被真容器验证——强依赖地址注入、可选依赖缺席时组件自行降级、配置项注入为同名环境变量、迁移在主服务之前执行且失败时阻断主服务。

**负责**

- `GET /api/v1/call`：用平台注入的 `DEMO_HELLO_ENDPOINT` 调 `demo/hello`，把上游响应原样带回
- `GET /api/v1/status`：报告可选依赖 `demo/bus` 的地址有没有注入（`available` / `degraded`）
- `GET /api/v1/env`：回显平台注入的组件标识、两个依赖地址和 `DATABASE_*` 配置，供平台核对注入结果
- 迁移命令 `/app/caller migrate`：配了数据库时检查它连得上，打开测试开关时故意失败
- `GET /healthz`：只报告本进程存活，上游挂了也照样 200

**不负责（归谁）**

- 问候语本身：归 `demo/hello`
- 真正使用事件总线：归 `demo/bus` 一类组件；这里只看它的地址在不在，从不调用它
- 数据库本身（实例、库、账号）：归部署方，见下一节；本组件没有真实的表，迁移不建表

## 部署前准备

- 镜像 `brickkit-demo/caller:1.0.0` 要先在本机存在：`brickkit up` 不会构建它。由跑自测的人构建——在 BrickKit 仓库根目录执行 `make demo-images`，或在本组件目录执行 `docker build -t brickkit-demo/caller:1.0.0 .`。用 `docker image inspect brickkit-demo/caller:1.0.0` 确认。
- 只有配了 `DATABASE_HOST` 时：那台 PostgreSQL 要在 `up` 之前就能从容器网络里连上 `DATABASE_HOST:DATABASE_PORT`，由部署方准备。迁移本身就是检查：连不上时迁移以非 0 退出，主服务不会启动。不需要预先建库、建表或建账号。

## 依赖说明

- `demo/hello`（强依赖）：`/api/v1/call` 调它的 `GET /api/v1/hello`，地址来自平台注入的 `DEMO_HELLO_ENDPOINT`，超时 3 秒。项目里没声明它时 CLI 直接报错、不会启动。地址意外为空时 `/api/v1/call` 返回 424 并点名这个变量；上游不可达、非 200 或不是合法 JSON 时返回 502，响应里带着用过的地址。两种情况下 `/healthz` 都不受影响。
- `demo/bus`（可选依赖）：只用来报告状态，组件从不向它发请求。项目里没有它时平台完全不注入 `DEMO_BUS_ENDPOINT`，`/api/v1/status` 的 `eventBus` 报 `degraded`，其余接口照常工作；有它时报 `available`。

## 配置指南

- **想验证配置注入**：只写 `DATABASE_*` 不需要真有数据库也能看到效果——`/api/v1/env` 会把它们原样回显。但只要写了 `DATABASE_HOST`，迁移就会去连它，连不上整个组件起不来。
- **`DATABASE_HOST`**：填迁移容器能解析并连上的 PostgreSQL 主机名或 IP。不写时迁移跳过数据库检查直接成功——没有数据库的部署不会被迁移卡住。
- **`DATABASE_PORT`**：只在写了 `DATABASE_HOST` 时才有用。
- **`DATABASE_NAME`、`DATABASE_USER`**：只出现在 `/api/v1/env` 的回显里，迁移不用它们登录（迁移只做 TCP 连通检查），所以填错不会让迁移失败。没有口令配置项。
- **`MIGRATION_SHOULD_FAIL`**：只有值恰好是 `1` 才让迁移失败，用来验证"迁移失败会阻断主服务"；其他任何值都等于没写。验证完要删掉，否则每次 `up` 主服务都起不来。

## 契约索引

- `openapi.json`：四条只读接口，都返回 JSON
  - `GET /healthz`：`{"status":"ok"}`，不探上游
  - `GET /api/v1/call`：成功 200，返回 `component`、`version`、`endpoint` 与 `upstream`（`demo/hello` 的原始响应）；依赖地址没注入 424，调上游失败 502，两者都带 `error` 字段
  - `GET /api/v1/status`：`component`、`version`、`hello`（注入的地址）、`eventBus`（`available` 或 `degraded`）
  - `GET /api/v1/env`：`{"env": {…}}`，键是 `COMPONENT_ID`、`COMPONENT_VERSION`、`DEMO_HELLO_ENDPOINT`、`DEMO_BUS_ENDPOINT` 与四个 `DATABASE_*`；没注入的值为空串，`DEMO_BUS_ENDPOINT` 为空正说明平台没注入它
- 不发布事件，也不消费事件。

## 外壳声明

不是外壳。
