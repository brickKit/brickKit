# BrickKit · AI 导读

> **这份文件是写给 AI 助手的。**
>
> 如果有人把 `https://github.com/brickKit/brickKit` 丢给你、让你了解或讨论这个项目，
> 读完这一份就够了——它是全站约 1.7MB 中文文档（14 本设计书 + 23 篇试用指南 + 566 条决策记录）
> 的压缩件，覆盖 BrickKit 的**全部核心理念与做法**。
>
> 读完之后你应该能做到：用中文和用户讨论这个项目的任何设计、判断某个提议是否符合它的哲学、
> 在需要细节时知道该去抓哪一份文档（见文末第 11 节）。
>
> 全站文档为中文，术语请沿用本文的中文说法（组件 / 强依赖 / 版本化服务名 / 级联关闭……）。

---

## 1. 一句话定位

**BrickKit 是一个组件管理与拼装平台。像搭积木一样构建系统。**

每块积木（组件）独立制造、独立测试、独立部署、独立调用。BrickKit CLI 负责把积木拉来、
排好顺序、生成图纸、交给 Docker 或 K8s 装好。剩下的，全是组件自己的事。

它**不是**操作系统，不是 ERP，不是任何一个具体的业务软件。它是让你**渐进式**长出架构的工具：
先写一个小组件跑通，再写一个跑通，然后写一个连接组件把它们串起来——最终拼出任何系统。

### 1.1 类比（帮助快速定位）

| BrickKit | 大致相当于 |
| --- | --- |
| BrickKit CLI | `npm` + `helm` + `docker compose` + `git clone`，但面向**业务组件** |
| BrickKit Market（组件市场） | npmjs.com / Docker Hub / App Store |
| Component（组件） | npm package / Docker image |
| `component.yaml`（Manifest） | `package.json` |
| `brickkit.yaml`（项目配置） | `docker-compose.yaml` 的"声明式输入" |
| `brickkit add` | `npm install` |
| `brickkit up` | `docker compose up -d` / `kubectl apply` |

**但有一个根本区别：** npm 装的是代码库，BrickKit 装的是**能独立跑起来的业务服务**。
所以它同时要管依赖解析、部署文件生成、地址注入、数据库迁移、启动顺序。

### 1.2 五个核心价值

| 价值 | 说明 |
| --- | --- |
| 渐进式构建 | 不需要一次性设计完整系统，一块积木一块积木地加 |
| 语言无关 | 任何语言只要能构建 Docker 镜像，就能成为组件 |
| 环境一致 | 本地（Docker）与生产（K8s）使用**同一套地址格式**，组件代码零修改 |
| 极简平台 | 平台不越界：业务逻辑、通信治理、多租户全部交给组件 |
| 开源优先 | CLI 与市场均开源，闭源组件通过市场受控分发 |

---

## 2. 系统组成与关键架构事实

### 2.1 四个部分

| 部分 | 形态 | 是否常驻 | 职责 |
| --- | --- | --- | --- |
| **BrickKit CLI** | 本地单二进制 | ❌ 用完即走 | 拉取、解析、生成、调用、发布、源码工作区管理 |
| **BrickKit Market** | 独立 SaaS（可私有化） | ✅ | 组件发布与发现、版本/可见性/签名、产物存储 |
| **组件层** | Docker 容器 / K8s Pod | ✅ | 业务本体，组件间 DNS 直连 |
| **基础设施层** | PostgreSQL / Redis 等 | ✅ | **运维手动部署**，在 `brickkit.yaml` 中声明绑定 |

### 2.2 五条容易被误解的架构事实

这五条是理解 BrickKit 的关键。它们都是**刻意的取舍**，不是"还没做"：

1. **没有常驻的"主系统服务"。** CLI 执行完命令就退出。期望状态由 `brickkit.yaml` 持有，
   实际状态由底层引擎（Docker / K8s）持有。没有后台进程、不监听端口、无攻击面。
2. **没有自建注册中心。** 服务发现 = Docker Compose service DNS / K8s Service DNS。
3. **没有平台轮询健康检查。** 健康检查由 K8s Probe / Compose healthcheck + 重启策略承担。
4. **CLI 不挂载 Docker Socket。** 它只是调用 `docker compose` / `kubectl` 命令行，权限边界清晰。
5. **所有组件都是 container。** 包括前端组件——用 nginx 等 Web Server 容器 serve 静态资源。
   平台里**没有**"无容器 / static"类型的组件。

### 2.3 数据流（一次 `brickkit up` 发生了什么）

```
brickkit.yaml（声明）
   ↓ ① 级联计算：算出这次实际该启动哪些组件（enabled 三态 + 依赖图）
   ↓ ② 依赖解析：递归展开依赖树，强依赖缺失报错，拓扑排序得出启动顺序
   ↓ ③ 环境变量注入：依赖地址、资源连接、自身配置 → 环境变量
   ↓ ④ 生成部署文件：docker-compose.yaml 或 K8s Deployment/Service/Ingress
   ↓ ⑤ 执行迁移：K8s Job / Docker 一次性 service，失败则阻断主服务
   ↓ ⑥ 调用底层引擎：docker compose up -d / kubectl apply
运行中的容器
```

