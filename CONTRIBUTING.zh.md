# 给 BrickKit 贡献代码

[English](CONTRIBUTING.md)

## 动手之前先看这个

先扫一遍 [AGENTS.zh.md](AGENTS.zh.md)——它把整个平台的三层文件、十条设计原则、命令集压缩进了一个文件，包括一份明确的"平台刻意不做的事"清单（AGENTS.zh.md §6）；每条原则的论证在[设计原则与取舍](docs/zh/06-architecture/05-design-principles.md)。很多"为什么不干脆……"的问题其实已经有答案了。如果你的改动会往这份清单里加东西，先开一个 issue 把道理讲清楚，再动手写代码——大概率是要先推翻某一条原则的论证，而不只是加个功能。

## 构建与跑测试

```bash
make build            # bin/brickkit + bin/market-server
make test             # 单元测试
make test-all         # 全部测试套件，含 checklist / regression 门禁
make lint             # vet + 全部文档一致性检查
make hooks            # 每个克隆一次：启用仓库自带的提交钩子（.githooks/）
```

**每个克隆运行一次 `make hooks`，启用提交钩子**（它会设置 `git config core.hooksPath .githooks`；git 从不自己启用仓库里的钩子）。
提交里动了 `docs/en/`、`docs/zh/`、`AGENTS*.md`、`llms*.txt` 或生成器本身时，钩子会跑 `make generate-llms`——重新生成 `llms/` 下给网页端
AI 读的文档合集——并把它们加进同一个提交（`docs/superpowers/` 下的规划文档不进合集，也不会触发它）。那些文件还有没暂存的改动、或者
`docs/en/`、`docs/zh/` 里有没跟踪的新页面时，钩子拒绝提交：从工作区生成的合集会与这次提交对不上。它也拒绝 `git commit <路径>`：
那种模式下钩子是对着一个临时暂存区跑的，重新生成的合集留不在暂存区里——先 `git add` 要提交的文件，再不带路径地提交。把整个文件暂存（或 stash 掉）再提交；没装钩子的话，自己跑 `make generate-llms`——`make lint` 里的 `make check-llms`
会在合集过期时失败。

**没有任何 CI 会在 PR 上自动跑。** 仓库里唯一的 GitHub Actions 工作流（`.github/workflows/release.yml`）只在推 `v*` tag 时触发，负责构建/签名/发布——分支或 PR 上不会跑。也就是说，开 PR 之前你自己在本机跑通 `make lint` 和 `make test-all`，是唯一的关卡。你机器上跑红的东西，到任何 reviewer 那里也一样是红的。

`make lint` 如果检测到装了 `golangci-lint` 就会跑它（`make tools-lint` 会把它装到 `.tools/bin`；仓库没有自己的 `.golangci.yml`，用的就是 golangci-lint v2 的默认规则集），没装就退回 `go vet`。开 PR 之前建议装上——光靠 `go vet` 抓不到 golangci-lint 能抓到的那些问题。

