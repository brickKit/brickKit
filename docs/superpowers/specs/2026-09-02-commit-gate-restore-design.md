# 提交前的结构自洽闸门与 brickkit restore

- 日期：2026-09-02
- 状态：设计已确认，待写实现计划
- 影响面：新增 `brickkit restore` 命令（含 `--check`）、`brickkit init` 行为新增（`--hooks`）、新增 `internal/gitrepo`、`internal/config/edit.go` 加两个方法、design/004 §3.9 / §3.10 / §8.2 与附录、AI-CONTEXT.md、llms.txt、README

---

## 1. 背景与动机

`brickkit sync` 的整套设计建立在一个前提上：`components/` 在 `.gitignore` 里
（004 §8.2）。§3.9 甚至两次自我纠正，把「让 `git status` 变干净」从理由里划掉，
因为「项目的 `git status` 根本看不见它们」。

**但使用者会把 `components/` 从 `.gitignore` 里去掉**，两种动机都真实存在：

1. 组件源码要跟项目一起进版本库（组件不是独立仓库，就是这个项目的一部分）
2. 组件是独立仓库，但想在项目里留一份指针，clone 项目后能一次拉齐

一旦 `components/` 被项目仓库跟踪，`sync` 的整目录 `os.Rename` 就会在项目的
diff 里留下「整棵树搬家」。于是出现这个失误，而且反复出现：

> 本地为了聚焦，给几个顶层写了 `enabled: false`，跑 `sync` 归档；干完活提交时
> **忘了还原结构**，把归档后的目录结构提交了出去。队友 clone 下来，源码全在
> `components/.archived/` 底下。

这不是 `sync` 不好用，是 §3.9 的前提在这些项目里不成立，而平台在提交这个
时点上一句话都没说。本设计补的就是这一处。

## 2. 目标与非目标

**目标**

1. 在 `git commit` 这个时点上拦住「结构变了、而声明这个结构的 yaml 没跟着进提交」。
2. 提供一条命令，把 yaml 与目录结构一次还原到「跟最后一次提交一致」。
3. `components/` 仍在 `.gitignore` 里的默认项目，**零成本、零感知**。

**非目标**

1. 不改变 `sync` 的任何行为，也不给它加参数（§3.9「没有参数」的口径不动）。
2. 不做 `--dry-run`、不做卸载命令（删掉那个 hook 文件就是卸载）。
3. 不管嵌套 Git 仓库（gitlink）该不该存在——只在检查时附带一句提醒（§4.6）。
4. 不装 `pre-merge-commit`：merge 带进来的归档结构来自别人的提交，不是本地失误。

## 3. 已定决策

| 决策 | 选择 | 理由 |
| --- | --- | --- |
| 判据读哪份 yaml | **index（`git show :<配置文件>`）**，不是 HEAD | 读 HEAD 会造出真死锁：yaml 的改动永远比结构晚一拍，「改 yaml + 归档结构」一起提交这件事永远做不成 |
| 判据读哪份结构 | **index（`git ls-files --cached`）** | 读工作区就管到了没 `git add` 的东西，那些不进这次提交 |
| 判据方向 | **只拦一个方向**（该跑却归档），外加双向的「两处都有」 | 反方向只是「没跑过 sync」，§3.9 明说那是允许的；拦它等于强迫全员跑 sync |
| 强制力 | brickkit 装 `pre-commit` hook | 只有 hook 能在 `git commit` 这个时点上真的拦住 |
| 判据不通过时 | **硬拦（exit 1）**，把出路印出来 | 判据只在真出错时才响；那时候「问一句 y/n」只会训练人闭眼按 y。且 `exec < /dev/tty` 在 VS Code 源代码管理面板里打不开（实测），交互式提问在主力环境里根本问不出来 |
| 还原命令形状 | 独立命令 `brickkit restore` | `--help` 里看得见，hook 提示的那一条也更好记 |
| 还原 yaml 的做法 | **节点级只改 `enabled` 字段**，不 `git checkout` 整个文件 | `git checkout brickkit.yaml` 会把未提交的 `add` 一起吃掉，而且没有任何地方留着——§3.10 批评 `reset` 的正是这一点 |
| 安装入口 | `brickkit init --hooks` | `init` 本来就是「把项目骨架就位」的命令，且已有 `--no-skills` 的先例，命令表不胖 |
| hooks 目录怎么定位 | `git rev-parse --git-path hooks` | 实测它同时正确处理 worktree、submodule 和 `core.hooksPath`（husky / lefthook 设的）；手拼 `.git/hooks` 三种都错 |

