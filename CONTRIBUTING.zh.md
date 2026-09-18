# 给 BrickKit 贡献代码

[English](CONTRIBUTING.md)

## 动手之前先看这个

先扫一遍 [AGENTS.md](AGENTS.zh.md)——它把整个平台的理念、术语、十二条设计原则压缩进了一个文件，包括一份明确的"平台刻意不做的事"清单（§4.1）和每一条的论证（§9）。很多"为什么不干脆……"的问题其实已经有答案了。如果你的改动会往这份拒绝清单里加东西，先开一个 issue 把道理讲清楚，再动手写代码——大概率是要先推翻二十三条论证里的某一条，而不只是加个功能。

## 构建与跑测试

```bash
make build            # bin/brickkit + bin/market-server
make test             # 单元测试
make test-all         # 全部测试套件，含 checklist / regression 门禁
make lint             # vet + 全部文档一致性检查
```

**没有任何 CI 会在 PR 上自动跑。** 仓库里唯一的 GitHub Actions 工作流（`.github/workflows/release.yml`）只在推 `v*` tag 时触发，负责构建/签名/发布——分支或 PR 上不会跑。也就是说，开 PR 之前你自己在本机跑通 `make lint` 和 `make test-all`，是唯一的关卡。你机器上跑红的东西，到任何 reviewer 那里也一样是红的。

`make lint` 如果检测到装了 `golangci-lint` 就会跑它（`make tools-lint` 会把它装到 `.tools/bin`；仓库没有自己的 `.golangci.yml`，用的就是 golangci-lint v2 的默认规则集），没装就退回 `go vet`。开 PR 之前建议装上——光靠 `go vet` 抓不到 golangci-lint 能抓到的那些问题。

`make lint` 还会跑一组文档一致性脚本（`scripts/check-*.py`）——悬空的小节引用、断链、文档里写的命令/参数其实不存在、文档画的 YAML 字段名和真实结构体对不上、docs/en↔docs/zh 镜像，还有几个别的（完整列表和每一条守住什么，见 README 的["构建与测试"](README.zh.md#构建与测试)一节）。这些不是摆设——好几条的存在就是因为某次改动破坏了一些测试套件根本没法察觉的东西：改名的参数、过期的示例、曾经指向真实位置、后来指向空处的链接。

## 测试放在哪

单元测试**紧挨着被测代码**（`internal/**/*_test.go`、`market-server/internal/**/*_test.go`）——不用维护一套平行的测试目录。`tests/` 只放真的没法挨着代码放的东西：`tests/checklist/` 和 `tests/regression/` 是验收清单，每一行都配着证明它的测试（两者都由 `make lint` 守着，具体见 README 的"构建与测试"表格），`tests/components/` 放的是好几个测试和教程文章实际会跑起来的真实夹具组件。

如果你要加一条值得进清单的行为（边界条件、错误场景、兼容性或安全保证），把这一行加进对应的 `tests/checklist/*/清单.tsv` 或 `tests/regression/清单.tsv`，再接一个真测试上去——清单里有一行没测试、或者测试已经不存在了，都会**故意**让构建失败（原因见 README"构建与测试"那张表）。

## 文档规范

- **`docs/en/` 和 `docs/zh/` 是两棵独立撰写、彼此对称的目录树**——不是一份原文配翻译。你在其中一棵改了或加了文档，另一棵在相同相对路径下也要有对应文件（`make check-docs-bilingual` 会守这条），而且要用那门语言自然地写，不是机械翻译过去。
- **教程或故障排除文档里展示的 CLI 输出必须是真实输出**，不是你以为 CLI 会打印的样子。真的跑一遍命令，把跑出来的东西贴进去。`make check-guide-output` 会对着动手教程系列核实这一点。
- **文档里画的 YAML 字段必须在真实结构体里存在**——`make check-doc-fields` 拿 `component.yaml`/`brickkit.yaml` 真实的 Go 类型去反查文档，而不是反过来。
- 如果你改了 `AGENTS.md`/`AGENTS.zh.md`，保证两份内容真的对等——它们各自独立撰写，不是互译关系，但该覆盖的内容要覆盖到。

## 提交信息和分支

这个仓库的提交标题习惯用"`<动词>：<改了什么>`"这种形状——`新增`/`修复`/`改进`/`更新` 是最常见的几个。翻一下 `git log` 就能看出规律；不强制照抄，但照着写能让历史更好扫。

在一个功能分支上改，对着 `main` 开 PR。目前还没配分支保护或强制 review（项目还年轻），实际上 PR 更多是"改动落地前先讨论一下"的地方，不是一道硬性技术关卡。

## 报 bug 或提功能请求

开一个 GitHub issue。报 bug 的话，CLI 自己的输出通常就够用了大半——`brickkit` 把结构化 JSON 日志写到 stderr（`--log-level debug` 看更多细节），人类可读的输出写到 stdout；把这两部分连同你的 `brickkit.yaml` 和相关组件的 `component.yaml` 一起贴出来，能省一轮来回。提一个不在[拒绝清单](AGENTS.zh.md#41-平台明确不做的事拒绝清单)上的功能请求时，讲清楚你要解决的问题，而不只是你想好的实现方式——新机制要过的门槛见["十二条设计原则"](AGENTS.zh.md#4-十二条设计原则理念内核)。
