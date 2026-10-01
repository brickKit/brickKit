# infra/api-docs

## 组件定位

把项目里各组件的 API 文档收拢到一个页面：HTTP 接口用 Swagger UI 展示，gRPC 接口列出服务与方法。哪个组件没装、连不上或没提供文档，页面上如实标出来，页面本身照常打开。

**负责**

- 探测已安装组件的 `GET {地址}/openapi.json`，并把抓到的 OpenAPI 原文代理给浏览器
- 通过 gRPC Reflection 列出组件的 gRPC 服务与方法（不需要 `.proto` 文件），在主端口或名为 `grpc` 的额外端口上
- 给出每个组件的文档状态：`ok`、`absent`、`unreachable`、`no-docs`
- 页面所需的 Swagger UI 静态资源（打进镜像，不从公网 CDN 加载）

**不负责（归谁）**

- 文档内容本身：各组件自己（在 `/openapi.json` 上提供 OpenAPI，或开启 gRPC Reflection）
- 业务请求：本组件只代理文档，不代理对各组件接口的调用；调用接口归调用者自己连那个组件
- 访问控制：本组件不做登录；谁能打开这个页面由部署方的网络与端口暴露决定
- 聚合下面六个组件以外的组件：要加一个目标归本组件的维护者改代码并发新版本

## 部署前准备

除下面的配置外无需准备：不需要数据库或任何外部系统，探测结果只放在内存里。

浏览器要能打开这个页面，部署方需要在部署文件里把本组件的端口（8080）暴露出来；其余组件不必暴露端口，它们的文档由本组件在内部网络里取来再代理出去。

## 依赖说明

六个依赖**全部是弱依赖**（`optional: true`）：装了几个就展示几个，一个都没装本组件也照样启动，页面上显示它们"未安装"。

- `department/tree`
- `people/basic`
- `auth/password-login`
- `authorization/rbac`
- `erp/backend`
- `infra/redis-event-bus`

对每个已注入地址的依赖，本组件两条路都试：取它主端口上的 `/openapi.json`，再用 gRPC Reflection 列服务——依赖声明了名为 `grpc` 的额外端口（平台另注入 `<ID>_GRPC_ENDPOINT`，例如 `people/basic` 的 9090）时在那个端口上列，否则在主端口上列（HTTP 与 gRPC 共用一个端口的 `department/tree`）；哪条通了就展示哪条，两条都通就都展示。gRPC 开在别的名字的额外端口上的组件只展示它的 OpenAPI。某个依赖缺席或出故障，最坏也只是它自己那一行显示成相应状态，不影响其余几个。

## 配置指南

没有必需配置，六个依赖的地址都由平台按弱依赖声明注入，不要在 `config/` 里手写。

- `LOG_LEVEL`：排查探测问题时调到 `debug`；不认识的值按 `info` 处理。

探测结果缓存 30 秒：新装上或刚恢复的组件，最多 30 秒后才会出现在页面上。

## 契约索引

本组件不发布契约文件（`component.yaml` 里没有 `artifacts`），它只是个展示入口。对外的 HTTP 接口：

- `GET /`：Swagger UI 与聚合状态表
- `GET /api/v1/sources`：每个目标组件的文档状态，`{sources, total}`；每项有 `componentId`、`status`、`reason`、`kinds`（`openapi` / `grpc`），有 OpenAPI 时带 `specUrl`，有 gRPC 时带 `grpcServices`。响应里**不包含**组件的内部地址。
- `GET /api/v1/openapi/{组件 ID}`：代理出去的 OpenAPI 原文，例如 `/api/v1/openapi/people/basic`；组件没有 OpenAPI 或 ID 不认识时返回 404。
- `GET /healthz`：只检查本进程存活，绝不探测那六个组件。

看到空页面时，先看 `/api/v1/sources` 的状态再决定怎么做：

| 状态 | 含义 | 该做什么 |
| --- | --- | --- |
| `ok` | 拿到文档了 | 无需处理 |
| `absent` | 平台没注入地址，即这个组件没装 | 装上它 |
| `unreachable` | 地址在，但连不上 | 去看那个组件为什么不在线 |
| `no-docs` | 组件在线，但两条路径都没有 | 让它提供 `/openapi.json` 或开启 gRPC Reflection |

```bash
curl -s "$INFRA_API_DOCS_ENDPOINT/api/v1/sources"
```

发布的事件：无。消费的事件：无。

## 外壳声明

不是外壳。