## 4. 判据：`brickkit restore --check`

hook 调的就是它，人也能在 CI 里跑。**它回答一个问题：这次提交里，yaml 与目录结构自洽吗。**

### 4.1 读哪两份东西

| 要判的东西 | 从哪读 |
| --- | --- |
| 即将提交的 yaml | `git show :<配置文件相对仓库根的路径>`（index） |
| 即将提交的目录结构 | `git ls-files --cached --stage -- <components 相对仓库根的路径>` |

目录结构**只查一次**，`--stage` 带上文件模式，这一份结果同时供三处使用：判定用的
「谁在活跃 / 谁在归档 / 谁两处都有」、§4.4 的短路、以及 §4.6 的 gitlink 提醒
（`160000` 就在模式列里）。分成多次查询会让短路把 gitlink 提醒一起短路掉。

配置文件路径一律按 `layout.ConfigFile` 推导（`--config` 可以改名、可以给绝对路径），
用 `filepath.Rel(仓库根, ...)` 算相对路径，并把分隔符转成 `/`——git 只认 `/`，
Windows 上必须转。

用 `config.ParseConfig(data, source)` 解析 index 里那份字节（它已存在，不必新增入口）。

### 4.2 完整状态表

两个维度：**index 那份 yaml 的启停判定结果** × **index 里源码在哪**。
判定复用 `up` / `sync` 同一套（`cascade.Compute`），且**按组件 ID 归集**
（与 `focusFrom` 一致）——按条目判会让同 ID 一个版本跑、一个不跑的情形自相矛盾。

| 判定（index yaml） | index 结构 | 行为 | 理由 |
| --- | --- | --- | --- |
| 该跑 | 活跃 | 放行 | 自洽 |
| 该跑 | **归档** | **拦** | 唯一的主判据，就是那个失误 |
| 该跑 | **两处都有** | **拦（专用信息）** | 违反 004 §8.1「一个组件 ID 只有一个源码目录」 |
| 该跑 | 都没有 | 放行 | 源码没进仓库（默认 ignore 情形），管不着 |
| 不该跑 | 活跃 | 放行 | 只是「没跑过 sync」，§3.9 明说这是允许的 |
| 不该跑 | 归档 | 放行 | **意图声明**：`enabled: false` 一起进了提交，就是他要这个结构 |
| 不该跑 | **两处都有** | **拦（专用信息）** | 两处都有从来都是错的，与 yaml 说什么无关 |
| 不该跑 | 都没有 | 放行 | 同上 |
| yaml 里没声明 | 活跃 / 归档 / 两处 / 无 | **一律放行** | 判定算不到它。那是使用者自己在开发、还没 `add` 的源码——`planSync` 也是这个边界 |

路径匹配要同时接受「**等于**该路径」和「以该路径 + `/` 开头」：嵌套仓库在 index 里是
一条 `160000` 的 gitlink，路径**没有尾斜杠**（实测：`components/erp/backend`）。

### 4.3 「两处都有」为什么必须独立、双向、且优先

它不是主判据的一个特例，它是一个**死循环的解药**。

`git add components/.archived`（窄 pathspec）**不会**暂存旧活跃路径的删除，于是
index 里同一个组件出现两次（实测确认；`git add components/` 才会带上删除）。
使用者手工在两处各放一份也是同样的状态。

