# 源码管理

`components/` 里放着你克隆下来、或者正在写的组件源码。项目一大，这些源码里多数是你此刻根本不碰的：IDE 要索引它们，全局搜索、
`grep`、替你读代码的 AI 都要把它们扫一遍。这一篇的两条命令管的就是这个目录。

## `brickkit sync`

把这次**不启动**的组件的源码挪进 `components/.archived/`，把**要启动**、却躺在归档里的挪回来。

判据与 `up` 完全一样。所以想收窄工作范围，就在部署文件里把顶层组件关掉——下面只为它们而跑的组件跟着一起不跑，`sync` 再跟着走：

```yaml
# deploy.yaml
components:
  - id: demo/hello
    mode: disable
  - id: demo/bus
  - id: demo/caller
    mode: disable
  - id: demo/hello@1.0.0
```

```bash
brickkit sync
```

```text
📂 工作区整理：
   📦 components/demo/hello/               → components/.archived/demo/hello
      原因：显式禁用（mode: disable）
✅ 工作区整理完成（0 个活跃，1 个归档，0 个激活）
```

规则：

- **双向。** 该归档的归档，该激活的移回来。
- **只动目录。** 不影响运行中的容器，也不改变 `up` 会启动谁。
- **按组件判断。** 一个组件只要还有任何一个版本这次会启动，它的源码就留在活跃目录（上面只关掉 `demo/hello` 的默认版本时，
  `demo/hello@1.0.0` 还因为 `demo/caller` 在跑，源码就不会被归档）。
- **只管 `brickkit.yaml` 里声明过、且已有源码的组件。**
- **整个目录连 `.git` 一起搬。** 归档之后在里面照常用 git。
- **不提供 `--dry-run`。** 搞错了再执行一次就回来了。
- **不看焦点。** [焦点运行](04-focus-run.md)是一次临时的收窄，不是"你需要哪些源码"的声明：`sync` 按*没有*焦点时项目要跑的组件留源码，
  所以换焦点从来不会让目录搬来搬去。
- **只有一个 `components/`。** 某个组件的源码嵌在另一个组件的目录里时，`sync` 在挪任何东西之前就停下，列出每一份——
  在一份认不清归属的源码周围挪目录只会更乱。见[在项目里就地开发](04-focus-run.md#只有一个-components)。

`sync` 是单独的命令，不并进 `up`：`up` 管运行，`sync` 管目录。如果 `up` 会顺手挪动你的文件，你会困惑它们为什么突然不见了。
`up` 也从不需要组件源码——它只读 `component.yaml`，归档里的也找得到。

## `brickkit restore`

把 `deploy.yaml` 里各组件的 `mode` 还原成最后一次提交时的值，再让源码目录跟着走：

```bash
brickkit restore
```

```text
📄 deploy.yaml：按最后一次提交还原 mode（其余改动未动）
   demo/hello                 mode: disable → 删除该字段（提交里没写）
   demo/caller                mode: disable → 删除该字段（提交里没写）
📂 工作区整理：
   📂 components/.archived/demo/hello      → components/demo/hello/
      原因：恢复启用
✅ 工作区整理完成（0 个活跃，0 个归档，1 个激活）
```

它只动 `deploy.yaml` 的 `mode` 字段，逐条目处理：

| 条目 | 处理 |
| --- | --- |
| 工作区与最后一次提交里都有 | `mode` 回到提交里的值；提交里没写就删掉这个字段 |
| 工作区新增的（刚 `add` 的） | 一个字不动 |
| 提交里有、工作区里没有 | 绝不加回来——这不是 `git revert` |

其余改动都不动，被覆盖的旧值在动手之前打印出来。`deploy.local.yaml` 是你个人的文件、不进版本库，没有可还原的基准，保持原样。

## 提交前检查

默认情况下 `components/` 在 `.gitignore` 里：组件源码不进项目仓库，每个组件是它自己的仓库。有的团队想让源码跟项目一起提交——
把 `components/` 从 `.gitignore` 去掉就行。新项目的 `.gitignore` 里写着它，但它不在 `brickkit init` 要求必须有的条目里：
那些条目挡着个人文件和密钥，必须留着。

那样一来，`sync` 的目录移动就会出现在项目的 diff 里，而下面这件事迟早会发生：

> 关掉几个顶层组件 → `sync` 归档 → 忘了还原 → 提交。

提交里，组件源码躺在 `components/.archived/`，而 `deploy.yaml` 说它该启动。队友拉下来，一切都对不上。

提交前检查就是拦这件事的。项目根是 Git 仓库根时，`init` 已经顺带装好了；项目嵌在别人的仓库里时，显式安装：

```bash
brickkit init --hooks
```

```text
   🪝 .git/hooks/pre-commit 提交前检查组件结构
```

钩子在每次 `git commit` 时调用 `brickkit restore --check`：只检查、不改任何东西——即将提交的 `deploy.yaml` 与即将提交的目录结构是否自洽。
不自洽就以非零退出，提交被拦下，并告诉你是该把 `mode` 改回来，还是该把目录移动一起提交。找不到 `brickkit` 时钩子放行，不会把你卡住。