CLI 的 Manifest 来自 `.brickkit/manifests/` 缓存，**不依赖 `components/` 下的源码目录**。
源码目录只服务于 IDE 里的开发，与运行时无关。

---

## 3. 术语表

| 术语 | 英文 | 定义 |
| --- | --- | --- |
| 主系统 | BrickKit CLI | 本地命令行工具，无常驻进程 |
| 组件市场 | BrickKit Market | 公开的组件发布和发现平台 |
| 组件 | Component | 最基本的安装和运行单元，**全部是 container** |
| Manifest | component.yaml | 组件的自我描述文件 |
| 项目配置 | brickkit.yaml | 项目级声明：组件列表、启停、本地调试、暴露、配置覆盖、资源、部署目标 |
| 强依赖 | Required Dependency | 缺失时 CLI **报错并阻断启动** |
| 弱依赖 | Optional Dependency | `optional: true`；缺失时警告但继续，且**完全不注入该环境变量** |
| 版本化服务名 | Versioned Service Name | 带精确版本号的服务名，如 `people-basic-1-0-0` |
| 本地调试模式 | Local Debug Mode | `local: true`；组件跑在宿主机 IDE 中，用 `extra_hosts` 映射进容器网络 |
| 安装源 | Source | 组件来源：市场（http）/ Git 仓库 / 本地目录 |
| 基础资源 | Resource | 组件依赖的外部系统（数据库、Redis 等），运维部署，`brickkit.yaml` 绑定 |
| 环境变量注入 | Env Injection | CLI 生成部署文件时写入依赖地址、资源连接、自身配置 |
| 部署目标 | Deploy Target | `docker` 或 `k8s`，决定 CLI 生成哪种部署文件 |
| 数据库迁移 | Migration | 组件声明 `migration.command`，CLI 在部署前执行 |
| 配置覆盖 | Config Override | `brickkit.yaml` 的 `config` 覆盖 configSchema 默认值，**CLI 不校验类型** |
| 连接组件 | Connector Component | 协调多个单一组件的编排组件 |
| 单一组件 | Standalone Component | 独立完成一个功能、内部事务自洽的组件 |
| 精确版本 | Exact Version | `major.minor.patch`，依赖声明**不接受** `^` / `~` 范围约束 |
| 级联关闭 | Cascade Disable | 没有任何启用中的组件需要它时，未钉住的组件自动跳过 |

---

## 4. 十二条设计原则（理念内核）

讨论任何设计提议时，用这十二条去判断它是否属于 BrickKit：

| 原则 | 说明 |
| --- | --- |
| **平台极简** | CLI 能不做的事就不做；每多一个功能就多一份维护成本 |
| **组件自治** | 语言、框架、API、事务、日志格式由组件自定；平台只要求 Manifest + 健康检查 + 环境变量 |
| **渐进式** | 不要求一次性设计完整系统；随时可停，随时可继续 |
| **环境无关** | 组件代码不感知自己跑在 Docker 还是 K8s |
| **精确优于隐式** | 精确版本、显式暴露、显式启用；拒绝一切"自动猜测"带来的不可控 |
| **安全默认** | 不映射端口 = 外部不可访问；不声明 expose = 无 Ingress；private = 未授权不可见 |
| **开源优先** | CLI 与市场开源；闭源组件通过市场受控分发 |
| **平台提供工具，不替人做决定** | 多版本默认共存；降级逻辑归组件；破坏性变更是组织协调问题 |
| **安装即信任** | 平台不做前置安全审查，只做事后 `blocked` 下架 |
| **configSchema 是说明书，不是安检机** | CLI 不校验使用者填的 config 值类型 |
| **一个组件一个仓库** | 不支持 monorepo 拆子目录；组件是独立的发布 / 移动 / 权限单元 |
| **brickkit.yaml 就是声明** | 配置即意图。写了就执行，CLI 不反问"你确定吗" |

### 4.1 平台明确**不做**的事（拒绝清单）

这份清单是"平台极简"的具体落地。**向用户提议时，不要建议 BrickKit 去做下面任何一项**——
它们都被明确论证过并拒绝了（理由见第 9 节）：

| 不做 | 替代方案 |
| --- | --- |
| 常驻服务 / 控制面 | CLI 用完即走，状态外置到 `brickkit.yaml` + 底层引擎 |
| 注册中心 / 地址簿 | Docker DNS / K8s Service DNS |
| 健康检查轮询 | K8s Probe / Compose healthcheck + 重启策略 |
| API 网关 / 服务网格 / 负载均衡 | 组件 DNS 直连；K8s Service 原生负载均衡 |
| 配置中心 / 动态热更新 | 环境变量注入；改配置就 `brickkit up` 重启 |
| 通信治理（熔断 / 限流 / 重试） | 组件自己的代码 |
| 弱依赖降级逻辑 | 组件自己的业务逻辑 |
| 多租户 | 组件自己的事 |
| 版本范围解析（`^1.0.0`） | 只接受精确版本 |
| 多环境 overlay / 继承合并 | 每个环境一份完整自包含的 `brickkit.yaml` |
| config 值类型校验 | configSchema 只是说明书 |
| 第三方组件安全审查 | 安装即信任 + 事后 `blocked` |
| monorepo 子目录组件 | 一个组件一个 Git 仓库 |
| 低代码 / BI / DevOps 流水线 | 不在范围内 |

