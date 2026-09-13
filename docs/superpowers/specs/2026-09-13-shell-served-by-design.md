# 设计书：`servedBy` —— 让"外壳合并部署"不需要平台理解外壳

状态：待双方确认
提出日期：2026-09-13
关联文档：
- `docs/archive/planning/组件合并部署.md`（现行"不支持"立场）
- `docs/archive/design/012-架构设计原理与考量.md` §2.21（"为什么不做"的原始论证）
- `AGENTS.md` §9.21 / `AGENTS.zh.md` 对应条目
- 输入来源：be-assembly-standard 项目反馈 `docs/dev/给brickKit的反馈.md` §六第6条、§七

**修订记录**：2026-09-13 首次提出后，be-assembly-standard 用真实部署数据（12/14 个组件声明了同名配置项 `pgSchema`，两个真实运行的外壳因此全部会撞车）指出 §6 原稿"合并每个成员完整环境变量集合"的规则在实践中会几乎必然触发同名不同值冲突。核实后确认这是 brickKit 自身命名规则的结构性后果（`configSchema` 派生的变量名不带组件 ID 前缀），不是该项目业务领域特有的问题，遂据此收窄 §6 的合并范围，详见该节。

## 0. 背景与边界

be-assembly-standard 是 brickKit 目前唯一的真实生产验证项目，也是本次设计的验收环境——但**本设计只吸收对所有 brickKit 用户都有价值的部分，不为了迁就某一个下游项目而定制平台行为**。经过与该项目反馈的比对分析，明确以下边界：

- **`local: true` 的语义不变、不受本设计影响。** 它是 brickKit 自己定义的、语义单一的字段——本机启动服务用于调试，从未有过"身兼两职"的设计问题。be-assembly-standard 此前把这个字段挪用去做外壳合并部署，是他们自己对字段的误用，不是 brickKit 字段设计有歧义；随本设计落地，他们会改回正确用法。本设计不改动 `local: true` 的任何现有行为、代码路径。
- **`servedBy` 是为一个全新场景（外壳合并部署）引入的全新、独立字段**，只是恰好和 `local: true` 共享"在依赖图里存在但不生成工作负载"这个底层形状，语义和实现都不应互相牵扯。
- 反馈中"合格外壳的九条实现者指南"和 Go/protobuf 全局注册表撞车的坑，全部是外壳实现者自己进程内部的正确性问题，brickKit 平台不需要也不应该理解——这部分只产出**文档**（新的 patterns 指南），不改动平台代码。
- "组件之间不共享代码"这条原则目前靠"每个组件独立仓库、独立编译"的物理结构保证，不是靠平台代码检查强制的；因此 protobuf 生成代码的共享例外，也只是一条文档层面的原则修订，不涉及任何校验逻辑改动。

## 1. 问题陈述

一个组件声明的工作负载，实际上可能由另一个部署单元（"外壳"）提供，而不是由平台生成的独立容器/Pod 提供。今天没有任何字段能表达这个意图：

- 用户如果借用 `local: true` 来模拟"不生成容器"，会在 K8s 下被 `internal/k8s/k8s.go` 的 `localNotSupported` 校验直接拒绝——这条校验对它的本意（"开发者笔记本"）完全正确，但会连带挡住一个本可以正常部署到集群的合并单元。
- `brickkit.yaml` 本身看不出"这个组件已经被合并进某个外壳"这个意图，只能靠人肉写注释维持这个区分。

## 2. 设计目标

1. 新增 `servedBy: <shell-component-id>` 字段，让一个组件声明"我的工作负载由另一个组件条目提供"。
2. 平台跳过为该组件生成工作负载（容器/Deployment）和迁移 Job，但照常为依赖它的其它组件计算正确的 `*_ENDPOINT`——地址指向外壳的实际位置 + 该组件自己声明的端口。
3. Docker 和 K8s 两个部署目标下行为完全对称，不再需要 `local: true` 时代那种"K8s 完全不支持"的落差。
4. 平台自始至终不需要理解外壳内部是什么语言、几个进程、怎么合并——这是与现行"不做外壳"决策（012 §2.21）保持一致的硬约束，本设计只是把"平台需要知道地址应该指向哪里"这一件更基础的事单独拆出来做。

