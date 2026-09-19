# 第四份改进提案的评判，以及"密钥流向"的设计

> 来源：仓库根目录未跟踪的 `改进计划.md`（"BrickKit 新特性与优化提案"，5 条）。那份文件是一次性的、
> 会被删除；本文档自足，删掉它不损失任何依据。
>
> 方法：每条提案里的**事实断言**都对着仓库核实（读源码 + 真跑命令），**取舍**类的判断单列并给出推荐。
> 没有凭"哲学契合度"表态——那一栏在提案里五条全写"完美契合"，不构成证据。

## 1. 结论一览

> **2026-09-19 第二次评判：** 用户指出"允许重构，只要新功能有价值、能帮到用户"，要求重新过一遍五条结论。
> 重新核实后，**只有第 1 条的结论变了**：机制层面仍然不做"CLI 调 Vault/AWS SDK"，但提案里"数据库密码、第三方
> API 密钥"这两个真实例子，用平台已有的"引用、不生成"模式（`serviceAccountName` 那一类）能完整解决——
> 于是新增 `resources[].existingSecret` 与 config 密钥的 `existingSecret` 引用写法，见 §3.2。其余 4 条结论不变，
> 且不是因为怕重构：3 是测量出来的事实（2ms），4 否决的是"谁担保 `Down` 语义"这条信任边界，5 否决的是"平台不解析
> 契约"这条组件自治边界，2 是价值判断（现有配方已经够用）——这三类否决都不会因为"愿意花更多工时"而改变。

| # | 提案 | 结论 | 一句话 |
| --- | --- | --- | --- |
| 1 | 部署时外部 Secret 集成（CLI 调 Vault / AWS SDK 取值） | **代取值的机制不做；但新增 `existingSecret` 引用，完整覆盖两个真实例子** | 进程环境优先的查找顺序已让任何密钥工具零代码接入值本身；`existingSecret` 再补上"引用一整个已建好的 Secret"这条路（§3.2），另有两处密钥流向自相矛盾要修（§3.1） |
| 2 | `brickkit diff` 对比两份环境配置 | **暂缓（取舍题），先补一条对比配方** | 头条场景（prod 里误关组件）现有的 `up --dry-run` 逐份输出一 diff 就看得见 |
| 3 | 增量生成引擎 | **否决（事实前提不成立）** | 50 个组件走完整链路只要约 1.9 ms；用户等的是引擎，而引擎本来就只动有变化的 |
| 4 | 引擎插件化（`--engine nomad`） | **否决** | 引擎接口早已存在；插件会让平台为没人担保的 `down` 语义报"成功"——正是撤掉 Podman 的教训 |
| 5 | `brickkit mock` / `up --with-mocks` | **命令否决；补一份"契约先行"配方** | 现有零件（`new --contract` + `local: true`）已能接通，缺的是文档 |

## 2. 逐条依据

### 2.1 提案 1：外部 Secret 集成

**提案的现状描述不准确。** 它说敏感信息"需要在本地 `.env` 或 `brickkit.yaml` 中以明文或加密形式存在"。
实际上 `brickkit.yaml` 里写的是 `${VAR}` 引用（写死会有警告：`internal/cli/up_secrets.go`），真值在环境里。
`${VAR}` 的查找顺序是**进程环境优先、`.env` 其次**（`internal/cli/up_k8s.go` 的 `envLookup`，注释写明"CI 里靠环境变量
注入真实密码"）。所以任何能把值放进环境的工具（密钥管理器自带的命令行、`sops exec-env` 一类包装器）
今天就能接入，**平台零代码**。

**为什么不由 CLI 直接调 SDK 取值：**

- 每接一种存储就多一个 SDK 依赖，而 CLI 的定位是单个小二进制；存储的种类没有上限。
- `up` / `--dry-run` 因此要带着存储的凭据、要联网。而 `--dry-run` 的语义是"只告诉我会发生什么"（`up.go` 里多处把阻断降级为警告）。
- 这是"配置中心"的邻居：提案自己承认要避开它，避开的办法是"只取一次"，但依赖、凭据、失败模式一样都没少。
- 与本平台"引用、不接管"的一贯做法相反：`serviceAccountName`、`imagePullSecrets`、`tlsSecret` 都是"运维建好，平台只引用"。

