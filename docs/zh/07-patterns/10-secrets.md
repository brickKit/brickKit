# 密钥：住在哪、最后落在哪、怎么接密钥管理器

先看一个很常见的翻车过程。有人图省事，把数据库密码直接写进了 `brickkit.yaml` 并提交；两天后觉得不妥，在新的提交里删掉了那一行，以为处理完了。可那个密码其实还好好地躺在上一次提交里——每个拉过代码的人的 `.git` 目录、每一个 fork、每一份 CI 缓存和备份里，都有一份。要真正收拾干净，得先去源头把密码作废重发，再把所有副本里的历史重写一遍。

这篇文章讲两件事：BrickKit 怎么让这种事不容易发生；以及一个真实的密钥，在 BrickKit 里从头到尾会经过哪些地方、最后落在哪。

## 密钥是什么，为什么要单独对待

**密钥**指的是：谁拿到它，谁就能以你的身份做事的值——数据库密码、第三方 API Key、访问令牌、私钥都是。要判断一个值算不算密钥，可以想想银行卡：卡号可以告诉别人（对方要给你转账，就得知道卡号），取款密码不行。项目配置里的值也分这两类：

| 像卡号：可以写进 `brickkit.yaml` | 像取款密码：绝不能写进去 |
| --- | --- |
| 组件 ID、版本号、资源的 `host`/`port`、`*_ENDPOINT` 这类地址 | 数据库密码、第三方 API Key、令牌、私钥 |

前一类写在那个要提交、要 review 的文件里没有问题；后一类一旦进了 Git 历史就收不回来，开头那个过程就是这么来的。

BrickKit 把"单独对待"落成三条约定：

1. `brickkit.yaml` 里只放**引用**——`${VAR}`，K8s 下还可以是 `existingSecret`——从不放值本身；
2. 真值只出现在真正需要它的那一处：运行中进程的环境，或者集群里的 Secret；
3. CLI 打印警告时只列出配置项的名字，从不把值打出来。

## 密钥在 BrickKit 里住在哪

CLI 要找一个 `${VAR}` 的真值时，只看两个地方，顺序固定：先看运行 CLI 的那个进程自己的环境变量，没有再看项目根目录的 `.env` 文件。`brickkit init` 建项目时就把 `.env` 和 `.brickkit/generated/`（CLI 生成的所有部署文件都在这里）写进了 `.gitignore`，下面是那份文件里相关的几行，原样摘出（注释是中文的）：

```
$ grep -B1 -E '^(\.brickkit/generated/|\.env)$' .gitignore
# BrickKit CLI 生成的文件（不提交到 Git）
.brickkit/generated/
--
# 环境变量文件（包含密码）
.env
```

三类会碰到密钥的位置，按"离部署文件有多远"从远到近排：

| 位置 | 怎么算密钥 | 会不会进部署文件 |
| --- | --- | --- |
| `sources[].authToken` | CLI 自己登录市场、拉私有 Git 源用的凭据，一律算 | 不会。只有 CLI 进程自己用，不会变成任何组件容器里的环境变量 |
| `configSchema` 里写了 `secret: true` 的配置项 | 组件作者声明了才算；平台不按键名去猜（一个只是叫 `apiKey`、没声明的配置项，按普通值投递） | 会 |
| `resources[].password` | 一律算，不管资源是哪种 `kind` | 会 |

后两行在 `brickkit.yaml` 里都只写引用。

## 最后落在哪

同一份 `brickkit.yaml`，`deploy.target` 不同，真值的去向完全不同。下面用一个很小的演示组件 `acme/hello` 把三种情况各跑一遍。这一节里的每条命令都是真跑的，输出块就是命令原样打印的内容（`2>/dev/null` 是为了藏掉 CLI 写在 stderr 上的 JSON 日志行，`grep '已生成'` 只留下"生成了什么"那一行）。

组件声明了一个 `database` 资源，和一个 `secret: true` 的配置项 `apiKey`：