**它只管 yaml 里声明过的组件**，与 §4.2 最后一行同一个边界：没声明的源码平台一律
不管，两处都有也不管——那是使用者自己的事，`planSync` 也碰不到它。

而这种状态下 `workspace.Locate` 活跃优先 → `planSync` 判它「已经在该在的位置」→
**`restore` 跑完什么都没变，而 hook 还在拦，且没有任何出路。**

所以「两处都有」必须先判、单独报，并给出手工出路。立场跟 `workspace.move` 里那句
「两处都有源码时，平台不替你决定保留哪一份」完全一致——平台指出来，不替他删。

### 4.4 何时短路、何时放行、何时根本不跑

| 情形 | 行为 |
| --- | --- |
| `components/` 还在 `.gitignore` 里（默认） | 唯一那次 `git ls-files --cached --stage -- <components>/` 返回空 → **立刻退出 0**。零成本，所以 hook 可以无条件装给所有人 |
| `.archived/` 下只有空目录 | git 不跟踪空目录 → 同上短路 |
| 正在 merge / rebase 冲突中（`git ls-files -u` 非空） | 放行 + 警告。冲突中 `git show :<path>` 直接 `fatal: not at stage 0`（实测），且提交本来就被 git 拦着 |
| 配置文件没被 git 跟踪 | 放行。这项目没把配置交给 git，管不着 |
| 空仓库（还没有 HEAD） | index 照样读，判据照跑 |
| `--config` 指到仓库外面（`Rel` 结果带 `..`） | 放行 + 说明 |
| 全图解析失败（Manifest 缺失 / 需要联网） | **放行 + 警告。** hook 是抓一个特定失误，不是质量门；把提交堵死在一次网络错误上，代价远大于漏掉一次 |
| `brickkit` 不在 PATH | **放行 + 警告**（见 §6.4）。否则新人 clone 下来第一件事就是提交不了，原因还跟他要做的事毫无关系 |
| merge 提交 | 不跑（实测：git 对 merge 走 `pre-merge-commit`）。`--amend` 会跑；`rebase` / `cherry-pick` 不跑 |
| `git commit --no-verify` | 绕过。无解，文档写明 |

### 4.5 被拦时的输出

两条出路都给全，因为两种都可能是真意图：

```
❌ 提交被拦下：组件源码提交在归档目录里，但 brickkit.yaml 说它该启动

   people/basic     即将提交的位置：components/.archived/people/basic
                    而本次提交的 brickkit.yaml 里它会启动

   两条路，选一条：
     想保留这个归档结构 → git add brickkit.yaml
                          （yaml 里的 enabled: false 进了提交，就是你的意图声明）
     不想 → brickkit restore，然后重新 git add
```

「两处都有」的专用信息：

```
❌ 提交被拦下：同一个组件的源码在提交里出现了两处

   people/basic     components/people/basic
                    components/.archived/people/basic

   一个组件 ID 只能有一个源码目录（004 §8.1）。多半是 git add 的路径太窄，
   漏掉了旧路径的删除。

   确认留哪一份之后：git add -A components/
   两处都有源码时，平台不替你决定保留哪一份。
```

### 4.6 gitlink 的附带提醒（不影响退出码）

即将提交的 `components/` 下有 `160000` 条目时，附带一句：

```
⚠️  components/erp/backend 是一个嵌套的 Git 仓库（提交进去的只是一个指针）
    仓库里没有 .gitmodules，队友 clone 下来只会得到一个空目录，
    git submodule update 也拉不回来——没有地方记着它的 URL。
```

只提醒，不改退出码。它超出「结构还原」的职责，但和「把 `components/` 从
`.gitignore` 去掉」是同一个决定引出来的坑，而 §8.2 早就点过「会出现 Git
嵌套仓库的问题」。如实汇报，不扩大职责。

