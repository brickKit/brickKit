# 计划：`target: podman` 生成 Quadlet 单元

意图文档：`docs/superpowers/specs/2026-11-02-podman-quadlet-design.md`（下称“设计”，§ 指它的节）。来源 FRR-025。

## 全局约束

- 项目原则照守：失败要大声（版本不够、单元起不来、迁移失败都当场报）；所见即所跑（生成的单元在 `.brickkit/generated/quadlet/`
  看得到，装进 systemd 的是它的原样副本）；显式（`podman.units` 不写就是 compose，绝不按环境猜）。
- compose 路的行为零变化：Task 3 的重构之后，所有现有测试与 compose 生成结果逐字节不变。
- 新消息同时写进 `internal/i18n/locales/en.yaml` 与 `zh.yaml`，`make generate-msgid`；文档中英文两份对齐（往好的一边）。
- 每个任务做完：`make lint`（全量，含覆盖率门槛）通过、提交；碰行为的任务在 `tests/regression/` 或 `tests/checklist/` 加条目指向证明它的测试。
- 不懂就测（用户级指令）：Task 1 先把设计里没实测过的 systemd / Quadlet 行为测掉，结论与设计不同就先改设计、再写代码。

## 任务

### Task 1　Quadlet 行为的最小实验

- **要做到什么**：设计里依赖、但还没实测的行为都有本机结论（Podman 5.7.0，无根，用户 systemd）。
- **依据**：设计 §5.2、§5.3、§5.4、§5.1、§8。
- **依赖**：无。
- **要测的**：
  1. 迁移 oneshot（不留存）+ 组件 `Requires=`/`After=`：迁移跑完组件不被停；`systemctl restart 组件` 会先再跑迁移；迁移以非 0 退出时组件不启动，`systemctl` 的报错长什么样。
  2. `Notify=healthy`：启动作业等到健康才完成；另一个单元 `After=` 它时等到它健康。没有健康检查的容器加不加它的区别。
  3. `Wants=`/`After=` 的依赖被 `systemctl restart` 时，依赖方不被连带重启。
  4. `NetworkAlias=`：别的容器按别名找得到；模板实例 `x@1`、`x@2` 用同一个别名时 DNS 返回两个地址。
  5. `HealthOnFailure=kill` + `Restart=always`：健康检查失败后容器被杀、被 systemd 拉起。
  6. `Secret=…,type=env,target=…` 经 Quadlet 交付多行值；`Environment=` 里 `%`、`$`、引号、反斜杠、空格的转义规则。
  7. `.network` 单元：`down` 时怎样删网络（`NetworkDeleteOnStop=` 或 `podman network rm`）；部署文件 `network:` 指向已有网络时 `Network=<名字>`。
  8. 有根 / 无根的安装目录与子目录被 `daemon-reload` 扫到；`[Install] WantedBy=` 对 Quadlet 单元生效。
  9. 最低 Podman 版本：按 release notes 查 `Notify=healthy`、`NetworkAlias=`、模板单元、`HealthOnFailure=`、子目录扫描各自起于哪版。
- **验收**：每项有一条结论与原始输出，存在会话 scratchpad；设计 §5、§8 按结论改正（不同之处写明原因）并提交。脚本离开本项目能跑的，留着给 Task 7 复用。
- **回退**：实验只建临时单元与容器，结束时删掉、`daemon-reload`；不碰用户已有的单元。

### Task 2　部署文件的 `podman:` 块

- **要做到什么**：部署文件能写 `podman: { units: quadlet }`；写错、写在别的 target 下、与 `mode: local` / `mode: debug` / `focus` 同用时，装载阶段就报错并指出字段。
- **依据**：设计 §2、§3、§10。
- **依赖**：无。
- **验收**：`deployfile` 单元测试覆盖合法值、非法值、别的 target 下写、与三种本机进程写法冲突（报错走与 k8s 同一组检查，改用 `LongRunning()`）；`lint` 同样报出；`schemas/` 重新生成；字段参考页与 deploy.yaml schema 页中英文更新。`make lint`。
- **可能动到的文件**：`internal/deployfile/{deployfile,validate}.go`、`internal/cascade/`（若拒绝规则在那）、`internal/project/check.go`、`schemas/`、`docs/{en,zh}/01-three-layers/09-field-reference.md`、`docs/{en,zh}/11-reference/03-deploy-yaml-schema.md`、i18n。
- **共用接口**：`deployfile.File.Podman`、`(*File).QuadletUnits() bool`、`(*File).LongRunning() bool`。

### Task 3　抽出单机容器目标共用的计划