```yaml
dependencies:
  resources:
    - kind: database
      engine: postgresql

configSchema:
  type: object
  properties:
    apiKey:
      type: string
      secret: true
```

项目的 `brickkit.yaml`（除了 `deploy.target`——每次运行按情况写 `docker` 或 `k8s`——其余如下）把数据库和配置值都写成引用；项目的 `.env` 文件里是 `PG_PASSWORD=pw-from-dotenv` 和 `THIRD_PARTY_KEY=key-from-dotenv`：

```yaml
components:
  - id: acme/hello
    version: 0.1.0
    config:
      apiKey: ${THIRD_PARTY_KEY}

resources:
  - kind: database
    engine: postgresql
    id: main-db
    host: postgres.internal
    port: 5432
    username: app
    password: ${PG_PASSWORD}
    bindings:
      - componentId: acme/hello
        database: hello
```

### K8s：CLI 自己求值，写成 Secret

先看 K8s，它最典型：CLI 自己求出值，再写进一份专门的 Secret 文件。这次运行时进程环境里什么都没有，所以值是从 `.env` 里找到的：

```
$ env -u PG_PASSWORD -u THIRD_PARTY_KEY brickkit up --dry-run 2>/dev/null | grep '已生成'
📄 已生成 5 份清单：.brickkit/generated/k8s/

$ ls .brickkit/generated/k8s
deployments
namespace.yaml
secrets
services

$ stat -c '%A %s %n' .brickkit/generated/k8s/secrets/*
-rw------- 548 .brickkit/generated/k8s/secrets/config-secrets.yaml
-rw------- 534 .brickkit/generated/k8s/secrets/resource-secrets.yaml
```

两份 Secret 文件的权限都是 `-rw-------`（即 `0600`）。资源密码和 `secret: true` 的配置值各生成各的文件，从不混在一起：

```
$ sed -n '/^apiVersion/,$p' .brickkit/generated/k8s/secrets/resource-secrets.yaml
apiVersion: v1
kind: Secret
metadata:
  labels:
    brickkit.io/project: demo
  name: main-db-secret
  namespace: brickkit-demo
stringData:
  password: pw-from-dotenv
type: Opaque
```

```
$ sed -n '/^apiVersion/,$p' .brickkit/generated/k8s/secrets/config-secrets.yaml
apiVersion: v1
kind: Secret
metadata:
  labels:
    brickkit.io/project: demo
  name: acme-hello-0-1-0-config-secret
  namespace: brickkit-demo
stringData:
  API_KEY: key-from-dotenv
type: Opaque
```

Deployment 里则完全没有值，只有指向这两份 Secret 的 `secretKeyRef`：

```
$ grep -A4 "name: API_KEY" .brickkit/generated/k8s/deployments/acme-hello-0-1-0.yaml
            - name: API_KEY
              valueFrom:
                secretKeyRef:
                  key: API_KEY
                  name: acme-hello-0-1-0-config-secret

$ grep -A4 "name: DATABASE_PASSWORD" .brickkit/generated/k8s/deployments/acme-hello-0-1-0.yaml
            - name: DATABASE_PASSWORD
              valueFrom:
                secretKeyRef:
                  key: password
                  name: main-db-secret
```

另外，一份文件只在真有东西要放的时候才会生成——一个只有资源密码、没有密钥配置项的项目，磁盘上根本不会出现 `config-secrets.yaml`。

顺序上，进程环境比 `.env` 优先。这次进程环境里也有值了，`.env` 还在原地，输给了环境：

```
$ PG_PASSWORD=pw-from-env THIRD_PARTY_KEY=key-from-env brickkit up --dry-run 2>/dev/null | grep '已生成'
📄 已生成 5 份清单：.brickkit/generated/k8s/

$ grep -h '^  password:\|^  API_KEY:' .brickkit/generated/k8s/secrets/*.yaml
  API_KEY: key-from-env
  password: pw-from-env
```

### Docker：CLI 从不求值

Docker 下走的是另一条路：CLI 根本不去求 `${VAR}` 的值，原样写进 `docker-compose.yaml`，等 `docker compose` 启动容器的那一刻再由它去求。这次值放在进程环境里：

