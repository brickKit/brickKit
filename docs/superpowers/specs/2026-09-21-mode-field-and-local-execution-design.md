# `mode` 字段与本地执行模式（`local`/`debug`）设计

> 背景：`enabled`（三态：未写/true/false）与 `local: true`（本地调试，用户自己启动进程）今天是两个独立字段。这次把它们合并成一个枚举字段 `mode`，并新增一种平台自己管理的本地执行方式（`mode: local`，brickkit 自己探测启动命令、自己拉起进程、自己收尾），跟今天完全由用户自己管的 `local: true`（改名 `mode: debug`）区分开。
>
> 本文档覆盖字段模型、校验规则、进程监管、启动命令探测、端口覆盖、配套机制六个方面的设计决策与理由。实现拆成四份独立计划（见各计划文件的 Spec 引用），本文档是四份计划共同的设计依据。
>
> **范围排除**：`docs/{en,zh}/07-patterns/05-deployment-selection-guide.md` 本次不改——另一个项目会先做一遍完整实操测试，根据反馈再更新这份文档，避免先写后被推翻。

## 1. 字段模型

新增单一字段 `mode`，取代 `enabled` + `local`：

```yaml
components:
  - id: people/basic
    version: 1.0.0
    mode: debug     # 不写 = 跟随上层（原 enabled 语义），走容器
                    # enabled = 强制运行，走容器（原 enabled: true）
                    # disable = 强制不运行（原 enabled: false）
                    # local   = 裸进程，brickkit 自动启动（新能力）
                    # debug   = 裸进程，用户自己启动（原 local: true）
```

- **k8s 阵营**：只认 不写(跟随上层)/`enabled`/`disable` 三态，`local`/`debug` 校验阶段直接拒绝——物理约束不变：集群里的 Pod 够不到开发者本机的进程，跟今天 `local: true` 撞 `deploy.target: k8s` 报错是同一条规则。
- **docker 阵营**：上面三态 + `local` + `debug`，五态全部合法。

**合并理由**：分开写会产生 `disable + local`、`disable + debug` 这类无意义组合，只能靠事后校验去挡；合并成一个枚举后，这些组合从设计上就写不出来——这是"显式优于隐式"更彻底的做法，不是简单的字段合并偷懒。

**连带规则，都是现有规则的泛化**：

- "两个冲突意图报错"（原来只针对 `enabled: true`）泛化到所有"肯定要跑"的值——`enabled`/`local`/`debug` 撞上必需依赖是 `disable`，同样报错。
- `mode: local`/`debug` 在 cascade 计算里的角色等同于原来的 `enabled: true`：即便这个组件不是顶层、也没有别的东西依赖它，写了 `local`/`debug` 也强制运行。
- `servedBy` 与 `local`/`debug` 互斥的规则原样保留。
- 组件没有本地源码时写 `mode: local`/`debug`，生成阶段报错——跟今天 `local: true` 隐含的前提一致，只是需要显式校验。

### 1.1 `Mode` 必须是唯一真相，不能是第二份包装

`internal/config/config.go` 里 `Component.Enabled *bool` 的注释记录过一次历史教训：早年这里包过一层 `EnabledState` 枚举，后来删掉了——原因是它变成了"第二份真相"（改判定规则要同时动两处，而测试只会覆盖到其中一处），且生产代码从未真正调用过它。这次引入 `Mode` 不是重蹈覆辙：**`Mode` 要取代 `Enabled`/`Local`/`LocalPort` 三个字段中的 `Enabled`/`Local` 两个（`LocalPort` 保留，见 §5），不是在它们外面再包一层。** 落地后，`internal/cascade`、`internal/compose`、`internal/k8s`、`internal/cli/{graph,lifecycle,restore,status,sync,up}.go`、`internal/config/validate.go`、`internal/logging` 这些消费点必须只读 `Mode`，不能留任何一处仍读旧字段。验收标准：`grep -rn "\.Enabled\b\|\.Local\b" internal/` 除了 `internal/config` 内定义/解析 `Mode` 本身之外，不应再有命中。

