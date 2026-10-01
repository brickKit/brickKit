# auth/password-login

校验用户名与口令，签发 JWT；身份来自 `people/basic`，不在这里另存一份。

## 组件定位

只管"怎么证明你是你"：用户名 → 口令哈希 → 令牌，并替下游校验它签出的令牌。"你是谁"（姓名、部门）在 `people/basic`，登录时现取——员工从人员系统删掉后，凭据哪怕还留着也登录不进来。

**负责**

- 登录名与口令哈希（`credentials` 表），校验口令
- 签发 HS256 令牌，以及 `POST /api/v1/verify` 替下游校验令牌（签名密钥只有本组件有）
- 把"认不了你"（401）与"我这边有问题"（503）分清楚

**不负责（归谁）**

- 建数据库、生成签名密钥：使用者（见下一节）
- 人员身份与是否在职：`people/basic`
- 这个人能做什么：`authorization/rbac`
- 注册账号、改口令、找回口令：本组件没有这些接口；账号由管库的人写进 `credentials` 表（见下一节）
- 吊销已签出的令牌：没有这个机制，令牌在过期前一直有效（见配置指南的 `TOKEN_TTL_SECONDS`）

## 部署前准备

**1. 建数据库。** 平台不会替你创建数据库：表由本组件的迁移建，库本身要使用者先建好，执行一次即可。约定的库名是 `brickkit_auth`（想用别的名字，改 `DATABASE_NAME` 即可）：

```bash
psql -U postgres -c "CREATE DATABASE brickkit_auth"

# 或者数据库跑在容器里
docker exec -i my-postgres psql -U postgres -c "CREATE DATABASE brickkit_auth"
```

确认建好了：`psql -U postgres -tc "SELECT 1 FROM pg_database WHERE datname='brickkit_auth'"` 输出 `1`。每个组件用自己的库：共用一个库意味着一个组件能读到另一个组件的表；迁移发现库里有别的组件的记录时会打一条警告。

**2. 准备签名密钥。** 由使用者生成一个至少 32 字节的随机值（例如 `openssl rand -base64 48`），放进项目的 `.env`（`JWT_SECRET=...`）或一个文件里，`config/` 里只写引用（见配置指南）。

**3. 样例账号。** 迁移 `0002_seed_credentials` 会写入四个账号，对应 `people/basic` 的样例人员，口令都是 `demo-password`：

| 用户名 | personId |
| --- | --- |
| zhangsan | p-001 |
| lisi | p-002 |
| wangwu | p-003 |
| zhaoliu | p-004 |

⚠️ **只能用于本地试用。** 真实部署在第一次 `brickkit up` 之后把它们删掉：

```sql
DELETE FROM credentials WHERE username IN ('zhangsan', 'lisi', 'wangwu', 'zhaoliu');
```

不要用 `migrate down 1` 回退这一版来清：平台每次 `brickkit up` 都会先跑迁移，被回退的 `0002_seed_credentials` 会被重新执行，账号又回来了。删行则不会，因为这一版仍记为已执行。

**4. 真实账号。** 往 `credentials` 表写 `username`、`person_id`、`password_hash`。`person_id` 必须是 `people/basic` 里存在的人，否则这个账号永远登录失败（401）。`password_hash` 的格式是 `pbkdf2-sha256$600000$<盐>$<派生密钥>`：PBKDF2-HMAC-SHA256、600000 轮、16 字节随机盐、32 字节派生密钥，盐和派生密钥都用不带填充的标准 base64。读不懂的哈希一律视为口令错误，不会放行。

## 依赖说明

- `people/basic`（强依赖）：口令校验通过后，用 `GET /api/v1/people/{personId}` 确认这个人还在人员系统里，并取它的部门 ID 写进令牌。地址由平台注入为 `PEOPLE_BASIC_ENDPOINT`，缺失时组件启动即失败。对方回 404：登录回 401，与口令错误完全相同；对方不可用、超时（5 秒）或回任何其他非 200：登录回 **503**，`/healthz` 仍是 200。`POST /api/v1/verify` 不调用它。

## 配置指南