```
$ PG_PASSWORD=pw-from-env THIRD_PARTY_KEY=key-from-env brickkit up --dry-run 2>/dev/null | grep '已生成'
📄 已生成：.brickkit/generated/docker-compose.yaml

$ grep -n "PASSWORD\|API_KEY" .brickkit/generated/docker-compose.yaml
22:      - API_KEY=${THIRD_PARTY_KEY}
27:      - DATABASE_PASSWORD=${PG_PASSWORD}
```

带着这两个变量的两行，写的就是 `brickkit.yaml` 里的原文。`pw-from-env`（运行 `up` 时确实在环境里）和 `pw-from-dotenv` 都没有出现在文件里的任何地方。这正是 AGENTS.zh.md §5.2 说的那种行为：CLI 自己解析这一步，对 `config` 和 `resources[].password` 里的 `${VAR}` 刻意从不求值（内部实现上，`internal/config` 的 `deferredRefs` 正是标记这两处字段的地方），这样它写出来的文件才始终能安全地打开看、进 git diff。

**排障时有个坑，先知道比日后踩到强。** 检查生成的 compose 文件，最顺手的命令是 `docker compose config`。可它**会**把 `${VAR}` 展开成真值再打印出来，顺序同样是进程环境优先、`.env` 其次。对着上面那份 Docker 目标的文件，先只靠 `.env`，再连进程环境也设上：

```
$ docker compose -f .brickkit/generated/docker-compose.yaml --project-directory . config | grep -n "API_KEY\|PASSWORD"
10:      API_KEY: key-from-dotenv
15:      DATABASE_PASSWORD: pw-from-dotenv
```

```
$ PG_PASSWORD=pw-from-real-env THIRD_PARTY_KEY=key-from-real-env docker compose -f .brickkit/generated/docker-compose.yaml --project-directory . config | grep -n "API_KEY\|PASSWORD"
10:      API_KEY: key-from-real-env
15:      DATABASE_PASSWORD: pw-from-real-env
```

CLI 写出来的文件里从来没有真值，对着它跑一遍 `docker compose config` 就有了。别把这条命令的输出贴进工单、聊天记录或 CI 日志——那等于把"占位符留在文件里"的意义整个抹掉。

### `local: true`：明文，给 IDE 用

`local: true` 只存在于 Docker 目标下。这样的组件没有容器，CLI 会为它写一份 `local-debug.<版本化服务名>.env`，由 IDE 加载后把进程跑起来。这份文件里的值是 CLI 求好的**明文**，这是刻意的——IDE 不做变量替换，拿到什么就用什么。它和其它生成物一样放在 `.brickkit/generated/` 下（已被 `.gitignore` 忽略），文件权限 `0600`。

### 小结

| 部署目标 | 真值落在哪 | 谁来求值 |
| --- | --- | --- |
| K8s | `.brickkit/generated/k8s/secrets/` 下的 Secret 文件（`0600`）；Deployment 里只有 `secretKeyRef` | CLI，生成时 |
| Docker | 不落在 CLI 写的任何文件里；compose 文件保留 `${VAR}` | `docker compose`，容器启动时 |
| `local: true` | `local-debug.<版本化服务名>.env`（`0600`），明文 | CLI，生成时 |

## 两种接密钥管理器的方式

先把两种方式放在一起比一比：

| | ① 值放进进程环境 | ② `existingSecret` |
| --- | --- | --- |
| 你把什么交给 BrickKit | **值**本身 | 一个 Secret 的**名字**（集群里已经有这个 Secret） |
| 值会不会经过 CLI | 会：K8s 下由 CLI 读到，Docker 下由 `docker compose` 读到 | 不会：CLI 从头到尾没见过值 |
| 适用的部署目标 | Docker、K8s 都行 | 仅 K8s |
| 每次 `up` 要不要你重新提供值 | 要 | 不要 |

### ① 值放进进程环境