---

## 5. 核心机制（做法）

### 5.1 版本化服务名与统一地址

**服务名 = 组件 ID 转换 + 精确版本号。** 转换规则：`/` → `-`，`.` → `-`，全部小写。

| 组件 ID | 版本 | 服务名 |
| --- | --- | --- |
| `people/basic` | 1.0.0 | `people-basic-1-0-0` |
| `erp/backend` | 2.1.3 | `erp-backend-2-1-3` |

地址格式在两个环境下**完全一样**：`http://<版本化服务名>:<端口>`
（本地 `http://people-basic-1-0-0:8080`，K8s 也是 `http://people-basic-1-0-0:8080`）。

这一条带来两个直接后果：**多版本天然共存**（`people-basic-1-0-0` 和 `people-basic-2-0-0`
是两个互不冲突的 DNS 名），以及**调用方永远明确知道自己调的是哪个版本**，不存在隐式升级。

### 5.2 环境变量注入规范

**核心原则：环境变量名不带版本号（基于组件 ID），值带版本号（指向具体服务）。**

```bash
DEPARTMENT_TREE_ENDPOINT=http://department-tree-1-0-0:8080
```

| 类别 | 命名规则 | 示例 |
| --- | --- | --- |
| 依赖组件主端口 | `{组件ID前缀}_ENDPOINT`（`/` 和 `-` → `_`，全大写） | `people/basic` → `PEOPLE_BASIC_ENDPOINT` |
| 依赖组件额外端口 | `{组件ID前缀}_{NAME大写}_ENDPOINT` | `PEOPLE_BASIC_GRPC_ENDPOINT` |
| 平台通用变量 | 固定 | `COMPONENT_ID`、`COMPONENT_VERSION` |
| 资源连接 | 按资源类型 | `DATABASE_HOST/PORT/NAME/USER/PASSWORD`、`REDIS_HOST/PORT/PASSWORD` |
| 自身配置 | configSchema 驼峰项转大写下划线 | `defaultPageSize` → `DEFAULT_PAGE_SIZE` |

**保留变量保护（两层防御）：** `COMPONENT_ID`、`COMPONENT_VERSION`（精确匹配），
`*_ENDPOINT`（后缀匹配），`DATABASE_*` / `REDIS_*` / `MQ_*` / `STORAGE_*` / `SEARCH_*` /
`SMTP_*` / `{envPrefix}_*`（前缀匹配）。
configSchema 里的配置项名转大写后不得与之冲突——**市场在发布时拒绝**，
**CLI 在注入时警告并跳过该配置项**（平台注入的值优先）。

### 5.3 强依赖与弱依赖

| | 声明 | 缺失时 CLI 的行为 |
| --- | --- | --- |
| 强依赖 | `- department/tree@1.0.0` | **报错阻断启动** |
| 弱依赖 | `- id: infra/redis@1.0.0` + `optional: true` | 警告但继续，**完全不注入该环境变量** |

⚠️ **"完全不注入"不是"注入空字符串"。** 这是 BrickKit 最容易被误解的设计之一。
组件代码必须用安全读取方式（Python 的 `os.environ.get()`、Java 的 `System.getenv()`）；
用 `os.environ["X"]` 会抛 `KeyError` 让组件立刻崩溃——**这是刻意的**，理由见 9.13。

降级逻辑（Redis 挂了是查数据库、返空列表还是写本地文件）是组件自己的业务代码，平台不管。

### 5.4 `enabled` 三态与级联关闭

| 写法 | 含义 | 行为 |
| --- | --- | --- |
| `enabled: true`（显式写出） | **钉住** | 无论依赖链如何变化一定启动，不可被级联关闭 |
| **不写** `enabled` 字段 | **默认开启，可被级联关闭** | 没有启用中的组件需要它时，CLI 自动跳过 |
| `enabled: false` | **显式关闭** | 一定不启动 |

**级联规则：** 一个组件实际启动，当且仅当满足任一条件——它被显式钉住；
它是根组件（不被任何其他组件依赖的入口，通常是前端或连接组件）且未被显式关闭；
至少有一个正在启动的组件强依赖它，且它未被显式关闭。

`brickkit add` 自动添加的组件**不写** `enabled` 字段（即第二态）。这就是为什么
"一个 50 组件的项目，本地开发时可能只有 4 个容器在跑"。

### 5.5 部署文件生成与数据库迁移

组件仓库里**绝不自带**任何环境相关的部署文件。CLI 读统一的 `component.yaml`，
按 `deploy.target` 动态生成 `docker-compose.yaml` 或 K8s `Deployment/Service/Ingress`。
切换环境只改一个字段：

```yaml
deploy:
  target: k8s        # 原本是 docker
```

**迁移：** 组件声明 `migration.command`，CLI 在主服务启动前执行——
K8s 生成独立的 **Job**（不是 InitContainer，理由见 9.6）并 `kubectl wait`，
Docker 用一次性 service。**迁移失败阻断主服务启动。**
K8s 下 CLI 会先 `kubectl delete job --ignore-not-found` 清理残留旧 Job，保证幂等。