## 3. 架构方案：共享预处理层

两个渲染器（`internal/compose`、`internal/k8s`）目前对结构相似的问题（`local: true`）各自独立实现分支逻辑，互不共享。但 `servedBy` 明确要求"Docker 和 K8s 逻辑一致"，如果照抄现有先例各写一份校验+合并逻辑，会产生实质性的重复代码，而不是简单分支，两边悄悄跑偏的风险很高。

**采用新增共享包 `internal/shell`**，在两个渲染器各自生成产物之前调用一次，把所有目标无关的工作一次做完：

```go
package shell

type Group struct {
    Shell   resolver.Ref
    Members []Member
}

type Member struct {
    Ref  resolver.Ref
    Port int              // 该组件自己 component.yaml 声明的 deployment.port
    Env  []inject.Var      // 该组件"如果独立部署会拿到"的 *_ENDPOINT 类变量（仅此一类，见 §6）
}

func Resolve(cfg *config.Config, graph *resolver.Graph, states cascade.Result) ([]Group, []*clierr.Error)
```

`internal/compose`、`internal/k8s` 各自消费 `Resolve` 的结果做纯机械的收尾（网络别名/Service 生成、把合并后的 env 写进外壳容器），校验和合并逻辑只有一份。

`cascade` 和 `inject.Build` 的既有主循环**不需要感知 `servedBy`**——和 `local: true` 一样，cascade 只回答"这东西该不该跑"，不回答"跑起来要几个容器"；这个边界维持不变。

## 4. Schema 变更

`internal/config/config.go`，`Component` 结构体新增：

```go
ServedBy string `yaml:"servedBy,omitempty"`
```

`yamlcheck.Walk` 已经统一覆盖 `brickkit.yaml` 的 `components[]` 元素，新字段自动获得拼写保护，无需改动 `yamlcheck`。

## 5. 校验规则（`internal/shell.Resolve`）

对每个运行中、且 `ServedBy != ""` 的组件：

1. **存在性**：`servedBy` 指向的 ID 必须存在于当前项目的 `components:` 里，且必须处于运行态（cascade 判定为 running）。组件在跑但它指向的外壳未运行/被禁用 → 报错，不能沉默失败。
2. **禁止自指**：不能指向自己。
3. **禁止链式嵌套**：外壳本身不能再有非空的 `servedBy`——一个外壳不能被另一个外壳收编。
4. **端口不冲突**：同一个外壳下，所有成员的端口（含外壳自己的 `deployment.port`、`healthCheck.port`）必须互不冲突——它们最终都要在同一个容器/Pod 上监听。

## 6. 环境变量合并（范围收窄至 `*_ENDPOINT`）

**合并范围只包含 `*_ENDPOINT` 后缀的变量**（组件自己的主端口 + `extraPorts`），其它一律不进入平台的合并逻辑——这是首版设计里"合并完整环境变量集合"的修正，原因见下。

**为什么收窄**：`*_ENDPOINT` 的变量名由依赖组件的 ID 派生（AGENTS.md §5.1/§9.23），这是它能被安全合并的前提——同一个名字在整个系统里理应指向同一个地址，值不一致本身就是错误，值得报错。但组件自己 `configSchema` 派生的配置变量、资源连接变量（`DATABASE_*`/`REDIS_*` 等）都**不带组件 ID 前缀**，两个独立开发的组件完全可能凑巧取同一个配置项名字（`pgSchema`、`logLevel`、`timeout` 这类通用名字非常常见）——独立部署时因为各自在自己的容器里从不冲突，但合并进同一个外壳、共享同一份 OS 环境后就会大概率撞车，这是 brickKit 命名规则本身的结构性后果，不是任何单个项目的业务巧合。`COMPONENT_ID`/`COMPONENT_VERSION` 更极端：这两个变量名固定不变，但只要外壳有 2 个以上成员，值必然互不相同——不是"可能冲突"，是每次都 100% 冲突，因此**无条件排除**在合并范围之外，没有例外。