**但提案的痛点带出了两处真缺口**（这是本次评判最有价值的产出，全部实测，不是读代码推的）：

在 scratch 项目里，组件声明 `configSchema.apiKey`，`brickkit.yaml` 写 `config: { apiKey: ${THIRD_PARTY_KEY} }`，
同时绑一个数据库资源、密码写 `${PG_PASSWORD}`：

| 场景 | 资源密码 | 组件自己的 API 密钥 |
| --- | --- | --- |
| K8s，值在 `.env` | Secret（`secrets/resource-secrets.yaml`，权限 0600），Deployment 里是 `secretKeyRef` ✅ | **明文写进 Deployment 的 `env: value:`** ❌ |
| Docker，值只在 `.env` | compose 里保留 `${PG_PASSWORD}` ✅ | 保留 `${THIRD_PARTY_KEY}` ✅ |
| Docker，值在**进程环境**（CI 最常见） | **compose 里是 `DATABASE_PASSWORD=pw-from-env` 明文** ❌ | **`API_KEY=key-from-env` 明文** ❌ |

- **缺口 A（Docker，同一份配置两种结果）：** `config.ParseConfig` 里的 `expandEnvNode` 在解析时就把整份 `brickkit.yaml` 里所有字符串值的
  `${VAR}` 展开，且只认进程环境（`os.LookupEnv`）。所以变量在进程环境里时，明文在解析阶段就被写进了配置结构，
  之后 compose 渲染器想"保留占位符"也保留不了。这与 `internal/compose/compose.go` 里 `Options.Lookup` 的注释直接矛盾
  （"compose 文件本身刻意保留占位符：那份文件会被人打开看、进 git diff，明文密码进去就等于泄露"），
  也与 `docs/en/07-patterns/07-shell-implementers-guide.md` 里"brickKit's Docker Compose generation deliberately never resolves those itself"矛盾。
  依赖变量放在哪（进程环境还是 `.env`）竟决定明文进不进文件，是设计不一致，不是取舍。
- **缺口 B（K8s，密钥有两套待遇）：** 注入层只有"资源连接变量"带敏感标记（`inject.Var.SecretKey` 只在 `resourceVars` 里设置），
  组件自己的配置项没有"敏感"这个概念。于是同样是密钥，资源密码"永远不出现在 Deployment 里"（`internal/k8s/secret.go` 头注释），
  而 CLI 自己警告文案建议的写法 `${MY_TOKEN}`（`up_secrets.go`）落到 K8s 上恰恰是明文进 Deployment。
  Secret 与 Deployment 在 K8s 里是分开授权的，这正是拆开它们的意义。

**第二次评判改判的一条：** 引用**已存在**的 K8s Secret（External Secrets Operator / Vault Secrets Operator / Sealed
Secrets 建的），让 CLI 完全不经手密钥值本身——第一次评判把它写进了"没采纳、以后有需求再做"，理由是"目前没有需求"。
重新核实后这个理由不成立：提案 1 举的两个例子（数据库密码、第三方 API 密钥）**恰恰就是**这个需求，而不是"另一种
以后才会有的需求"；平台已经有同构、验证过的模式（`serviceAccountName`："运维已经建好，平台只引用、不生成"），
Helm 生态里 `existingSecret` 也是被验证过的成熟写法；实现不需要任何 SDK、不需要联网，只是"生成 Secret 那步换成
引用一个名字"。这是"有价值、该做"的重构，设计见 §3.2，不再是暂缓项。

### 2.2 提案 2：`brickkit diff`

**提案说的困难被夸大了。** 多环境要求每份文件自包含，`docs/en/06-architecture/01-design-principles.md` 与 AGENTS §9.9 的论证正是
"整份文件所见即所得，Git diff 一目了然"。文本级 `diff -u brickkit.dev.yaml brickkit.prod.yaml` 已经能读。

**提案的头条场景——"哪些组件在 prod 里被意外 `enabled: false`"——现有命令已经回答了。** 实测：

```
diff <(brickkit up --dry-run --config brickkit.dev.yaml) <(brickkit up --dry-run --config brickkit.prod.yaml)
```