**暴露：** 默认不暴露。`expose: true` 时，K8s 生成 Ingress（需填 `hostname`），
Docker 映射端口到宿主机（可用 `exposePort` 自定义，端口冲突时 CLI 报错）。

### 5.6 本地调试（`local: true`）

要在 IDE 里断点调试某个组件，同时它还要被 Docker 网络里的其他组件访问：

- `brickkit.yaml` 中标记 `local: true` 的组件**不生成容器**
- 其他容器通过 `extra_hosts` 把该组件的版本化服务名解析到 `host-gateway`
- 多个组件可同时本地调试，用不同 `localPort`，CLI 自动注入对应端口
- CLI 生成 `local-debug.env` 供 IDE 加载
- **组件代码零修改**（照常读环境变量）

### 5.7 组件源码工作区

| 命令 | 行为 |
| --- | --- |
| `brickkit add <id>@<ver> --repo` | 额外 clone 该组件的完整 Git 仓库到 `components/`（仅开源组件） |
| `brickkit add <id>@<ver> --repo-all` | clone 所有递归依赖中开源组件的仓库（闭源跳过并提示） |
| `brickkit sync` | 按级联计算结果**双向**归档 / 激活：不启动的移到 `components/.archived/`，需要启动的移回 |
| `brickkit remove <id>` | 自动删除对应源码目录，**含已归档的那一份** |

`brickkit add` **默认不 clone 源码**（只拉 Manifest + artifacts）。
`brickkit sync` 是**独立命令**，刻意不集成进 `brickkit up`（理由见 9.17）。
CLI **不管 Git 权限**：fork、remote、push 全是用户自己的事。

### 5.8 市场、签名与信任模型

市场是独立的公共平台，**不是组件，不需要被安装**。它只回答两个问题：
**有什么可以装？谁有权装？** 它不安装组件、不运行组件、不管运行状态。

| 能力 | 说明 |
| --- | --- |
| 发布 / 发现 | 上传 Manifest + 镜像引用（+ 签名）；搜索、标签、命名空间筛选 |
| 版本管理 | 精确版本列表 + 状态：`draft` / `stable` / `deprecated` / `blocked` |
| 可见性 | `public` / `private` |
| 签名 | 发布方用 **cosign** 签名；**安装方用 Go 标准库验签**（不需要装 cosign）；生产强制 |
| 产物存储 | 开源：登记 Git 仓库地址；闭源：市场存 Manifest + 镜像引用 |

**信任模型：安装即信任。** 平台不做前置安全审查（不扫码、不沙箱、不静态分析），
只做最后的"城管"——发现确凿恶意组件时标记 `blocked`，阻止新安装。
一句话：**平台提供"集市"，不提供"保险箱"。**

认证：`brickkit login` 终端交互输入账密，Token 存 `.brickkit/credentials`。

---

## 6. `component.yaml`（Manifest）字段骨架

```yaml
apiVersion: brickkit/v1
kind: Component

metadata:
  id: <scope>/<name>             # 必须，组件唯一标识
  name: <显示名称>                # 必须
  version: <major.minor.patch>   # 必须，精确版本
  description: <组件描述>         # 必须
  vendor: <发布者>                # 可选
  license: <许可证>               # 可选
  apiDocs: <API 文档地址>         # 可选

tags: [<标签>]                    # 可选，市场搜索用

artifacts:                       # 可选，组件附带的产物（API 契约 / SDK / 文档）
  - type: api-contract           # 必须，自由字符串
    format: protobuf             # 可选，自由字符串
    description: <描述>           # 可选
    files: [<相对仓库根的路径>]     # 必须

dependencies:                    # 可选
  components:
    - department/tree@1.0.0                # 强依赖（精确版本）
    - id: infra/redis-event-bus@1.0.0      # 弱依赖
      optional: true
  resources:
    - kind: database             # database / cache / mq / storage / search / smtp
      engine: postgresql

configSchema:                    # 可选，自身配置项的"说明书"（CLI 不校验值类型）
  type: object                   # 配置项名不得与保留变量冲突（见 5.2）
  properties:
    defaultPageSize:
      type: integer              # string | integer | number | boolean | array | object
      default: 20
      description: <说明>
      enum: [...]                # 可选
  required: [<必填项>]

deployment:                      # 必须
  type: container                # 固定为 container（前端组件也是）
  image: <镜像地址>               # 必须
  port: 8080                     # 必须，主端口（健康检查 + _ENDPOINT 变量）
  extraPorts:                    # 可选，如 gRPC
    - name: grpc
      port: 9090
  resources:                     # 可选，**推荐值**，CLI 透传不校验
    requests: { cpu: "100m", memory: "128Mi" }
    limits:   { cpu: "500m", memory: "512Mi" }

migration:                       # 可选
  command: ["<命令>", "<参数>"]   # 数组格式

healthCheck:                     # 必须
  type: http                     # http | tcp | none
  path: /healthz                 # http 必填
  # ⚠️ 只检查本进程存活，禁止检查数据库 / 依赖组件 / 任何外部系统

observability:                   # 可选
  metrics: true
  tracing: false

compatibility:                   # 可选
  minCliVersion: 1.0.0
```

