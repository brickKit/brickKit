# infra/api-docs

文档聚合组件：把各组件的 API 文档收拢到一个入口。BrickKit Phase 5 的第八个、
也是最后一个测试组件。

## 它在平台里的位置很特殊

**全部依赖都是弱依赖** —— 这是唯一一个这样的组件。六个目标组件装了几个就展示几个，
一个都没装也照样起得来。

这不是"容错做得好"，而是这个组件的本来面目：**文档入口不该因为某个业务组件没装
就打不开**，而且业务组件全挂的时候，正是最需要看文档的时候。开发计划 28.3
要验的就是这一点。

## 两条发现路径

| 路径 | 怎么拿 | 谁提供 |
| --- | --- | --- |
| OpenAPI | `GET {endpoint}/openapi.json` | FastAPI 之类的框架自带（people/basic） |
| gRPC | **Reflection**，不需要 `.proto` 文件 | department/tree、authorization/rbac |

Reflection 的价值在于**不必预先 vendored 一堆契约**：组件升级加了新方法，
这里自动跟上。Step 21 验证过 `grpcurl` 能这么用，这里是同一套机制的程序化调用。

## 四种状态，各自对应不同的处置

这是这个组件最要紧的设计。看到空页面时，使用者需要知道**接下来该做什么**：

| 状态 | 含义 | 该做什么 |
| --- | --- | --- |
| `ok` | 拿到文档了 | — |
| `absent` | 平台没注入地址 = 组件**没装** | 装上它 |
| `unreachable` | 地址在，但连不上 | 去看那个组件 |
| `no-docs` | 组件在线，两条路径都没有 | 让它提供 `/openapi.json` 或开 Reflection |

混成一种的话，使用者只能对着空页面猜。`GET /api/v1/sources` 本身就是排障工具。

## API

| 端点 | 说明 |
| --- | --- |
| `GET /` | Swagger UI + 聚合状态表 |
| `GET /api/v1/sources` | 谁有文档、谁没装、谁连不上 |
| `GET /api/v1/openapi/{组件ID}` | 代理出去的 OpenAPI 原文 |
| `GET /healthz` | 只检查本进程存活 |

### 为什么要代理 OpenAPI 而不是让浏览器直连

那些组件**默认不暴露端口**（008 §5.2），浏览器根本连不上；就算连得上也会撞跨域。
由本组件代理是唯一走得通的路。

同理，`/api/v1/sources` 的响应里**不包含组件的内部地址** —— 那等于把内网结构
告诉任何能打开这个页面的人。

## Swagger UI 从镜像拷，不从 CDN 加载

```dockerfile
FROM swaggerapi/swagger-ui:v5.17.14 AS swagger
...
COPY --from=swagger /usr/share/nginx/html/swagger-ui.css ... /app/web/swagger-ui/
```

008 §5 的"默认不暴露"意味着这个页面很可能跑在内网甚至气隙环境里。
指向公网 CDN 的 `<script>` 会让页面永远转圈 —— 而症状看起来像"文档组件坏了"。
版本钉死才可复现。

## 探测结果有 30 秒缓存

每次刷新页面都去探六个组件的话，一个卡住的上游会让页面很慢；
而组件的 API 文档几乎不会在几十秒内变。缓存会过期 —— 否则新上线的组件永远看不到。

探测是**并发**的，且每个目标各自捕获错误：一个组件出问题最坏也只是它自己
显示成"不可用"。

## 配置

| 环境变量 | 来源 | 必需 |
| --- | --- | --- |
| `DEPARTMENT_TREE_ENDPOINT` 等六个 | 平台按**弱依赖**注入 | ❌ 全都不是 |
| `LOG_LEVEL` | `configSchema.logLevel` | ❌ |

这个组件**没有任何必需配置**。把任何一个列成必需，就等于要求使用者必须把
六个组件全装上才能看文档。

`config.go` 里的 `aggregated` 清单必须与 `component.yaml` 的弱依赖声明一一对应 ——
漏声明的表现是"那个组件在页面上永远显示未安装"，因为平台根本不会注入它的地址。

## 已知的空白

Go 组件目前**不在运行时暴露 `/openapi.json`**（它们的 `openapi.json` 是作为
市场产物分发的，没进镜像）。所以 department/tree 与 authorization/rbac 在这里
只显示 gRPC 文档，auth/password-login 与 erp/backend 显示 `no-docs`。

要让它们出现在 Swagger UI 里，得给这些组件加一个 `GET /openapi.json`。
那是对四个已完成组件的改动，登记在《开发进度》延后清单里。

## 本地运行

```bash
go test ./...    # 不需要任何外部服务：目标组件都有 httptest 替身
```

## 设计依据

002 组件规范（§1.4 组件约束、§7 契约即产物、§9.4 健康检查、§11 日志）、
003 §4.3（弱依赖）、008 §5.2（默认不暴露端口）、开发计划 §0.2（Swagger UI +
gRPC Reflection 聚合）。
