# Market API 参考

BrickKit Market 的 HTTP API 参考。这里描述的是**已实现的端点**，来源是 `market-server/internal/handler/handler.go` 的真实路由注册表——这份表由测试守着，不能凭空多写一个不存在的端点。

正常情况下你不需要直接调这些接口：`brickkit login`/`add`/`publish` 已经把它们封装好了。这篇文档面向的是要自己写市场客户端、或者要理解市场行为细节的人。

## 基础约定

**Base URL：** 你自己部署或使用的市场实例地址，所有路径都以 `/api/v1` 开头。

**认证：** 需要认证的接口在请求头带 Bearer Token：

```
Authorization: Bearer <market-token>
```

Token 从 `POST /api/v1/auth/login` 拿到——就是 `brickkit login` 写进 `.brickkit/credentials` 的那个值。public 组件的查询接口不需要认证；private 组件的所有操作、以及组件发布，都需要认证。

**响应信封：** 所有响应都是同一个形状：

```json
// 成功
{"success": true, "data": { ... }}

// 失败
{"success": false, "error": {"code": "...", "message": "...", "details": { ... }}}
```

`error.details` 是结构化详情（校验问题清单、冲突信息等），不是所有错误都有。

## 错误码

| Code | HTTP 状态 | 含义 |
| --- | --- | --- |
| `INVALID_REQUEST` | 400 | 请求体不是合法 JSON，或者字段校验没过 |
| `MANIFEST_INVALID` | 400 | 发布时提交的 Manifest 校验不过 |
| `CONFIG_SCHEMA_RESERVED_VARIABLE_CONFLICT` | 400 | `configSchema` 里的字段名和保留环境变量冲突 |
| `CLOSED_SOURCE_MISSING_API_CONTRACT` | 400 | 闭源组件没有提供 API 契约类产物 |
| `UNAUTHORIZED` | 401 | 没带 Token，或者 Token 已过期/无效 |
| `FORBIDDEN` | 403 | Token 有效，但这个身份没权限做这件事 |
| `COMPONENT_BLOCKED` | 403 | 组件已被市场管理员下架（`blocked`） |
| `NOT_FOUND` | 404 | 组件 / 版本 / 组织等资源不存在 |
| `CONFLICT` / `VERSION_ALREADY_EXISTS` | 409 | 状态冲突（比如版本号已经发布过） |
| `INTERNAL` | 500 | 市场内部错误，具体原因只留在服务端日志里，不对外泄漏 |

## 认证与账号

| 方法 | 路径 | 认证 | 说明 |
| --- | --- | --- | --- |
| POST | `/api/v1/auth/register` | 否 | 注册账号。请求体：`username`、`password`、`email`（可选）。响应体是 `User` 对象，不含密码哈希 |
| POST | `/api/v1/auth/login` | 否 | 登录。请求体：`username`、`password`。响应体含 `token`、`expiresAt`——就是 `brickkit login` 存进 `.brickkit/credentials` 的内容 |
| POST | `/api/v1/auth/logout` | 是 | 注销当前请求带的 Token（不是账号下所有 Token）。重复注销是幂等的，不需要请求体 |

⚠️ **注册请求体里如果带了 `orgId`，会被服务端直接忽略。** 组织成员关系本身就是私有组件的授权依据——如果注册时能自报组织，任何人写上别人的组织 ID 就能读走该组织全部私有组件的详情、Manifest、产物。入组只有一条路：下面的"添加组织成员"接口，而且只有组织所有者或市场管理员能调。

## 组织

| 方法 | 路径 | 认证 | 说明 |
| --- | --- | --- | --- |
| GET | `/api/v1/organizations` | 是 | 查询组织列表。普通用户只看得到自己所属的那个，市场管理员看全部 |
| POST | `/api/v1/organizations` | 是 | 创建组织，创建者自动成为所有者并入组 |
| POST | `/api/v1/organizations/{orgId}/members` | 是（仅组织所有者或市场管理员） | 添加组织成员 |

一个用户至多属于一个组织。把已经在别的组织里的人加进来时会**明确报冲突**，不会悄悄把他挪走——那样会让他原组织的私有组件突然读不到。重复添加同一个人是幂等的。

## 组件

| 方法 | 路径 | 认证 | 说明 |
| --- | --- | --- | --- |
| GET | `/api/v1/components` | 视可见性而定 | 搜索组件。查询参数：`keyword`、`page`、`pageSize`、`tags`（逗号分隔，可重复传）。只返回未下架的、且当前身份有权限看到的组件 |
| GET | `/api/v1/components/{scope}/{name}` | 视可见性而定 | 组件详情 |
| PUT | `/api/v1/components/{scope}/{name}/visibility` | 是 | 设置可见性（`public`/`private`） |
| GET | `/api/v1/components/{scope}/{name}/access` | 是 | 查询访问策略（private 组件授权给了哪些用户/组织） |
| PUT | `/api/v1/components/{scope}/{name}/access` | 是 | 更新访问策略 |