差异里直接出现
`⬜ acme/web@0.1.0  不启动（强依赖 erp/backend 不启动）`——**后果**（谁跟着停了）而不只是那行 `enabled: false`。
（两次 dry-run 会各自覆盖 `.brickkit/generated/`，只比对标准输出不受影响；配方里要说明。）

**做成命令的成本：** 第 15 个命令，`AGENTS.md` §8、`09-cli-reference`（中英、含真实输出）、`check-cli-docs`、错误码文档都要跟；
更实质的是 `buildUpPlan`（`internal/cli/up.go`）边算边打印、还会写文件，不是纯函数，要先拆出"计算"与"渲染"；
比较的模型每加一个 brickkit.yaml 字段都要跟，漏了就是"diff 说没差异、实际有差异"这种看着正常的假话。

**判断：** 这是取舍，不是缺陷。推荐**暂缓**：先补配方（§3.3），等有一个真实的双环境项目、且配方答不了他的问题时再做。
如果要做，收窄的做法见 §5。`--env1 dev --env2 prod` 的写法不采用——平台没有"环境"这个概念，只有 `--config <文件>`。

### 2.3 提案 3：增量生成引擎

**前提不成立。** 仓库里已有基准 `tests/perf/perf_test.go` 的 `BenchmarkGenerateCompose50`，走的是使用者感知到的整条链路
（解析 → 级联 → 注入 → 生成）。本机实测（`go test ./tests/perf -run '^$' -bench . -benchtime 20x`）：

| 基准 | 耗时 |
| --- | --- |
| 生成 50 个组件的 compose（整条链路） | ≈ 1.9 ms |
| 解析 100 个组件的 `brickkit.yaml` | ≈ 0.9 ms |
| 100 层依赖链求解 | ≈ 0.18 ms |
| 50 个组件拓扑排序 | ≈ 0.03 ms |

使用者真正等的是 `docker compose up` / `kubectl apply`，而这两个引擎**本来就只动有变化的部分**
（compose 只重建变了的 service，`kubectl apply` 做三方合并）。

**代价：** 要在 `.brickkit/` 里维护一份哈希状态。环境变量注入依赖整张图（依赖地址、级联结果、servedBy 分组），
"受影响的下游节点"算下来几乎就是全部；缓存一旦过期，产出的是**看着正常的错误部署文件**。
与"显式优于隐式"和"CLI 不持有状态"直接冲突。

**重启条件：** 在真实项目上量到生成阶段超过约 100 ms。

### 2.4 提案 4：引擎插件化

**前提有一半是错的。** 提案说 `internal/engine/` "硬编码了对 Docker Compose 和 Kubernetes 的支持"。
实际上 `engine.Engine` 早就是接口（`Up / Down / Status / CheckImage / CurrentContext`，`internal/engine/engine.go`）。
真正散落的是**生成侧**的目标分发：`internal/cli` 里约 10 处 `cfg.Deploy.Target == config.TargetK8s`。

**否决的理由：**

- 接口的每个方法背后都是一条条踩出来的正确性保证（见该文件里的长注释）：`Down` 只认项目名不认文件、
  孤儿清理按标签选择器、迁移按组件分组串行、`createNamespace: false` 时选择器不能为空……
  这些是"能拆干净"的前提。插件要自己担保它们，而 CLI 会替它报"成功"——
  这正是 AGENTS §4.1 里撤掉 Podman 的理由："一个拆不掉的项目比一个没起来的项目更糟"。
- `brickkit up --engine nomad` 会绕开 `deploy.target` 这个**声明**：同一份 `brickkit.yaml` 因命令行参数不同而部署到不同地方，
  违反"`brickkit.yaml` 是声明"。
- "标准化组件拓扑数据结构"会变成永远不能改的公共契约。目前没有任何人要 Nomad / ECS。
- 想要新目标的人可以走仓库内的正式贡献路径，那样它能拿到全套测试守卫。

**没做的重构：** 把约 10 处目标分支收敛成策略接口。没有第三个目标在排期，是推测性的通用化。

### 2.5 提案 5：契约 Mock

