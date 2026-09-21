# CLI 多语言（i18n）支持设计

> 触发：`install.sh | sh` 是任何语言的 GitHub 访客拿到 CLI 之前的唯一入口，而它现在的
> 用户反馈是中文。核实后发现这不是 install.sh 一个文件的问题——`internal/cli`（81 个
> 文件）以及 resolver/compose/k8s/inject/cascade/workspace/security/manifest/config
> 等包的全部用户可见文本都是硬编码中文，源头是早期决定 D11："设计书全部输出为中文，
> 混入英文 `Usage:`/`Available Commands:` 会破坏一致性"。
>
> `docs/en/06-architecture/10-error-codes.md` 现在明确写着"The CLI's messages are
> written in Chinese. This page quotes them verbatim"——英文文档目前照抄中文原文当
> 查找 key；`docs/zh/06-architecture/02-dependency-resolution.md` 也有一句"CLI 没有
> 英文输出模式"。这些都是文档里已经承认、但跟这个仓库"开源、面向国际用户"的定位
> 不再匹配的已知限制。

## 0. 已经落地的部分

`install.sh` 已改为英文反馈（含 `less install.sh` 会看到的头部注释），`scripts/check-install-sh.sh`
的断言同步改过，`make lint` 全绿。见 commit `c555da1`。

这份设计只覆盖 CLI 本体（运行时输出）该怎么支持多语言，不重复 install.sh 那部分。

## 1. 范围拆分

CLI 本体多语言的完整改动面太大，拆成四块分别设计和验收，不追求一次到位：

| # | 子项目 | 范围 | 状态 |
| --- | --- | --- | --- |
| 1 | 核心机制 | 消息目录、语言配置存储与优先级、cobra 接入方式；只挑一小片真实命令验证可行 | **本文档设计** |
| 2 | 全量迁移 | 把其余 80+ 个命令文件、以及 resolver/compose/k8s/inject/cascade/workspace/security/manifest/config 里的中文字面量全部改成走消息目录，同步改测试断言 | 未设计，待第 1 块落地后再单独立项 |
| 3 | 文档工具链 | 改 `10-error-codes.md`"照抄中文原文当 key"的写法；重新设计 `check-guide-output.py`"中英文教程输出必须逐字相同"的假设；修掉 `02-dependency-resolution.md` 里过时的"没有英文输出模式"；AGENTS.md/AGENTS.zh.md 的命令集标题从"16 commands + version"改成"16 commands + version + lang"（新增的 `brickkit lang` 跟 `version` 一样是 CLI 自身命令，不计入业务命令数） | **已完成**（2026-09-21，见 full-migration 设计 §7） |
| 4 | 扩展更多语言 | 视需求决定是否支持超出 en/zh 的语言 | 未设计，机制本身（内嵌 map）天然支持，暂不主动做 |

本文档只详细设计第 1 块。第 2-4 块留到各自需要动手时再单独走一遍需求澄清+设计流程，不在这里预先钉死细节。

## 2. 已否决的方案：按需下载语言包

讨论中曾提出"默认只下载英语，切换语言时才下载对应语言文件，减小项目体积"。核实后否决：

- **体量不成立**：现有全部中文用户可见文本（107 个文件）总共约 220KB 字符串字面量；编译后的二进制
  已经有 11MB。就算以后扩到 10 种语言、每种都跟中文这份一样大，全部内嵌也只增加约 2MB（<20%）；
  只算英/中两种，增量在 2% 以内。CLI 自己的界面文案天然就小，不会长成"值得按需下载"的体量。
- **代价是实打实的**：会把"改语言"从纯本地操作变成需要联网的操作（跟 `brickkit lint` 坚持离线的
  态度相反）；需要新开一条语言包的分发与校验链路，并解决语言包版本与 CLI 版本对齐的问题（错误码
  只增不减，旧语言包缺新 key 会静默回落到英文，这正是这个项目自己警告过的"看起来正常、其实有一条
  路悄悄没生效"）。
- 跟这个项目否决"增量生成缓存"时的理由同构："没什么好提速的，缓存徒增一种会过期的状态"。

**结论：全部语言在编译期直接内嵌进二进制，不做按需下载。** 真遇到体量问题再回头优化。