**资源配额优先级链：** `brickkit.yaml` 的 `resources` > `component.yaml` 的 `resources` > CLI 默认值。

**⚠️ 健康检查禁令：** `/healthz` 只检查本进程存活。在健康检查里查数据库或依赖组件
会导致生产环境雪崩——一个下游抖动会让所有上游同时被判不健康并重启。

---

## 7. `brickkit.yaml`（项目配置）字段骨架

```yaml
project: my-shop                 # 必须，用于 K8s namespace 与 Docker network 命名

deploy:
  target: docker                 # 必须：docker | k8s
  # ↓ 以下仅 K8s 生效
  context: <kubeconfig 上下文>     # 可选，钉住部到哪个集群
  namespace: <命名空间>            # 可选，默认 brickkit-<项目名>
  createNamespace: true          # 可选，只有命名空间级权限时置 false
  podSecurity: restricted        # 可选，目前只支持 restricted
  imagePullSecrets: [<secret>]   # 可选
  ingressClass: <class 名>
  ingressAnnotations: { <键>: <值> }
  serviceAccount: { enabled: true }        # 每组件一个不挂载令牌的 SA
  networkPolicy:                           # 按依赖图生成网络策略
    enabled: true
    ingressController: { namespace: <ns>, podSelector: {<键>: <值>} }
    allowFrom: [ { name: <为谁开>, namespace: <ns>, podSelector: {...}, ports: [...] } ]
    egress:
      enabled: true
      allowTo: [ { name: <为谁开>, resource: <resources[].id> } ]

sources:                         # 安装源
  - id: <安装源ID>
    type: market                 # market | git | local
    url: <API 地址或 Git 地址>     # market/git 必填
    path: <本地路径>              # local 必填
    authToken: ${ENV_VAR}        # 可选；已 login 时优先用 .brickkit/credentials
    enabled: true

components:
  - id: people/basic
    version: 1.0.0               # 必须，精确版本
    enabled: true                # 可选，三态见 5.4
    local: false                 # 可选，本地调试模式
    localPort: 8081              # local: true 时的宿主机端口
    expose: false                # 可选，默认 false
    hostname: <域名>              # expose + k8s 时必填
    exposePort: 8080             # 可选，仅 Docker 生效
    config:                      # 可选，覆盖 configSchema 默认值（不校验类型）
      defaultPageSize: 50
    resources:                   # 可选，覆盖组件推荐配额
      requests: { cpu: "200m", memory: "256Mi" }
      limits:   { cpu: "1", memory: "1Gi" }

resources:                       # 基础资源声明与绑定（资源本身由运维部署）
  - kind: database
    engine: postgresql
    id: main-db
    host: <主机>
    port: 5432
    username: <用户名>
    password: ${DB_PASSWORD}     # 必须通过环境变量引用
    bindings:
      - componentId: people/basic
        database: people         # 数据库资源特有；database 由使用者创建一次
        envPrefix: <前缀>         # 可选，多同类资源时区分环境变量

installer:
  requireSignature: true         # 可选，默认 true
```

**多环境：** 每个环境一份**完整自包含**的 `brickkit.yaml`（如 `brickkit.prod.yaml`），
用 `brickkit up --config brickkit.prod.yaml` 指定。**没有 overlay / 继承 / 合并机制**（理由见 9.9）。

---

## 8. CLI 命令集（11 个命令 + `version`）

| 命令 | 核心行为 |
| --- | --- |
| `brickkit init <name>` | 生成 `brickkit.yaml` 骨架和 `.brickkit/` 目录 |
| `brickkit add <id>[@ver]` | 递归拉取依赖，下载 artifacts，写入配置（**不写 `enabled` 字段**）。不写版本时取安装源上最新可安装版本，并以**精确版本**落盘 |
| `brickkit remove <id>` | 检查强依赖方后移除，自动删除源码目录（含归档的那份）。多版本共存时必须指定版本 |
| `brickkit fetch <id>[@版本]` | 只下载组件的产物到 `.brickkit/artifacts/<版本化服务名>/`，**不写入 brickkit.yaml、不部署**。跨项目调用别人的服务时用（003 §4.9） |
| `brickkit up` | 级联计算 → 生成部署文件 → 生成 `local-debug.env` → 检测镜像权限 → 执行迁移 → 调用引擎 |
| `brickkit down` | 停止所有组件。**不删除 volume，保留数据** |
| `brickkit status` | 读底层引擎，展示运行表格（含多版本检测、级联跳过展示） |
| `brickkit sync` | 按级联结果双向归档 / 激活组件源码 |
| `brickkit login` | 终端交互登录市场，Token 存 `.brickkit/credentials` |
| `brickkit publish` | 上传 Manifest + 镜像引用 + 产物到市场（需先 login） |

**常用参数：**