**命令否决，理由是事实性的：**

- `artifacts[].format` 是自由字符串（AGENTS §6："平台从不解释"），平台不解析契约内容。
  从 OpenAPI 生成 mock 要 OpenAPI 解析、示例合成；protobuf/gRPC 还要反射或代码生成。
- **命名自相矛盾：** 提案让 mock 叫 `mock-erp-backend-1-0-0`，可调用方拿到的是
  `ERP_BACKEND_ENDPOINT=http://erp-backend-1-0-0:PORT`（变量值指向**真实**组件的版本化服务名）。
  mock 起在另一个名字下，没有任何流量会打到它。
- **`up --with-mocks` 自动把缺失的强依赖换成 mock**，违反"强依赖缺失 → 报错并阻断启动"与"显式优于隐式"；
  一次误用就可能把替身部署上生产。
- CLI 用完即走、从不构建镜像也不挂 Docker socket，没法拥有一个 mock 进程/镜像。

**需求本身用现有零件已经能满足**（在 scratch 项目里真跑过，`--dry-run` 的输出与生成物如下）：

```
brickkit new erp/backend --contract openapi     # 按约定的契约立一个桩，同时写好 artifacts
brickkit new acme/web                           # 消费方，依赖 erp/backend@0.1.0
brickkit add --local
# brickkit.yaml：给 erp/backend 加 local: true、localPort: 18081
brickkit up --dry-run
```

消费方容器得到 `ERP_BACKEND_ENDPOINT=http://erp-backend-0-1-0:18081` 与
`extra_hosts: erp-backend-0-1-0:host-gateway`。主机上任何 mock 工具监听 18081 就是那个上游。
缺的只是把这条路写下来：`docs/{en,zh}/` 里搜不到"上游还没好怎么办"（只有一篇讲测试的文章反对用 mock 代替真实依赖）。

## 3. 采纳项的设计

### 3.1 密钥流向：每个值只在"最终需要它的那一处"求值，密钥性质由声明给出

规则（修完之后）：

| 落点 | `${VAR}` 什么时候求值 | 密钥落在哪 |
| --- | --- | --- |
| Docker compose 文件 | **不求值**，占位符原样留给 `docker compose`（它启动时按"进程环境 → `.env`"求值） | 文件里永远没有密钥 |
| K8s 清单 | 生成时求值（`envLookup`，进程环境 → `.env`） | 声明为密钥的进 Secret（0600），Deployment 里只有 `secretKeyRef` |
| `local-debug.*.env`（给 IDE） | 生成时求值（IDE 不做变量替换） | 给 IDE 读的文件，本就是明文 |

**修缺口 A：** `expandEnvNode` 不再展开两处字段——`components[].config` 与 `resources[].password`。
理由：这两处装的是要进部署清单的密钥候选值，而"什么时候求值"只有渲染器知道。其余字段（`sources[].url`、`authToken`、
`deploy.*`、资源的 `host` 等）是 CLI 自己要用的，仍在解析时展开。这两处的 `${VAR}` 因此原样进入 `config.Config`，
三个渲染器早就各自会求值（compose 交给 docker、K8s 用 `expander`、local-debug 用 `Lookup`）——所以这是**改回文档写的样子**，不是新机制。
顺带：`config.Component.ConfigFromEnv` 与 `config.Resource.PasswordFromEnv` 两个"展开前先记下写的是不是引用"的补丁字段，
失去存在理由（值本身就是原文），一并删除，不留冗余状态。
边界已核实：`brickkit add` / `remove` 走 YAML 节点编辑（`internal/config/edit.go`），从不经过 `Config` 结构体，
所以不存在"把展开后的密钥写回文件"的路径；`internal/engine` 不设置子进程环境，`docker compose` 继承进程环境。

**修缺口 B：** `configSchema.properties.<key>.secret: true`（布尔，默认 false）。

- **为什么放在 Manifest 里、而不是按名字猜：** 是不是凭据只有组件作者最清楚；`secretishKey` 那套名字启发式
  （`up_secrets.go`）本来就"宁可漏报也不误报"，只配拿来发警告，不配拿来决定值写到哪。
  这与 `resources` 的做法一致——`kind` 决定哪些变量是密钥，是声明，不是猜测。