### 1.2 `mode: local`/`debug` 撞 k8s 的报错位置：从生成阶段挪到解析阶段

今天 `local: true` 撞 `deploy.target: k8s` 是在 K8s **生成阶段**报错（`internal/k8s/k8s.go` 的 `localNotSupported`），不是在 `internal/config/validate.go` 的静态校验里——纯粹是历史实现顺序，不是有意选择。这次借字段重构顺手挪到 `validate.go`：这条检查只需要 `Mode` 和 `deploy.target`，不需要依赖图，`validateComponentPorts`/`validateServedBy` 已经在解析期访问 `c.Deploy.Target` 做同类组合校验。挪过去之后，`brickkit lint`（纯离线）阶段就能拿到这个错误，不用等到真正生成部署文件，反馈更快。

## 2. 字段归属

- **`runCommand`/`debugCommand`/`language`**：落在 `component.yaml`，新增顶层块 `local:`，跟 `migration`/`healthCheck`/`deployment` 平级：
  ```yaml
  local:
    language: go                          # 可选，多数情况能自动探测
    runCommand: ["go", "run", "."]        # 可选，探测失败/歧义时的手动覆盖
    debugCommand: ["dlv", "exec", "..."]  # 可选，仅在适配器默认产出的调试方式不适用时才需要
  ```
  理由：这些是组件自身工具链的事实，不随装它的项目变化，跟 `migration.command` 是同一类东西。
- **`mode`**：落在 `brickkit.yaml`，是项目对组件的部署意图，跟今天 `local: true`/`enabled` 位置一致。

## 3. 本地进程监管（`mode: local` 专属，不适用于 `debug`）

**范围澄清**：`mode: debug` 从头到尾都是用户自己在 IDE/终端里启动进程，brickkit 没有一个自己启动、需要自己收尾的子进程，跟今天的 `local: true` 一样——`up` 该生成文件、该退出还是退出，不会因为存在 `debug` 组件而切换成前台模式。以下机制全部只对 `mode: local` 成立。

- **只在项目里存在 `mode: local` 组件时，`up` 才切换成前台监管模式**；docker/k8s 部分不受影响，该退出还是退出（容器交给 dockerd/kubelet 管）。
- **不需要跨会话存活**：进程只在开着这个终端会话期间存在，`Ctrl+C`/关终端/关电脑统一收尾——跟今天 `debug` 要求开着 IDE 才算活着是类似的使用习惯，只是这次是 brickkit 自己的前台会话在扮演"IDE"这个角色。
- **技术方案：内置在 brickkit 自己（Go），不依赖外部工具/系统服务**（不用 systemd/launchd——这两个只有"要跨会话存活"才必须用，且三平台没有统一方案）：
  - macOS/Linux：子进程独立进程组（`setpgid`），退出时对整个组发信号。
  - Windows：Job Object（`CreateJobObject`/`AssignProcessToJobObject`），关闭 Job 时整棵进程树一起收掉。
  - 多个 `local` 进程同时跑，日志转发到同一个终端时按组件名做行前缀（参考 foreman/overmind 的做法）。
- **崩溃处理**：不自动重启，前台直接把 stdout/stderr 转发到终端，子进程退出码本身就是崩溃信号。
  - **运行期一个 `local` 进程崩了（不是我们叫它停的、退出码非零，含被 OOM killer 之类的外部信号杀死），整个会话随即收尾**：其余 `local` 进程被体面地停掉；docker/k8s 容器不受影响（`up` 本来就不管容器的生死）。自己干净退出（退出码 0）不算崩溃，也不连累别人。启动阶段进程直接崩了属于 §6.4 的硬失败，走同一套收尾。
  - **结束前要告诉用户什么崩了，让他能立刻排查——只打印崩溃的那个进程，只打一次**：收尾结束之后，在**最后一屏**打印崩溃的进程——哪个组件、退出码或信号、跑了多久，以及它**最后若干行输出**；被叫停的、干净退出的都不打印。不做"崩溃时立刻打一行"：收尾很快，而且收尾时其余进程的关闭输出会刷屏、把崩溃现场冲出屏幕之外，汇总放在最后才一定看得见。
  - **输出多少行用户可调**：默认 20 行。给用户的旋钮建议是 `brickkit up --crash-lines N`（`0` = 只打印崩溃信息、不打印输出行）——它是每个开发者当下的偏好，要"随时调整"，所以是命令行 flag，不是会进版本库的 `brickkit.yaml` 字段。这是新增的 CLI 表面，实现前先跟用户确认名字与位置；想要个人的长期偏好，可以再加环境变量（参照 `BRICKKIT_LANG`）。
  - 这条是 2026-09-22 用户拍板的（此前的实现草案是"崩了其余的不停"）。