```bash
brickkit up --only people/basic,department/tree   # 只启动指定组件（遇 enabled:false 报错阻断）
brickkit up --config brickkit.prod.yaml           # 多环境
brickkit up --dry-run                             # 只生成部署文件，供审查
brickkit add people/basic@1.1.0 --yes             # 非交互（CI/CD）
brickkit add --local                              # 把本地安装源里的组件一次全部添加
brickkit add erp/backend@1.0.0 --repo-all         # clone 所有开源依赖的源码
brickkit fetch infra/notifier@1.0.0               # 只取产物（跨项目调用，不装进项目）
brickkit remove people/basic@1.0.0                # 多版本共存时指定版本移除
```

### 8.1 一分钟示例

```bash
brickkit init my-shop                 # 创建项目
brickkit add erp/backend@1.0.0        # 一条命令拉下整棵依赖树
brickkit up --dry-run                        # 看启动顺序（拓扑排序）
brickkit up                           # 生成部署文件 → 跑迁移 → 起容器
```

---

## 9. 二十个"为什么"（架构辩护）

这一节是 BrickKit 理念最值钱的部分。每一条都是对一个**看起来反直觉**的设计的辩护。
用户问"为什么不……"时，答案基本都在这里。（完整论证见设计书 012。）

**9.1 为什么没有注册中心，也不做健康检查轮询？**
自建注册中心意味着平台必须是常驻高可用集群，运维成本剧增；而 Docker / K8s 原生的 DNS 和 Probe
已经足够好，且比"每 10 秒轮询一次 `/healthz`"响应更快。组件也因此**零侵入**——不需要任何注册 SDK。

**9.2 为什么强制精确版本，拒绝 `^1.0.0`？**
范围版本是"在我机器上能跑，在生产挂了"的罪魁祸首。精确版本还让版本号能直接拼进服务名，
使多版本共存成为零成本的天然能力。
⚠️ 注意区分：`brickkit add people/basic`（省略版本号）**是允许的**——CLI 在 add 那一刻问出
一个具体版本、把精确版本钉进配置，等同 npm 写 lockfile。被拒绝的是**配置里写范围约束**
（`brickkit add people/basic@^1.0.0` 仍然报错）。区别在**解析时机**：一次 vs 每次。

**9.3 为什么多版本默认共存，而不是报错？**
版本化服务名让共存不需要任何额外机制。而且 `brickkit.yaml` 里写了两个版本条目，
这就是用户的意图，CLI 不需要再问一次"你确定吗"。破坏性变更是**组织协同问题**，
多版本共存只是物理缓冲，数据层兼容由使用者保证。

**9.4 为什么组件不自带部署文件？**
一套 Manifest、两种环境。组件开发者不必同时维护 `docker-compose.yaml` 和 K8s 三件套。
组件只描述"我是什么、我需要什么"，不关心"我跑在哪里"。

**9.5 为什么 CLI 是用完即走的本地工具，不是常驻 Server？**
没有后台进程 = 没有单点故障、不占资源、不监听端口、无攻击面。
`brickkit.yaml` 是唯一的真相来源，可以完美接入 Git 做版本控制和 Code Review。
未来若要可视化 Console，它应该是一个普通的前端组件 + API 组件，而不是硬编码进平台核心。

**9.6 为什么 K8s 迁移用 Job 而不是 InitContainer？**
InitContainer 是 **Pod 级**的。`replicas: 3` 时会有 3 个 InitContainer **并发**跑同一份迁移脚本，
极易死锁或损坏表结构。Job 是**集群级**的，CLI 串行控制，保证整个集群只执行一次。

**9.7 为什么不做通信治理和弱依赖降级？**
熔断阈值、限流算法、重试退避策略因业务而异，平台做"一刀切"既做不好又限制灵活性。
降级更是纯业务逻辑：Redis 挂了，组件 A 想查数据库，组件 B 想返空列表，组件 C 想写本地文件稍后重发——
平台无法统一定义什么叫"降级"。

**9.8 为什么不做配置中心（动态热更新）？**
配置中心意味着长连接、配置推送、版本比对等一整套沉重机制。而 90% 的基础配置
（连接池大小、超时时间）重启本来就是唯一安全的生效方式。真需要毫秒级热更新的业务开关，
组件自己去连 Redis 轮询即可。

**9.9 为什么多环境不引入 overlay 继承？**
overlay 要求开发者理解"基础层 / 覆盖层 / 合并规则 / 数组合并策略"，
打开一个文件看不到全貌、必须脑补继承链。自包含的完整文件所见即所得，
Git diff 时一目了然，也不会因基础层变更**隐式**影响到生产。

**9.10 为什么前端和后端组件在平台眼中没有区别？**
前端组件同样要监听端口、提供健康检查、被 Ingress 暴露、通过环境变量拿后端地址。
如果发明一种"static"特殊类型，CLI 就会长满 `if type == static` 的分支，架构出现裂痕。

**9.11 为什么平台不做第三方组件安全审查？**
与 VSCode 插件市场、npm、GitHub 一致：你用它说明你信他。开源组件代码全透明，
闭源组件基于商业协议。前置审查要么误报率极高（阻断正常组件），要么漏报率极高
（放过精心伪装的恶意代码）。`blocked` 是成本极低且足够有效的最后安全网。

