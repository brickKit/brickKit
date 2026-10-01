# demo/hello

## 组件定位

BrickKit 平台自己的自测组件，不是业务组件：一个最小的 HTTP 服务，回一句问候语，并把平台注入的环境变量原样回显，让环境变量注入、部署文件生成、健康检查、多版本共存这些平台能力有一个真能跑起来的容器可验证。

**负责**

- `GET /api/v1/hello`：用配置的问候语回答，并报出自己的组件 ID 与版本
- `GET /api/v1/env`：回显 `COMPONENT_ID`、`COMPONENT_VERSION`、`GREETING` 三个变量的实际值，供平台核对注入结果
- `GET /healthz`：只报告本进程存活

**不负责（归谁）**

- 调用其他组件、依赖注入的另一端：归 `demo/caller`，它把 `demo/hello` 当强依赖来调
- 任何业务数据与持久化：没有，平台自测不需要
- 构建与发布镜像：归跑自测的人（见下一节）

## 部署前准备

镜像 `brickkit-demo/hello:1.0.0` 要先在本机存在：`brickkit up` 不会构建它。由跑自测的人构建——在 BrickKit 仓库根目录执行 `make demo-images`（同一份源码会打出 `1.0.0` 与 `2.0.0` 两个标签，供多版本共存验证），或在本组件目录执行 `docker build -t brickkit-demo/hello:1.0.0 .`。用 `docker image inspect brickkit-demo/hello:1.0.0` 确认已经有了。

除此之外无需准备。

## 依赖说明

没有依赖：不依赖任何组件，也不需要数据库或其他外部系统。

## 配置指南

- `GREETING` 只影响 `/api/v1/hello` 的 `greeting` 字段和拼出来的 `message`（形如 `<GREETING>, I'm demo/hello@1.0.0`），也会出现在 `/api/v1/env` 的回显里。想验证"配置改了、容器里拿到的确实是新值"，改它再 `brickkit up`，然后看这两个接口。
- 写成空串等于没写：组件读到空值时回退到 `Hello`，所以没法用它把问候语清空。

## 契约索引

- `openapi.json`：三条只读接口，都返回 JSON
  - `GET /healthz`：`{"status":"ok"}`
  - `GET /api/v1/hello`：`component`、`version`、`greeting`、`message` 四个字段
  - `GET /api/v1/env`：`{"env": {"COMPONENT_ID": …, "COMPONENT_VERSION": …, "GREETING": …}}`，没注入的变量值为空串
- 不发布事件，也不消费事件。

## 外壳声明

不是外壳。
