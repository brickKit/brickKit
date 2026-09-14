# CLI 命令完整参考

AGENTS.zh.md §8 给每个命令一句话概括，加一小撮精选的参数示例——够把整个
命令集装进脑子里。这份文档是另外一半：每个命令、每个参数、真实生成的输
出都在这。想知道整个 CLI 长什么样，看 AGENTS.zh.md；想知道某一个命令到
底干什么、某一个参数到底改变了什么，看这份。

这份文档里出现的每一个命令名、参数名，都会被 `scripts/check-cli-docs.py`
（`make check-cli-docs`）拿真实二进制核对——改名或删掉的参数会让构建直接
报错，不会悄悄过期。

下面的示例，除非特别注明，都是对着一个真实项目跑出来的（`tests/components/`
里两个真实组件，一条真实的依赖边）。

---

## brickkit init

**用法：** `brickkit init <项目名称> [flags]`

在当前目录生成 `brickkit.yaml`、`components/`、`.brickkit/`，把平台自己
那份规则追加进 `.gitignore`（003 §11），并装入 AI 助手技能
（`.claude/skills/`、`AGENTS.md`）。项目名称必须显式指定，没有默认值，
只能是小写字母/数字/中划线，且以字母或数字开头结尾（要喂给 Docker 网络
名和 K8s namespace）。

装入的技能只覆盖"照常识会猜错"的那些东西——保留变量、健康检查禁令、启
停跟着上层走——从不复刻某个参数的具体写法，那部分一律指向 `--help`。它
们跟着项目一起提交、团队共享；CLI 升级后用 `brickkit skills update` 刷
新。`init` 绝不碰你自己的 `CLAUDE.md`——那是你自己的文件。

如果项目还把组件源码一起纳入版本控制（把 `components/` 从 `.gitignore`
里去掉），`init` 会额外装一个 pre-commit hook，拦住"归档状态变了、
`enabled` 却没跟着改"这个失误（004 §3.14）——但只有当项目根目录**就是**
Git 仓库根目录时才会自动装；嵌套在别人仓库里的项目，需要用 `--hooks` 显
式补装。

**参数**

| 参数 | 默认值 | 作用 |
| --- | --- | --- |
| `--no-skills` | 关闭 | 不装 AI 助手技能 |
| `--hooks` | 关闭 | 只装 pre-commit hook（给已有项目、或嵌套在别的仓库里的项目补装用） |

**示例**

```
$ brickkit init demo-shop
✅ 项目已初始化：demo-shop
   📁 brickkit.yaml        项目配置
   📁 components/          组件源码（已配为本地安装源 local-dev）
   📁 .brickkit/           CLI 工作目录
   📁 .claude/skills/      AI 助手技能（4 个）
   📁 AGENTS.md            AI 助手项目导读
   💡 组件源码要跟项目一起进 Git 的话：brickkit init --hooks 装上提交前检查

下一步：
  brickkit add --local               把 components/ 下的组件全加进来
  brickkit add people/basic@1.0.0    从安装源添加组件
  brickkit up                        一键启动
```

```bash
brickkit init demo-shop --config brickkit.prod.yaml   # 初始化非默认环境的配置文件
brickkit init demo-shop --no-skills                   # 不装 AI 助手技能
brickkit init --hooks                                 # 给已有项目单独补装 pre-commit hook
```

---

## brickkit skills

**用法：** `brickkit skills [flags]` / `brickkit skills <status|update> [flags]`

管理项目 `init` 时装入的 AI 助手技能。它们描述的是**当前这个 CLI 版本**
的行为，所以 CLI 升级后需要刷新一次。裸的 `brickkit skills` 和
`brickkit skills status` 是只读的，效果一样；`brickkit skills update` 把
缺的装上、把旧的刷新。

**手改过的文件绝不会被覆盖。** `update` 会把它们列出来并跳过。想放弃本
地修改，删掉那个文件再跑一次 `update` 就行——刻意不提供 `--force`：删文
件这个动作本身已经足够明确，多一个开关只会多一条误伤路径。跟 `init` 一
样，`skills` 也绝不碰你自己的 `CLAUDE.md`。

**子命令**

