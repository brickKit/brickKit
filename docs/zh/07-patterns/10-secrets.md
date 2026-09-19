# 密钥：住在哪、最后落在哪、怎么接密钥管理器

## 密钥是什么，为什么要单独对待

**密钥**是这样一种值：谁拿到它，就能冒充你——数据库密码、API Key、登录令牌都是。它相当于一把钥匙：复制一份出去，那份复制品照样能开同一把锁，没人会再多问一句，直到有人把锁换掉。**地址**——组件的 `PEOPLE_BASIC_ENDPOINT`、数据库的 `host`、一个公开 API 的 URL——相当于门牌号：刷在墙上、印在名片上，被陌生人看到也没关系。知道地址只是让人找到了门，不代表能进去。

正是这个区别，让 BrickKit 对这两类值的态度完全不同。`brickkit.yaml` 是平台明确希望提交进 Git、像代码一样被 review 的文件——地址、组件 ID、资源的 `host`/`port` 都堂堂正正地写在里面，这没问题，因为它们都不是钥匙。真正的密钥值一旦躺进同一份文件，就是另一种性质的错误，而且撤销的代价要大得多：下一次提交里删掉那一行，并不会删掉这个密钥。曾经拉取过更早那次提交的每一个克隆，`.git` 历史里都还留着它，每一个 fork、每一个 CI 缓存、每一份备份也都是——直到有人发现、去源头把这个凭据吊销重发，然后（这是另一件更痛苦的事）把历史在所有可能扩散到的地方都重写一遍。门牌号泄露了不用"修"，因为它本来就不是秘密。钥匙泄露了，得换锁。

**单独对待**是 BrickKit 对这种不对等给出的答案：密钥的真值只允许同时存在于一个地方——真正运行它的那个进程的环境里——而项目通常会写值的其它每一处（要提交的 `brickkit.yaml`、CLI 生成的部署文件、警告打印到终端的那一行）都只放一个**指向真值所在处**的引用，从不放真值本身。

## 密钥在 BrickKit 里住在哪

| 声明在哪 | 一定是密钥？ | 会不会进生成的部署文件？ |
| --- | --- | --- |
| `resources[].password` | 是，无条件——资源密码不管 `kind` 是什么，一律当密钥处理 | 会（见下文） |
| `configSchema` 里声明了 `secret: true` 的属性 | 只有组件作者声明了才算——平台从不按键名去猜（一个只叫 `apiKey`、没写 `secret: true` 的配置项就是一个普通值） | 会（见下文） |
| `sources[].authToken` | 是——它是 CLI 自己登录市场或克隆私有 Git 源时用的凭据 | 不会。它只被 CLI 进程自己读来给 `brickkit add`/`login`/`publish` 认证，从不会变成任何组件容器里的一条环境变量 |

前两行，`brickkit.yaml` 里都从来不放真值——只放一个 `${VAR}` 引用（K8s 下也可以改放 `existingSecret` 引用，见下文"`existingSecret`"一节）。真值住在 CLI 会去查的两个地方之一，顺序固定：先看运行 `brickkit up`（或 `add`、`login`）那个进程自己的环境；查不到再看项目根目录的 `.env` 文件。`.env` 从 `brickkit init` 建出项目的第一刻起就已经在 `.gitignore` 里——一个刚初始化的项目里，真实、未经编辑的内容：

```
# 环境变量文件（包含密码）
.env
```

（这一份 `.env` 同时兜着上表前两行，也兜着 `sources[].authToken`。）

## 最后落在哪

