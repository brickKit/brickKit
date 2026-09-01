# BrickKit Skills：init 时装进项目的 AI 助手技能

- 日期：2026-09-01
- 状态：设计已确认，待写实现计划
- 影响面：`brickkit init`（行为新增）、新增 `brickkit skills` 命令、design/004 与附录 E、AI-CONTEXT.md、llms.txt、README、试用指南 01

---

## 1. 背景与动机

BrickKit 的上手成本几乎全部集中在一处：**11 条命令与两个 yaml 的字段规则记不住**。
仓库已经用 `AI-CONTEXT.md`（48KB 压缩件）与 `llms.txt` 承认了这条路线——让 AI 助手替
使用者记住这些。但这两份文件服务的是「有人把仓库链接丢给 AI」的场景；在**使用者自己的
项目里**，AI 手边什么都没有。

本设计把这件事往前推一步：`brickkit init` 时，把一套 AI 助手技能直接装进项目。
从「让 AI 先去读一份大文档」变成「AI 手边就有可按需调用的操作手册」。

`brickkit sync` 的说明里已经写着「以及替你读代码的 AI 都得连它们一起扫」——
平台把 AI 当作项目的一等读者，这不是新立场，只是第一次落成可执行的产物。

## 2. 目标与非目标

**目标**

1. `brickkit init` 默认在项目里装好四个语言无关的 skill 与一份 `AGENTS.md`。
2. 提供一条显式刷新通道，让 CLI 升级后项目里那份技能能跟上。
3. 保证「skill 开始说谎」会在 CI 里失败，而不是等用户照着敲一遍才发现。

**非目标**

1. 不做语言/框架相关的技能（见 §11 拒绝清单第 1 条，与 §12 的后续路线）。
2. 不改变任何现有命令的行为，`init` 之外的命令一律不碰用户仓库里的 skill 文件。
3. 不引入任何网络依赖：全部内容内嵌在 CLI 二进制里，离线可用。

## 3. 已定决策

| 决策 | 选择 | 理由 |
| --- | --- | --- |
| 技能形态 | `.claude/skills/<name>/SKILL.md` + 一份 `AGENTS.md` | 前者服务 Claude Code，可按需加载；后者让其他 agent 不至于两眼一黑 |
| 内容粒度 | 按任务切的四个小 skill | 按「人在什么时刻卡住」切，而不是按文档章节切 |
| 内容是否含项目状态 | **纯静态模板** | 项目状态就在旁边的 `brickkit.yaml` 里，比任何快照都准；静态才可内嵌、可校验、永不与现实脱节 |
| 资产来源 | `//go:embed` 内嵌 | 贴合单二进制、无常驻、离线优先；且「skill 版本严格绑定 CLI 版本」正是想要的语义 |
| 升级方式 | 显式命令刷新 + 指纹校验，手改过就不碰 | 与 `checkNotInitialized`、`writeNewFile` 的 `O_EXCL` 是同一种态度 |
| 是否入库 | 提交，团队共享，不写 `.gitignore` | 与 artifacts/manifests 默认提交的调子一致；升级也会在 diff 里看得见 |

## 4. 装进用户项目的东西

```
<project>/
├── AGENTS.md                              一页导读（已存在则不碰）
├── .claude/skills/
│   ├── brickkit-assemble/SKILL.md
│   ├── brickkit-component/SKILL.md
│   ├── brickkit-deploy/SKILL.md
│   └── brickkit-troubleshoot/SKILL.md
└── .brickkit/skills.lock                  托管清单，跟着项目入库
```

**为什么不写 `CLAUDE.md`。** 官方文档明确写着「Claude Code reads `CLAUDE.md`, not
`AGENTS.md`」，推荐做法是写一个 `CLAUDE.md` 用 `@AGENTS.md` 把它导入进来。
本设计**刻意不这么做**。

`CLAUDE.md` 是使用者自己的流程文件——他可能已经有一套自己的做法，也可能压根不用
Claude Code。往那儿写东西是这套方案里唯一真正侵入的动作，而收益很有限：
skill 的 `description` 是常驻上下文，**Claude Code 不靠 `CLAUDE.md` 也能发现那四个
技能**；`CLAUDE.md` 只是让 `AGENTS.md` 那一页导读也被读到。

少一页导读，换掉「我们动了你最要紧的配置文件」，这笔交换是值的。
所以 `AGENTS.md` 末尾写一行说明——想接上就自己在 `CLAUDE.md` 里加 `@AGENTS.md`，
接不接由他决定。

这条也定下了整套东西的分量：**装进项目的技能是为了让人少踩几个猜不到的坑，
不是为了替他安排流程。**