- **不违背 §9.12（configSchema 是说明书，不是安检机）：** `secret` 不校验任何值，不拒绝任何输入；
  它只决定**值写到哪个位置**。与 `enum` / `minimum` 的区别只在"有没有渲染行为"，而这一点在文档里要写明白。
- **K8s：** 声明了 `secret: true` 的配置项 → `Secret/<版本化服务名>-config-secret`（key = 该项的环境变量名），
  写进 `secrets/config-secrets.yaml`（与资源 Secret 的 `resource-secrets.yaml` 分开：两类归属不同，名字一眼看得出；
  仍在 `secrets/` 下，所以权限同为 0600）。Deployment 里是 `secretKeyRef`。
  Secret 名加 `-config-` 是为了永远不与资源 Secret（`<资源ID>-secret`）撞名——资源 ID 是使用者起的，可能恰好等于某个服务名。
- **servedBy：** 成员的配置类变量并入外壳环境时会被改名（加组件 ID 前缀）。Secret 仍归**成员**
  （`Var.Owner` 记着成员的服务名，改名时原样带着），外壳的 Deployment 里 `MDM_CUSTOMER_API_KEY` 是指向成员 Secret 的 `secretKeyRef`。
  `BRICKKIT_SERVED_MEMBERS_CONFIG` 本来就只带变量名、不带值，不受影响。
- **Docker：** 不受影响（已由缺口 A 的修复保证占位符不被提前展开）。
- **警告：** `warnConfigSecrets` 除了名字启发式，也认 `secret: true`：声明了是密钥、`brickkit.yaml` 里却写了字面值，就警告，
  不论名字长什么样（比如 `webhookUrl`）。写成 `${VAR}` 引用永远不警告。
- **不做：** 按名字自动判定；给 `brickkit.yaml` 加"把这个 config 项当密钥"的项目侧覆盖（作者与部署方各说一半会打架，单一来源更简单；真有需求再加）；
  平台代取值（§2.1，机制本身不做）；校验 `existingSecret` 指向的 Secret 里真的有对应的 key（K8s 自己在 Pod 启动时报，CLI 不重复校验一遍集群状态）。

### 3.2 `existingSecret`：引用外部系统已经建好的 Secret（仅 K8s）

**动机：** 提案 1 的两个例子——数据库密码、第三方 API 密钥——都是"运维/安全团队已经用 Vault Secrets Operator /
External Secrets Operator / Sealed Secrets 之类的工具，在集群里建好了一个 K8s Secret"，而不是"CLI 自己去问 Vault
要一个值"。平台需要做的只是"引用那个 Secret，别自己生成一份"——与 `serviceAccountName`（"运维已建好，平台只引用"）
同一个模式，Helm 生态里也是被验证过的成熟写法（很多 chart 的 `values.yaml` 都有一个 `existingSecret` 字段）。
这个模式不需要平台理解 Vault/AWS 是什么，只需要理解"K8s 里已经有一个叫这个名字的 Secret"，与 §2.1 否决的
"CLI 调 SDK 取值"完全是两件事：这里 CLI 从头到尾不接触密钥的值。

**资源层：`resources[].existingSecret: <K8s Secret 名>`（仅 K8s）**

- 与 `password` 二选一：两者都写就报错（"不能同时写 password——已经有一个外部管理的 Secret 时，password 是多余的、也可能对不上"），
  校验形状照抄 `validateServedBy` 里"两种意图矛盾"的报错风格。
- 语义：这个资源的密钥字段（`database`/`cache`/`mq`/`smtp` 的 `password`，`storage` 的 `secret-key`）不再由平台生成 Secret，
  Deployment/合并进外壳的环境变量里的 `secretKeyRef.name` 直接指向 `existingSecret` 写的名字，`key` 用平台自己会用的那个
  固定 key 名（`password` 或 `secret-key`）——不新增"这个 key 叫什么"的字段，因为平台自己生成时就是用这个名字，
  引用别人建的 Secret 时约定它也叫这个名字，最省心。