## 5. `brickkit restore`

### 5.1 yaml 部分：逐条处理，绝不 `git checkout` 整个文件

基准永远是 **HEAD**（`git show HEAD:<配置文件>`）。按 `(id, version)` 精确匹配：

| 条目 | 动作 |
| --- | --- |
| 工作区有、HEAD 有同 `(id, version)` | `enabled` 设成 HEAD 的值；HEAD 里没写 `enabled` 就把工作区这个字段**删掉**（004 §3.3：不写才是默认，写了才是「钉住」） |
| 工作区有、HEAD 没有该条目（本地新 `add` 的，或本地改了版本号） | **一个字不动 + 如实汇报**。这是「不吃掉未提交的 add」的解药 |
| HEAD 有、工作区没有（本地 `remove` 掉的） | **不动，绝不加回来。** `restore` 不是 `revert` |
| HEAD 里那个 `enabled` 不是普通标量（anchor / alias / 非布尔） | 跳过这一条 + 如实汇报。只照抄普通标量的值与 style |

改写走 `internal/config/edit.go` 的节点级编辑器——注释、字段顺序、空行、`${ENV}`
全部原样。需要给它加两个方法：设置与删除单个组件条目的 `enabled` 字段。

### 5.2 结构部分

yaml 落盘后，调用 `sync` **同一个**函数链（`syncFocus` → `planSync` → `applySync`）。
判定不复制，所以不可能与 `up` / `sync` 分叉——这正是 §3.9.2 删掉 `--only` 时守的那条线。

### 5.3 执行顺序与幂等

**顺序必须是：** 解析工作区 yaml → 在内存的 `*config.Config` 上还原 `enabled` →
解析全图、算出判定 → **判定算成功了才落盘 yaml** → 再移动目录。

顺序反了就会留下「yaml 改了、结构没动」的半成品：判定要解析全图，Manifest 缺失或
需要联网时会失败。按这个顺序，失败时 yaml 一个字没改。

`restore` 必须**幂等**：重跑一次仍是 HEAD 的值。移动目录中途失败时（撞上「目标目录
已存在」），重跑一次即可，与 `sync` 的现有行为一致。

### 5.4 前置条件（都要报清楚，不能悄悄什么都不做）

不在 git 仓库、没有 HEAD 基准（空仓库）、配置文件没被 git 跟踪、配置文件在仓库外、
工作区 yaml 非法、**HEAD 版 yaml 非法**（基准坏了）、正在冲突中——七种都拦下并说明。

另外两种必须拦：

- **`components/` 下有已暂存的改动。** 004 §3.9.3 明说允许直接在
  `components/.archived/<id>/` 下改代码。如果他 `git add` 了那些改动再跑
  `restore`，目录被 rename 走，index 里那些路径就变成「删除」，提交出去等于删文件。
  → 拦下，让他先提交或 `git reset`。
- **某个组件两处都有源码。** `planSync` 会判它「已在该在的位置」，什么都不做；
  不报出来就与 hook 形成死循环（§4.3）。

yaml 与 HEAD 完全一致不算错，照样往下跑结构整理——结构可能还是歪的。

### 5.5 输出

它不可逆（被覆盖的 `enabled` 没有第二份副本），所以**动手前把新旧值都印出来**，
使用者从 scrollback 里就能读回来。处理办法不是加 `--yes` 确认——那两三行本来就是
§3.9.2 教人 `git checkout` 掉的东西。

```
$ brickkit restore
📄 brickkit.yaml：按最后一次提交还原 enabled（其余改动未动）
   people/basic@1.2.0        enabled: false → 删除该字段（提交里没写）
   erp/backend@0.4.1         enabled: false → true
   demo/new@0.1.0            未动（这个条目在最后一次提交里不存在）
📂 工作区整理：
   📂 components/.archived/people/basic → components/people/basic
      原因：恢复启用
✅ 已还原（yaml 改动 2 处，1 个激活）
```