**重构 `internal/inject`**：把 `Build` 主循环内"单个组件的依赖端点怎么算"这段逻辑（即现有的 `addEndpoints`）拆成独立可复用函数（如 `BuildEndpoints(ref, ...) []Var`），只返回 `*_ENDPOINT` 类变量。`internal/shell.Resolve` 对每个 member 调用它，取得它"如果独立部署"本该拿到的端点变量集合，再合并进外壳。

**合并规则**：以 name→value 增量构建一个 map，先放外壳自己的端点变量，再逐个 member 合并——同名同值（例如两个成员恰好依赖同一个第三方组件）跳过，不算冲突；同名不同值，报错并指出是哪两个组件、哪个变量名冲突。这条规则同时兜住一个合法但需要拒绝的场景：两个 member 各自依赖同一个组件的**不同精确版本**（独立部署时互不冲突的合法钻石依赖），合并进同一个外壳后地址值真的会冲突，此时应该报错拒绝生成，而不是悄悄选一个版本的地址。

**组件自己的配置、资源连接怎么办**：完全留给外壳实现者自己解决——这正是九条实现者指南里"进程级全局状态必须只初始化一次、模块间配置不能互相覆盖"这条原则的具体落地场景，不需要平台提供通用机制（见 §10）。

`labels`（`brickkit.yaml` 组件级）的合并规则不变：同名同值跳过，同名不同值报错——这是可选的部署元数据透传，没有证据表明存在类似 `*_ENDPOINT`/配置变量那样的结构性冲突风险，维持原设计。

## 7. Docker 渲染（`internal/compose`）

- 外壳组件的 compose service 照常生成，额外做两件事：
  - 挂上每个 member 的版本化服务名作为**网络别名**（Docker Compose 原生支持一个容器挂多个别名）。这是全新代码路径，不涉及 `local: true` 现有的 `extra_hosts`/`host-gateway` 技巧，因此不会撞上旧文档记录的"别名和 extra_hosts 不能共存"的限制。
  - 把 `internal/shell.Resolve` 算出的合并端点变量（仅 `*_ENDPOINT` 类，见 §6）写进这一个 service 的 `environment:`。
- 每个 member **不生成**独立的 compose service。
- member 若声明了 `expose`/`exposePort`（host 端口映射），可以正常支持：只是在外壳这一个 service 的 `ports:` 列表里再加一条映射，指向外壳容器上 member 自己的端口——这是对旧方案（"expose 会失效"）的实质改进，不是新增复杂度。
- member 若声明了 `migration`，跳过生成迁移容器，改为发一条警告：**外壳作者需要确保自己的启动逻辑覆盖了这个组件的迁移**（措辞与 `local: true` 现有的"请手动执行"警告区分，因为责任主体不同——一个是外壳作者的编排责任，一个是调试者本人的手动操作）。
- member 若声明了 `healthCheck`，发一条警告：**这个字段不会生效，健康检查完全是外壳实现者自己的责任，平台不做任何聚合、也不替外壳生成任何健康检查逻辑**。

## 8. K8s 渲染（`internal/k8s`）

- `localNotSupported` 校验完全不变，`local: true` 在 K8s 下依旧照常拒绝。
- 新增：遇到 `ServedBy != ""` 的组件，跳过生成它的 Deployment/Job，改为生成一个**指向外壳 Pod 的 Service**（selector = 外壳的 Pod 标签，`targetPort` = 该 member 自己声明的端口）。
- 合并后的端点变量（仅 `*_ENDPOINT` 类，见 §6）写进外壳的 Deployment。
- 孤儿清理无需额外处理：member 的 Service 携带与其它资源相同的项目标签，`internal/engine/kubectl.go` 现有的 `prune()`（P38）按标签选择器 + 本次生成的 `Desired` 集合做差集，`servedBy` 关系改变或撤销后，多余的 Service 会被现有机制自然识别为孤儿清理掉，不需要为合并部署单独加一层清理逻辑。