- **Docker 下没有效力，只警告不报错**（与 `replicas`/`tlsSecret`/`serviceAccountName` 等"只在 K8s 生效"的字段同一条路）：
  Docker Compose 没有"引用一个外部建好的 Secret"这个概念（Swarm 的 `secrets: external: true` 不在本平台的范围内），
  警告文案提示"Docker 下请直接写 `password`"。
- 实现落点：`resourceVars`（`internal/inject/inject.go`）在 `r.ExistingSecret != ""` 时，即使 `r.Password == ""` 也要
  产出那条密钥变量（否则这条变量整个消失，K8s 侧就没有东西可以挂 `secretKeyRef`）——但 `Var.Value` 留空，因为压根没有
  值可言。`Var` 新增字段 `ExistingSecretRef string`：非空表示"K8s 直接引用这个名字，不生成 Secret"。
  `k8s.secretRef`（Task 2 引入）优先检查它；`k8s.collectSecrets` 跳过这类变量（不生成 Secret 条目，没有值可写）。
  **Docker 侧必须新增一条防线**：`compose.environmentOf` 目前对每条 `Var` 无条件写 `NAME=VALUE`；`Var.Value == ""` 在
  今天的代码里根本不会发生（所有产生空值的路径都在生成 `Var`之前就被跳过了），所以这是一条新增但从未被触发过的边界——
  加一条"值为空就不写这一行"的防线，行为上等价于"这个连接项没配"，与 §9.13 的"缺失就不注入，绝不注入空值"是同一条原则，
  不是新发明一条规则。
- 好处：不会像"平台代取值"那样引入 SDK/网络依赖，`brickkit up`（含 `--dry-run`）在生成阶段完全不需要知道密钥的值，
  值什么时候进 Secret、怎么轮换，都完全是 Vault Secrets Operator/ESO 自己的事——这正是"平台只当连接器和翻译器"（§9.24）。

**组件配置层：声明了 `secret: true` 的配置项，值可以写成 `{ existingSecret: <名>, key: <Secret 里的 key> }`（仅 K8s）**

- 形状：`brickkit.yaml` 里 `config.<key>` 平时是标量（字符串/数字/布尔/`${VAR}`），现在**同一个位置**允许写一个两键对象：
  ```yaml
  components:
    - id: acme/hello
      version: 1.0.0
      config:
        apiKey:
          existingSecret: acme-hello-vault-synced
          key: api-key
  ```
  之所以要带 `key`：资源层的密钥字段名是平台自己定的（`password`/`secret-key`），约定"引用的 Secret 也用这个名字"很自然；
  组件配置项的名字是**组件作者自己起的**（`apiKey`），没有理由假设外部同步过来的 Secret 里刚好用同一个 key，所以必须让使用者显式写。
- 只在该配置项 `configSchema.properties.<key>.secret: true` 时才认这个形状；**同一个位置**如果不是这个形状，走原来的标量/`${VAR}` 路径，两者不冲突（靠"是不是这个精确的两键对象"来分辨，不新增外层字段）。
- **没声明 `secret: true` 却写了这个形状：** 警告"这个配置项没有声明 `secret: true`，`existingSecret` 写法不会生效"，
  且**不注入**这条变量（不能眼看着 `formatValue` 把这个对象字面翻译成 Go 的 `map[...]` 字符串糊给组件——那比"完全不注入"更误导人）。
- **Docker 下的这个形状：** 同资源层，只警告"仅 K8s 生效"，该配置项在 Docker 下当作未配置（不注入），提示改成直接写 `${VAR}` 或字面值。
- 实现落点：`internal/config` 新增一个小的形状识别函数（`map[string]any` 且恰好两个键 `existingSecret`/`key`，都非空字符串），
  供 `internal/inject`（生成对应 `Var`）与 `internal/cli`（K8s-only 字段警告、明文密钥警告的豁免）复用，不重复写判断逻辑。
  `inject.addConfig` 遇到这个形状时跳过 `formatValue`，直接产出 `Var{ExistingSecretRef: <名>, SecretKey: <key>, Owner: b.service}`（`Value` 留空）。
  `warnConfigSecrets`（明文密钥告警任务引入 `declared` 判据，existingSecret 任务补上豁免）要豁免这个形状——
  它是"做对了"，不是"写了明文"，否则会被误判成明文密钥警告的对象。