机制只有一句话：**把值放进进程环境，再跑 `brickkit up`**。值是怎么到那儿的，BrickKit 不关心：

```
PG_PASSWORD="$(cat /run/secrets/pg-password)" brickkit up
```

这行命令从某个挂载点读出一个值——Docker/Swarm 的 secret 文件、K8s 挂载的 Secret 卷、CI 导出的密钥文件、systemd 的 credential，凡是最终在跑 CLI 的这台机器上成为一个文件或环境变量的东西都行——再当作一条带 shell 前缀的普通环境变量交给 `brickkit up`。前面演示过的查找顺序（进程环境优先、`.env` 其次）会照单全收，和你手打一个值没有区别。能把值放进环境变量的工具都能这么用；BrickKit 不认识其中任何一个，也不需要认识。

### ② `existingSecret`：引用别的东西已经建好的 Secret

第一种方式的前提是：每次运行，都由你把值交给 `brickkit up`。有些场景你不想这样——值已经在集群里的一个 K8s Secret 里了，由别的东西建好、持续更新，你只想指向它。最简单的情形，是运维手工跑过一次 `kubectl create secret`。另外几种是专门做这件事的工具，名字常被一口气列出来，但它们的产出并不一样：

| 工具 | 大致是什么 | 最后会不会有一个 K8s Secret | `existingSecret` 能不能指向它 |
| --- | --- | --- | --- |
| External Secrets Operator | 跑在集群里的程序，盯着集群外的密钥库（HashiCorp Vault、AWS Secrets Manager 之类），把里面的值同步成普通的 K8s Secret | 会 | 能 |
| Vault Secrets Operator | HashiCorp 官方专门为 Vault 做的同类程序：跑在集群里，把 Vault 里的值同步成普通的 K8s Secret | 会 | 能 |
| Sealed Secrets | 让你把**加密后**的 Secret 提交进 Git，只有集群里的控制器持有解密的钥匙，在集群内把它还原成真正的 Secret | 会 | 能 |
| Vault Agent Injector | 给你的 Pod 加一个辅助容器，登录 Vault 取值，写成 Pod 内部的文件 | 不会——值只以文件的形式给到 Pod | 不能。存储是 Vault 的话，中间得放一个会写出真 Secret 的工具：Vault Secrets Operator 或 External Secrets Operator，选哪个都行 |

你不需要知道怎么运行其中任何一个。`existingSecret` 只要求：Pod 启动的时候，命名空间里有一个你写的那个名字的普通 Secret。

用法是：资源写 `existingSecret: <名字>` 代替 `password: ${VAR}`；`secret: true` 的配置值写成 `{ existingSecret: <名字>, key: <Secret 里的 key> }` 代替标量。两处都仅限 K8s，因为 Docker 里根本没有"引用一个已经存在的 Secret 对象"这种概念。同一个演示组件，两处都这样改写（只列出 `components` 和 `resources`，`deploy.target` 是 `k8s`）：

```yaml
components:
  - id: acme/hello
    version: 0.1.0
    config:
      apiKey: { existingSecret: acme-thirdparty-vault-synced, key: api-key }

resources:
  - kind: database
    engine: postgresql
    id: main-db
    host: postgres.internal
    port: 5432
    username: app
    existingSecret: acme-db-vault-synced
    bindings:
      - componentId: acme/hello
        database: hello
```

```
$ brickkit up --dry-run 2>/dev/null | grep '已生成'
📄 已生成 3 份清单：.brickkit/generated/k8s/

$ ls .brickkit/generated/k8s
deployments
namespace.yaml
services
```

```
$ grep -A4 "name: API_KEY" .brickkit/generated/k8s/deployments/acme-hello-0-1-0.yaml
            - name: API_KEY
              valueFrom:
                secretKeyRef:
                  key: api-key
                  name: acme-thirdparty-vault-synced

$ grep -A4 "name: DATABASE_PASSWORD" .brickkit/generated/k8s/deployments/acme-hello-0-1-0.yaml
            - name: DATABASE_PASSWORD
              valueFrom:
                secretKeyRef:
                  key: password
                  name: acme-db-vault-synced
```