| 部署目标 | 真值落在哪 | 谁来求值、什么时候求值 |
| --- | --- | --- |
| Docker（`deploy.target: docker`） | 从来不在 CLI 写的任何文件里——`docker-compose.yaml` 原样保留 `${VAR}` 占位符 | `docker compose` 自己，在容器真正启动的那一刻（先看进程环境，再看 `.env`） |
| K8s（`deploy.target: k8s`） | 一份生成的 `Secret`，落在 `.brickkit/generated/k8s/secrets/` 下（文件权限 `0600`）——Deployment 的 `env` 项里只有一个指向它的 `secretKeyRef` | CLI 自己，在生成阶段——顺序同样是先进程环境、再 `.env` |
| `local: true`（仅 Docker） | `local-debug.<版本化服务名>.env`（同样 `0600`，同样在 `.brickkit/generated/` 下）——刻意是明文，因为这正是 IDE 要拿去真正跑起这个进程用的东西 | CLI 自己，在生成阶段 |

上面三个 `.brickkit/generated/` 路径，`brickkit init` 默认写的 `.gitignore` 早就盖住了——不需要项目再多记一条规则。

这一节剩下的部分是真实、未经编辑的输出——不是"应该发生什么"的描述——来自一个很小的演示组件 `acme/hello`，它绑定了一个 `database` 资源，并声明了一个 `secret: true` 的配置项 `apiKey`：

```yaml
# component.yaml（节选）
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

```yaml
# brickkit.yaml（节选）
components:
  - id: acme/hello
    version: 0.1.0
    config:
      apiKey: ${THIRD_PARTY_KEY}
resources:
  - kind: database
    engine: postgresql
    id: main-db
    password: ${PG_PASSWORD}
    bindings:
      - componentId: acme/hello
        database: hello
```

**Docker，真值放在进程环境里：**

```
$ PG_PASSWORD=pw-from-env THIRD_PARTY_KEY=key-from-env brickkit up --dry-run
...
📄 已生成：.brickkit/generated/docker-compose.yaml

$ grep -n "PASSWORD\|API_KEY" .brickkit/generated/docker-compose.yaml
22:      - API_KEY=${THIRD_PARTY_KEY}
27:      - DATABASE_PASSWORD=${PG_PASSWORD}
```

占位符原样留在那里——尽管 `up` 运行时的环境里确确实实设了 `key-from-env`、`pw-from-env`，生成的文件里却哪儿都找不到这两个字符串。这正是 AGENTS.zh.md §5.2 说的那种行为：CLI 自己解析这一步，对 `config` 和 `resources[].password` 里的 `${VAR}` 刻意从不求值（内部实现上，`internal/config` 的 `deferredRefs` 正是标记这两处字段的地方），这样它写出来的文件才始终能安全地打开看、进 git diff。

**K8s，同一个组件，真值只能从 `.env` 里找到（进程环境里没有）：**

```
$ env -u PG_PASSWORD -u THIRD_PARTY_KEY brickkit up --dry-run
...
📄 已生成 5 份清单：.brickkit/generated/k8s/

$ ls -l .brickkit/generated/k8s/secrets
-rw------- 1 you you 548 ... config-secrets.yaml
-rw------- 1 you you 534 ... resource-secrets.yaml

$ cat .brickkit/generated/k8s/secrets/resource-secrets.yaml
apiVersion: v1
kind: Secret
metadata:
  name: main-db-secret
  namespace: brickkit-demo
stringData:
  password: pw-from-dotenv
type: Opaque

$ cat .brickkit/generated/k8s/secrets/config-secrets.yaml
apiVersion: v1
kind: Secret
metadata:
  name: acme-hello-0-1-0-config-secret
  namespace: brickkit-demo
stringData:
  API_KEY: key-from-dotenv
type: Opaque

$ grep -n -B1 -A4 "API_KEY" .brickkit/generated/k8s/deployments/acme-hello-0-1-0.yaml
            - name: API_KEY
              valueFrom:
                secretKeyRef:
                  key: API_KEY
                  name: acme-hello-0-1-0-config-secret