## 6. pre-commit hook

### 6.1 装在哪

`git rev-parse --git-path hooks` 的结果 + `/pre-commit`。实测这一条同时正确处理：
worktree（返回主仓库共享的那个）、submodule（`.git` 是文件）、以及
`core.hooksPath` 被 husky / lefthook 设过的情形。

### 6.2 冲突策略与标记行

hook 脚本首部带一行标记（含 CLI 版本）。已存在 `pre-commit` 时：

- 是 brickkit 写的（认标记行）→ 覆盖升级
- 不是 → **绝不覆盖**，报错并打印要插进去的那一行

脚本必须是 `#!/bin/sh`，**不用任何 bash 特性**——Windows 上 Git for Windows 用自带
的 sh 跑 hook。

### 6.3 一个 git 仓库里多个 brickkit 项目

hook 里存一个「项目根相对仓库根」的**路径列表**，逐个 `cd` 过去调 `--check`
（hook 的 cwd 是仓库根，git 保证）。`brickkit init --hooks` **幂等追加**：
同一个路径重复安装不重复加。

因此不需要给命令加 `--dir` 之类的新旗标。

### 6.4 找不到 brickkit 时

安装时把**当前可执行文件的绝对路径**写进 hook，回落 `command -v brickkit`，
两者都没有 → 警告并放行（exit 0）。

这条决定它在 VS Code 里能不能真的跑起来：GUI 客户端的 PATH 常常不含
`~/.local/bin`。

### 6.5 与 `init` 的集成

- `brickkit init`：有 `.git` 就装；没有就在输出末尾提示 `brickkit init --hooks`。
  `init` 常常跑在 `git init` **之前**，装不上是常态，不是错误
- `brickkit init --hooks`：只装 hook，不动别的（在已有项目里补装）。`init` 的
  `Args` 已是 `MaximumNArgs(1)`，不带项目名即可

## 7. 已知边界与接受的近似

1. **Manifest 取自工作区/缓存，不是 index。** 判据用 index 的 yaml + index 的结构，
   但依赖关系（`cascade` 要的图）来自工作区或缓存。要从 index 解析 Manifest，得把
   整个安装源层架在 git 对象之上，代价远超收益；而依赖关系与「这次提交的结构对不对」
   几乎无关。
2. **`--no-verify` 绕得过。** git 的机制如此，文档写明。
3. **`.git/hooks` 不进版本库。** 团队每人各装一次；`brickkit init --hooks` 就是那一次。
4. **使用者手工把源码塞进 `.archived/`、又没写 `enabled: false`，会被拦。** 这是
   有意为之的严格：他绕过了 sync，而出路很清楚（写 `enabled: false` 或跑 `restore`）。

## 8. 与既有设计口径的关系

| 既有口径 | 是否冲突 | 处理 |
| --- | --- | --- |
| §3.9「`sync` 没有参数」 | **不冲突** | 还原做成独立的 `restore`，`sync` 一个参数都不加 |
| §3.9.2「只有一条路：改 `enabled` 再 `sync`」 | **不冲突** | `restore` 换的是判定的**输入**（yaml），判定本身一个字没改。被删掉的 `--only` 是**另造一套判据**，那才会分叉 |
| §3.9「不包括 `git status`」 | **要补一句** | 那句话的前提是 `components/` 在 `.gitignore` 里。前提不成立的项目存在，§3.9 要点明这一点并指向本设计 |
| §3.10「没有 `brickkit reset`」 | **要写清区别** | `reset` 是把配置整体覆盖回某个快照，且自己不可逆；`restore` 只动 `enabled` 这一个字段、逐条动、其余改动一律不碰，并且把覆盖掉的旧值印出来。§3.10 反对的是「自建一套不如 git 的备份机制」，`restore` 恰恰是**用** git 当基准 |
| §8.2「`components/` 不提交」 | **保留建议，承认现实** | 建议不变，但要写明「去掉之后会怎样、平台在提交时点上做了什么」，并点出 gitlink 那个死记录 |