`.gitignore` 不新增任何规则：以上文件全部提交。
`.brickkit/skills.lock` 落在 `.brickkit/` 下但**不在** `generated/` 里——
`.gitignore` 只忽略 `.brickkit/generated/` 与 `.brickkit/credentials`，所以它默认入库，
与 skill 文件同进同退，diff 一致。

## 5. CLI 侧结构与命令面

```
internal/skills/
├── assets/                    //go:embed 的源文件（纯文本，与 §4 交付物一一对应）
│   ├── AGENTS.md
│   └── claude/skills/<name>/SKILL.md
├── assets.go                  embed 声明 + 资产清单：源路径 → 项目内目标路径
├── install.go                 Install / Update / Status 三个入口
├── lock.go                    skills.lock 读写
└── *_test.go
```

命令面只新增**一条顶层命令**（11 → 12），两个子命令，归入 `groupProject`：

| 命令 | 行为 |
| --- | --- |
| `brickkit init`（默认） | 安装全部资产；`--no-skills` 关闭 |
| `brickkit skills update` | 缺的装上、旧的刷新、手改过与未托管的跳过并列出 |
| `brickkit skills status` | 只读。列每个托管文件的状态 |

老项目补装也走 `update`，不单设 `install` 子命令——「缺失」本就是 update 要处理的一种状态。

刻意**不复用 `sync` 这个词**：它在本项目已专指「整理组件源码工作区」（004 §3.9），
占用会制造歧义。

表格输出复用 `internal/cli/table.go`；错误一律走 `clierr`。

## 6. 托管状态模型

`skills.lock` 为每个托管文件记三样：相对路径、写入时的 CLI 版本、写入时的内容 sha256。

| 状态 | 判定 | `update` 行为 | `status` 显示 |
| --- | --- | --- | --- |
| 缺失 | 目标文件不存在（lock 有无记录都算） | 写入 | `缺失` |
| 最新 | 磁盘 sha256 == 当前内嵌资产 sha256 | 不动 | `最新` |
| 待更新 | 磁盘 sha256 == lock 记录，且 ≠ 当前资产 | 覆盖写入 | `待更新（0.1.0 → 0.2.0）` |
| **已手改** | 磁盘 sha256 ≠ lock 记录 | **不碰** | `已手改，update 会跳过` |
| **未托管** | lock 无记录，文件却存在 | **不碰** | `未托管，update 会跳过` |

「缺失」与「未托管」以文件是否存在划界，两者合起来覆盖了 lock 无记录的全部情形——
所以老项目补装（lock 里什么都没有、文件也都不存在）走的正是「缺失」这一支，
`brickkit skills update` 一条命令就够，无需单设 `install`。

后两种状态是本节的重点：**CLI 绝不覆盖用户可能亲手写过的东西。**

- 想放弃本地修改：删掉那个文件，再跑一次 `update`。
- **不提供 `--force`**。理由与本项目不提供 `--dry-run` 相同：删文件这个动作本身已经
  足够明确，而多一个开关就多一条误伤路径。
- lock 文件丢失的项目：**没动过的文件判「最新」并补登记**（内容本就与资产逐字节
  相同，补登记不覆盖任何东西，却把升级通道修回来了）；**改过的判「未托管」，
  一个字不动**。
  反过来做——一律判「未托管」——的后果是：用户误删一次 lock，这个项目的技能就
  永远升不上去，哪怕他一个字都没改过。那是坑，不是保守。
  （这一条最初写的正是「一律未托管」，被 `TestLostLockRecoversUnmodifiedAndSparesTheRest`
  抓出来与「『最新』排在 lock 查询之前」自相矛盾。）

`init` 走的是同一套写入逻辑，但因为 `checkNotInitialized` 已保证目录未初始化，
正常路径上只会遇到「缺失」。若用户目录里已有 `AGENTS.md`（完全可能——它和 BrickKit
无关也会存在），按「未托管」处理：不碰，并在 init 输出里明确说这一份跳过了。

## 7. 四个 skill 的边界与内容

`description` 既是常驻上下文、也是加载开关，所以必须写成**「什么时候用」**而不是
「这是什么」。这四行字是本设计里最需要打磨的部分。

| skill | description 的调子 |
| --- | --- |
| `brickkit-assemble` | 要增删组件、调启停、启动/停止整套服务、看运行状态时。含依赖解析、启动顺序、「跟着上层走」的判定 |
| `brickkit-component` | 要新写组件、改 `component.yaml`、加数据库迁移、对外提供 gRPC/HTTP 时。含组件硬性契约与保留变量禁区 |
| `brickkit-deploy` | 要部署到 Docker 或 K8s、绑定基础资源、配置密钥、对外暴露服务时 |
| `brickkit-troubleshoot` | brickkit 命令报错、组件起不来、地址注入不生效、要按错误码定位时 |