```

有两点值得注意：资源密码和 `secret: true` 的配置值各自生成**自己的** Secret 文件（`resource-secrets.yaml` 和 `config-secrets.yaml`），从不混在一起——所以一个只有资源密码、没有密钥配置项的项目，磁盘上根本看不到 `config-secrets.yaml`；而且进程环境被清空之后，真值确确实实来自 `.env`（`pw-from-dotenv`、`key-from-dotenv`），证明这个回退顺序是真实生效的，不只是文档里这么写而已。

**一个值得先知道、免得日后排障吃亏的地方：** `docker compose config`——排查生成的 compose 文件时最标准的做法——**确实**会按同样"先进程环境、再 `.env`"的顺序展开 `${VAR}` 占位符，并把真值打印出来：

```
$ docker compose -f .brickkit/generated/docker-compose.yaml --project-directory . config | grep -n "API_KEY\|PASSWORD"
10:      API_KEY: key-from-dotenv
15:      DATABASE_PASSWORD: pw-from-dotenv
```

CLI 写出来的那份文件里从来没有真值。对着它跑一遍 `docker compose config` 就有了。别把这条命令的输出贴进工单、聊天记录或者 CI 日志——那样就把"占位符留在文件里"这件事的意义整个抹掉了。

## 两种接密钥管理器的方式

### 把值放进进程环境，再 `brickkit up`

机制只有一句话：**把值放进进程环境，再跑 `brickkit up`。** BrickKit 不关心这个值是怎么到那儿的：

```
PG_PASSWORD="$(cat /run/secrets/pg-password)" brickkit up
```

这一行从某个挂载点读出一个值（可能是 Docker/Swarm 的 secret 文件、K8s 挂载出来的 Secret 卷、CI 系统导出的密钥文件、systemd 的 credential——任何最终会在跑 CLI 的这台机器上变成一个文件或一条环境变量的东西），再把它当成一条普通的、加了 shell 前缀的环境变量交给 `brickkit up`。CLI 自己的查找顺序（先进程环境、再 `.env`——上面已经验证过）会照单全收，跟你自己手打一个值没有任何区别。任何能把一个值放进进程环境的工具都能这么用；BrickKit 不需要认识、也确实不认识你具体用的是哪一个。

### `existingSecret`：引用别的东西已经建好的 Secret

第一种机制仍然要求**你**每一次都把值亲手交给 `brickkit up`。有时候这不是你想要的——值可能已经躺在一个由别的系统在集群里持续同步的 K8s `Secret` 里（一个 Vault Agent Injector 侧车、External Secrets Operator、Sealed Secrets，或者只是运维手工跑过一次 `kubectl create secret`），你更想直接指向它，完全不想再经手一遍 CLI。

这就是 `existingSecret`：一个资源可以写 `existingSecret: <名字>` 代替 `password: ${VAR}`；一个 `secret: true` 的配置值可以写成 `{ existingSecret: <名字>, key: <Secret 里的 key> }` 代替一个标量。两处都仅限 K8s——Docker 根本没有"一个已经存在的 Secret 对象"这种概念可以引用。同一个演示组件，两处都改写成这个样子后，真实、未经编辑的输出：

```yaml
# brickkit.yaml（节选）
components:
  - id: acme/hello
    version: 0.1.0
    config:
      apiKey: { existingSecret: acme-thirdparty-vault-synced, key: api-key }
resources:
  - kind: database
    engine: postgresql
    id: main-db
    existingSecret: acme-db-vault-synced
    bindings:
      - componentId: acme/hello
        database: hello
```

```
$ brickkit up --dry-run
...
📄 已生成 3 份清单：.brickkit/generated/k8s/

$ find .brickkit/generated/k8s -type f
.brickkit/generated/k8s/namespace.yaml
.brickkit/generated/k8s/services/acme-hello-0-1-0.yaml
.brickkit/generated/k8s/deployments/acme-hello-0-1-0.yaml

$ grep -rn "vault-synced\|api-key" .brickkit/generated/k8s/secrets/
（连这个目录都不存在——两份生成物都没有）

$ grep -n -B1 -A4 "API_KEY\|DATABASE_PASSWORD" .brickkit/generated/k8s/deployments/acme-hello-0-1-0.yaml
            - name: API_KEY
              valueFrom:
                secretKeyRef:
                  key: api-key
                  name: acme-thirdparty-vault-synced
