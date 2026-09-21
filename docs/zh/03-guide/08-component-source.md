# 8. 管理组件源码

前面的教程里，`components/` 要么是空的，要么是从 `tests/components/` 拷进来的一份 Manifest。有一个很自然的问题一直没回答：**别人写的组件，我想看看它的源码、甚至改一改，怎么办？** 这一篇讲组件源码的整套生命周期：把它克隆下来、只留手边要动的那几个、改完推回去、用完删干净，以及源码跟着项目一起提交时怎么不出岔子。全部对着真实的 CLI 跑过，用的还是那两个最小夹具（[`demo/hello`](../../../tests/components/demo-hello/) 和 [`demo/caller`](../../../tests/components/demo-caller/)），只是这次把它们做成了两个 Git 仓库。

**前置条件：** 已经构建好 BrickKit CLI，装了 `git`。**不需要 Docker**——这一篇不启动任何容器，只用 `up --dry-run` 看判定。另外，建议在 BrickKit 仓库**外面**建一个空目录来做，因为后面要给项目 `git init`，嵌在别的仓库里会让两边的 Git 状态混在一起。

**先弄清一件事：`up` 只要 Manifest，从不碰组件的代码。** 对来自市场或 Git 源的组件，Manifest 来自 `.brickkit/manifests/` 里的缓存，`components/` 里有没有它的源码，`up` 根本不在乎；`components/` 只服务于开发——读源码、改源码、在 IDE 里调试（[第 3 篇](03-local-debugging.md)）。下面会遇到一个例外：由**本地**安装源提供的组件（`init` 默认配的 `local-dev` 指向的就是 `components/`）不走缓存，它的 `component.yaml` 每次都直接从那个目录重读（AGENTS.zh.md §2.3）。所以这一篇里绝大多数操作都不影响"这个项目能不能跑起来"，只影响"你手边有没有那份源码"——例外我们会在克隆完之后亲眼看一下。

## 准备：两个"远端"仓库

`--repo` 要克隆的是组件自己的 Git 仓库，所以得先有仓库可克隆。真实场景里它是 GitHub 上的一个仓库，或者组件发布到市场时登记的那个 `gitUrl`。这里没有网络也没有市场，就拿本地目录当"远端"：把两个夹具组件各做成一个**裸仓库**（只存历史、没有工作区的仓库，往里 `git push` 没有任何限制），再加一个空的，扮演你在 GitHub 上 fork 出来的那份。这一步跟 BrickKit 无关，只是搭个舞台：

```bash
export BRICKKIT_REPO=~/brickKit      # 改成你克隆本仓库的位置
mkdir workspace-lab && cd workspace-lab
mkdir remotes && cd remotes
for name in hello caller; do
  cp -r $BRICKKIT_REPO/tests/components/demo-$name $name
  git -C $name init -q -b main
  git -C $name add .
  git -C $name commit -q -m "demo/$name 1.0.0"
  git clone -q --bare $name $name.git
  rm -rf $name
done
git init -q --bare -b main hello-fork.git
cd ..
```

（如果 Git 抱怨没有配置用户名和邮箱，先 `git config --global user.name` / `user.email` 配一下。）`demo/caller` 强依赖 `demo/hello`，第 2 篇里见过这条依赖边。

接着建项目，把这两个仓库配成 `git` 类型的安装源：

```bash
mkdir workspace-demo && cd workspace-demo
brickkit init workspace-demo
```

输出和第 1 篇一样，这里略过。编辑 `brickkit.yaml`，让 `sources:` 变成下面这样——两个 `git` 源**排在** `init` 生成的 `local-dev` **后面**，这个顺序后面会用到：

```yaml
sources:
  - id: local-dev
    type: local
    path: ./components
  - id: hello-remote
    type: git
    url: ../remotes/hello.git
  - id: caller-remote
    type: git
    url: ../remotes/caller.git
```

一个 `git` 类型的安装源就是"一个 Git 仓库，根目录就是一份 `component.yaml`"（也支持一个仓库里按 `<scope>/<name>/component.yaml` 放好几个组件）——正是"一个组件一个仓库"的形状（AGENTS.zh.md §9.16）。装组件时，CLI 按声明顺序一个个问过去，谁有就用谁。

## `add` 默认不克隆源码

```bash
brickkit add demo/caller@1.0.0
```

