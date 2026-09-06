# `components/` 登记为真正的 git submodule 时，`sync`/`restore --check`/`remove` 的行为缺口

- 日期：2026-09-06
- 状态：**问题已确认（代码级证据 + 最小复现），修复方向待评估**——不是已批准的设计，写法与决策由你定
- 发现于：`be-assembly-standard` 项目（brickKit 的真实使用方）在阶段一给第一个组件 `mdm/customer` 接子模块时
- 影响面（如果决定修）：`internal/gitrepo`（新增 `.gitmodules` 读取能力）、`internal/workspace`（`Archive`/`Activate`/`RemoveSource`/`RemoveArchived`）、`internal/cli/restore_check.go` 的 `warnGitlinks`、design/004 §3.9 / §8.2

---

## 1. 背景：这不是一个全新场景，是 004 §8.2 已经点名、但只写了一半的场景

`004-CLI 设计.md` §8.2 早就承认过"组件源码要跟项目一起进版本库是真实需求"，并记录了去掉 `components/` 的 `.gitignore` 之后会发生什么：

> 2. `--repo` clone 下来的组件自带 `.git`，被项目仓库跟踪时是一条 `160000` 的
>    **gitlink**。仓库里没有 `.gitmodules`，队友 clone 下来只会得到一个空目录，
>    `git submodule update` 也拉不回来——没有地方记着它的 URL。
>    `brickkit restore --check` 会在提交里出现 gitlink 时附带一句提醒（不影响退出码）

`2026-09-02-commit-gate-restore-design.md`（restore/restore --check 那份设计）的"非目标"里也明确写了：

> 3. 不管嵌套 Git 仓库（gitlink）该不该存在——只在检查时附带一句提醒（§4.6）。

这两处的共同前提是：**"gitlink 但没有 `.gitmodules`" 是唯一被考虑过的形态**。

但 `be-assembly-standard` 项目走的是第三条路，一条这两处都没设计过的路：**`git submodule add` 把 `.gitmodules` 正确建好**，也就是真正意义上的 git submodule（不是"意外产生的死 gitlink"）。这条路完全合理——组件仓库→精确 commit 的映射表正是它想要的东西——但目前 `sync`/`restore --check`/`remove` 都没有区分"这个 gitlink 有没有登记"，一律按"没有登记"的假设去处理，于是在这条路上出现三处新的、此前设计没覆盖到的行为缺口。

## 2. 三个具体缺口

### 2.1 `restore --check` 对已登记的 submodule 仍然误报

- 位置：`internal/cli/restore_check.go` 的 `warnGitlinks()`（约 244–258 行）
- 现状：只要 index 里有 mode `160000` 的记录就无条件打印"仓库里没有 `.gitmodules`"、"`git submodule update` 也拉不回来"——**不检查 `.gitmodules` 是否已经覆盖这个路径**
- 实测：`be-assembly-standard` 的 `.gitmodules` 里明明有 `components/mdm/customer` 的条目（`git submodule status` 也确认已初始化），每次 `git commit` 仍然收到这条警告
- 根因：`internal/gitrepo` 整个包没有任何解析 `.gitmodules` 的能力——`IndexEntry.IsGitlink()` 只看 index 的文件模式，答不出"这个 gitlink 有没有登记"这一问
- 影响：不影响退出码（不阻断提交），但会造成"狼来了"——已经正确处理过的项目每次提交都收到一条无意义的警告，容易让人对下面两条**真正**的问题掉以轻心

### 2.2 `sync` 的归档/激活会打断已登记的 submodule

- 位置：`internal/workspace/workspace.go` 的 `move()`（被 `Archive`/`Activate` 调用），核心是 `os.Rename(from.path, to.path)`
- 对"gitlink 但没有 `.gitmodules`"这种形态是安全的——反正没有登记信息要维护，整棵目录（含内嵌 `.git`）搬到哪都不影响它自己的历史
- 但对**真正登记过的 submodule**，移动之后：
  - `.gitmodules` 的 `path` 字段没有跟着变，仍指向旧路径
  - superproject 的 git index 仍在旧路径记录着那条 gitlink，`git status` 把旧路径显示为 `D`（删除），新路径显示为 `??`（未跟踪）
  - `git submodule status` 对该组件报 `-`（未初始化）——它按 `.gitmodules` 里的旧路径去找，找不到
  - **最危险的**：如果这时候有人（或 CI）顺手 `git add -A` 想"把这次归档提交掉"，git 不会把新路径识别成 gitlink，而是把子模块工作区里的每个文件按 `100644` 普通文件收进 superproject——子模块自己的独立版本历史从此和 superproject 脱钩，"组件仓库→精确 commit"这条映射当场作废，且**没有任何报错**