- **要做到什么**：compose 与 Quadlet 共用一份“要起哪些容器、迁移怎么串、外壳挂哪些别名、环境变量去哪、端口与副本检查、健康检查命令、配额”的计划；compose 独有的本机进程部分留在 compose。
- **依据**：设计 §7。
- **依赖**：无（与 Task 1、2 并行）。
- **验收**：compose 包的全部测试不改期望就通过；对 `internal/cli/testdata/` 与 `tests/components/` 的项目跑 `up --dry-run`，生成的 `compose.yaml` 与重构前逐字节相同（重构前先存一份对照）；`make test-all`。
- **可能动到的文件**：新包（名字实现时定，如 `internal/container/`）、`internal/compose/*.go`。
- **共用接口**：计划类型及其构造函数（组件、迁移、迁移链、外壳别名、等待关系、健康检查、配额、端口），设计 §4 的对应表就是它要提供的东西。

### Task 4　Quadlet 渲染器

- **要做到什么**：给定共用计划，得到每个单元文件的内容、要建的 Podman secret 列表、警告；纯函数，不碰磁盘与命令。
- **依据**：设计 §4、§5.1–§5.5。
- **依赖**：Task 1（行为结论）、Task 3（共用计划）。
- **验收**：单元测试按组合对照期望输出：独立组件、外壳与成员别名、成员迁移、迁移链（多版本）、副本（模板与实例）、`network:`、`expose`、`mount: file`、健康检查两种（有无 `readinessCheck`）、配额、labels、有根的 `After=podman-docker-forward.service`、`WantedBy` 有根 / 无根；systemd 转义的边界值（`%`、`$`、引号、反斜杠、空格、空值）；`${VAR}` 未定义时与 k8s 同样报错。`make lint`。
- **可能动到的文件**：新包 `internal/quadlet/`。

### Task 5　Quadlet 引擎

- **要做到什么**：`engine.Quadlet` 能把一组单元装进对应目录、建 / 换 / 删 secret、`daemon-reload`、按设计 §6 启动与等待、停与删、报状态（含失败的单元），失败时说清是哪个单元、日志怎么看；Podman 版本不够时报错说要几。
- **依据**：设计 §4、§6、§8。
- **依赖**：Task 1。
- **验收**：用替身命令执行器的单元测试覆盖：首装、内容不变（只 `start`）、内容变了（`restart`）、单元被删（先 `stop` 再删文件）、迁移失败、启动超时、`down`、`status`；有根 / 无根选目录与 `--user`。真命令的验证放 Task 7。`make lint`。
- **可能动到的文件**：`internal/engine/quadlet.go`（新）、`internal/engine/engine.go`（若接口要补）。
- **回退**：`down` 就是回退；单元只装在 `brickkit-<项目名>/` 子目录里，不碰别的文件。

### Task 6　命令接入

- **要做到什么**：`up`（含 `--dry-run`）、`down`、`status`、`lint` 在 `podman.units: quadlet` 下走新路径；compose ↔ Quadlet 切换时清掉另一种的残留并打印一行；输出与现有命令同一种样子。
- **依据**：设计 §6。
- **依赖**：Task 2、4、5。
- **验收**：CLI 进程内测试（`internal/cli/testdata/`，替身引擎）：dry-run 只写生成目录；up 成功、迁移失败报 `MIGRATION_FAILED`、单元失败给 `journalctl` 命令；down；status 列出失败的单元；从 compose 切过来与切回去各一次；`pruneOtherTarget` 扩到三种生成物。回归条目。`make lint`、`make test-all`。
- **可能动到的文件**：`internal/cli/{up,up_k8s,down,status,lifecycle,lint}.go`、新的 `internal/cli/up_quadlet.go`、i18n、错误码页（若有新码）。

### Task 7　本机真跑

- **要做到什么**：在这台机器的无根 Podman 上用真 systemd 跑通，覆盖测试替身测不到的部分。
- **依据**：设计 §9。
- **依赖**：Task 6。
- **验收**：用 `tests/components/` 里带迁移、带依赖、带外壳的组件拼一个项目：`up` 全部 healthy；改一个组件的配置再 `up`，只有它重启、它的迁移再跑；让迁移失败，看报错；`down` 后单元、secret、网络都没了；compose → Quadlet → compose 各切一次，端口不冲突；多行密钥逐字节到达。`systemctl --user list-units --state=failed` 不留东西。记录原始输出（会话 scratchpad），能复用的脚本放进 `tests/`（与现有真引擎测试同样的开关）。有根部分要 sudo：写好脚本交给维护者在自己终端跑。
- **回退**：`brickkit down`；脚本末尾清理。

### Task 8　环境检查脚本