| 子命令 | 作用 |
| --- | --- |
| `status` | 看每个技能文件的状态（已装、缺失、过期、手改过）——只读 |
| `update` | 缺的装上、旧的刷新、手改过的跳过 |

**示例**

```bash
brickkit skills           # 看装了什么、有没有过期
brickkit skills update    # 刷新到当前 CLI 版本
```

---

## brickkit add

**用法：** `brickkit add [<组件ID>[@精确版本]] [flags]`

递归解析并安装一个组件及其依赖树：按 `sources:` 声明的顺序从第一个有它
的安装源取 Manifest，递归进 `dependencies.components`，强依赖取不到就报
错终止（弱依赖取不到只警告并继续），把 `artifacts` 下载到
`.brickkit/artifacts/<版本化服务名>/`，把结果写进 `brickkit.yaml`——**不
写** `enabled` 字段，所以这个组件默认按"跟着上层走"（AGENTS.zh.md §5.4）
的规则决定启停。

不写版本号时 CLI 会替你解析出一个：`local`/`git` 安装源目录里只有一份
`component.yaml`，那份定义上就是"这个源上的最新版"；`market` 安装源会
先排除不可安装的状态（`draft`、`blocked`），再取剩下里版本号最大的。安
装源按声明顺序依次尝试，第一个有这个组件的源说了算，不跨源比大小。落到
盘上的永远是精确版本；CLI 从不接受（也从不写）`^1.0.0` 这种范围写法——
能省略的只是版本号参数本身，不是配置里最终写下的精度（AGENTS.zh.md
§9.2）。

给一个已装组件添加第二个版本时，会先弹出确认再让两者共存（非交互模式用
`--yes` 跳过）。

**参数**

| 参数 | 默认值 | 作用 |
| --- | --- | --- |
| `--local` | 关闭 | 把本项目 `local` 类型安装源里的组件一次全部添加。不接组件 ID，也不能与 `--repo`/`--repo-all` 同用。已经在配置里的会跳过；同 ID 但版本不同的会单独提示并跳过（不替你决定要不要多起一个容器）。任一组件解析失败就整体中止，`brickkit.yaml` 一个字节不动，不是部分写入 |
| `--repo` | 关闭 | 额外 clone 该组件的完整 Git 仓库到 `components/`（仅开源组件） |
| `--repo-all` | 关闭 | clone 解析出的依赖树中每一个开源组件的 Git 仓库（闭源组件会被点名并跳过） |
| `-y`, `--yes` | 关闭 | 非交互模式：自动回答所有确认提示（供 CI/CD 用） |

**示例**

```
$ brickkit add people/basic@1.0.0 --yes
📦 添加 people/basic@1.0.0
   ├── Manifest ✅
   ├── 依赖 department/tree@1.0.0 ✅ 已拉取（artifacts 2 个文件）
   └── artifacts ✅（2 个文件）
⚠️ 警告：弱依赖缺失：infra/redis-event-bus@1.0.0
   影响组件：people/basic@1.0.0
   原因：该组件在所有安装源中均未找到
   影响：该组件的环境变量 INFRA_REDIS_EVENT_BUS_ENDPOINT 不会被注入
   💡 弱依赖降级由组件自行处理（002 §3.4）；如需启用，请确认该组件已发布并可从安装源获取
✅ 已写入 brickkit.yaml（2 个组件）
📁 已下载 artifacts 到 .brickkit/artifacts/（4 个文件）
```

留意这次真实运行展示的对比：一个缺失的**强**依赖（`department/tree`）静
默解析成功；一个缺失的**弱**依赖（`infra/redis-event-bus`）只警告，而且
警告里直接点名了哪个环境变量不会被注入——这正是 AGENTS.zh.md §5.3、§9.13
论证的"不注入，而不是注入空字符串"，在它真正生效的那一刻被看见。

```bash
brickkit add erp/backend@1.0.0 --repo-all   # clone 所有开源依赖的源码
brickkit add --local                        # 把本地安装源声明的组件一次全部添加
```

---

## brickkit remove

**用法：** `brickkit remove <组件ID>[@版本] [flags]`