```
📦 添加 demo/caller@1.0.0
   ├── Manifest ✅
   ├── 依赖 demo/hello@1.0.0 ✅ 已拉取（artifacts 1 个文件）
   └── artifacts ✅（1 个文件）
⚠️ 警告：弱依赖缺失：demo/bus@1.0.0
   影响组件：demo/caller@1.0.0
   原因：该组件在所有安装源中均未找到
   影响：该组件的环境变量 DEMO_BUS_ENDPOINT 不会被注入
   💡 弱依赖降级由组件自行处理；如需启用，请确认该组件已发布并可从安装源获取
✅ 已写入 brickkit.yaml（2 个组件）
📁 已下载 artifacts 到 .brickkit/artifacts/（2 个文件）
```

（那条弱依赖警告第 2 篇讲过：`demo/caller` 声明了一个可选依赖，这里没装。）

两个组件都加进来了，依赖也自动拉齐了，可是 `components/` 里什么都没有：

```bash
ls components              # 没有任何东西（里面只有一个隐藏的空目录 .archived/）
ls .brickkit/manifests
```

```
demo-caller-1.0.0.sig.json
demo-caller-1.0.0.yaml
demo-hello-1.0.0.sig.json
demo-hello-1.0.0.yaml
```

`add` 只拉 Manifest 和产物——这就够 `up` 用了。源码是**开发时**的需要，不是**安装时**的需要（AGENTS.zh.md §9.18）：一次 `add` 可能顺着依赖树拉进五六个组件，把它们的仓库全克隆下来又慢又占地方，而大多数时候你只是想*用*它们。

## `--repo`：把一个组件的完整仓库克隆下来

现在想看看 `demo/hello` 的源码：

```bash
brickkit add demo/hello@1.0.0 --repo --yes
```

```
ℹ️ demo/hello@1.0.0 已存在于 brickkit.yaml，--yes 已指定：直接刷新缓存
📦 添加 demo/hello@1.0.0
   ├── Manifest ✅
   └── artifacts ✅（1 个文件）
✅ 已刷新 demo/hello@1.0.0 的 Manifest 与 artifacts 缓存
📁 已下载 artifacts 到 .brickkit/artifacts/（1 个文件）
📁 已 clone 源码到 components/demo/hello/
💡 改了源码怎么推回去、以及之后怎么管这份源码，见 docs/zh/03-guide/08-component-source.md（英文版把 zh 换 en）
```

这里的 `--yes` 是因为 `demo/hello` 已经在 `brickkit.yaml` 里了：`add` 遇到已经加过的组件，会先问一句"是否刷新 Manifest 与 artifacts 缓存？[y/N]"，`--yes` 等于替你答了 y。在没有终端可以回答的地方（脚本、CI）不加 `--yes`，它会当作你答了 N——什么都不做，也不会克隆。**组件还没写进 `brickkit.yaml` 时没有这一问**，直接 `brickkit add demo/hello@1.0.0 --repo` 就行。

克隆下来的是一份**完整**的 Git 仓库：

```bash
ls -A components/demo/hello
```

```
.dockerignore
.git
Dockerfile
component.yaml
go.mod
main.go
main_test.go
openapi.json
```

有 `.git`，有全部历史，没有 `--depth` 之类的截断——因为你要能在里面提交、切分支、推送。BrickKit 只负责克隆这一下，之后 Git 怎么用（权限、fork、分支策略）是你自己的事：用户可能在 GitHub、GitLab、Gitee 或自建的 Gitea 上，CLI 不可能支持每一家的 fork 接口（AGENTS.zh.md §9.19）。

`--repo` 有两个前提：**这个组件得有 Git 地址**（来自 `git` 类型的安装源，或者市场上登记了仓库地址的开源组件；闭源组件没有仓库，会被明确拒绝，并告诉你不带 `--repo` 照样能正常安装使用），**而且它的源码不能已经在盘上**。第二个前提马上就能看到——对同一个组件再来一次：

```bash
brickkit add demo/hello@1.0.0 --repo --yes
```

```
ℹ️ demo/hello@1.0.0 已存在于 brickkit.yaml，--yes 已指定：直接刷新缓存
❌ clone 失败：目录已存在
   组件：demo/hello@1.0.0
   目录：components/demo/hello/
   原因：该目录已存在，可能包含你正在开发的组件源码
   建议：
   1. 如果是误操作，请先删除或重命名该目录
   2. 如果已有源码，无需再次 clone
```