`make lint` 还会跑一组文档一致性脚本（`scripts/check-*.py`）——指向归档的引用、悬空或没写文档名的小节引用、断链与断掉的锚点、文档里写的命令/参数其实不存在、命令参考没写全、文档画的 YAML 字段名和真实结构体对不上、docs/zh↔docs/en 镜像，还有几个别的（完整列表和每一条守住什么，见 README 的["构建与测试"](README.zh.md#构建与测试)一节）。这些不是摆设——好几条的存在就是因为某次改动破坏了一些测试套件根本没法察觉的东西：改名的参数、过期的示例、曾经指向真实位置、后来指向空处的链接。

`make lint` 还守着 `schemas/` 里的 JSON Schema（`schemas/component.schema.json`、`schemas/brickkit.schema.json`、`schemas/deploy.schema.json`——编辑器用它们给 `component.yaml`、`brickkit.yaml` 和部署文件做补全与检查）。它们是从 `internal/manifest`、`internal/projfile` 与 `internal/deployfile` 的 Go 结构体生成出来的，从不手改。**只要你在那里新增、删除或改了某个字段的类型，或者动了它的 `omitempty`、某个 `jsonschema` tag，就要跑 `make generate-schemas`，把重新生成的文件和你的改动一起提交**——否则 `make check-schemas`（`make lint` 的一部分）会失败。这道检查还会拿真实的校验器去核对 schema 里的必填字段、封闭取值、正则和范围，所以 `jsonschema` tag 不会悄悄和 `Validate` 脱节。

## 测试放在哪

单元测试**紧挨着被测代码**（`internal/**/*_test.go`、`market-server/internal/**/*_test.go`）——不用维护一套平行的测试目录。`tests/` 只放真的没法挨着代码放的东西：`tests/checklist/` 和 `tests/regression/` 是验收清单，每一行都配着证明它的测试（两者都由 `make lint` 守着，具体见 README 的"构建与测试"表格），`tests/components/` 放的是好几个测试和教程文章实际会跑起来的真实夹具组件。

如果你要加一条值得进清单的行为（边界条件、错误场景、兼容性或安全保证），把这一行加进对应的 `tests/checklist/清单.tsv` 或 `tests/regression/清单.tsv`，再接一个真测试上去——清单里有一行没测试、或者测试已经不存在了，都会**故意**让构建失败（原因见 README"构建与测试"那张表）。

## 文档规范

- **`docs/zh/` 和 `docs/en/` 是两棵独立撰写、彼此对称的目录树**——不是一份原文配翻译。你在其中一棵改了或加了文档，另一棵在相同相对路径下也要有对应文件（`make check-docs-bilingual` 会守这条），而且要用那门语言自然地写，不是机械翻译过去。
- **教程或故障排除文档里展示的 CLI 输出必须是真实输出**，不是你以为 CLI 会打印的样子。真的跑一遍命令，把跑出来的东西贴进去。`make check-doc-fields` 会核对文档里的每一行输出都是 CLI 真能打印出来的那一行。
- **文档里画的 YAML 字段必须在真实结构体里存在**——`make check-doc-fields` 拿 `component.yaml`/`brickkit.yaml`/部署文件真实的 Go 类型去反查文档，而不是反过来；字段参考（`docs/*/11-reference/`）还必须把每个字段都写到。
- 如果你改了 `AGENTS.md`/`AGENTS.zh.md`，保证两份内容真的对等——它们各自独立撰写，不是互译关系，但该覆盖的内容要覆盖到。

## 消息与语言

CLI 打给人看的每一句话——错误、建议、进度行、`--help`——都来自**消息目录**：`internal/i18n/locales/` 下每种语言一个文件（`en.yaml`、`zh.yaml`），一行是一个 key 配上这种语言的文案：

```yaml
# internal/i18n/locales/zh.yaml
cli.release.done: "✅ 已发布 %[1]s：tag %[2]s 已推送"
```

```yaml
# internal/i18n/locales/en.yaml
cli.release.done: "✅ Released %[1]s: tag %[2]s pushed"
```

代码里不写文案，只通过一个 Go 常量点名 key、把参数传进去：

```go
opts.Printf("%s\n", i18n.T(msgid.CliReleaseDone, target.Ref(), target.Tag))
```

英文是**源目录**：每个 key 先在 `en.yaml` 里声明，别的目录 key 一条不多、一条不少，顺序也一样。`internal/msgid/messages_gen.go` 里的常量由 `en.yaml` 生成，从不手写。

- **改一条文案**：改 `en.yaml` 和其他每份 `locales/*.yaml` 里那一行，别的都不用动。
- **加一条文案**：在 `en.yaml` 里加 `key: "文案"`，其他每份目录在同一位置也加上；跑 `make generate-msgid`；代码里用 `msgid.<名字>`（`cli.release.done` 对应 `msgid.CliReleaseDone`）。key 的写法是 `<范围>.<它是什么>`，小写加下划线：范围是包或命令（`cli.release.`、`configdir.`），后面说这条消息是什么（建议、标签、原因分别以 `hint_`、`label_`、`reason_` 开头）。`en.yaml` 里 key 上方的注释说明什么时候用它。
- **加一种语言**：把 `en.yaml` 复制成 `locales/<代码>.yaml`，译完每一条，再在 `internal/i18n/languages.go` 的 `registry` 里加一行 `{Code: "<代码>"}`——不用改别的 Go 代码。从此 `BRICKKIT_LANG=<代码>` 与 `brickkit lang set` 都认它。`brickkit init` 装进项目的 AI 助手技能单独翻译（`internal/skills/assets/<代码>/`），没译之前，这种语言的项目装的是英文技能。
- **文案怎么写**：写在双引号里；多行文案写成 `|` 块。其他 YAML 写法会被拒绝并报出文件和行号，因为 YAML 会悄悄改掉它们（`yes`、开头的空格、`#`）。参数按位置写——`%[1]s`、`%[2]d`——这样译文可以调整语序；每种语言用到的参数与英文那一条完全相同。有几条 `cobra.*` 在英文里是空字符串：意思是保留 cobra 自带的英文。

这些都由 `make lint` 守着：缺 key、多 key、顺序不同、参数与英文对不上、写法不对，以及常量没跟上 `en.yaml`（`make check-msgid`）。给 key 改名用 `go run ./tools/i18n/rekey`，它把代码和每份目录一次改完（见 [tools/i18n/README.md](tools/i18n/README.md)）。

## 提交信息和分支

这个仓库的提交标题习惯用"`<类型>: <改了什么>`"这种形状——`fix`、`feat`、`docs`、`refactor`、`test` 是最常见的几种，可以带一个范围（`docs(zh): …`）。翻一下 `git log` 就能看出规律；不强制照抄，但照着写能让历史更好扫。

在一个功能分支上改，对着 `main` 开 PR。目前还没配分支保护或强制 review（项目还年轻），实际上 PR 更多是"改动落地前先讨论一下"的地方，不是一道硬性技术关卡。

## 报 bug 或提功能请求

开一个 GitHub issue。报 bug 的话，CLI 自己的输出通常就够用了大半——`brickkit` 把结构化 JSON 日志写到 stderr（`--log-level debug` 看更多细节），人类可读的输出写到 stdout；把这两部分连同你的 `brickkit.yaml`、部署文件和相关组件的 `component.yaml` 一起贴出来（`config/` 里的密钥记得先抹掉），能省一轮来回。提一个不在["不做"清单](AGENTS.zh.md#6-不做清单)上的功能请求时，讲清楚你要解决的问题，而不只是你想好的实现方式——新机制要过的门槛见["十条设计原则"](AGENTS.zh.md#2-核心设计原则十条)。