- **docker ↔ local 模式切换**：
  - docker → local：复用现成的孤儿清理机制（Docker 侧 `--remove-orphans`，K8s 侧 `orphansIn` 比对期望态删除多余资源）。
  - local → docker：不需要额外处理，local 进程不跨会话存活。
- **并发保护**：项目目录级锁文件，持有者是活着的前台监管进程，进程死锁自动释放（生命周期严格绑定在持有它的这段前台会话上，不是 PID 文件那种容易陈旧的机制）。
- **`status`/`down` 跨终端可见性**：复用同一把锁文件，不建独立的状态文件/IPC 机制。锁存在就打印一句指向信息（"这个项目有个 local 会话在跑，PID xxx，去那边看/Ctrl+C"），底线是不能在有会话时假装什么都没有。
- **local 和 debug 真正共享的东西，只有网络可达性**：两者共用同一套 `extra_hosts` → `host-gateway` 寻址机制。本节其余内容（前台会话/锁/崩溃检测/启动顺序）都是 `local` 独有。
- **多个本地进程的启动顺序（仅 `local`）**：不做强制等待健康的门槛，但拓扑序已经为容器算好了，直接复用同一份顺序去启动，只是不卡在每一步等健康检查——零成本优化，不是启动顺序保证。

## 4. 启动命令怎么来（`mode: local` 专属）

- **`runCommand`/`debugCommand` 不是并列的两套机制**：`local` 模式的动机就是"要盯着它调试"，每个语言适配器默认直接产出"可调试挂载"的启动方式，不需要额外开关。两个字段都只作为自动探测失败/歧义时的手动覆盖出口。
- **机制**：读取各语言生态自己的标准约定文件（`package.json`/`go.mod`/`Cargo.toml`/`*.csproj`/`pom.xml`+`build.gradle`/`manage.py` 等），是 Nixpacks/Cloud Native Buildpacks 同类技术，不是猜测。
- **可靠性分级**：
  - 高（接近确定性）：Go（`go run .`）、Rust（`cargo run`）、.NET（`dotnet run`）。
  - 高：Node（读 `package.json` 的 `scripts`，锁文件区分 npm/yarn/pnpm）。
  - 较高但多一步：Java（区分 Maven/Gradle，扫依赖是否有 `spring-boot-starter`，优先用 wrapper 脚本）。
  - 低，刻意收窄：Python/Ruby，只吃 `manage.py`/`bin/rails` 这类强信号，其余情况直接走兜底——猜错了还能跑起来比直接报错更难排查。
- **语言字段基本免填**：标记文件本身能自动识别语言，`language` 只是少数歧义场景的可选手动覆盖。
- **调试挂载**：只跟语言/运行时相关，跟框架无关。
  - Java/Node：环境变量注入（`JAVA_TOOL_OPTIONS`/`NODE_OPTIONS`），不改用户命令本身。
  - Go：不改启动方式，正常跑起来后 `dlv attach <pid>`。
  - Python：需要包一层命令（`python -m debugpy --listen ...`）。
  - 启动期断点不会丢：`debugpy --wait-for-client`、JDWP `suspend=y`、dlv 默认等客户端连上再继续。
