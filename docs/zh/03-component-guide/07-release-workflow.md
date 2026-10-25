# 发布

在 Git 源上，**发布一个版本就是往组件自己的仓库推一个 tag**。使用方的 CLI 按 tag 找版本：tag `0.1.0` 就是版本 0.1.0。
`brickkit release` 替你做这件事，并在动手之前把会出错的情况全部挡住。

（发布到组件市场是另一条命令 `brickkit publish`，见 [CLI 命令参考](../07-cli-reference/README.md#brickkit-publish)。）

## 流程

```bash
cd demo-quote
brickkit release
```

```text
✅ 已发布 demo/quote@0.1.0：tag 0.1.0 已推送
```

它做了这些：

1. **读版本。** 取 `component.yaml` 的 `metadata.version`——版本号只有这一个来源。
2. **校验 `component.yaml`。** 解析得了、字段合法，才谈得上发布。写了 `release.goModule` 时，旁边的 `go.mod` 也要与版本对得上
   （见[下文](#go-模块再打一个-go-的-tag)）。
3. **检查工作区。** 组件目录里不能有未提交的改动。
4. **检查分支。** 当前分支要有上游，而且没有未推送的提交：tag 指向的提交必须已经在远端的历史里。
5. **检查 tag。** 这个 tag 在本地和远端都还不存在。写了 `release.goModule` 时，Go 的 tag 也一样。
6. **跑你自己的检查。** `component.yaml` 声明了才有（见[下文](#你自己的检查releasechecks)）。
7. **打 tag、推送。** 给了[发版说明](#发版说明)时，打的是带着说明的带注释 tag。

前面每一步都通过，才会写任何东西。

## 会被挡下的情况

**有未提交的改动：** tag 装的是一次提交，不是你的工作区——你没提交的那点改动不会跟着发布。

```text
❌ 错误：demo/quote@0.1.0 有未提交的改动
   目录：.
   改动：M main.go
   建议：先提交（或丢弃）它们——tag 装的是提交，不是你的工作区
```

**分支还没推送过：** 使用方从远端取 tag；一个只在你本机的提交，别人取到的 tag 会指向一个他拿不到的东西。

```text
❌ 错误：发布 demo/quote@0.1.0 的这个分支没有上游
   分支：main
   建议：推送分支并设好上游：git push -u origin main
```

**这个版本已经发布过：**

```text
❌ 错误：demo/quote@0.1.0 已经发布过（tag 0.1.0 就在当前提交上）
   建议：发布过的版本不再改动：提高 component.yaml 里的 metadata.version，提交、推送后再发布
```

发布出去的版本不再变：已经有人按 0.1.0 装了它，同一个版本号指向两份不同的代码，就再也说不清谁跑的是哪一份。改了东西就提版本号。

## 你自己的检查：`release.checks`

一个版本发出去之前要过哪些关——测试、对运行中服务跑的一致性套件、你们项目自己的门禁——每个组件不一样，只有你知道。
写进 `component.yaml`，每次发布都会跑，不管是谁、在哪台机器上敲的 `brickkit release`：

```yaml
release:
  checks:
    - [go, test, ./...]
    - [./scripts/conformance.sh]
```

每条检查是写成数组的一条命令：程序，然后是参数，一项一个。在组件目录下执行，不经过 shell（没有管道、没有 `&&`，要用就写进脚本），
环境变量是你终端里的那一份；程序名里带 `/` 的，相对组件目录。它们在平台自己的检查之后按顺序跑，输出实时显示，
第一条非零退出就停止发布，什么都还没打：

```text
🧪 demo/quote@0.1.0 的发布前检查：go test ./...
ok  	example.com/quote	0.004s
🧪 demo/quote@0.1.0 的发布前检查：./scripts/conformance.sh
conformance: 12 cases, 1 failed (GET /quotes/42 returned 500)
❌ 错误：demo/quote@0.1.0 的一条发布前检查没有通过：./scripts/conformance.sh 以退出码 1 退出
   目录：.
   建议：
   1. 它的输出就在上面。没有打 tag、没有上传；按它说的改好、提交，再发布一次
   2. 不跑检查直接发布：--skip-checks（输出里会写明跳过了）
```

`--skip-checks` 不跑它们直接发布，并且会说出来：

```text
⚠️  跳过了 demo/quote@0.1.0 的发布前检查（--skip-checks）
✅ 已发布 demo/quote@0.1.0：tag 0.1.0 已推送
```

**检查跑完，组件目录得和跑之前一样。** tag 装的是提交。检查要是改写了 Git 跟踪的文件——直接改而不是只报告的格式化器、
代码生成器、会更新 `go.mod` 的构建——那么检查过的内容就不是提交里的内容，tag 发出去的是没人检查过的东西。所以最后一条
检查跑完之后，`release` 会再看一遍：组件目录里仍然没有未提交的改动，当前提交还是检查开始时的那一个。不是的话，不打 tag：

```text
🧪 demo/quote@0.1.0 的发布前检查：go test ./...
ok  	example.com/quote	0.004s
🧪 demo/quote@0.1.0 的发布前检查：./scripts/conformance.sh
conformance: 12 cases, all passed
❌ 错误：发布前检查跑完之后，demo/quote@0.1.0 有未提交的改动
   目录：.
   改动：M go.mod
   建议：没有打 tag：tag 装的是提交，而检查时用的文件不在提交里。把检查改动的文件提交、推送，再 release 一次；或者让检查别改文件——它必须留下的产物（构建输出、覆盖率报告）写进 .gitignore
```

两条出路：把检查改动的文件提交、推送，再发布一次（检查会对着那个提交重跑）；或者让检查只报告、不动手改（用 `gofmt -l`，
不用 `gofmt -w`）。新出现的文件也算改动，和跑检查之前的规则一样——检查必须留下的产物（构建输出、覆盖率报告）写进
`.gitignore`。检查自己做了提交，同样会被拦下：检查过的提交已经不是当前提交了。

按开源发布的版本（组件目录有 `origin`，或 `--source-type git`）要先 `brickkit release` 才能发到市场：市场记下仓库与这个版本的 tag，
`add --repo` 克隆之后检出它。tag 不在远端、或不在要发布的这个提交上时，`publish` 在任何东西到达市场之前就停下。按闭源发布
（`--source-type registry`）不需要 tag。

`brickkit publish` 在任何东西到达市场之前跑同样的检查，也认同一个 `--skip-checks`。它跑完检查之后不再看目录：
它上传的东西（`component.yaml` 和文档）在检查开始之前就读好了。

**为什么写在 `component.yaml`，为什么平台不替你决定。** tag 一推出去、版本一传上市场，就收不回来了；而 `release` / `publish`
是每一种发布方式都绕不开的那一步。Git 钩子、`make` 目标只守得住一台机器、一种习惯；写进 `component.yaml` 的检查跟着组件走。
查什么仍然是你的决定：平台只跑命令、看退出码，别的什么都不做。只有在组件自己的目录里发布时才会跑——项目里 `add`、`fetch`
读的也是这份 `component.yaml`，但从不执行里面的任何东西。

## 要么完整做完，要么不留痕迹

打完 tag、推送失败（网络断了、没有推送权限）时，`release` 把本地刚打的 tag 删掉。否则下次重试会被"tag 已存在"挡住，
而那个 tag 其实从没到过远端。写了 `release.goModule` 时，两个 tag 用一次原子推送（`git push --atomic`）推上去：要么都到远端，
要么都没到，推送失败时两个都删掉。

## 发一个新版本

版本号在动手之前提，不是改完再提：先改 `component.yaml` 的 `metadata.version`，在项目里 `brickkit upgrade <id>@<新版本>`，
再改代码、测（见[在项目里就地开发](../02-project-guide/04-focus-run.md#版本往前走)）。发布之前，这个新版本想重建几次就重建几次。
改完才提的话，中途 `brickkit build` 看到同版本的镜像已经在，就跳过，`up` 跑的是改之前的代码，不会有任何提示。

改完、测完，发布：

```bash
# 1. component.yaml 里已经是 version: 0.2.0（契约的 info.version 一起改）
# 2. 提交、推送
git commit -am "demo/quote 0.2.0：……"
git push
# 3. 发布，带上改了什么（见下面的"发版说明"）
brickkit release --notes-file ../notes-0.2.0.md
```

提哪一位，按版本规则：组件的 `AGENTS.md` 或项目 `AGENTS.md` 的约定写了版本规则（例如开发阶段每次只提最后一位），照它；
都没写，按语义化版本：只改了内部实现、接口没变，提修订号（0.1.0 → 0.1.1）；加了接口或配置项、旧的调用方照样能用，提次版本号；
删了接口、改了已有字段的含义，提主版本号——使用方升级时要自己改代码。平台自己只要求两件事：版本号是三段数字（`1.2.0`，
不带 `v`，也不带 `-alpha` 这样的后缀）；新版本比旧版本大——平台按数字比较版本，同一组件几个版本的迁移按版本号先后串起来跑，
`upgrade` 按它列出跨过的发版说明。

## 发版说明

版本号只说明"变了"，发版说明讲"变了什么"——最要紧的是使用方升级时得做什么：哪个配置键的含义变了、哪个接口没了。
它是你交给 `release` 的几行 Markdown；以后每个跨过这个版本升级的项目，都会在任何东西被改动之前先读到它。

可写可不写。不写，版本照样发布，打的是轻量 tag（只是一个指向提交的名字）；写了，`release` 打的是带注释的 tag（自己带一段
说明文字的 tag），说明就放在里面，原样保留：`#` 开头的行也留着——Git 默认会把它们当注释删掉，Markdown 的标题就跟着没了。

说明文件写在**组件目录外面**：组件目录里一个没提交的文件，哪怕是新建的，也会被上面第 3 步的工作区检查挡下。

```markdown
## 不兼容的改动

- `QUOTE_TTL` 的单位从分钟改成了秒：把原来的值乘以 60。

## 新增

- `GET /quotes/{id}/history`
```

```bash
brickkit release --notes-file ../notes-0.3.0.md     # 路径相对当前目录
brickkit release --notes "报价单多了 currency 字段。配置不用改。"   # 短的直接写
```

```text
✅ 已发布 demo/quote@0.3.0：tag 0.3.0 已推送
   📝 发版说明已写进 tag：使用它的项目 upgrade 时会看到
```

`--notes` 和 `--notes-file` 同时给是错误，文件读不了也是——两种情况都什么都不发布。`--local` 两个都不接：一份说明讲的是
一个组件的一个版本，那就每个组件带上自己的说明分别发（`brickkit release --path <目录> --notes-file <文件>`）。说明和它所属的
版本一样只写一次：还想补充，就发下一个版本。

**使用方看到什么。** `brickkit upgrade`——以及 `upgrade --dry-run`——在动任何东西之前，先列出项目现有版本之后、直到目标版本
（含）的每个版本的说明；没写说明的版本不列：

```text
📋 demo/quote 的发版说明，0.1.0 → 0.3.0：

   ── 0.2.0 ──
      报价单多了 currency 字段。配置不用改。

   ── 0.3.0 ──
      ## 不兼容的改动

      - `QUOTE_TTL` 的单位从分钟改成了秒：把原来的值乘以 60。

      ## 新增

      - `GET /quotes/{id}/history`

📋 upgrade 会做这些：
   ⬆️  demo/quote：0.1.0 → 0.3.0
📝 会写：brickkit.yaml, deploy.yaml

💡 --dry-run：一个文件都没有改
```

发布到市场接同样两个参数——`brickkit publish --notes-file <文件>`——市场把它存成那个版本的 changelog，`upgrade` 照样列出来。
使用方那边的更多内容见 [升级与迁移](../02-project-guide/07-upgrade-and-migration.md)。

## 批量发布：`--local`

一个项目里自己写了好几个组件（都在本地源 `components/` 下、各是一个仓库）时：

```bash
brickkit release --local
```

先把每一个都检查一遍（先是平台对所有组件的检查，再是每个组件自己的 `release.checks`，最后核对检查没有改动其中任何一个），全部通过才开始逐个打 tag、推送；已经发布过的（tag 就在当前提交上）跳过；遇到第一个推送失败就停——之前发布的保留，之后的不再尝试。

## 组件在仓库子目录里时

几个组件放在同一个仓库（monorepo）的子目录里时，用 `--path` 指定组件目录。这时 tag 带上命名空间，各组件各打各的：

```bash
brickkit release --path svc/api        # 组件 erp/api，tag 是 erp-api/1.2.0
```

检查工作区时也只看这个组件自己的目录：旁边组件的改动与它无关。使用这些组件的项目把这个仓库声明成一个写 `repo` 的安装源
（见 [Git 分发](10-git-distribution.md#组件地址怎么推导)）。

## Go 模块：再打一个 Go 的 tag

Go 也按 Git tag 取一个模块某个版本的代码，但 tag 有它自己的叫法：模块在仓库根上是 `v1.2.0`，在子目录 `svc/api` 里是
`svc/api/v1.2.0`——按目录起名，不按组件 ID。BrickKit 的版本不带 `v`（`1.2.0`、`erp-api/1.2.0`），带 `v` 的 tag 也不算版本
（见 [Git 分发](10-git-distribution.md)），一个 tag 满足不了两边。别的 Go 代码要 import 的组件两个都要——最常见的是 Go 外壳编进的成员：
外壳的 `go.mod` 按 `shell.members` 写的精确版本引用成员的模块，Go 去找的是那个 `v` 标签（见
[开发一个外壳](../04-shell/05-shell-development.md#外壳怎样拿到成员的代码)）。

在 `component.yaml` 里写明：

```yaml
release:
  goModule: true
```

`release` 就在平台的 tag 所在的同一个提交上再打 Go 的 tag，带同一份发版说明，两个一起推送：

```text
✅ 已发布 erp/sales@3.0.7：tag 3.0.7、v3.0.7 已推送
```

打任何 tag 之前，它先查 Go 会查的东西，因为 tag 推出去就收不回来：组件目录里有 `go.mod`、里面有 `module` 行；模块路径从主版本 2 起
以主版本结尾（3.0.7 是 `example.com/erp/sales/v3`），低于 2 时不带这个后缀。模块路径里没有对应主版本的 tag，Go 一概不认：
外壳构建会报 `unknown revision`，唯一的出路是再发一个版本。

```text
❌ Go 不会认 erp/sales 的 tag v3.0.7：主版本 2 起，go.mod 里的模块路径要以主版本结尾，这里是 /v3
   文件：go.mod
   module：example.com/erp/sales
   目录：.
   建议：Go 只在模块路径以主版本结尾时才认它的 tag（/v2、/v3……；0 和 1 不带）。改 go.mod 的 module 行和用到它的 import，或者改 metadata.version——tag 推出去就收不回来，所以打任何 tag 之前先查这一条
```

`brickkit lint` 把同一件事当警告报，升版本号的时候就能知道，不必等到发布。

平台的 tag 已经在当前提交上、只缺 Go 的 tag 时（被删过，或者平台的 tag 是手推的），`release` 只推送 Go 的 tag，并说明哪个原来就在；
Go 的 tag 已经指向别的提交时，和平台的 tag 一样拦下。写这个字段**之前**发布的版本没法这样补：加字段本身是一个新提交，而已发布的版本不会挪。
外壳需要那个版本时，先自己核对模块路径，再在它的版本 tag 所在的提交上手动推送 Go 的 tag（`git tag v1.2.0 '1.2.0^{commit}' && git push origin v1.2.0`）——
或者发一个新版本，让外壳改用它。

为什么是一个字段，而不是凡有 `go.mod` 的目录都自动打：很多 Go 组件是没人 import 的服务，到了主版本 2 以后模块路径照样是
`example.com/order`。都打的话，要么为一个它们用不着的后缀拒绝发布，要么推上去 Go 不认的 tag。它不查的是模块路径是否指向这个仓库——
用了自定义的导入域名时离线查不出来；外壳构建时 Go 会报。

## 工作台与发布无关

组件仓库里的 `brickkit.yaml`、部署文件、`config/`（你的本地联调工作台，见 [分形的本地开发](05-local-dev-fractal.md)）不参与发布：
`release` 只读 `component.yaml`。它们如果有未提交的改动，同样会被第 3 步挡下——提交或丢弃之后再发布。