## 3. 子项目 1：核心机制设计

### 3.1 `internal/i18n`：只管"选哪种语言的文字"

新增一个职责单一的包，纯查表，不掺和错误渲染、不掺和 cobra：

```go
package i18n

type Lang string

const (
    EN Lang = "en"
    ZH Lang = "zh"
)

func T(id string, args ...any) string // 查当前语言的目录 + fmt.Sprintf
func Current() Lang
func SetCurrent(l Lang)
func Resolve() Lang // BRICKKIT_LANG 环境变量 > 全局配置文件 > 默认 EN
```

`internal/clierr` 的签名保持不变（`New(code Code, message string)`），不引入 i18n 概念——调用点
先用 `i18n.T(...)` 把文案解析出来再传给 `clierr.New`。两个包各管一件事：`i18n` 只管"文字选哪种
语言"，`clierr` 只管"错误怎么渲染成 ❌/⚠️ 块"。任何直接 `Printf` 的现有代码（如 `add_local.go` 里的
`opts.Printf("📂 ...")`）也能用同一个 `i18n.T` 改造，不需要先套上 clierr。

message ID 是一批 Go 字符串常量（不是裸字符串字面量），例如：

```go
package msgid

const ProjectMissing = "project.missing"
const LabelPath       = "label.path"
```

调用点写成 `i18n.T(msgid.ProjectMissing)`——常量能被编辑器跳转、查引用，不容易手滑打错 key。

### 3.2 目录格式与参数插值

目录是普通 Go 源文件里的 map 字面量，随二进制编译，不走 `embed.FS` 读外部文件（没有独立翻译流程
的需求，也没必要多一层文件解析和运行时读盘失败的可能）：

```go
// internal/i18n/catalog_en.go
var en = map[string]string{
    msgid.ProjectMissing: "Project config file not found",
    msgid.LabelPath:      "Path",
}
// internal/i18n/catalog_zh.go
var zh = map[string]string{
    msgid.ProjectMissing: "项目配置文件不存在",
    msgid.LabelPath:      "路径",
}
```

带参数的文案用 Go `fmt` 的**位置 verb**（`%[1]s`），不用裸 `%s` 顺序拼接——中英文语序经常不同，
位置 verb 让两份文案各自决定参数出现的顺序：

```go
msgid.ComponentMissingFrom: "%[1]s not found in any install source", // en
msgid.ComponentMissingFrom: "在任何安装源中都找不到 %[1]s",             // zh
```

`clierr.Error.WithDetail(key, value)` 的 `key`（"组件"、"路径"这类标签）要过 `i18n.T`，`value`
（组件 ID、文件路径这类具体数据）**永远不翻译**——数据与文案分开，跟"弱依赖缺失时环境变量的值
不猜"是同一种态度。

**新增一个 lint 检查**（跟 `check-docs-bilingual.py`"双语镜像必须完整"同一个思路）：扫描 `msgid`
包声明的每一个 key，确认 `en`/`zh` 两份 map 里都有，缺一个直接 lint 失败——不允许"注册了 key 但
漏翻译，运行时才发现某句话是空字符串"这种半成品状态。挂进 `make lint`。

### 3.3 语言偏好存哪、优先级怎么排

BrickKit 现在**没有任何"项目之外"的状态**——`.brickkit/credentials`、`.brickkit/manifests/`
全部长在某个项目目录里（`internal/config.Layout` 的路径都是相对 `Root` 推导的）。但很多命令
（`brickkit init`、`brickkit version`、`brickkit login`）本来就可能在还没有项目、或不在任何项目
目录里执行，所以语言偏好必须是机器级、跨项目的——这是 BrickKit 第一次引入这类状态。

新开一个小包，不往 `internal/config`（项目级 Layout）里塞，避免把"项目布局"和"机器级设置"这两
个概念混进同一个类型：

```go
// internal/userconfig（新包）
func Dir() string             // os.UserConfigDir() + "/brickkit"，跨平台正确
                               // （Linux 走 XDG，macOS 走 ~/Library/Application Support）
func Load() (*Config, error)  // 文件不存在 → 默认值，不是错误（跟 LoadCredentials 的
                               // "未登录不是错误"一个道理）
func Save(*Config) error      // 先写临时文件再 rename，跟 internal/source/credentials.go
                               // 的写法一致
```