**不做的边界（写进文档，不是留白待做）：**

- 不校验 `existingSecret` 指向的 Secret 在集群里真的存在、真的有那个 key——那是 K8s 自己在 Pod 启动时的事（`secretKeyRef`
  指向不存在的 Secret/key，Pod 会卡在 `CreateContainerConfigError`），CLI 生成阶段本来就不连集群，没有能力也没有必要抢在
  K8s 前面做同一件事。
- 不支持"整份 `config` 都从一个 Secret 灌进来"（Helm 里常见的 `envFrom.secretRef`）——那是"整份环境变量表来自一个不透明的
  Secret"，会让"这个变量为什么是这个值"变得不可追踪（正是本平台"环境变量名基于组件 ID，双向可推导"这条设计的反面）；
  逐项引用（本设计）保留了这条可追溯性。

### 3.3 文档：把"密钥怎么流"写清楚

新增 `docs/{en,zh}/07-patterns/10-secrets.md`：先用大白话讲"密钥"和"为什么要与普通配置分开对待"（读者不预设背景），
再讲三个落点的行为（§3.1 表）、`.env` 的位置与 `.gitignore`、密钥管理器怎么接——**两条路**：①把值放进进程环境，
然后 `brickkit up`（**不写没跑过的具体工具命令**，用能真跑的写法演示机制）；②`existingSecret`，引用外部系统已经建好的
整个 Secret，CLI 从头到尾不接触值（§3.2）。以及如实交代的边界：CLI 生成的 Secret 明文落在
`.brickkit/generated/k8s/secrets/`（0600、可再生、在 `.gitignore` 里）；`existingSecret` 不校验集群里那个 Secret
是否真的存在。

### 3.4 配方（都是文档，不是新机制）

- **契约先行：** `docs/{en,zh}/03-guide/07-consuming-artifacts.md` 新增一节。用 `demo/caller` 当消费方、
  `brickkit new demo/hello --contract openapi` 立桩（改成消费方依赖的版本），`add --local`，`local: true` + `localPort`，
  主机上起任意 mock。**确定性的部分**（`new`、`add --local`、`up --dry-run`）进 `scripts/check-guide-output.py`，
  需要 Docker 的端到端部分单独写明。
- **对比两份环境配置：** `docs/{en,zh}/07-patterns/05-deployment-selection-guide.md` 的多环境那条补上
  `diff -u` 与 `diff <(up --dry-run …) <(up --dry-run …)` 两行配方，附真实输出与"dry-run 会覆盖 `.brickkit/generated/`"的提醒。

### 3.5 记录被否决的想法

`AGENTS.md` §4.1 的拒绝清单（这张表就是为了让下一个 AI/贡献者别再提议同样的东西）补 4 行；
`docs/{en,zh}/06-architecture/00-overview.md` 的"刻意不做"补 4 条编号详解（18–21）。AGENTS.zh.md 同步。
"平台代取外部密钥存储的值"这一条要**带上 `existingSecret` 的carve-out**——写法照抄 API 网关那条的先例
（"平台不做网关，但 `labels` 透传让外部网关接得上"）：明确说"CLI 不调任何 SDK，但 `existingSecret` 能引用一个
已经建好的 Secret"，不能让读者以为"平台完全没有密钥管理器集成的路"。

| 新增 | 一句话 |
| --- | --- |
| 平台代取外部密钥存储的值（Vault / AWS Secrets Manager SDK） | 进程环境优先已让任何工具零代码接入值本身；`resources[].existingSecret` 与 config 密钥的 `existingSecret` 引用（§3.2）让整个 Secret 也能被引用而不必平台生成；平台代取值本身仍不做——那要带 SDK、带凭据、`--dry-run` 也得联网 |
| 引擎插件 / 第三方部署目标（`--engine nomad`） | 目标的 `Down` 等语义保证由谁担保？平台会替它报"成功"——撤掉 Podman 的同一个理由；且 `deploy.target` 是声明 |
| 增量生成缓存 | 50 个组件整条链路约 2 ms；引擎本来就只动变化的；缓存过期产出的是看着正常的错误文件 |
| 契约 mock 生成 / `--with-mocks` 自动替换缺失依赖 | 平台不解析契约；替身会被误部署；名字对不上注入的地址。用 `new --contract` + `local: true` + 任意 mock 工具 |