**已核实的 frontmatter 约束**（决定资产自检怎么写断言）：

- 只有开头的 `---` 位于**文件第一行**时 frontmatter 才被解析；否则整个文件连 `---`
  都被当作正文。
- `name` 是**可选**的：项目级 skill 的调用名来自**目录名**，`name` 只是显示标签。
  我们仍然显式写 `name`，并让它与目录名一致——两者不一致会让 `/brickkit-deploy`
  和列表里显示的名字对不上。
- `description` 建议写，且 skill listing 里 `description` 会在 **1,536 字符**处被截断。
  本设计给每条 description 的预算是 200–350 字，硬上限 1,024（自检断言按 1,024 判）。
- 为了跨工具可移植，frontmatter **只用 Agent Skills 规范的六个字段**
  （`name`、`description`、`allowed-tools`、`metadata`、`license`、`compatibility`）。
  写规范外的键会在其他分发路径上报 `Unexpected key(s) in SKILL.md frontmatter`。

顺带修正一处原先的顾虑：规范里有一个 `metadata` 自由映射，官方明说是留给自有工具链的
（Claude Code 不解释其内容）。所以往 frontmatter 放元数据本身并不违规。本设计仍把指纹
放在 `.brickkit/skills.lock` 而不是 `metadata`，理由变成两条纯技术的：
① 一个文件的 sha256 没法写在它自己里面（自指）；
② `status` 要一次读完全部状态，读一个 lock 比逐个解析 frontmatter 简单得多。

**正文不规定流程。** 只讲机制与禁区（依赖怎么算、什么是保留变量、哪些行为是刻意
设计的），不写「你应该先做 A 再做 B」。使用者很可能已经有自己的一套做法，技能的职责
是保证他不撞上那几个猜不到的坑，不是安排他怎么干活。

**内容判据（贯穿全部正文）：不写这条，AI 会不会猜错？猜错才值得写。**

必然猜错、因此必须写的：保留变量的确切模式（`COMPONENT_ID`、`COMPONENT_VERSION`、
`*_ENDPOINT`、`DATABASE_*`/`REDIS_*`/`MQ_*`/`STORAGE_*`/`SEARCH_*`/`SMTP_*`）、
版本化服务名格式、健康检查禁令、`startPeriodSeconds` 的适用阈值、迁移状态表约定、
「跟着上层走」的启停判定、错误码到动作的映射。

**一律不写**：任何命令的参数清单（`--help` 是唯一真相，且永不过期）、
任何通用语言/框架知识（读者本来就会）。

`AGENTS.md` 末尾必须有一行告诉读者怎么接上 Claude Code（在自己的 `CLAUDE.md` 里
加 `@AGENTS.md`）——我们不替他写那个文件，但得让他知道这个选项存在。

`AGENTS.md` 是一页导读：BrickKit 是什么、两个 yaml 在哪、几条铁律
（精确版本、保留变量不许碰、健康检查禁令、跟着上层走），并指向 skills 与在线文档。

## 8. 输出样例

`init`（在现有输出后追加两行，沿用 `%-21s` 对齐）。
注意现有输出对 `brickkit.yaml` 这个文件也用 📁，所以这里跟着用 📁 而不引入第二种图标：

```
✅ 项目已初始化：my-project
   📁 brickkit.yaml         项目配置
   📁 components/           组件源码（已配为本地安装源 local-dev）
   📁 .brickkit/            CLI 工作目录
   📁 .claude/skills/       AI 助手技能（4 个）
   📁 AGENTS.md             AI 助手项目导读
```

`skills update`：

```
✅ AI 助手技能已更新
   已写入 2 个：brickkit-component、brickkit-troubleshoot
   已跳过 1 个：brickkit-deploy（已手改）

提示：想放弃本地修改，删掉该文件后重新执行 brickkit skills update
```

全部文件都已是最新时，输出一行「AI 助手技能已是最新（CLI v0.1.0）」（版本用 `version.Display()`，带 v 前缀是 004 §11.3 的既有格式），不做多余动作。

## 9. 测试策略

常规单元与集成之外，有两个自检是本设计的关键：

**资产自检（单元测试）**
遍历所有内嵌 SKILL.md，断言：frontmatter 可解析、含 `name` 与 `description`、
`name` 与所在目录名一致、资产清单里声明的每个源路径都真实存在。

**防说谎检查（纳入现有 CI 脚本）**
把 `internal/skills/assets/` 纳入 `scripts/check-cli-docs.py` 的扫描范围：
skill 正文里出现的每个 `brickkit <cmd>` 与 `--flag` 都必须真实存在。