和前面 K8s 那次比：那次五份清单，因为有两份 Secret 文件；这次三份，`secrets/` 目录根本没有建出来——平台已经没有什么要生成的了。Deployment 里的 `secretKeyRef.name`，就是你写的那个名字，不是 CLI 算出来的任何东西。

### 为什么 BrickKit 不自己去调密钥管理器的 SDK

一种很自然的想法是：既然值在 Vault 或 AWS Secrets Manager 里，就让 BrickKit 内置一个"去那里取值"的功能。它属于同一份[拒绝清单](../06-architecture/00-overview.md#平台刻意不做的事以及为什么)（AGENTS.zh.md §4.1），和被拒绝的[配置中心](../06-architecture/00-overview.md#6-配置中心与动态热更新)是近邻，理由也几乎一样。真做了会碰上三件事，按严重程度排：

- **CLI 得拿着存储本身的凭据。** 要去 Vault 或 AWS 取值，CLI——以及每一个跑它的 CI 任务——就得带上一份能登录那个存储的凭据，而这类凭据能读的，通常远不止一个项目要用的那几个值。今天的两种方式里，CLI 经手的只有项目自己放进环境的那几个值。
- **`--dry-run` 不再是"不碰真东西"。** 这条命令存在的全部意义，就是只生成文件、不动任何真实系统。一旦要取值，它就得联网、登录存储，只为了打印出一份根本不会被用到的文件。
- **一种存储一个 SDK。** Vault、AWS、GCP、Azure 的 API 各不相同，一个项目只会用到其中一个，剩下几个就成了 CLI 也得维护下去的死重。

而交给 CLI 一个能直接写进生成文件的值，或者一个能直接写进 `secretKeyRef` 的名字，这三件事都不会发生。

## 如实交代的边界

### CLI 自己产出的东西

- **生成的 K8s Secret 文件在磁盘上是明文。** 权限是 `0600`、默认被 `.gitignore` 忽略，但终究是明文，而且每次 `up` 都会重新生成。别把 `.brickkit/generated/` 当 CI 产物上传，也别以为文件权限能替代真正的密钥库才有的访问控制。
- **没有"整块 `config` 从一个 Secret 灌进来"的捷径。** Kubernetes 自己支持这种批量导入（`envFrom`），BrickKit 不做：每一项引用都得逐个写。这是有意保留的——AGENTS.zh.md §9.23 对依赖命名做的是同一个选择：一个由某样东西（这里是配置键）算出来的变量名，就该能顺着追回那样东西；批量导入省下的几行打字，是拿这条可追溯性换的。

### CLI 管不到的东西

- **`existingSecret` 不校验它指向的 Secret 是否存在、是否真有那个 key。** 生成阶段 CLI 根本不会去联真实集群，所以这件事只能等 Pod 真正启动的那一刻由 Kubernetes 自己发现，表现为一个起不来的 Pod，而不是 `brickkit up` 能提前抓到的东西。

### 警告会说什么，Docker 下哪些不起作用

- **写了字面值会被警告。** 一个 `secret: true` 的配置项该写引用（`${VAR}` 或 `existingSecret`），你却写了个普通字符串，`brickkit up` 一定会警告，不管这个键叫什么。没声明过 `secret: true` 的键，只有名字长得像密钥（`password`、`token`、`apiKey`……）才会被警告——这一条只看名字。警告里只列键名，不打印值。
- **Docker 下，`secret: true` 不改变任何行为。** 生成的 compose 文件里本来就只有 `${VAR}` 占位符，不会有被求值成明文的内容；写不写 `secret: true`，生成的文件完全一样。`secret: true` 决定的是"求好的值往哪放"，而 Docker 下 CLI 从不求值，没有什么可决定的。
- **Docker 下，`existingSecret` 会被忽略。** Docker 没有对应的概念，`brickkit up` 会警告，那个变量也不会被注入，就像这项设置没写过一样。