文件内容是最小的 JSON（跟 `.brickkit/credentials` 一样的写法习惯，不需要 0600——语言偏好不是
密钥）：

```json
{ "lang": "zh" }
```

**优先级：`BRICKKIT_LANG` 环境变量 > 这个全局文件 > 默认英语。** 刻意不加 `--lang` 命令行参数：
环境变量已经完全覆盖"这一次单独指定"的场景（`BRICKKIT_LANG=en brickkit up`），而语言必须在
cobra 搭命令树**之前**确定（因为每个命令的 `Short`/`Long` 是建树时写死的字符串），命令行参数要
等 cobra 解析完才能拿到值，会变成要先手动扫一遍 `os.Args` 的麻烦事；环境变量在进程一启动就能读，
两种方式能力相同时选简单的那个。

新增两个命令（跟 `version` 一样是 CLI 自身命令，不属于现有四个分组，也不计入"16 个业务命令"这个数字）：

- `brickkit lang set en|zh`：严格校验只能是已内嵌的语言之一，写错了报错列出支持哪些
- `brickkit lang`（不带参数）：显示当前生效语言，以及它来自哪一层（环境变量 / 全局文件 / 默认）

### 3.4 cobra 怎么接进来

`internal/cli/root.go` 现在的 `localize(root)` 额外写了一份中文 `usageTemplate` 把 cobra 默认的
（本来就是英文的）模板换掉，外加手改 `help`/`completion`/`-h` 的短文案。好消息是：**英文模式什么
都不用做，cobra 原生默认值就是对的**。现有的中文模板与手改逻辑原样保留，只在语言=中文时才调用：

```go
func Execute() int {
    lang := i18n.Resolve()
    i18n.SetCurrent(lang)
    opts := NewOptions()
    root := NewRootCommand(opts)
    if lang == i18n.ZH {
        localizeCobra(root) // 就是现在的 localize()，原样搬过来、改个名
    }
    return Run(root, opts, os.Args[1:])
}
```

每个子命令自己的 `Short`/`Long`/`Example`（目前硬编码中文，如 `newLoginCommand` 里的
`"登录组件市场，Token 存入 .brickkit/credentials"`）留给子项目 2（全量迁移）处理，本设计只搭机制。

### 3.5 验收范围：一个纵向切片

不碰另外 80 个命令文件，只挑一个纵向切片跑通三种文案形态：

- `internal/i18n`（含双语完整性 lint）+ `internal/userconfig`——全新代码，直接双语写好
- `brickkit lang` / `brickkit lang set`——全新命令，直接双语写好
- 三个已有的改造点，各代表一种文案形态：
  - `brickkit version`（纯 `Printf` 输出）
  - `PROJECT_MISSING` 错误（`clierr.Error` 结构化输出；也是 AGENTS.md/错误码文档里反复引用的
    示例，改完之后文档里这个例子的中文块需要在子项目 3 里补一份对应的英文块）
  - root 命令的 `--help`（cobra 模板切换）
- 验证方式：`brickkit --help`、`BRICKKIT_LANG=zh brickkit --help`、空目录下 `brickkit up`（触发
  `PROJECT_MISSING`）分别在两种语言下跑一遍、`brickkit lang set zh` 之后确认全局配置文件写入且
  后续命令读到——跑真实二进制看输出，不只是跑单测
- 这三个改动点原有的单测（断言中文原文的）同步改成走 `i18n.T` 断言或改成默认英文断言
- `make lint` 全绿是过关线

### 3.6 明确不做（留给后续子项目）

- 其余 80+ 命令文件与 resolver/compose/k8s/inject/cascade/workspace/security/manifest/config
  里的中文字面量迁移，以及对应测试断言的同步——子项目 2
- `10-error-codes.md` 的查找 key 改法、`check-guide-output.py` 的跨语言核对逻辑重设计、
  `02-dependency-resolution.md` 里过时的断言、AGENTS.md/AGENTS.zh.md 命令数更新——子项目 3
- 超出 en/zh 的语言——子项目 4，视需求决定是否启动
- `--lang` 一次性命令行参数——已否决（§3.3），环境变量已覆盖同等场景
- 按需下载语言包——已否决（§2）