移除一个组件：如果还有其他组件把它声明为**强依赖**就拒绝移除（并点名调
用方），否则删掉它在 `brickkit.yaml` 里的条目、解除所有指向它的
`resources[].bindings`、清掉它的 Manifest/产物缓存，并删除它的源码目
录——`components/<scope>/<name>/` 以及归档中的
`components/.archived/<scope>/<name>/`——除非同 ID 还有其他已装版本仍然
需要那份源码。多个版本共存时必须显式指定版本。

**参数**

| 参数 | 默认值 | 作用 |
| --- | --- | --- |
| `--force` | 关闭 | 即使源码目录不是 Git 仓库、有未提交的改动、或有从没推送过的提交，也照样删——这三种情况正常情况下都会阻止删除，因为一旦删了就找不回来 |

**示例**

```
$ brickkit remove department/tree
❌ 无法移除 department/tree
   版本：1.0.0
   以下组件强依赖它：people/basic@1.0.0
   建议：请先移除依赖方
```

```bash
brickkit remove people/basic@1.0.0            # 多版本共存时指定版本
brickkit remove people/basic@1.0.0 --force    # 有未提交/未推送的改动也照样删源码
```

---

## brickkit fetch

**用法：** `brickkit fetch <组件ID>[@版本] [flags]`

下载一个组件声明的产物（`.proto`、一份 OpenAPI 文档、一个 SDK——
Manifest 的 `artifacts` 段里列的东西），但**不把它装进本项目**——不写
`brickkit.yaml`，不参与部署，不进依赖图。这是跨项目场景：你需要另一个
团队的契约去生成客户端，但那个服务是他们的项目部署的，不是你的，把它写
成依赖会让平台在你这边再部署一份（003 §4.9）。

产物落在 `.brickkit/artifacts/<版本化服务名>/<type>/...`——跟
`brickkit add` 下载的完全同一个位置，默认同样跟着项目提交、团队共享。
不写版本号时按 `add` 同样的规则取最新版本。

**示例**

```bash
brickkit fetch infra/notifier@1.0.0   # 取指定版本的产物
brickkit fetch infra/notifier         # 取最新版本的产物
```

---

## brickkit up

**用法：** `brickkit up [flags]`

一次性把声明变成运行中的容器：读取 `brickkit.yaml` 和每个组件的
Manifest → 启停判定（跟着上层走：顶层组件没写 `enabled` 就默认跑，下层
跟着上层里需要它的那个走，AGENTS.zh.md §5.4）→ 检查强依赖（缺失报错）和
弱依赖（缺失警告，且完全不注入那个依赖的 `*_ENDPOINT`）→ 拓扑排序得出启
动顺序 → 生成 `docker-compose.yaml`（或 K8s 清单），注入环境变量、合并
资源配额 → 给任何 `local: true` 组件生成
`local-debug.<版本化服务名>.env` → 检测镜像拉取权限（未授权时提示
`docker login`）→ 调用底层引擎，先跑一次性容器执行声明的迁移，失败则阻
断主服务。

改 `brickkit.yaml` 里某个组件的版本号**就是**升级——`up` 会拉新的
Manifest 和产物，跑一遍跟全新安装一样的兼容性检查（004 §3.5.1）。

**参数**

| 参数 | 默认值 | 作用 |
| --- | --- | --- |
| `--dry-run` | 关闭 | 只生成部署文件并打印计划（启停判定、启动顺序、资源绑定警告、依赖图），不启动任何东西。升级场景下还会额外打印一份变更摘要 |
| `--context` | （取 `deploy.context`） | 本次运行覆盖要部署到哪个 kubeconfig 上下文。仅 k8s 有意义——`deploy.target: docker` 下写这个参数会被拒绝 |

**示例**（真实项目：`people/basic` 依赖 `department/tree`，有一个缺失的
弱依赖，还没绑定任何 `resources:`）