它**绝不覆盖**：活跃目录里多半是你自己正在改的源码，平台不替你决定删不删。一个组件 ID 在 `components/` 下永远只有一份源码目录，同一个组件的多个版本共用它。

还有一件事值得知道：克隆完之后，这个组件就由排在最前面的 `local-dev` 认领了——`local-dev` 扫描的正是 `components/`，而源码现在就在那里。这不是问题，反而是想要的效果：从这一刻起，`components/demo/hello/` 就是你的工作副本，而且它的 `component.yaml` 不再走缓存，每次都从这里重读。亲眼看一下：把 `components/demo/hello/component.yaml` 里 `deployment.port` 从 8080 改成 9090，再看 `up --dry-run` 生成的依赖地址：

```bash
brickkit up --dry-run > /dev/null
grep -m1 DEMO_HELLO_ENDPOINT .brickkit/generated/docker-compose.yaml
```

```
      - DEMO_HELLO_ENDPOINT=http://demo-hello-1-0-0:9090
```

改动在下一次 `up` 就生效了，用不着重新 `add`。（试完把端口改回 8080——组件的代码还在监听 8080。）

## `--repo-all`：整棵依赖树

想要 `demo/caller` 和它依赖的所有开源组件的源码：

```bash
brickkit add demo/caller@1.0.0 --repo-all --yes
```

```
ℹ️ demo/caller@1.0.0 已存在于 brickkit.yaml，--yes 已指定：直接刷新缓存
📦 添加 demo/caller@1.0.0
   ├── Manifest ✅
   ├── 依赖 demo/hello@1.0.0 ✅ 已拉取（artifacts 1 个文件）
   └── artifacts ✅（1 个文件）
⚠️ 警告：弱依赖缺失：demo/bus@1.0.0
   影响组件：demo/caller@1.0.0
   原因：该组件在所有安装源中均未找到
   影响：该组件的环境变量 DEMO_BUS_ENDPOINT 不会被注入
   💡 弱依赖降级由组件自行处理；如需启用，请确认该组件已发布并可从安装源获取
✅ 已刷新 demo/caller@1.0.0 的 Manifest 与 artifacts 缓存
📁 已下载 artifacts 到 .brickkit/artifacts/（2 个文件）
   ⏭️ demo/hello             → 已有源码目录，跳过 clone
   ✅ demo/caller            → clone 完成（components/demo/caller/）
📁 已 clone 1 个开源组件仓库（跳过 1 个，理由见上）
```

最后三行是重点。`--repo-all` 是批量操作：依赖树里每个组件，能克隆的克隆，不能的**逐个说明理由再跳过**，不会因为一个组件就整批失败——所以这里 `demo/hello` 被跳过（上一节刚克隆过），`demo/caller` 克隆成功。跳过的理由一共三种，每一行 ⏭️ 会告诉你是哪一种：闭源组件（没有仓库）、来源没有记录 Git 地址、源码已经在盘上。汇总行不去断言某一个具体理由，正是因为上面每一行已经分别说清了。

## 改了源码，怎么推回去

`demo/hello` 现在是一份普通的 Git 仓库，改它就是平常的 Git 工作流。走哪条路取决于你对原仓库有没有 push 权限。

**有 push 权限**（比如公司内部的仓库）：开个分支，改，提交，推：

```bash
git -C components/demo/hello checkout -b feature/greeting
echo '// 换一句问候' >> components/demo/hello/main.go
git -C components/demo/hello commit -am "调整问候语"
git -C components/demo/hello push origin feature/greeting
```

**没有**（比如公共的开源仓库）：先在 GitHub / GitLab 上 fork 一份（在浏览器里点，对应这里那个空的 `hello-fork.git`），再把你的 fork 加成**另一个** remote，推到它上面。原来的 `origin` 留着，以后还能从上游拉更新：

```bash
git -C components/demo/hello remote add myfork "$PWD/../remotes/hello-fork.git"
git -C components/demo/hello push myfork feature/greeting
```

（只想彻底转到自己的 fork、不再关心上游，就把 `origin` 换掉：`git remote set-url origin <你的 fork 地址>`。）