**9.12 为什么 CLI 不校验 config 值的类型？**
一旦开了校验的口子，就要追问：要不要校验 `enum`？`minimum`？`pattern`？`required`？
JSON Schema 能力极其丰富，CLI 会越来越臃肿。而且组件自治——组件自己决定怎么处理错误配置
（报错、降级、用默认值），不该由平台越俎代庖。**说明书已经给你了，你不看或看错，平台不兜底。**

**9.13 为什么弱依赖缺失时完全不注入环境变量，而不是注入空字符串？**
这是最能体现 BrickKit 哲学的一条。注入空字符串最致命的问题是**制造静默失败**：
开发者忘记判空，写出 `requests.get(f"{ENDPOINT}/healthz")`，空字符串会拼出 `/healthz`，
请求打到**容器自身**的 8080 端口，而自己的 `/healthz` 返回 200——
开发者于是误以为弱依赖是健康的。这种 bug 极难排查。
**宁可让组件在启动时"响亮地崩溃"，也不要让它在运行时"安静地出错"。**

**9.14 为什么 `enabled` 有三种状态而不是 true/false？**
用户希望"关掉一个顶层组件，它专属的依赖链自动跟着关掉，但共享的依赖不被误伤"——
简单的 true/false 表达不了这种级联关系。而"钉住"则保护了权限组件这类无论如何都必须运行的组件。

**9.15 为什么 50 个组件不会失控？**
① **局部性原则**：组件 A 只需要知道自己直接依赖的 B、C、D 的 API 契约，
B 后面连着什么与 A 无关——单个开发者的认知边界永远是"我的直接依赖"（通常 1~3 个）。
② **按需启用**：50 个组件的项目，本地开发时可能只有 4 个容器在跑，其余 46 个"不存在"。
③ **CLI 封装了传递依赖**：一条 `add` 命令，系统就完整了。
④ **`brickkit sync`** 让源码目录也只剩下你当前关注的那 2~3 个。
**这就是"搭积木"的本质——你不需要同时看着所有积木。**

**9.16 为什么不支持 monorepo？**
组件是独立的**发布单元**（各自版本号，monorepo 的 Git tag 怎么打？）、独立的**移动单元**
（`sync` 归档移动的是含 `.git` 的完整仓库目录）、独立的**权限单元**（各自的可见性和发布者）。
注意：同一业务的多部分内容（proto + 后端代码 + 迁移脚本）属于**同一个组件**，不必拆开。

**9.17 为什么 `brickkit sync` 不集成进 `brickkit up`？**
职责分离：`up` 管运行时，`sync` 管源码目录。而且 `up` 根本不依赖源码目录
（Manifest 从 `.brickkit/manifests/` 缓存读）。如果 `up` 自动移动文件，
用户会困惑"我的文件怎么突然 moved 了"。

**9.18 为什么 `add --repo` 不自动 clone 所有源码？**
大多数用户只想**使用**组件，不想**修改**它。一条 `add` 递归下来可能有 5~10 个依赖，
全部 clone 又慢又占盘。**源码是"开发时"需求，不是"安装时"需求。**

**9.19 为什么 clone 后的 Git 权限不归 CLI 管？**
用户可能用 GitHub / GitLab / Gitee / 自建 Gitea，CLI 不可能适配所有平台的 fork API。
fork、remote、分支策略、PR 流程都是 Git 工作流的一部分，与 BrickKit 无关。

**9.20 为什么 `brickkit remove` 自动删除源码目录？**
`remove` 的语义就是"彻底移除"。留着源码会产生"僵尸目录"——不在 `brickkit.yaml` 里
但文件还在。而且归档目录以 `.` 开头（文件管理器默认隐藏），一旦组件被 remove，
`sync` 就再也不认识它了，比普通僵尸目录更难被发现。有未提交的修改？
那应该在 remove 前先 commit——平台提供工具，不替人做决定。

### 9.21 一句话总结

> **平台只做"连接器"和"翻译官"，绝不越界去做"业务逻辑"和"基础设施"已经做好的事情。**

---

## 10. 和用户讨论时的注意事项

这一节是给你（AI）的提醒，都是实际踩过的坑：

| 场景 | 正确做法 |
| --- | --- |
| 用户问"能不能加个注册中心 / 配置中心 / 网关" | 先说明这是被**明确论证并拒绝**的（第 4.1 节 + 第 9 节），再讨论他真正想解决的问题 |
| 写依赖声明 | **只能写精确版本**。`^1.0.0` / `~1.0.0` / `latest` 全部报错 |
| 写健康检查 | `/healthz` 只查本进程。**不要**在里面 ping 数据库或依赖组件 |
| 读弱依赖的环境变量 | 必须 `os.environ.get()` / `System.getenv()`。**绝不能**用 `os.environ["X"]` |
| 组件镜像里没有 `wget` / `curl` | Compose healthcheck 会判它 unhealthy——组件日志写着"已就绪"平台却说不健康，多半是这个 |
| 用户说"前端不用打包成镜像吧" | 平台里没有 static 类型。前端 = nginx 容器，`port: 80` |
| 用户想在一个仓库里放多个组件 | 不支持。一个组件一个 Git 仓库 |
| 用户问 `database` 谁来建 | **数据库由使用者创建一次**（运维侧），表由组件的 migration 建 |
| 两个组件共用一个数据库 | 迁移状态表的主键**必须含组件标识**，否则迁移会互相顶掉 |
| 用 `docker compose logs` 看不到东西 | 漏了 `-p brickkit-<项目名>`，compose 找的是另一个项目 |
| 改了本地源的 `component.yaml` 但 `up` 没反应 | 本地源不吃缓存；确认组件确实来自本地源 |
| `local: true` 后调用方持续 503 | 进程实际监听的端口与 `localPort` 不一致 |
| `local: true` 的组件报 `relation does not exist` | local 组件不生成迁移容器，迁移要自己手动跑一次 |
| 讨论签名 | 发布方需要装 **cosign**；**安装方不需要**（验签用 Go 标准库） |
| 用户想让平台帮忙做安全审查 | 安装即信任。平台只在事后 `blocked` |