必填的 `JWT_SECRET`、`DATABASE_HOST`、`DATABASE_NAME`、`DATABASE_USER` 缺任何一个，组件启动即失败并一次列出缺了哪几个，不会用默认值顶上。

- `JWT_SECRET`：令牌签名密钥，**故意没有默认值**——内置默认密钥意味着所有装了这个组件的人共用同一把钥匙，任何人都能给任何一处部署签出管理员令牌，而且看起来一切正常。短于 32 字节也拒绝启动（RFC 8725 §3.5）：HS256 的安全性完全等于密钥强度，弱密钥可以离线暴力破解，破了不会留下痕迹。写成 `JWT_SECRET: ${JWT_SECRET}` 或 `JWT_SECRET: file://<路径>`，不要明文写进 `config/`。换密钥会让所有已签出的令牌在 verify 时变成 401，相当于让所有人重新登录。
- `TOKEN_TTL_SECONDS`：令牌签出后改不了也撤不回；verify 只验签名与有效期，不回查 `people/basic`。所以一个人被删除或调部门后，他已有的令牌在过期前仍然有效，里面的 `departmentId` 仍是登录时的值。调长省了重新登录，代价是这个窗口变长；必须是正整数。
- `DATABASE_HOST`：容器网络里 PostgreSQL 的服务名，不是宿主机上的 `localhost`。`DATABASE_NAME` 是「部署前准备」里建好的库。`DATABASE_USER` / `DATABASE_PASSWORD` 按库的账号填，口令写成 `${VAR}` 或 `file://` 引用；口令里的 `@ : /` 没有问题。

`config/auth-password-login.yaml` 填好后大致是：

```yaml
JWT_SECRET: ${JWT_SECRET}
DATABASE_HOST: $var:PG_HOST
DATABASE_NAME: brickkit_auth
DATABASE_USER: $var:PG_USER
DATABASE_PASSWORD: $var:PG_PASSWORD
```

`LOG_LEVEL` 只影响日志详细程度；口令、令牌、密钥、哈希一类字段无论哪一级都会打码。

## 契约索引

- `openapi.json`：HTTP 接口（本组件没有 gRPC）。调用方拿到的地址是 `AUTH_PASSWORD_LOGIN_ENDPOINT`。
  - `POST /api/v1/login`：`{username, password}` → `{token, expiresAt, personId, username}`
  - `POST /api/v1/verify`：`{token}` → `{personId, username, departmentId, expiresAt}`；令牌无效或过期一律 401，不说明是哪一种
  - `GET /healthz`：只检查本进程存活，不查库、不调 `people/basic`
  - 运行中的组件也在 `GET /openapi.json` 发出同一份文档

状态码的语义：

| 码 | 含义 | 什么时候 |
| --- | --- | --- |
| 400 | 你没说清楚要什么 | 缺字段、不是 JSON |
| 401 | 你是谁我不认 | 口令错、用户不存在、人已不在 `people/basic`、令牌无效或过期 |
| 503 | 我这边有问题 | 数据库连不上、`people/basic` 不可用 |

把 503 报成 401，使用者会在自己的密码上白折腾半天；反之又会让人去查根本没坏的依赖。用户不存在与口令错误返回完全相同的响应（状态码、文案、字段），耗时也接近：任何差别都能被拿来筛出"哪些账号真实存在"。

令牌：HS256，载荷含 `sub`（personId）、`iat`、`exp`、`nbf`、`username`、`departmentId`。载荷只是 base64、**不是加密**，拿到令牌的人都能读，所以里面不放任何秘密。下游不要自己验签（拿不到密钥），调 verify。

```bash
curl -s -X POST "$AUTH_PASSWORD_LOGIN_ENDPOINT/api/v1/login" \
  -H 'Content-Type: application/json' \
  -d '{"username":"zhangsan","password":"demo-password"}'

curl -s -X POST "$AUTH_PASSWORD_LOGIN_ENDPOINT/api/v1/verify" \
  -H 'Content-Type: application/json' \
  -d '{"token":"<上一步返回的 token>"}'
```

不发布事件，也不消费事件。

## 外壳声明

不是外壳。