改完想让别人（或者你自己的别的项目）用上这个改动，就是发布一个新版本的事了：改 `component.yaml` 里的版本号、构建新镜像、`brickkit publish`（[第 9 篇](09-marketplace.md)）；已经在用旧版本的项目怎么升级，是[第 5 篇](05-upgrades-and-versions.md)讲的。

## 只留手边要动的：`enabled` 和 `sync`

`components/` 会越长越大。一个 50 个组件的项目里，你这会儿真正在看的可能就两三个。`brickkit sync` 让 `components/` 里只留那几个：把这次**不会启动**的组件源码收进 `components/.archived/`，需要的搬回来。"这次会启动谁"的判据跟 `up` 完全一样，所以你控制它的方式也一样——改 `enabled`（[第 2 篇](02-what-runs.md)）。

把 `demo/caller` 关掉，给它加一行 `enabled: false`：

```yaml
components:
  - id: demo/hello
    version: 1.0.0
  - id: demo/caller
    version: 1.0.0
    enabled: false
```

```bash
brickkit up --dry-run
```

下面只截了判定那一段：

```
📋 组件状态计算：
   ⬜ demo/hello@1.0.0   不启动（上层都不启动）
   ⬜ demo/caller@1.0.0  显式禁用（enabled: false）
```

`demo/hello` 也跟着不启动了——它只被 `demo/caller` 需要，"跟着上层走"。现在让 `sync` 把这两个的源码都收起来：

```bash
brickkit sync
```

```
📂 工作区整理：
   📦 components/demo/caller/              → components/.archived/demo/caller
      原因：显式禁用（enabled: false）
   📦 components/demo/hello/               → components/.archived/demo/hello
      原因：不启动（上层都不启动）
✅ 工作区整理完成（0 个活跃，2 个归档，0 个激活）
```

```bash
ls components                  # 空的：.archived 以点开头，默认不显示
ls -A components
ls components/.archived/demo
```

```
.archived
caller
hello
```

几件事值得知道：

- **搬的是整个目录，连 `.git` 一起。** 归档后的组件照样能用 Git（`git -C components/.archived/demo/hello log`），IDE 也照常打开；`up` 也照样读得到它的 `component.yaml`。归档只改变"看不看得见"，不改变"取不取得到"。
- **`sync` 从不碰运行中的容器，也不改变 `up` 会启动谁**——它只搬目录。
- **没有 `--dry-run`。** 搬错了，改回 `enabled` 再跑一次就换回来了。
- **它是一个独立的命令，故意没有并进 `up`。** `up` 管运行，`sync` 管源码目录；要是 `up` 顺手搬文件，你会奇怪文件怎么自己动了（AGENTS.zh.md §9.17）。
- 括号里三个数的意思依次是：本来就在原位的、这次收起来的、这次搬回来的。

现在你只想看 `demo/hello` 的源码。给它钉上 `enabled: true`——"不管上层怎么样，它都要跑"：

```yaml
components:
  - id: demo/hello
    version: 1.0.0
    enabled: true
  - id: demo/caller
    version: 1.0.0
    enabled: false
```

```bash
brickkit up --dry-run
```

```
📋 组件状态计算：
   ✅ demo/hello@1.0.0   启动（enabled: true）
   ⬜ demo/caller@1.0.0  显式禁用（enabled: false）
```

```bash
brickkit sync
```

```
📂 工作区整理：
   📂 components/.archived/demo/hello      → components/demo/hello/
      原因：恢复启用
✅ 工作区整理完成（0 个活跃，0 个归档，1 个激活）
```

`demo/hello` 搬回来了，`demo/caller` 继续留在归档里。等你想要全部回来，把两个 `enabled` 都删掉（回到"不写"，也就是跟着上层走），再跑一次：

```bash
brickkit sync
```

```
📂 工作区整理：
   📂 components/.archived/demo/caller     → components/demo/caller/
      原因：恢复启用
   ✅ components/demo/hello/               活跃
✅ 工作区整理完成（1 个活跃，0 个归档，1 个激活）
```

## `remove`：删得干净，也不让你误删

`brickkit remove` 的语义是"彻底移除"：从 `brickkit.yaml` 里去掉、清缓存、**连源码目录一起删掉**（AGENTS.zh.md §9.20）。留一份没人认领的源码在盘上，比删干净更糟。可删源码是不可逆的，所以它在动手之前，有两道关要过。