```
$ brickkit up --dry-run
🚀 启动项目 demo-shop（deploy.target: docker）
⚠️ 警告：弱依赖缺失：infra/redis-event-bus@1.0.0
   影响组件：people/basic@1.0.0
   原因：该组件在所有安装源中均未找到
   影响：该组件的环境变量 INFRA_REDIS_EVENT_BUS_ENDPOINT 不会被注入
   💡 弱依赖降级由组件自行处理（002 §3.4）；如需启用，请确认该组件已发布并可从安装源获取
📋 组件状态计算：
   ✅ department/tree@1.0.0  启动（people/basic 需要）
   ✅ people/basic@1.0.0     启动（顶层）

⚠️ 警告：资源依赖未满足（--dry-run 不阻断）
   department/tree@1.0.0：需要 kind: database、engine: postgresql（brickkit.yaml 的 resources 中未声明）
   people/basic@1.0.0：需要 kind: database、engine: postgresql（brickkit.yaml 的 resources 中未声明）
   建议：
   1. 生成的部署文件里**不会有**这些组件的资源连接变量（DATABASE_* 等）
   2. 在 brickkit.yaml → resources 中声明并绑定后再 up；不加 --dry-run 时这里会直接阻断
📋 启动顺序（拓扑排序）：
   1. department-tree-1-0-0  无依赖
   2. people-basic-1-0-0     ← 依赖 1

可独立启动：department-tree-1-0-0（无依赖）
最长依赖链（2 层）：department-tree-1-0-0 → people-basic-1-0-0

依赖图：
   people/basic@1.0.0 → department/tree@1.0.0
                      → infra/redis-event-bus@1.0.0（弱，未安装）
📄 已生成：.brickkit/generated/docker-compose.yaml

🔧 启动前会执行的数据库迁移（失败则该组件不会启动）：
   department/tree@1.0.0  /app/department-tree migrate
   people/basic@1.0.0  python -m app.main migrate

💡 --dry-run 只生成文件，未启动任何组件
```

这份输出里每一行都带着自己的理由——`department/tree` 写着
`（people/basic 需要）`，`people/basic` 写着`（顶层）`——正是
AGENTS.zh.md §5.4 说的"每一条启停决定都带着理由"这条性质。也留意一下：
资源绑定没满足，在 `--dry-run` 下只是**警告**；真跑一次 `up` 会直接阻
断——这正是建议第二行想说的那件事。

```bash
brickkit up --config brickkit.prod.yaml   # 对非默认环境的配置文件生效
brickkit up --context prod-cluster        # 本次运行指定某个 kubeconfig 上下文（仅 k8s）
```

---

## brickkit down

**用法：** `brickkit down [flags]`

停止所有组件。停止顺序与启动顺序相反（依赖方先停，被依赖方后停），交给
底层引擎处理，CLI 自己不管这件事。只想停其中几个：在 `brickkit.yaml`
里给它们写 `enabled: false`，再跑一次 `brickkit up`——它们会从生成的部
署文件里消失，引擎会把对应容器一并移除，效果跟"只停这几个"一样，而且意
图被记录进了 `brickkit.yaml`，下次 `up` 不会意外把它们又拉起来。

**`down` 从不删除 volume——数据库数据永远保留。** 真想清掉，手动
`docker volume rm` 或 `docker compose down -v`。

**参数**

| 参数 | 默认值 | 作用 |
| --- | --- | --- |
| `--context` | （取 `deploy.context`） | 跟 `up --context` 一样的覆盖——本次运行指定某个 kubeconfig 上下文（仅 k8s） |

**示例**

```bash
brickkit down
brickkit down --context prod-cluster   # 针对指定集群关停
```

---

## brickkit status

**用法：** `brickkit status [flags]`

查看每个组件的运行状态。CLI 自己不存运行状态——每次都直接问底层引擎
（Docker 下是 `docker compose ps --format json`）。输出覆盖运行中的组
件、未启动的组件（附带原因）、`local: true` 的组件、基础资源可达性。

**示例**

```
$ brickkit status
📊 项目状态：demo-shop（deploy.target: docker）

❌ 未在运行（2 个组件）
 ┌─────────────────┬───────┬────────┐
 │ 组件            │ 版本  │ 状态   │
 ├─────────────────┼───────┼────────┤
 │ department/tree │ 1.0.0 │ 未创建 │
 │ people/basic    │ 1.0.0 │ 未创建 │
 └─────────────────┴───────┴────────┘
   看日志定位：docker compose -p brickkit-demo-shop logs <服务名>

📋 没有正在运行的组件（可能已经 brickkit down 过）
   重新启动：brickkit up
```