...
            - name: DATABASE_PASSWORD
              valueFrom:
                secretKeyRef:
                  key: password
                  name: acme-db-vault-synced
```

对比上一节的文件数：那里是五份清单（因为存在两份 Secret 文件），这里是三份——`secrets/` 目录根本没被建出来，因为平台已经没有什么需要生成的了。Deployment 里 `secretKeyRef.name` 直接指向你写的那个名字，不是 CLI 算出来的任何东西。

两种机制的区别正好在这里：第一种是每次运行都把**值**交给 `brickkit up`；第二种是把**整个 Secret 对象**交给 Kubernetes，CLI 从头到尾都不碰这个值——生成阶段不碰，任何时候都不碰。两种写法都不需要 BrickKit 知道 Vault、AWS Secrets Manager 或者任何别的产品到底是什么。

这是故意的，也正是 BrickKit 不会替你直接去调一个密钥管理器 SDK 的原因——它跟[配置中心](../06-architecture/00-overview.md#6-配置中心与动态热更新)一样，都在同一份[拒绝清单](../06-architecture/00-overview.md#平台刻意不做的事以及为什么)上（AGENTS.zh.md §4.1），理由也几乎一样。真要接一个密钥管理器 SDK，意味着一种存储要配一个客户端库——Vault、AWS、GCP、Azure 的 API 各不相同，而一个项目永远只会用到其中一个，剩下三个就成了 CLI 也要维护下去的死重。也意味着 `brickkit up --dry-run`——这条命令存在的全部意义就是"只生成文件，不碰任何真东西"——突然需要联网、需要一份真凭据，才能打印出一份根本不会被用到的文件。还意味着平台自己的内存里，会在一次运行的整个过程中攥着一个解密后的密钥值，这恰恰是安全审查"这个值被谁碰过"清单上最扎眼的那一种事。而交给 CLI 一个能直接写进生成文件的值，或者一个能直接写进 `secretKeyRef` 的名字，都不需要这些。

## 如实交代的边界

- **CLI 生成的 K8s Secret 文件在磁盘上是明文。** 权限 `0600`，默认在 `.gitignore` 里，没错，但终究是明文，而且每次 `up` 都会重新生成。别把 `.brickkit/generated/` 当 CI 产物上传，也别以为文件权限本身能替代真正的密钥库才有的访问控制。
- **`existingSecret` 不会校验它指向的 Secret 是不是真的在集群里、是不是真的有那个 key。** 这确确实实是 Kubernetes 自己的事，不是 CLI 的事——那要等到 Pod 真正尝试启动的那一刻才会暴露出来，表现成一个卡住起不来的 Pod，而不是 `brickkit up` 在生成阶段就能抓到的任何东西（生成阶段 CLI 根本不会去联真实集群）。
- **写字面值会被警告，`secret: true` 的项这条是无条件的。** 一个 `secret: true` 的属性本该写引用，你却写了一个普通字符串，`brickkit up` 一定会警告，不管这个键叫什么名字——那条按名字猜的启发式（`password`、`token`、`apiKey`……）只对**没声明**的、长得像密钥的键生效；声明过的这一条，任何时候都会被检查。
- **Docker 下，`secret: true` 和 `existingSecret` 都不改变任何真实行为。** `secret: true` 不改变行为，是因为 `${VAR}` 占位符本来就从不会被写进 compose 文件——压根没有另一条"Docker 密钥投递"可以切换。`existingSecret` 在 Docker 下会收到一条警告，并被当成完全没配置——Docker 就是没有对应的概念可以把它路由过去。
- **没有"整块 `config` 从一个 Secret 灌进来"的捷径**（Kubernetes 自己支持的那种 `envFrom` 式批量导入）。每一条引用都是逐项、一个一个写的，这是故意的：跟 AGENTS.zh.md §9.23 对依赖命名做的选择是同一个道理——一个由什么东西（这里是配置键）算出来的变量名，就该能顺着追回那个东西；批量导入省下几行打字，换来的是丢掉这条可追溯性。