**第一道：还有人依赖它吗？** `demo/caller` 强依赖 `demo/hello`，直接移除 `demo/hello`：

```bash
brickkit remove demo/hello
```

```
❌ 无法移除 demo/hello
   版本：1.0.0
   以下组件强依赖它：demo/caller@1.0.0
   建议：请先移除依赖方
```

**第二道：源码删掉之后，还找得回来吗？** 它问的只有一件事——这些字节在别的地方还有没有。有三种情况会拦下：不是 Git 仓库（没有别的副本）、有未提交的改动、有提交还没推到任何远端。在 `demo/caller` 的源码里改一点东西，不提交，然后试着移除它：

```bash
echo '// 我正在改这里' >> components/demo/caller/main.go
brickkit remove demo/caller
```

```
❌ 错误：源码删掉就找不回来了
   组件：demo/caller@1.0.0
   目录：components/demo/caller/
   原因：有未提交的改动（含未跟踪的文件）
   建议：
   1. 先把它保住：提交并推到远端，或者把这个目录拷走 / 改名
   2. 确认不要了就加 --force：brickkit remove demo/caller --force
   3. 只是暂时不用的话，给它写 enabled: false 再 brickkit sync——那会把源码收进归档目录，而不是删掉
```

提交了，但没推：

```bash
git -C components/demo/caller commit -am "wip: 调整 caller"
brickkit remove demo/caller
```

```
❌ 错误：源码删掉就找不回来了
   组件：demo/caller@1.0.0
   目录：components/demo/caller/
   原因：有提交还没推到任何远端
   ...
```

（后面的建议和上一条一样。）推上去，再移除：

```bash
git -C components/demo/caller push origin main
brickkit remove demo/caller
```

```
✅ 已移除 demo/caller@1.0.0
   🗑️ 已删除源码目录 components/demo/caller/
   🗑️ 已清理 Manifest 缓存
   🗑️ 已清理 artifacts 缓存
```

这一次没有任何东西被拦——一份干净的、全部推上去了的克隆，删掉不丢任何东西。上面那条报错里的三条出路，也是这一节的要点：**保住它**（提交并推），**确认不要了**（`--force`），或者**只是暂时不用**——那就别删，`enabled: false` 加 `sync`，源码收进归档，一点不丢。

归档里的那份也会一起删。给 `demo/hello` 写上 `enabled: false`，跑 `brickkit sync` 把它收进归档（输出和前面一样，略过），再移除它：

```bash
brickkit remove demo/hello
```

```
✅ 已移除 demo/hello@1.0.0
   🗑️ 已删除归档源码目录 components/.archived/demo/hello
   🗑️ 已清理 Manifest 缓存
   🗑️ 已清理 artifacts 缓存
```

`remove` 必须连归档目录一起清：组件已经不在 `brickkit.yaml` 里了，`sync` 就再也不认识它，那份归档的源码就成了永远没人回收的孤儿。

最后一种拦截：如果源码是项目仓库里登记过的 **git submodule**（团队把组件源码挂进项目仓库的一种常见做法），`remove` 也会拒绝直接删——只删工作目录，`.gitmodules`、索引里的记录和 `.git/modules/` 下的数据都会留成悬空的，之后 Git 会一直引用一个不存在的东西。它不替你动这些，而是把等价的手工步骤原样告诉你：

```bash
brickkit remove demo/hello
```

```
❌ 错误：无法删除组件源码——它是一个已登记的 git submodule
   组件：demo/hello
   路径：components/demo/hello/
   原因：直接删除工作目录不会清理 .gitmodules、superproject 索引里的 gitlink 记录、以及 .git/modules/ 下的内部仓库数据，git 状态会从此引用一个不存在的东西
   建议：
   1. 手工执行：git submodule deinit -f -- components/demo/hello/
   2. 再执行：git rm -f components/demo/hello/
   3. 需要彻底清理时：rm -rf .git/modules/components/demo/hello/
```

这一道**不受 `--force` 影响**：`--force` 是"数据会不会丢"这类风险的出路，而 submodule 这道拦的是"Git 的账会不会乱"，强行跳过只会把账搞得更乱。

## 源码跟着项目一起提交：`restore` 和提交钩子