- 已用最小复现验证（见第 4 节），不是推测

### 2.3 `remove` 同样不清理 submodule 登记

- 位置：`internal/workspace/workspace.go` 的 `removeDir()`（被 `RemoveSource`/`RemoveArchived` 调用），核心是 `os.RemoveAll(loc.path)`
- 对真正的 submodule：删除工作目录后，`.gitmodules` 里的 stanza、superproject index 里的 gitlink 记录、以及 `.git/modules/<path>` 下的内部仓库数据都还留着——没有走 `git submodule deinit` + `git rm --cached` + 清理 `.git/modules/` 那一套，superproject 的 git 状态从此"引用了一个不存在的东西"，需要人工善后
- `be-assembly-standard` 目前只能靠一条运维禁令兜底："`brickkit remove` 前必须先 commit & push"——但那只解决"没推送的改动会丢"，不解决"删完之后 superproject 的 git 状态本身要手工清理"这一层

## 3. 共同根因

`internal/gitrepo` 包目前只能回答"index 里这一条是不是 gitlink"（`IsGitlink()`，靠 mode `160000`），完全不能回答"这个 gitlink 有没有在 `.gitmodules` 里登记、登记的 path 跟 index 里的路径是否一致"。三处缺口是同一个盲点的三种表现——一旦补上这个能力，三处修法都是它的直接消费者。

## 4. 最小复现（隔离环境，不涉及真实项目数据）

```bash
# 造一个"组件仓库"（裸仓库当远端，避免依赖网络）
mkdir comp-repo && cd comp-repo && git init -q -b main
echo "hello" > file.txt && git add -A && git commit -q -m "init"
cd .. && git clone -q --bare comp-repo comp-repo.git

# 造"装配仓库"，把组件登记为真正的 submodule
mkdir assembly && cd assembly && git init -q -b main
git -c protocol.file.allow=always submodule add -q ../comp-repo.git components/demo/widget
git commit -q -m "add widget submodule"

git submodule status
# a42edf8... components/demo/widget (heads/main)      ← 已正确初始化

# 模拟 workspace.Archive() 内部做的事：纯 os.Rename
mkdir -p components/.archived/demo
mv components/demo/widget components/.archived/demo/widget

git status --short
#  D components/demo/widget          ← 旧路径：删除
# ?? components/.archived/           ← 新路径：未跟踪，不是 gitlink

git submodule status
# -a42edf8... components/demo/widget                  ← 未初始化（按旧路径找，找不到）

cat .gitmodules
# path 仍是 components/demo/widget，没有跟着变

# 如果这时候顺手 git add -A（很自然的下一步）：
git add -A
git ls-files -s components
# 100644 ce01362... components/.archived/demo/widget/file.txt   ← 普通文件，不是 160000！
```

子模块的独立历史从这一刻起和 superproject 脱钩，且整个过程没有任何报错或警告。

## 5. 可能的修复方向（未决定，供评估）

1. **`gitrepo` 加一个"读 `.gitmodules`"的能力**。不需要手写解析器，`git config -f .gitmodules --get-regexp '^submodule\.'` 就能拿到全部 `path`/`url`，映射成 path→url。这是后面几条的共同基础设施。
2. **`restore_check.go` 的 `warnGitlinks` 改成条件性**：gitlink 路径不在 `.gitmodules` 映射里才报现在这句话；路径在但 URL 或 path 字段本身对不上时报另一句更准确的话。
3. **`sync`（`Archive`/`Activate`）与 `remove`（`RemoveSource`/`RemoveArchived`）检测到目标是"已登记的 submodule"时改用对应的 git 语义**（等价于 `git mv` 一个 submodule、`git submodule deinit` + `git rm --cached` + 清理 `.git/modules/`），未登记时保留现在的行为完全不变——这一条改动量最大，也最容易牵扯到"跨平台 git 版本差异"这类新坑，值得单独评估要不要做、做到什么程度（比如可以先只做"检测到风险时报错阻断并给出手工修复步骤"，而不是自动执行 git 操作）。
4. `004-CLI设计.md` §8.2、`2026-09-02-commit-gate-restore-design.md` 都需要补一段：把"track `components/` 且用 git submodule 正确登记"列为第三种明确支持（或明确不支持）的形态，而不是隐含在"没有 `.gitmodules`"那一种里。

**这条改动的价值面向所有把 `components/` 当 git submodule 用的 brickKit 使用者，不是 `be-assembly-standard` 专属的需求**——任何选择"组件仓库→精确 commit"这种更强审计轨迹的团队都会撞上同样三个缺口。要不要做、按第 5 节哪个方向做、做到多深，由你决定。