---

## brickkit sync

**用法：** `brickkit sync [flags]`

在 `components/` 和 `components/.archived/` 之间双向搬运组件源码，判据
**跟 `up` 完全一样**：这次会启动的留在（或搬回）活跃目录，不会启动的搬
进归档。它从不碰运行中的容器，也从不改变 `up` 会启动谁——只搬目录。想
收窄范围就在 `brickkit.yaml` 里改 `enabled`（跟着上层走，AGENTS.zh.md
§5.4），`sync` 会跟着走。搬的时候整个目录一起走，连 `.git` 都不例外，
所以归档后的组件照样能正常用 Git 命令。只处理已经写进
`brickkit.yaml`、且已有源码的组件。没有 `--dry-run`——搞错了再跑一次就
换回来了。

**示例**

```
$ brickkit sync
📂 工作区无需整理
   components 下没有需要归档或激活的组件源码
```

---

## brickkit restore

**用法：** `brickkit restore [flags]`

把 `brickkit.yaml` 里每个组件的 `enabled` 字段还原到最后一次提交时的值
（提交里没写就把这个字段整个删掉），再让源码目录结构跟着这次还原走，走
的是跟 `sync` 一样的判据。这是为那些把组件源码跟项目一起提交的项目准备
的（`components/` 从 `.gitignore` 里去掉）：这类项目里 `sync` 的目录搬
运会进 diff，"本地关掉几个顶层组件、跑了 sync、干完活忘了改回来就提交"
是个反复出现的失误，这条命令专门堵住它。

它只动 `enabled` 这一个字段，逐条处理：

- 工作区和最后一次提交都有的条目 → 还原成提交里的值
- 工作区新增的条目（刚 `add` 的，或改了版本号的）→ 一个字不动
- 提交里有、但工作区没有的 → 绝不加回来——这不是 `git revert`

要覆盖的旧值会在真正改动之前先打印出来。

**参数**

| 参数 | 默认值 | 作用 |
| --- | --- | --- |
| `--check` | 关闭 | 只检查、不改任何东西：即将提交的 yaml 与目录结构是否自洽，不自洽就非零退出。pre-commit hook（`brickkit init --hooks` 装的那个）调的就是这个 |

**示例**

```bash
brickkit restore           # 把 enabled 和源码结构还原到最后一次提交
brickkit restore --check   # 只检查；非零退出码表示这次提交不自洽
```

---

## brickkit login

**用法：** `brickkit login [flags]`

登录市场：提示输入用户名，提示输入密码（隐藏输入），调用市场的认证
API，成功后把 Token 写进 `.brickkit/credentials`（权限 `0600`）。市场地
址取自 `sources:` 里 `type: market` 的那一条；配了多个时用 `--market`
指定。Token 优先级是 `.brickkit/credentials` 高于 `sources.authToken`。
CLI 每次使用 Token 前都会检查 `expiresAt`，过期就提示重新登录——不会自
动刷新。

**参数**

| 参数 | 默认值 | 作用 |
| --- | --- | --- |
| `--market` | （配置里的 market 安装源） | 配了多个市场安装源时，指定登录哪一个 |
| `--username` | （交互式输入） | 非交互地指定用户名 |
| `--password-stdin` | 关闭 | 从标准输入读密码，不走交互提示（供 CI 用——不回显、不进历史） |

**示例**

```bash
brickkit login
brickkit login --market https://market.example.com/api/v1
echo "$PASSWORD" | brickkit login --username ci-bot --password-stdin
```

---

## brickkit logout

**用法：** `brickkit logout [flags]`

分两步：先调市场的 `POST /auth/logout` 作废这个 Token（服务端那一
侧），再删掉本地的 `.brickkit/credentials`。**本地那一份一定会删，即使
市场连不上**——否则一次网络抖动就会让人以为自己已经退出，而凭据其实还
躺在盘上；市场不可达时只会警告一句，并说明那个 Token 仍然有效到自然过
期为止。本来就没登录？`logout` 什么都不做，也不算失败。

