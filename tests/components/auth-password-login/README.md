# auth/password-login

口令登录组件：校验用户名与口令，签发 JWT。BrickKit Phase 5 的第三个测试组件。

## 它验证平台的什么

前两个组件（department/tree、people/basic）都有自己的数据可查。这一个不一样：
**它强依赖别人，自己却没有可展示的数据**，全部价值在于"依赖正常/异常时能不能给出正确的判断"。

| 验证点 | 怎么体现 |
| --- | --- |
| 强依赖注入 | `PEOPLE_BASIC_ENDPOINT` 由平台按 Manifest 的 `dependencies.components` 注入 |
| 强依赖故障的正确表现 | people/basic 挂掉时报 **503**，不是 401；健康检查仍然 200 |
| 健康检查不越界 | `/healthz` 不查库、不调 people/basic（002 §9.4） |
| 配置只来自环境变量 | 缺 `JWT_SECRET` 等任一项直接启动失败，**绝不用默认值顶上** |
| migration | 与服务共用同一个二进制，幂等 |
| 敏感信息不落地 | 日志、响应、令牌里都不出现口令与哈希 |

## 职责边界（这是它最值得抄的地方）

```
本组件        只管「怎么证明你是你」：用户名 → 口令哈希 → 令牌
people/basic  管「你是谁」：姓名、部门、职务
```

登录时向 people/basic 现取身份，不在本组件重复存储。好处有两个：

- **主体的存废由人员系统说了算。** 员工从 people/basic 删掉后，凭据表哪怕还留着也登录不进来。
- **不会出现两份会漂移的身份数据。** 部门调整只需要改一处。

代价是多了一次网络调用，以及 people/basic 挂掉时登录不可用——这正是"强依赖"的含义，
所以要如实报 503，而不是假装成认证失败。

## API

| 端点 | 说明 |
| --- | --- |
| `POST /api/v1/login` | `{username, password}` → `{token, expiresAt, personId, username}` |
| `POST /api/v1/verify` | `{token}` → `{personId, username, departmentId, expiresAt}` |
| `GET /healthz` | 只检查本进程存活 |

**`/api/v1/verify` 为什么存在：** 令牌用 HS256 签名，密钥只有本组件有。没有这个端点，
签出去的令牌对下游（erp/backend、authorization/rbac）就是一串不可用的字符串。

### 状态码的语义

| 码 | 含义 | 什么时候 |
| --- | --- | --- |
| 400 | 你没说清楚要什么 | 缺字段、不是 JSON |
| 401 | 你是谁我不认 | 口令错、用户不存在、人已不在 people/basic |
| 503 | 我这边有问题 | 数据库连不上、people/basic 不可用 |

把 503 报成 401，使用者会在自己的密码上白折腾半天；反之又会让人去查根本没坏的依赖。

**用户不存在与口令错误返回完全相同的响应**（状态码、文案、字段全都一样）。
任何差别都能被拿来把一份用户名字典筛成"这些账号真实存在"，那是撞库的第一步。

## 配置

| 环境变量 | 来源 | 说明 |
| --- | --- | --- |
| `DATABASE_*` | 平台按资源绑定注入（006 §5） | HOST / PORT / NAME / USER / PASSWORD |
| `PEOPLE_BASIC_ENDPOINT` | 平台按强依赖注入（003 §4.5） | people/basic 的地址 |
| `JWT_SECRET` | **你自己提供**（`.env` / K8s Secret） | 令牌签名密钥，至少 32 字节 |
| `LOG_LEVEL` | `configSchema.logLevel` | debug / info / warn / error |
| `TOKEN_TTL_SECONDS` | `configSchema.tokenTtlSeconds` | 令牌有效期，默认 1800 |

**缺任何一项都直接启动失败，不会用默认值顶上。** 对 `JWT_SECRET` 尤其重要：
一个内置默认密钥意味着所有装了这个组件的人共用同一把钥匙，任何人都能给任何一处部署
签出管理员令牌，而且看起来一切正常。

密钥短于 32 字节也拒绝启动（RFC 8725 §3.5）：HS256 的安全性完全等于密钥强度，
弱密钥可以离线暴力破解，破了不会在任何地方留下痕迹。

## 样例账号

`0002_seed_credentials` 会写入四个账号，对应 people/basic 的样例人员：

| 用户名 | personId | 口令 |
| --- | --- | --- |
| zhangsan | p-001 | `demo-password` |
| lisi | p-002 | `demo-password` |
| wangwu | p-003 | `demo-password` |
| zhaoliu | p-004 | `demo-password` |

⚠️ **只能用于本地试用。** 真实部署请先 `migrate down 1` 把这一版数据回退掉。

四个账号口令相同，但库里的哈希各不相同——每行的盐都不一样。这正是加盐要解决的问题：
否则一眼就能看出"这几个人用了同一个密码"。

## 本地运行

组件的数据库按设计由人创建（006 §9.1：CLI 不负责建库）：

```bash
docker exec my-postgres psql -U postgres -c "CREATE DATABASE brickkit_auth"
```

跑测试：

```bash
# 单元测试（内存实现，不需要数据库）
go test ./...

# 含 PostgreSQL 契约测试与迁移测试
AUTH_TEST_DATABASE_URL="postgres://postgres:PASSWORD@localhost:5432/brickkit_auth?sslmode=disable" \
  go test ./...
```

或用仓库根目录的 `make test-components` / `make test-components-integration`。

## 口令哈希

PBKDF2-HMAC-SHA256，600000 轮（OWASP 2023 建议值），16 字节随机盐。

存储格式：`pbkdf2-sha256$<迭代次数>$<盐 base64>$<派生密钥 base64>`——
带上算法与参数，将来换算法时旧哈希还认得出该怎么验。

比较用 `subtle.ConstantTimeCompare`：普通的 `==` 会在第一个不同的字节处返回，
攻击者据此可以逐字节把哈希试出来。

## 令牌

HS256，载荷含 `sub`（personId）、`iat`、`exp`、`nbf`、`username`、`departmentId`。

校验时**只接受 HS256**，不看令牌自称的算法——那正是 `alg=none` 与算法混淆攻击的入口
（RFC 8725 §3.1）。`token_test.go` 里有专门的用例锁这一条。

载荷只是 base64，**不是加密**：任何拿到令牌的人都能读。所以里面不放口令、不放哈希、
不放任何秘密。

## 设计依据

002 组件规范（§1.4 组件约束、§8 迁移、§9.4 健康检查、§11 日志）、
003 §4.5（依赖地址注入）、006 §5（资源环境变量）、008（安全与治理）。