到现在为止，`workspace-demo` 是"一个组件一个仓库"的形状：`init` 追加进 `.gitignore` 的规则把 `components/` 整个忽略了，项目仓库只管 `brickkit.yaml`，源码在各自的仓库里。有的团队想要另一种：**组件源码跟着项目一起提交**，一次提交带上代码和配置。这时 `sync` 的目录搬运会直接出现在 `git status` 里（一大片 `D` 和 `??`），也就引出了一个反复发生的失误：本地关掉几个组件、跑了 `sync`，然后只把源码提交了，`brickkit.yaml` 里的 `enabled` 没跟着——仓库里成了一个"配置说它该跑，源码却躺在归档目录里"的矛盾状态，队友一拉下来就懵了。提交钩子和 `restore` 就是专门堵这个失误的。

另起一个项目。这次先 `git init`，**再** `brickkit init`：

```bash
cd ..
mkdir shared-src && cd shared-src
git init -q -b main
brickkit init shared-src
```

```
✅ 项目已初始化：shared-src
   📁 brickkit.yaml        项目配置
   📁 components/          组件源码（已配为本地安装源 local-dev）
   📁 .brickkit/           CLI 工作目录
   📁 .claude/skills/      AI 助手技能（4 个）
   📁 AGENTS.md            AI 助手项目导读
   🪝 .git/hooks/pre-commit 提交前检查组件结构
...
```

多出来的那一行 🪝 就是新东西：`init` 发现项目根**就是**仓库根，顺手装了一个 pre-commit 钩子。顺序在这里有用——先 `git init` 再 `brickkit init`，钩子才会自动装上；反过来也行，事后用 `brickkit init --hooks` 补装。项目嵌在别人的仓库里时也必须用 `--hooks`：`init` 不替你往别人的仓库里写东西。

按"源码跟项目走"的做法，把 `.gitignore` 里这两行删掉：

```
# 组件源码目录（每个组件是独立的 Git 仓库，不提交到项目仓库）
components/
```

然后把两个夹具组件的源码放进 `components/`——这次是普通文件，不是克隆——加进项目，提交：

```bash
mkdir -p components/demo
cp -r $BRICKKIT_REPO/tests/components/demo-hello components/demo/hello
cp -r $BRICKKIT_REPO/tests/components/demo-caller components/demo/caller
brickkit add --local
git add -A
git commit -m "初始：hello 与 caller"
```

这次提交时钩子也跑了，但一切自洽，它一声不吭。

**现在来制造那个失误。** 你想专心改 `demo/hello`：把 `demo/caller` 关掉，`demo/hello` 钉住，跑 `sync`：

```yaml
components:
  - id: demo/hello
    version: 1.0.0
    enabled: true
  - id: demo/caller
    version: 1.0.0
    enabled: false
```

```bash
brickkit sync
echo '// 调整 hello 的问候' >> components/demo/hello/main.go
git add components/          # 只想提交源码——却把 sync 造成的搬运也一起带上了
git commit -m "调整 hello 的问候"
```

```
❌ 提交被拦下：组件源码提交在归档目录里，但 brickkit.yaml 说它该启动
   demo/caller：即将提交的位置：components/.archived/demo/caller
   建议：
   1. 想保留这个归档结构 → git add brickkit.yaml（yaml 里的 enabled: false 进了提交，就是你的意图声明）
   2. 不想 → git reset components/ && brickkit restore，然后重新 git add
```

钩子看的是**即将提交的内容**（暂存区），不是你的工作区：暂存区里 `brickkit.yaml` 没变（没有 `enabled: false`，也就是"它该跑"），可 `demo/caller` 的源码却在归档目录里——矛盾，拦下。它给了两条出路：

**出路一：这就是你要的。** `git add brickkit.yaml`，让 `enabled: false` 和归档结构一起进这次提交。两者自洽，钩子放行——那是你有意收窄了范围，平台没有立场替你改主意。

**出路二：那是个失误。** 取消暂存，然后 `brickkit restore`：

```bash
git reset -q components/
brickkit restore
```

```
📄 brickkit.yaml：按最后一次提交还原 mode（其余改动未动）
   demo/hello@1.0.0           mode: enabled → 删除该字段（提交里没写）
   demo/caller@1.0.0          mode: disable → 删除该字段（提交里没写）
📂 工作区整理：
   📂 components/.archived/demo/caller     → components/demo/caller/
      原因：恢复启用
   ✅ components/demo/hello/               活跃
✅ 工作区整理完成（1 个活跃，0 个归档，1 个激活）
```