这是整个设计中最有价值的一环。`check-cli-docs.py` 已经在为文档做这件事，
skill 只是多一批文档；接上之后，「skill 开始说谎」会在 CI 里失败，
而不是等用户照着敲一遍、得到 `unknown flag`、然后怀疑自己装错了版本。

**集成测试**

- `init` 后文件树与 lock 内容正确
- `init --no-skills` 不产生任何 skill 文件，也不产生 lock
- `update` 幂等：连跑两次，第二次零写入
- 手改某个文件后 `update` 不覆盖它，退出码为 0，提示里点出该文件
- 目录里预先存在 `AGENTS.md` 时，`init` 不覆盖它并在输出里说明
- 删掉 lock 后 `update` 一律跳过，不覆盖任何文件
- `status` 五种状态各覆盖一次

## 10. 文档同步清单

新增命令必须连着文档一起动，否则 `check-cli-docs.py` 与 `check-guide-output.py` 会失败：

- [ ] `design/004-CLI 设计.md`：新增 `skills` 一节；`init` 行为补一条
- [ ] `design/附录合集.md` 附录 E（CLI 命令完整参考）
- [ ] `AI-CONTEXT.md`：「11 条命令」→ 12；第 11 节文档导航
- [ ] `llms.txt`：同上
- [ ] `README.md`：第 25 行「11 条命令 + version」
- [ ] `试用指南/01-初始化项目.md`：init 输出样例（受 `check-guide-output.py` 校验）
- [ ] `开发进度/决策索引.md`：记一条决策
- [ ] `scripts/check-cli-docs.py`：扫描范围纳入 `internal/skills/assets/`

## 11. 拒绝清单

按 `design/012` 的传统，把「不做什么」与理由一起记下来。

1. **不做框架级 skill**（`brickkit-fastapi`、`brickkit-spring` 之类）。两个理由：
   一是 `design/001` 把「语言无关」与「平台不越界」写成了系统边界，CLI 一旦发一份
   框架技能，平台就在替组件作者选框架；二是维护数学不成立——6 语言 × 3 框架 = 18 份
   散文，CI 里一份都跑不到，而它们会集体腐烂在没人看的角落。
2. **不做 `--force` 覆盖**。删掉文件再跑 `update` 已经足够明确，多一个开关多一条误伤路径。
3. **不在别的命令里自动刷新**。CLI 是「用完即走」的工具，不该在用户没要求时改他的仓库文件。
4. **不从远端拉取 skill**。会引入签名信任问题，与「安装即信任」的既有立场和离线优先的定位冲突。
5. **skill 里不复刻参数清单**。`--help` 是唯一真相；复刻一份就是承诺维护两份。
6. **不写使用者的 `CLAUDE.md`**（哪怕只有一行导入）。那是他自己的流程文件，
   而 skill 的 `description` 常驻、不靠它也能被发现——侵入换来的收益太小。
   见 §4。
7. **技能正文不规定流程**。只讲机制与禁区。使用者有自己的做法，我们只保证他
   不踩坑。见 §7。

## 12. 已核实的外部事实

原先留作「实现前需验证」的两项已经查证，结论直接改了设计：

1. **Claude Code 不读 `AGENTS.md`。** 文档原文：「Claude Code reads `CLAUDE.md`,
   not `AGENTS.md`」，并推荐用 `@AGENTS.md` 导入。原设计里「若 Claude Code 读
   AGENTS.md 则一份文件通吃」的分支作废。
   但结论**不是**「那就替他写一份 `CLAUDE.md`」——见 §4 与 §11 第 6 条：
   那是他自己的流程文件，而 skill 的 `description` 常驻，不靠它也能被发现。
2. **SKILL.md 的 frontmatter 约束**见 §7，要点：`name` 可选且只是显示标签、
   调用名来自目录名、`description` 在 1,536 字符处截断、frontmatter 必须从第一行开始、
   跨工具可移植只用规范的六个字段。

## 13. 后续路线（不在本次范围）

- **语言适配层**：在 `brickkit-component/references/` 下加 `go.md`、`python.md`，
  各约 60 行，只讲「这门语言里怎么读注入的环境变量、Dockerfile 暴露哪个端口、
  迁移脚本挂在哪」。只覆盖仓库 CI 真跑过的语言（当前正是 Go 与 Python）。
- **真正的正解是模板而非散文**：`brickkit component new --lang <x>` 生成一个真能
  build、真能 up 起来的骨架，由 CI 每次构建。然后 skill 里那一段缩成一句话
  「用这个命令生成骨架，照着改」——一句话不会过期。
  散文描述代码必然腐烂，被 CI 跑过的代码不会。