**故意不做的三个：**

| 没有的端点 | 为什么 |
| --- | --- |
| `POST /api/v1/components`（单独建组件） | 第一次 `publish` 时服务端自动建组件，单独的创建接口没有调用方 |
| `PUT /api/v1/components/{scope}/{name}`（改组件元数据） | 组件的 name/description/vendor 跟着 Manifest 走，发新版本时一并更新——单独能改会让市场上的描述和 Manifest 分叉 |
| `DELETE /api/v1/components/{scope}/{name}`（物理删除） | 已发布的东西不允许物理删除；要下架用版本状态改成 `blocked` |

## 版本

| 方法 | 路径 | 认证 | 说明 |
| --- | --- | --- | --- |
| GET | `/api/v1/components/{scope}/{name}/versions` | 视可见性而定 | 版本列表 |
| POST | `/api/v1/components/{scope}/{name}/versions` | 是 | 发布新版本（下面单独讲） |
| PUT | `/api/v1/components/{scope}/{name}/versions/{version}` | 是 | 更新版本状态（`draft`/`stable`/`deprecated`/`blocked`） |
| DELETE | `/api/v1/components/{scope}/{name}/versions/{version}` | 是 | 软删除版本 |
| GET | `/api/v1/components/{scope}/{name}/versions/{version}/manifest` | 视可见性而定 | 获取该版本的 Manifest |

**故意不做：** 单独的"版本详情"接口。版本列表里已经有全部信息，`brickkit` CLI 自己取的也是列表。

### 发布一个新版本

`POST /api/v1/components/{scope}/{name}/versions` 的请求体：

```json
{
  "version": "1.0.0",
  "status": "draft",
  "manifest": { /* component.yaml 解析后的 JSON */ },
  "sourceType": "git",
  "gitUrl": "https://github.com/org/repo",
  "changelog": "首次发布",
  "visibility": "public",
  "signature": {
    "algorithm": "cosign",
    "publicKeyRef": "keys/vendor.pub",
    "value": "<base64 签名值>",
    "signedBy": "release-bot@example.com"
  }
}
```

`sourceType` 是 `git`（开源，CLI 可以 clone 源码）或 `registry`（闭源，只有镜像和产物，必须提供 `api-contract` 类型的产物）。`signature` 可选——市场只存下来、做结构校验（算法认不认识、必填项在不在、`value` 是不是合法 base64），**不做密码学校验**，因为市场手里没有任何可信的公钥；真正的验签发生在安装方，用 `installer.publicKeys` 里配的公钥。

发布分三步：`POST .../versions`（建 `draft` 版本）→ 逐个 `POST .../artifacts/{artifactId}/upload`（上传产物内容）→ `PUT .../versions/{version}`（置为 `stable`）。`brickkit publish` 把这三步封装成了一条命令。

## 产物

| 方法 | 路径 | 认证 | 说明 |
| --- | --- | --- | --- |
| GET | `/api/v1/components/{scope}/{name}/versions/{version}/artifacts` | 视可见性而定 | 该版本的产物列表 |
| POST | `/api/v1/components/{scope}/{name}/versions/{version}/artifacts/{artifactId}/upload` | 是 | 上传某个产物的内容 |
| GET | `/api/v1/components/{scope}/{name}/versions/{version}/artifacts/{artifactId}/download` | 视可见性而定 | 下载产物文件 |

⚠️ 产物**清单**（有哪些产物、每个的类型）是发布版本时随 Manifest 一起登记的；上传接口传的是其中某一个产物的**内容**，`{artifactId}` 对应清单里的一项。

## 运维与审计

| 方法 | 路径 | 认证 | 说明 |
| --- | --- | --- | --- |
| GET | `/api/v1/health` | 否 | 健康检查。自托管市场的 compose healthcheck 探的就是这个端点 |
| GET | `/api/v1/audit` | 是 | 查询审计日志 |

## 深入阅读

- [签名与信任模型](architecture/signing-and-trust.md) —— 市场存签名但不验签名，为什么这么设计
- [自己搭一套 BrickKit Market](patterns/deployment/self-hosted-market.md) —— 部署市场本身
- [CLI 命令完整参考](architecture/cli-reference.md) —— `login`/`publish`/`add` 等命令怎么用这套 API