## 9. 与"六、第 6 条"protobuf 坑的关系（文档，非代码）

be-assembly-standard 反馈的 Go/protobuf 全局注册表撞车问题，本质是"进程级全局唯一注册表 + 同一份生成代码被独立复制多份"的通用性质，不是 brickKit 需要处理的问题——但第 6 节里"依赖地址（`*_ENDPOINT`）由平台直接注入外壳容器的 OS 环境，而非指望外壳内部再手动 relay 一遍"这条设计，恰好让九条指南第 6 条提到的"地址发现机制在合并之后失效"这类坑，对遵循这个约定的外壳实现者不会存在，值得写进指南作为佐证。

**处理方式**：不修改平台代码或校验逻辑。作为文档产出的一部分（见第 10 节），在原则文档里补充一条经过泛化、语言无关的说明——"组件之间不共享代码"这条原则允许一个例外：纯生成物契约包（只含消息类型和客户端桩代码，不含业务逻辑）可以作为独立可发布单元被多个组件直接引用，因为它不是手写业务逻辑，共享它不违反"组件自治"的精神。

## 10. 文档产出（新增，随本功能一起交付，不计入被搁置的"阶段二"）

不生成一个可用的 `servedBy` 字段而不解释"什么是合格的外壳"，这个字段就没法被安全使用——所以这份指南必须随功能代码一起交付，而不是等阶段二的文档重构再补：

- `docs/en/patterns/shell-implementers-guide.md` + `docs/zh/patterns/` 镜像：把反馈文档九条实现者指南改写成语言/框架无关的通用指南（去掉 be-assembly-standard 专属的 FastAPI/Go errgroup/PostgreSQL 具体细节，保留"性质是什么、违反了什么后果、需要满足什么不变量"的结构，具体实现留给读者按自己技术栈判断）。**明确写清楚平台的合并边界**：只有 `*_ENDPOINT` 类地址变量会被平台直接写进外壳容器的共享 OS 环境，组件自己的 `configSchema` 配置、资源连接信息、`COMPONENT_ID`/`COMPONENT_VERSION` 身份变量一律不会——每个被收编模块拿到自己独有的配置/身份，完全是外壳实现者自己的责任（第 5 条"进程级全局状态只初始化一次"的具体应用场景）。
- `AGENTS.md` §9.21 / `AGENTS.zh.md` 对应条目：更新为反映 `servedBy` 已经落地，而不是"平台完全不做，只提供 DIY 路径"。
- `docs/archive/planning/组件合并部署.md`：保持归档不动（历史记录冻结原则），新文档不回填改写旧档。

## 11. 测试策略

- `internal/shell`：表驱动单元测试覆盖全部校验规则（目标不存在、自指、链式嵌套、端口冲突、`*_ENDPOINT` 合并同值跳过/异值报错，含"两个 member 依赖同一组件不同精确版本"这个具体场景）；并验证 `configSchema` 派生变量、资源连接变量、`COMPONENT_ID`/`COMPONENT_VERSION` 不会出现在合并结果里，即使它们同名同值也不会。
- `internal/inject`：`BuildEndpoints` 拆分后的行为与原 `Build` 循环逐组件产出的端点变量一致（回归）。
- `internal/compose`：网络别名生成、合并 env 写入、`expose` 端口映射、migration/healthCheck 警告。
- `internal/k8s`：member Service 生成、合并 env 写入 Deployment、`local: true` 拒绝行为不变（回归）、孤儿清理沿用现有 P38 测试模式验证。

## 12. 明确不做的事

- 不提供 `brickkit up --consolidated` 或任何外壳脚手架/模板。
- 不校验外壳镜像内部实际编译的组件版本与 `brickkit.yaml` 声明的版本是否一致（镜像对平台永远是不透明引用，这是合并部署固有的、诚实承认的限制）。
- 不为 `servedBy` 链路引入任何进程管理器、健康检查聚合、日志切分之类的平台机制——这些始终是外壳实现者自己的判断，与现行 012 §2.21 的论证保持一致。