- **要做到什么**：`scripts/podman/check-environment.sh` 对 Quadlet 部署报它要的东西（linger、AppArmor、`nofail` 盘、版本），不再对它报 `podman-restart.service`、等网络的 drop-in、`podman.socket`。
- **依据**：设计 §8。
- **依赖**：Task 6（脚本怎样知道是哪种：读部署文件，或加一个参数——实现时定，写进脚本说明）。
- **验收**：本机无根两种方式各跑一次，输出符合预期、退出码 0；`bash -n`；变动的分支用替身命令跑过，并在真实的冷状态下跑一遍（2026-11-02 的教训）。

### Task 9　文档与技能

- **要做到什么**：读者照文档就能用 Quadlet 交付；单机 Podman 交付的推荐改成 Quadlet（设计 §10 第 2 条），compose 留给开发机。
- **依据**：设计 §10；用户级规则“任务收尾全量排查文档”。
- **依赖**：Task 7（文档写实测过的行为）、Task 8。
- **验收**：部署选择页（“简短的回答”、一台机器、两种主机准备、重启机器之后、实测对比）、排障页（Quadlet 的现象 → 原因 → 解决，日志在 journald）、`podman-docker-forward.service` 的说明补上对 Quadlet 单元的顺序、字段参考、CLI 参考（up / down / status）、错误码页、`AGENTS.md` 与 `AGENTS.zh.md` 里的规则行、`brickkit-deploy` 与 `brickkit-troubleshoot` 技能，中英文对齐；`make generate-llms`；按文档面清单逐类过一遍。`make lint`。

### Task 10　发版与回复

- **要做到什么**：发一个小版本（新功能），回复 FRR-025，记忆更新。
- **依赖**：Task 9。
- **验收**：发版说明起草（Highlights / What's new / Documentation）；推送与 `make release` 由维护者执行（会话里被权限拦下）；CI 绿、产物齐；FRR-025 文件写回复与复测步骤（设计 §9 的真重启请 be-assembly-standard 做）；记忆里的反馈进度与引擎推荐更新。

## 整体验证

`make lint`、`make test-all`；Task 7 的真跑记录；be-assembly-standard 的真重启复测。

## 完成标准

十个任务验收全过；实验与真跑留下的临时单元、容器、secret、网络都已清掉；设计文档与实现一致（实现中改了的地方回写设计并注明）。

## 进度

| 任务 | 状态 |
| --- | --- |
| 1 | 完成（`8e51608a`）：九项都有结论，设计 §5.1、§5.2、§6、§8 已改。AI 定的（待复核）：`up` 先单独跑全部迁移、成功才动组件（代价：变了的组件迁移多跑一次）；最低 Podman 5.3；配额全走 `PodmanArgs=` |
| 2 | 完成：`podman.units`（compose / quadlet）、`Quadlet()`、`LongRunning()`；本机进程与焦点在 Quadlet 下装载即拒绝；`podman:` 写在别的 target 下警告（与 `k8s:` 一致，设计 §3 已改）；schema、字段参考、deploy.yaml schema 页；R98、R99 |
| 3 | 完成：新包 `internal/machine`（计划、本机进程、外壳、文件交付、配额、变量去处判定、启动等待、健康检查规格），compose 只剩渲染；209 个 compose 测试场景的全部生成物重构前后逐字节相同（临时路径归一化后），`make test-all` 通过。顺带去掉了不起作用的 `compose.Options.Engine`。注意到但没动：11 个文件原本就没有 gofmt；`AGENTS.zh-CN.md` 是旧版中文入口 |
| 4 | 完成：`internal/quadlet` 渲染单元文件、副本的模板与实例链接、Podman secret、迁移与组件单元名；15 个测试覆盖设计 §4–§5 的组合。AI 定的（待复核）：单元不写生成时间（引擎靠内容比较判断要不要重启）；启动时限 = startPeriod + 30 秒 + 30 秒余量，没有健康检查 90 秒；requests 的 CPU 不生成参数（与 compose 一样只是记录） |
| 5 | 完成：`engine.Quadlet`（`UpRequest.Quadlet`），安装目录 `<Quadlet 目录>/brickkit-<项目>/` 是部署记录；Podman 5.3 起；secret 经标准输入建；迁移先跑、变了的组件 restart、其余 start；不再生成的单元先停再删；`down` 删网络与 secret；`status` 按 `PODMAN_SYSTEMD_UNIT` 标签找容器、另列失败的单元。10 个替身测试。待 Task 7 真跑核实：`PODMAN_SYSTEMD_UNIT` 标签、`secret create --replace` 与 `secret rm` 在容器用着时的行为、未加载单元的 `stop` |
| 6 | 未开始 |
| 7 | 未开始 |
| 8 | 未开始 |
| 9 | 未开始 |
| 10 | 未开始 |