- **健康检查：不上完整 healthCheck 机制**（不需要启动顺序保证）。只保留一个轻量的一次性端口监听检测，复用 `deployment.port`，专门发现"命令跑起来了但没监听正确端口"这一种风险。**检测失败是终端里的醒目警告，不是自动杀掉进程**——慢启动是正常情况，不该被误判。
- **迁移**：`local`/`debug` 都不生成迁移容器，用户自己手动跑一次迁移。

## 5. 端口覆盖机制

- **不是"任意字段可被覆盖"的通用机制**，只在端口相关（`port`/`extraPorts`）开口子，直接复用已有的 `exposePort` 模式（组件声明端口 → 进入共享端口空间可能冲突 → 项目层面覆盖，`exposePort` 解决的是"暴露给宿主机"，这里是"本地裸进程共享同一台机器"，形状相同），冲突校验也复用同一逻辑。
- **`debug` 模式**：端口覆盖是纯路由信息（`LocalPort` 保留，不合并进 `mode`）——用户自己的进程听哪个端口是他自己决定的，`localPort` 只是告诉 brickkit 该往哪路由。
- **`local` 模式**：端口覆盖要真正通过环境变量传给被启动的进程，能不能生效取决于组件代码有没有遵守约定（组件自治边界）。因为 brickkit 自己控制启动，`local` 模式默认自动分配空闲端口，只有想固定端口时才手动覆盖——这是 `debug` 模式做不到的简化。
- **排除通用覆盖的字段**：`image`（破坏精确版本保证）、`migration.command`（数据正确性）、`healthCheck`（没有冲突驱动的覆盖需求）、`resources`/`labels`（已有现成覆盖链路）。

## 6. 保留变量、密钥、graph/status、端口检测

1. **`PORT` 进保留变量精确匹配名单**（`internal/inject/reserved.go` 的 `reservedExact`）。用裸名字（不加前缀）能让相当一部分组件不需要作者额外配合就直接生效（很多语言/框架社区本来就读 `PORT` 决定监听端口），但正因为通用必须进保留名单，避免撞组件自己 `configSchema` 里的同名 key。同一套规则被市场侧（`market-server/internal/validator/reserved.go`）镜像维护，加 `PORT` 时要同步。
2. **密钥解析：local 模式比 debug 模式更安全，不需要新解析逻辑。** debug 模式要把解析好的值（含密钥）写进明文的 `local-debug.env`（给 IDE `source`）；local 模式是 brickkit 自己拉子进程，直接用 `exec.Cmd.Env` 在内存里传，全程不落盘。解析函数复用现有的 `internal/cli/up_k8s.go` 的 `envLookup`（先查进程环境、再查 `.env`）。为了排查方便，local 模式也生成一份同款可读 env 文件，定位成纯展示快照，不是启动时真正依赖的东西。
3. **`graph`/`status` 展示**：`graph` 给 local/debug 节点都加标签（纯展示），复用 `internal/cli/graph.go` 已有的 `classDef`/`class` 机制新增一个样式。`status` 表格只展示 docker/k8s 真实运行态；锁文件存在时加一行提示，只针对 `local`。
4. **端口检测失败，两种情况分别处理**：进程直接崩了/根本没起来（退出码非零、立刻退出）——硬失败，触发兜底提示（报错、点名组件、请手动提供 `runCommand`）；进程在跑但迟迟没监听到期望端口——软警告，不杀进程，交给用户自己判断。
5. **Windows 下每个语言适配器的命令差异**（`mvnw` vs `mvnw.cmd`、`.venv/bin/python` vs `.venv\Scripts\python.exe`）——每个适配器自带一张 `{标记文件, Unix 命令, Windows 命令}` 对照表，纯数据性质，留到实现具体语言适配器时再填。**验证范围**：只在 Linux 上实际验证，Windows/macOS 只要理论上站得住就行，不强求真机跑通。
6. **`expose`/`exposePort`/`hostname`/`replicas`/`resources`/`serviceAccountName` 在 local/debug 模式下的处理**：警告、不报错、不静默忽略，复用 `internal/compose/local.go` 已有的 `localExposeWarnings`/`localLabelWarnings`（今天已经是 `local: true` 组件的模板，迁移到 `mode: debug` 时把判断条件从 `c.Local` 改成 `c.Mode == ModeDebug` 即可）、`internal/compose/servedby.go` 的 `servedUnsupportedFieldWarnings`。
   - **判断标准**：一个字段被忽略之后，实际跑起来的东西变不变——不变（惰性字段）→ 警告；会变（忽略等于悄悄换了一种行为）→ 保持硬错误。`expose`/`replicas` 在 local/debug 模式下属于惰性（没有容器/Pod）；`local`/`debug` 撞 `deploy.target: k8s` 不属于这条（忽略它等于把组件悄悄按普通容器部署），继续硬错误。
   - **更深一层的区分**：`servedBy`、local/debug 这类字段该 warn 而不是 reject，是因为它们描述"经常要来回切换的临时状态"，切换应该低摩擦、可逆。brickkit.yaml 对"字段不适用"的默认处理方式是直接拒绝，warn 是这几个场景的特例。
   - **`exposePort`+k8s 不是漏洞，是文档过期了**：`internal/cli/k8s_cluster.go` 的 `warnTargetOnlyFields`/`dockerOnlyFields` 已经在 `up`（含 `--dry-run`）里对这个组合发警告，`docs/en/06-architecture/08-brickkit-yaml-reference.md:89` 那句"目前没有任何东西会挡它"是过期文案，这次顺手改成"是警告，不是静默失效"。