## 9. 实现落点

| 文件 | 内容 |
| --- | --- |
| `internal/gitrepo/`（新包） | git 查询的唯一入口：仓库根、index blob、`ls-files`、未合并条目、hooks 目录。不读使用者的全局配置（照 `workspace.gitOut` 的做法设 `GIT_CONFIG_GLOBAL=/dev/null`） |
| `internal/cli/restore.go`（新） | `newRestoreCommand`、`runRestore`、`runRestoreCheck` |
| `internal/cli/hooks.go`（新） | hook 脚本模板（`//go:embed`）+ 安装、标记行识别、路径列表幂等追加 |
| `internal/config/edit.go` | 加 `SetComponentEnabled` / `ClearComponentEnabled` 两个方法 |
| `internal/cli/init.go` | `--hooks` 旗标；装不上时的末尾提示 |
| `internal/cli/root.go` | 注册 `restore`（`groupProject`） |
| `internal/cli/sync.go` | 不改。`syncFocus` / `planSync` / `applySync` 原样复用 |

## 10. 测试要点

判据的 3 × 4 状态表**每一格一个测试**，另加：窄 pathspec 造出的「两处都有」、
gitlink 路径匹配（无尾斜杠）、冲突中放行、解析失败放行、配置文件改名、配置文件在
仓库外、同 ID 多版本按 ID 归集、空仓库。

`restore` 的每一条条目规则一个测试，另加：未提交的 `add` 不被吃掉、
`components/` 下有已暂存改动时拦下、判定失败时 yaml 一个字没改、幂等（连跑两次）。

hook 的安装：已有别人的 `pre-commit` 不覆盖、自己写的覆盖升级、`core.hooksPath`、
worktree、多项目幂等追加、找不到二进制时放行。

## 11. 拒绝清单

| 不做 | 理由 |
| --- | --- |
| hook 里交互提问 y/n | `exec < /dev/tty` 在 VS Code 源代码管理面板 / CI 里打不开（实测），主力环境根本问不出来；而判据只在真出错时才响，那时候「问一句」只会训练人闭眼按 y |
| 「只要 yaml 改了就问」这种简化判据 | yaml 最常见的改动是 `add` 加了个组件，那种提交完全正当却每次都被问；而「yaml 已还原、结构还归档着」它一声不响——恰好是最该拦的那种。噪音大又漏掉主场景的 hook，装上就是等着被卸 |
| 判据读 HEAD 的 yaml | 造出真死锁：yaml 的改动永远比结构晚一拍 |
| 拦反方向（源码活跃、yaml 说不跑） | 那只是「没跑过 sync」，§3.9 明说允许。拦它等于把 sync 从可选变成必跑 |
| 给 `sync` 加 `--restore` 参数 | §3.9「没有参数」是有理由的口径；独立命令在 `--help` 里也更好发现 |
| `restore --dry-run` | 动手前就把新旧值全印出来了，`--dry-run` 是同一份信息的第二个入口 |
| 卸载命令 | 删掉那个 hook 文件就是卸载 |
| 装 `pre-merge-commit` | merge 带进来的归档结构来自别人的提交，不是本地失误 |
| 管 gitlink 该不该存在（拦下来） | 超出「结构还原」的职责，而且会拦住那些确实想这么干、心里有数的人。只附带一句提醒 |

## 12. 要跟着改的文档

`design/004-CLI 设计.md`（§3.9 补前提、新增 `restore` 一节、§3.10 写清与 `reset`
的区别、§8.2 补「去掉之后会怎样」、附录的命令表）、`AI-CONTEXT.md`、`llms.txt`、
`README.md`、试用指南（加一段「组件源码要不要进项目仓库」）。