**参数**

| 参数 | 默认值 | 作用 |
| --- | --- | --- |
| `--keep-remote` | 关闭 | 只删本地凭据——跳过调用市场（离线工作时用） |

**示例**

```bash
brickkit logout
brickkit logout --keep-remote   # 只删本地，供离线使用
```

---

## brickkit publish

**用法：** `brickkit publish [flags]`

把组件发布到市场：先确认已登录（`.brickkit/credentials` 或
`sources.authToken`），读取并校验 `--path` 下的 `component.yaml`，确认
镜像引用能解析、`artifacts` 声明的每个文件都真实存在，把这个版本建成
`draft`，上传产物，再转成 `stable`，最后（如果给了）应用
`--visibility`。draft → 上传 → stable 这三步是刻意设计的：市场在转
stable 的那一刻会校验"声明的每个产物文件是不是真的都传齐了"，先建
draft 正是为了保证绝不会出现一个已经 stable、却文件没传全的半成品。
`--path` 也接受归档目录，比如 `./components/.archived/erp/backend`。

**参数**

| 参数 | 默认值 | 作用 |
| --- | --- | --- |
| `--path` | `.` | 组件源码目录（含 `component.yaml`） |
| `--market` | （配置里的 market 安装源） | 发布到哪个市场 |
| `--visibility` | （沿用市场侧已有设置） | `public` 或 `private` |
| `--changelog` | （无） | 这个版本的自由文本更新说明 |
| `--git-url` | （目录自己的 `origin` remote） | 开源组件的 Git 仓库地址 |
| `--source-type` | （按目录的 `git remote` 推断） | `git`（开源）或 `registry`（闭源） |
| `--sign` | 关闭 | 发布前用 cosign 对组件签名 |
| `--key` | `cosign.key` | 用来签名的 cosign 私钥（只有配合 `--sign` 才有意义） |
| `--signed-by` | （无） | 人类可读的签名者标识，如 `release-bot@example.com` |
| `--public-key-ref` | （按 `--key` 的 `.key` → `.pub` 推导） | 跟签名一起记录的公钥 ref |
| `--no-pin-digest` | 关闭（默认会把镜像钉成 digest） | 发布时保留镜像引用为可变 tag，不先解析成 digest——具体这样做会放弃什么保证，见 AGENTS.zh.md 的闭源加固指南 |

**示例**

```bash
brickkit publish --path ./components/people/basic
brickkit publish --path ./components/people/basic --visibility private
brickkit publish --path ./components/people/basic --changelog "给人员记录加了状态字段"
brickkit publish --path ./components/people/basic --market https://market.example.com/api/v1 --visibility private --changelog "新增 X"
brickkit publish --path ./components/people/basic --git-url https://github.com/org/people-basic --sign --key cosign.key --signed-by release-bot@example.com --public-key-ref keys/vendor.pub
```

---

## brickkit version

**用法：** `brickkit version [flags]`

打印 CLI 版本、支持的 Manifest `apiVersion`、支持的部署目标。

**参数**

| 参数 | 默认值 | 作用 |
| --- | --- | --- |
| `-v`, `--verbose` | 关闭 | 额外打印 Git commit 哈希与构建时间 |

**示例**

```
$ brickkit version
BrickKit CLI v0.1.0
Supported manifest version: brickkit/v1
Supported deploy targets: docker, k8s
```

```bash
brickkit version --verbose   # 额外输出 git commit 与构建时间
```

---

## 两个全局参数，每个命令都有

| 参数 | 默认值 | 作用 |
| --- | --- | --- |
| `-c`, `--config` | `brickkit.yaml` | 对哪份项目配置文件生效——这是多环境机制（`brickkit up --config brickkit.prod.yaml`），不是合并或覆盖层（AGENTS.zh.md §9.9） |
| `--log-level` | `info` | CLI 写到 stderr 的结构化 JSON 日志级别（`debug`/`info`/`warn`/`error`/`off`）。这些是诊断信息，不是命令的实际结果——正常输出不受这个参数影响，`off` 只是让 JSON 日志行完全消失，不会改变命令报告的内容 |