`restore` 把每个组件的 `enabled` 还原成最后一次提交时的值（提交里没写，就把这个字段整个删掉），再让源码目录跟着走，判据和 `sync` 一样。要记住它的边界：

- **只动 `enabled` 这一个字段。** 要覆盖的旧值会在动手之前先打印出来（上面开头那两行）。
- 新 `add` 的、改了版本号的条目，**一个字不动**。
- 提交里有、工作区没有的，**绝不加回来**——它不是 `git revert`。

你对 `demo/hello` 源码的改动完好无损，就在 `components/demo/hello/main.go` 里。重新 `git add components/`，这一次钩子放行：

```bash
git add components/
git commit -m "调整 hello 的问候"
```

`brickkit restore --check` 就是钩子调用的那一条：只检查、不改任何东西，不自洽就以非零状态退出——所以你也可以把它接进 CI，在合并前再拦一次。

钩子只拦**一个方向**：源码在归档目录、而 yaml 说它该跑。反方向——yaml 说不跑、源码还在活跃目录——只是"还没跑过 `sync`"，而 `sync` 本来就是可选的，拦它等于强迫全员都跑 `sync`。想卸载钩子，删掉 `.git/hooks/pre-commit` 就行；升级 CLI 之后想刷新它（里面写死了 `brickkit` 二进制的路径和版本），再跑一次 `brickkit init --hooks`。

## 常见的坑

| 你看到的 | 是怎么回事 | 怎么办 |
| --- | --- | --- |
| `add … --repo` 打印"已取消，brickkit.yaml 未修改"，什么都没克隆 | 组件已经在 `brickkit.yaml` 里，`add` 问了"是否刷新缓存"，没有终端时当作答了 N | 加 `--yes` |
| `--repo` 说"clone 失败：目录已存在" | `components/<scope>/<name>/` 里已经有源码：克隆过，或者是你自己手写的 | 直接用它。确实想重新克隆，先把那个目录挪走 |
| `--repo` 说"源码已经在了，只是被归档着" | 源码被 `sync` 收进了 `.archived/` | 让它回来：改 `enabled`，再跑 `brickkit sync` |
| `--repo` 说"clone 失败：该组件为闭源组件" | 闭源组件没有 Git 仓库 | 去掉 `--repo`，照常 `add` 就能用 |
| `sync` 之后 `git status` 一大片 `D` 和 `??` | `components/` 被项目仓库跟踪时，归档就是目录搬运，会进 diff | 预期之中。把 `enabled` 和搬运一起提交，或者 `brickkit restore` 撤回 |
| 提交被钩子拦下 | 归档的源码进了提交，而 `brickkit.yaml` 说它该跑 | 按提示：`git add brickkit.yaml`，或者 `git reset components/` 再 `brickkit restore` |
| `remove` 说"源码删掉就找不回来了" | 源码不是 Git 仓库、有未提交的改动，或有提交没推到任何远端 | 提交并推；或拷走；或 `--force`；或者只是暂时不用，改用 `enabled: false` 加 `sync` |
| `remove` 说"它是一个已登记的 git submodule" | 直接删会让 `.gitmodules` 和索引悬空 | 照提示手工执行那几条 git 命令；`--force` 对它无效 |
| 想把归档的组件拿回来 | | 改 `enabled`，再跑 `brickkit sync`；或者直接进 `components/.archived/<scope>/<name>/` 用，Git 命令和 IDE 都照常 |

## 一张对照表

| 想做的事 | 用什么 |
| --- | --- |
| 看、改某个组件的源码 | `brickkit add <组件> --repo` |
| 拿到整棵依赖树的源码 | `brickkit add <组件> --repo-all` |
| 让 `components/` 里只留手边要动的 | 改 `enabled`，再 `brickkit sync` |
| 彻底不要某个组件了 | `brickkit remove <组件>` |
| 撤回一次没提交的收窄 | `brickkit restore` |
| 提交前检查结构是否自洽 | `brickkit restore --check`（钩子自动调用） |

---

下一篇：[从市场发布与安装](09-marketplace.md)——把一个组件发布到市场，再从市场上装它，而不是用本地源。
