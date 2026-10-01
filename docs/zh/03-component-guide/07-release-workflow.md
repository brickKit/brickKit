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
2. **校验 `component.yaml`。** 解析得了、字段合法，才谈得上发布。
3. **检查工作区。** 组件目录里不能有未提交的改动。
4. **检查分支。** 当前分支要有上游，而且没有未推送的提交：tag 指向的提交必须已经在远端的历史里。
5. **检查 tag。** 这个 tag 在本地和远端都还不存在。
6. **打 tag、推送。** 给了[发版说明](#发版说明)时，打的是带着说明的带注释 tag。

前五步是只读的检查，全部通过才会写任何东西。

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

## 要么完整做完，要么不留痕迹

打完 tag、推送失败（网络断了、没有推送权限）时，`release` 把本地刚打的 tag 删掉。否则下次重试会被"tag 已存在"挡住，
而那个 tag 其实从没到过远端。

## 发一个新版本

```bash
# 1. 改 component.yaml：version: 0.1.0 → 0.2.0（契约的 info.version 一起改）
# 2. 提交、推送
git commit -am "demo/quote 0.2.0：……"
git push
# 3. 发布，带上改了什么（见下面的"发版说明"）
brickkit release --notes-file ../notes-0.2.0.md
```

版本号怎么提：只改了内部实现、接口没变，提修订号（0.1.0 → 0.1.1）；加了接口或配置项、旧的调用方照样能用，提次版本号；
删了接口、改了已有字段的含义，提主版本号——使用方升级时要自己改代码。

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

先把每一个都检查一遍，全部通过才开始逐个打 tag、推送；已经发布过的（tag 就在当前提交上）跳过；遇到第一个推送失败就停——之前发布的保留，之后的不再尝试。

## 组件在仓库子目录里时

几个组件放在同一个仓库（monorepo）的子目录里时，用 `--path` 指定组件目录。这时 tag 带上命名空间，各组件各打各的：

```bash
brickkit release --path svc/api        # 组件 erp/api，tag 是 erp-api/1.2.0
```

检查工作区时也只看这个组件自己的目录：旁边组件的改动与它无关。

## 工作台与发布无关

组件仓库里的 `brickkit.yaml`、部署文件、`config/`（你的本地联调工作台，见 [分形的本地开发](05-local-dev-fractal.md)）不参与发布：
`release` 只读 `component.yaml`。它们如果有未提交的改动，同样会被第 3 步挡下——提交或丢弃之后再发布。