## 7. 配套工作范围（规模评估，非设计问题）

- **测试**：`mode: local` 的真实启动验证走 `scripts/check-guides.sh` + `tests/guides/清单.tsv`（不在 CI 里，`make check-guides` 手动跑），照 `core`/`docker`/`k8s` 三层的模式新增一层 `local`。`mode: debug` 测的是 `extra_hosts`/`local-debug.env` 生成，留在现有表格测试层（`internal/compose` 等）。需要新增测试固件组件（延用 `demo-hello`/`demo-caller` 这类最简单组件的路子）。
- **i18n**：新增文案走 `internal/msgid` + 两份 `catalog_*.go`，`tests/i18nguard` 会拦住写死字符串。
- **错误码**：不新增 `Code` 常量，复用 `CodeConfigInvalid`；每条新增文案在 `docs/{en,zh}/06-architecture/10-error-codes.md` 补一行（`tests/docfields` 会拦住漏写）。
- **JSON Schema**：`make generate-schemas` 重新生成 `schemas/*.json`，`check-schemas` 校验不漂移。
- **文档**：`AGENTS.md`/`AGENTS.zh.md`、`docs/{en,zh}/06-architecture/{04-environment-variables,07-component-yaml-reference,08-brickkit-yaml-reference}.md`、`08-troubleshooting.md`、`10-error-codes.md`，以及仓库自己的示例 yaml/测试固件里的旧字段全部换成新字段。**`07-patterns/05-deployment-selection-guide.md` 本次不改**（见文档开头范围排除）。`make lint` 的 `check-docs-bilingual` 会拦住中英文不同步。
- **不做迁移兼容**：`enabled`/`local: true`/`localPort` 直接换成新字段，没有过渡期、没有双写兼容层——项目目前只有一个人用，没有外部已用旧字段的包袱。

## 8. 已回收、不再做的方向

- **docker/k8s 混搭部署、地址手动接**：不做。`local`/`debug` 模式已经覆盖了"部分组件想用不同方式跑、其余保持稳定"这个核心诉求的绝大部分场景，不需要为此打破"地址格式在所有环境下一致"这条 brickkit 最核心的保证。
- **CLI 变成长期监管所有本地进程的后台服务**：不需要，前台会话内监管已经足够，不违反"CLI 跑完即退出"这条原则本身（只有存在 `mode: local` 组件时才临时切换）。