## 4. 范围之外（明确不在本次做）

- 新增任何 CLI 命令或参数（`existingSecret` 是**字段**，不是命令）。
- `brickkit diff`（§5）。
- 把约 10 处 `Deploy.Target ==` 分支重构成策略接口——这是观察到的一处代码不够整齐，不是任何一条提案要求的，
  单独拿出来重构风险与工作量都不小，不在本次范围（见下方"观察但不处理"）。
- 校验 `secret: true` 的值、按名字自动判定密钥；校验 `existingSecret` 指向的 Secret 是否真的存在（§3.2 的"不做的边界"）。
- 整份 `config` 从一个 Secret 灌入（`envFrom` 语义）——逐项引用保留可追溯性，见 §3.2。

**观察但不处理：** `internal/cli` 里约 10 处 `cfg.Deploy.Target == config.TargetK8s` 分支散落在 `up.go`/`down.go`/
`status.go`/`up_k8s.go`/`k8s_cluster.go`。这不是提案 4 要求的东西（提案 4 要的是**对外插件接口**，已否决），
单纯把这些分支收敛成一个内部策略接口是纯代码质量重构，没有新增任何用户可见能力，不属于"新功能有价值就该做"的范围；
记在这里是为了不让下一次评判把它当成新发现。

## 5. 暂缓项与重启条件

- **`brickkit diff`。** 重启条件：有一个真实的双环境项目，且 §3.4 的配方答不了他的问题。若做，收窄为：
  ① `config.Config` 的**反射式**结构比较（按 `id@version` 配对组件、按资源 `id` 配对资源，逐字段比较——不为每个字段写代码，新字段自动被覆盖）
  + ② 两边的级联结果并排（谁跑、为什么）；**不**比环境变量与渲染后的文件（dev 是 docker、prod 是 k8s 时渲染文件不可比，环境变量差异是配置差异的推论）；
  两个位置参数（不是 `--env1/--env2`）；含密钥的值（`password`、`authToken`、名字像密钥的 `config` 项、`existingSecret`）一律只显示"已设置/引用名"，不显示值；
  前置重构：把 `buildUpPlan` 拆成纯计算 + 渲染。

## 6. 风险

- **缺口 A 的修复改变了用户可见的行为：** 进程环境里有值时，compose 文件里从明文变成占位符。运行时的值不变（`docker compose` 用同一个进程环境求值）。
  测试要钉死两件事：占位符保留；K8s 目标的求值结果不变。
- **`ConfigFromEnv` / `PasswordFromEnv` 是导出字段：** 删除前用 `git grep` 确认只有 `internal/cli/up_secrets.go`、`internal/config/parse.go`、
  `internal/config/edge_test.go` 用到（`tests/components/*/…_test.go` 里的 `TestConfigFromEnv` 是同名的另一回事，与此无关）。
- **新增 `secret` 字段会触发文档守卫：** `tests/docfields/reference_test.go` 要求 component.yaml 参考文档（中英）覆盖每个结构体字段，
  AGENTS 骨架（中英）也有对照测试。这是好事——它逼着文档跟上。
- **`config.<key>` 的值现在可能是一个两键对象，不再总是标量：** `formatValue` 的默认分支（`fmt.Sprint`）今天会把任何
  非标量值糊成 Go 的 `map[...]` 字符串糊给组件——这是一个已经存在、与本次改动无关的既有小问题（非 secret 属性写了嵌套值时）。
  本次改动只保证"声明了 `secret: true` 且形状匹配"这一种情况被正确识别并绕开 `formatValue`；不修复既有的默认分支行为，
  不属于本次范围。
- **`existingSecret` 与 `secret: true` 的组合校验容易漏一种情形：** 声明了 `existingSecret` 形状但配置项**没有**声明
  `secret: true` 时，必须是"警告 + 不注入"，绝不能落进 `formatValue` 把对象糊成字符串注入给组件——这条要单独测。