---

## 11. 仓库地图与深挖入口

### 11.1 代码结构

```
cmd/brickkit/          CLI 入口
internal/              CLI 实现
  ├── config/            brickkit.yaml 解析与校验
  ├── manifest/          component.yaml 解析与校验
  ├── resolver/          依赖解析、拓扑排序
  ├── cascade/           级联计算：算出这次实际启动谁
  ├── inject/            环境变量注入与资源配额合并
  ├── compose/           docker-compose.yaml 生成
  ├── k8s/               Kubernetes 清单生成
  ├── engine/            docker compose / kubectl 驱动
  ├── source/            安装源：market / git / local
  ├── security/          cosign 签名与标准库验签
  ├── workspace/         组件源码工作区（--repo / sync）
  └── market/            市场客户端
market-server/         组件市场后端（独立 Go module）
design/                14 本设计书 —— 规范性文档，有歧义时以它为准
试用指南/               23 篇动手指南，每一篇都真跑过
tests/components/      10 个真实组件，用来测试平台本身
tests/checklist/       验收清单 → 证明它们的测试
deploy/market/         市场的 compose / kustomize / Helm
```

单元测试**紧挨被测代码**（`internal/**/*_test.go`），不建平行目录。
`tests/` 只放没法放在旁边的：清单、基准、以及当夹具用的组件。

### 11.2 深挖入口（需要细节时抓这些）

所有文档都在同一个仓库里，raw 链接前缀为
`https://raw.githubusercontent.com/brickKit/brickKit/main/`。
完整的机器可读索引见仓库根的 **[`llms.txt`](llms.txt)**。

| 想深挖什么 | 抓哪一份 |
| --- | --- |
| 全站文档索引（带链接） | `llms.txt` |
| 平台理念与总体架构（根文档） | `design/001-平台理念与总体架构.md` |
| `component.yaml` 全部字段与规则 | `design/002-组件规范.md` |
| `brickkit.yaml` 全部字段与规则 | `design/003-项目配置规范.md` |
| CLI 命令、依赖解析引擎、生成逻辑 | `design/004-CLI 设计.md` |
| Docker / K8s 部署、本地调试、迁移 | `design/005-部署与运行规范.md` |
| 数据库 / Redis 等资源的声明与绑定 | `design/006-基础资源规范.md` |
| 市场 API、数据模型、权限、签名 | `design/007-组件市场设计.md` |
| 信任模型、密钥、保留变量保护 | `design/008-安全与治理.md` |
| 手把手写第一个组件 | `design/009-组件开发快速入门.md` |
| 构建、签名、发布到市场 | `design/010-组件发布与上架指南.md` |
| 安装、拼装、调试、更新、回滚 | `design/011-组件安装与拼装指南.md` |
| **所有"为什么"的完整论证** | `design/012-架构设计原理与考量.md` |
| 术语表 / Manifest 与配置完整参考 / 环境变量规范 / 生成产物示例 / 通信实践模板 | `design/附录合集.md` |
| 按角色和场景的阅读路径 | `design/000 阅读指南与文档导航.md` |
| 动手跑一遍（23 篇） | `试用指南/README.md` |
| 某个决策当初为什么这么定（566 条） | `开发进度/决策索引.md` |
| 市场自己怎么部署 | `市场部署与运维指南.md` |
| 市场和项目分别跑在哪 | `部署模式.md` |

**文档权威顺序：** `design/` 下的设计书是规范性文档；`试用指南/` 是可执行的验证；
`开发进度/` 是执行台账。三者冲突时以 `design/` 为准。

---

## 12. 项目状态

| | |
| --- | --- |
| 开发进度 | 计划内的每一步都已完成，延后项也已全部结清 |
| 测试 | 1728 个测试函数，race-clean |
| 试用指南 | 23 篇，全部对着真实 Docker / Kubernetes / 活的市场跑过 |
| 设计书 | 14 本，与实现交叉复核过两轮 |
| 决策记录 | 566 条，每条都带当初的推理 |

**运行要求：** Go 1.22+、Docker 20.10+（Compose V2）。
K8s 相关指南需要 minikube；签名需要 cosign（**仅发布方**，验签用 Go 标准库）。

**许可：** Apache License 2.0。
