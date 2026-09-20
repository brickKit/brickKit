# `brickkit graph` / `brickkit lint` / JSON Schema：设计

> 来源：仓库根目录未跟踪的 `改进计划.md`（第二次更新后的版本，"依赖拓扑可视化与结构级 Manifest Linter"）。
> 那份文件是一次性的、会被删除；本文档自足，删掉它不损失任何依据。

## 1. 背景与范围裁决

`改进计划.md` 提了两条提案：`brickkit graph`（依赖拓扑可视化）与 `brickkit lint`（结构级校验 + JSON Schema）。
逐条对着仓库核实后的结论（细节见 §2-§4，这里先给结论）：

| 提案原话 | 裁决 | 为什么 |
| --- | --- | --- |
| `brickkit graph`：Mermaid + HTML/SVG 两种输出 | **只做 Mermaid，砍掉 HTML/SVG** | GitHub 上的 `.mmd` / `.mermaid` 文件与 Markdown 里的 ```` ```mermaid ```` 围栏本来就直接渲染（别的编辑器有对应插件；本仓库没有核实过哪些编辑器内置支持，所以文档里不替它们担保）；自己再造一个 HTML/SVG 渲染器是重新发明已经免费拿到的东西，直接违反"平台极简"（每一个额外渲染器都是永久维护成本） |
| `brickkit lint`：四类结构错误 + 跨文件引用检查 | **四类错误的校验逻辑本来就已经存在，只是没有独立入口；跨文件引用检查明确不做** | 实测确认 `manifest.Validate`/`config.Validate` 早就覆盖了拼写、类型、版本格式（保留变量冲突的规则也早就存在，只是散在市场发布与 `up` 注入两处，见 §3.1）；真正缺的是"不用 `add`/`up` 就能单独跑一遍"这个入口。跨文件引用（`servedBy` 目标是否真实存在）需要完整依赖图，天然要联网，跟"离线秒回"的定位不是一回事，留给 `up`/`add` |
| JSON Schema：随便写一份 | **从 Go struct 反射生成，不手写** | 手写的 schema 是仓库里从没有过的一种"容易悄悄过期"的东西，跟 `tests/docfields` 已经在防的问题是同一类；生成 + 防漂移测试才是"架构正确的完整方案" |

三处都已经过 `AskUserQuestion` 得到确认：graph 只做 Mermaid；lint 按收窄范围；schema 用 struct tag 表达
enum/pattern 这类约束（而不是纯反射，纯反射拿不到 `deploy.target` 只能是 `docker`/`k8s` 这类约束，而这恰恰是
提案最想要的"IDE 红线"效果）。

## 2. `brickkit graph`

### 2.1 做什么

新增命令 `brickkit graph`（连同 `brickkit lint`，命令总数从 14 个变成 16 个），把当前项目的依赖拓扑渲染成 Mermaid 代码，
打印到 stdout。数据来源与 `up --dry-run` 相同的两样东西——`resolver.Graph`（依赖图）与 `cascade.Result`（谁跑谁不跑），
再加 `brickkit.yaml` 里每个组件条目自己写的 `local` / `servedBy`。它跳过 `up` 才需要的一切：
镜像权限检查、迁移展示、引擎解析、环境变量注入、生成部署文件。

### 2.2 复用而不是重复计算——抽出 `resolveTopology`

"解析依赖图 → 算级联"这两步今天在**四处**各写了一遍：`up.go` 的 `buildUpPlan`、`lifecycle.go` 的
`project.resolve`（down/status 用）、`sync.go` 的 `syncFocus`，`graph` 是第四个。
把这两步抽成一个函数，放进新文件 `internal/cli/topology.go`：

```go
// resolveTopology 解析依赖图并算出级联状态——up、down/status、sync 与 graph 共用的两步。
func resolveTopology(
    ctx context.Context, client *source.Client, cfg *config.Config,
) (*resolver.Graph, *cascade.Result, error)
```

三处现有调用点改成调用它，行为一字不变。这是一处真实的、值得顺手做的重构而不是新引入的抽象：
四处各写一遍意味着以后这两步的调用方式任何一处变了（比如 `Compute` 多要一个参数），都要记得同步四处。

> **更正（对着代码核实后）：** 早先的草稿写的是"`buildUpPlan` 开头三步（含 `shell.Resolve`）"，并让
> `resolveTopology` 返回 `[]shell.Group`。这是错的：`shell.Resolve` 需要 `inject.Build` 的结果（`*inject.Result`）
> 作输入，并且只在 compose / k8s 生成阶段被调用，不属于"拓扑"。`graph` 需要的外壳分组信息直接来自
> `cfg.Components[].ServedBy`（见 §2.4），不需要也不该跑一遍环境变量注入。

`--ignore-served-by` 的"清空所有 `servedBy`"这一小段（今天内联在 `buildUpPlan` 里）同样抽成
`clearServedBy(cfg *config.Config)`，放在同一个文件，`up` 与 `graph` 共用——它只清空、不打印，
各自决定怎么告诉使用者（见 §2.3）。

### 2.3 输出

- 只有 Mermaid（`graph TD`），只写 stdout，**没有 `--output <文件>` 参数**——要存文件用 `brickkit graph > graph.mmd`，
  平台不需要为"写文件"这件事再开一个关。
- **stdout 里只有 Mermaid，一个多余的字符都没有**——否则 `> graph.mmd` 存下来的文件不是合法的 Mermaid。所以：
  - `--ignore-served-by` 的提示写成 Mermaid 注释行（`%% 已忽略全部 servedBy 声明……`），而不是像 `up` 那样 `Printf`；
  - 依赖解析产生的警告（弱依赖取不到之类）写到 stderr；
  - 项目里没有组件时，输出 `graph TD` 加一行 `%% 当前项目没有组件`，退出码 0。
- 支持 `--ignore-served-by`（与 `up` 同名同义，因为共用 `clearServedBy`，成本接近零）。
- 支持全局 `--config`（root 的 persistent flag，新命令自动继承，不用额外接线）。
- 依赖解析失败（强依赖找不到、依赖成环……）时与 `up` 一样报错退出，报错文案就是 `up` 那一份——`graph` 不发明新的解释。
- 需要读组件 Manifest：市场 / Git 源里还没缓存的组件会联网取（和 `up --dry-run`、`status` 一样）。
  `graph` **不**承诺离线——承诺离线的是 `lint`（§3）。

### 2.4 Mermaid 具体形状

- **节点 ID**：`manifest.ServiceName(id, version)`（版本化服务名）再把 `-` 全部换成 `_`（`demo-hello-1-0-0` → `demo_hello_1_0_0`）。
  不能直接用服务名：组件 ID 的规则允许 `--`（`my--scope/a`）与任何单词作 scope（`graph/store`、`end/x`），而 Mermaid
  里含 `--` 的 ID 会被当成边、以 `end` / `style` / `class` / `graph` / `subgraph` / `flowchart` / `interpolate` 开头的 ID
  会被当成关键字，两种都让整段输出**静默变成任何渲染器都不认的文本**（`up` 对这些 ID 完全正常，`graph` 却退出码 0）。
  服务名只含 `[a-z0-9-]`，所以 `-`→`_` 是单射，不会让两个组件撞 ID。边一律写成 `A --> B`（箭头两侧留空格），
  避开 Mermaid 里 `o`/`x` 开头的节点名被吞进箭头的坑。
- **节点标签**：原始的 `id@version`，写在引号里（`["erp/backend@1.0.0"]`）；`local: true` 的节点标签追加一行
  `本地调试`（Mermaid 标签内 `<br/>` 换行）——使用者在 `brickkit.yaml` 里写了 `localPort` 时连端口一起标
  （`本地调试 :8081`），**没写时不画端口**：`localPort` 是可选的，没写时由 compose 在生成阶段分配
  （默认取组件自己声明的主端口，被占了才另选），图上算不出真值，宁可不说，也不编一个使用者会照着去连的假端口。
- **边**：强依赖（`resolver.Node.Requires`）实线 `-->`；弱依赖（`resolver.Node.Optional`）虚线 `-.->`；
  弱依赖里**取不到**的那些（`resolver.Node.MissingOptional`）同样画虚线，指向一个标签为 `id@version<br/>未安装` 的
  `missing` 节点——`up` 的"依赖图"一节今天就是把它们写成"（弱，未安装）"，图里不画等于悄悄丢掉一条声明过的依赖，
  而"为什么这个地址没注入"恰恰是这张图最该回答的问题之一。
  边总是画出来（不管对方这次有没有实际启动）——图要展示的是**声明的结构**，"启动与否"用节点样式表达
  （见下一条），两件事分开表达，别混在一起。
- **样式（`classDef` + `class`，不逐节点写 `style`；某个 class 没有节点用到时不输出它的 `classDef`）**：
  - `classDef disabled fill:#eee,stroke:#999,color:#999;`——套给 `cascade.Result` 里没有运行的组件
    （被 `enabled: false` 关掉的与跟着上层一起不跑的，两种都算）。
  - `classDef local fill:#e6f2ff,stroke:#3673a8;`——套给 `local: true` 的组件。
  - `classDef missing fill:#fff4e5,stroke:#c77700,stroke-dasharray:4 3;`——套给上面说的"未安装"占位节点。
  - `local` 只套给**在跑**的 `local: true` 组件。`cascade` 从不读 `local`——`local: true` 的组件与别的组件一样
    跟着上层走，也可能被跳过——所以"`local` 与 `disabled` 不会同时出现"靠**构造**成立（不在跑的节点只套 `disabled`，
    标签里的"本地调试"仍保留），而不依赖各家 Mermaid 渲染器怎么合并同一个节点上的两个 class。
    "置灰 = 这次不会启动"因此是唯一的信号。
- **`servedBy` 外壳分组**：来自 `brickkit.yaml` 里各组件条目自己的 `servedBy`（用 `shell.ParseRef` 拆成外壳的
  `id@version`）。一个 `subgraph <外壳节点 ID>_members["外壳：<外壳 id@version>"] ... end` 包住这个外壳收编的全部成员节点；
  子图 ID 带 `_members` 后缀，是因为外壳自己也是一个以它的节点 ID 为 ID 的普通节点（它是独立的容器，画在 `subgraph` 外面，
  收编的成员才没有自己的容器），两者不能同名。节点 ID 总是以 `_<数字>_<数字>_<数字>` 结尾，
  所以带 `_members` 的 ID 永远不会跟任何组件节点撞名。
  外壳不在图里（`servedBy` 指向的目标不存在）时照样画出这个分组——`graph` 展示的是声明的结构；
  目标是否存在是 `up` 在生成阶段报的事。

### 2.5 不做什么

- 不做 HTML/SVG（§1 已裁决）。
- 不做"只画某个组件为中心的局部子图"这类过滤参数——v1 就是"画出这次 `up` 会考虑的全部拓扑"，
  过滤是明显的 YAGNI（没有人问过这个）。
- 不新增任何生成逻辑或状态——纯读取 `resolveTopology` 已经算出来的结果。

## 3. `brickkit lint`

### 3.1 做什么

新增命令 `brickkit lint`，对着当前目录**离线**（不联网、不需要 Docker/K8s）跑一遍结构校验，输出错误/警告列表，
退出码 0（干净）或 1（有结构性错误，或 `--strict` 下有警告）。

**四类错误全部复用已有的规则，一条新规则都不增加**——但对着代码核实后，"已有"分成两种：

| 类别 | 已有的规则在哪 | `lint` 怎么用它 |
| --- | --- | --- |
| 必填字段缺失、类型错误、未知字段、端口范围、版本号格式 | `manifest.Parse` + `Manifest.Validate`（component.yaml）、`config.ParseConfigFile` + `Config.Validate`（brickkit.yaml）——今天 `up`/`add`/`publish` 一律会跑 | 直接调用 |
| `configSchema.properties.<key>` 里拼错的键（如 `defualt:`） | `manifest.PropertyKeyWarnings`——今天只在 `publish` 与 `add --local` 时跑 | 直接调用，结果是警告 |
| 配置项名字撞上平台保留变量 | **不在 `Validate` 里。** 今天只有两处：市场发布时拒绝（`market-server/internal/validator`），与 `up` 注入时警告并跳过该项（`inject.envBuilder.matchReserved`）——组件作者在自己的仓库里、发布之前，没有任何办法提前知道 | `inject` 新导出 `ReservedKeyWarnings(m)`：把 `matchReserved` 里"不依赖 envPrefix 的那部分"提成包级函数，`up` 注入与 `lint` 共用同一份，结果是警告（与 `up` 的严重程度一致：写错一个配置项名不该让整个项目起不来）。`envPrefix` 是使用者在 `brickkit.yaml` 里定的，组件仓库里看不到，所以不在检查范围内——与市场发布时看不到它是同一个道理 |

### 3.2 真正的缺口：入口，不是规则

实测确认了两处今天没有任何命令能覆盖的场景：

1. **独立组件仓库**（只有 `component.yaml`、没有 `brickkit.yaml`）——AGENTS §8 明确说这是被支持的场景
   （`brickkit skills` 已经认这个模式），但没有任何命令能对着这一种仓库单独跑校验。
2. **已经 `add --local` 过的本地组件**——读代码确认：`internal/cli/add_local.go` 对"已经在 `brickkit.yaml`
   里的同版本组件"是**静默跳过**（`configured` 分支），不会重新校验。编辑一份已加入的 `component.yaml` 引入拼写错误，
   唯一会发现的时机是真的跑 `up`（需要引擎、走完整级联计算），没有一个轻量、离线、秒回的校验入口。

### 3.3 双模式——复用一个共享的判断函数

`internal/cli/skills.go` 的 `skillsInstaller` 函数里内联了"项目 vs 独立组件仓库"的判断（两个 `os.Stat`
+ "两者都有时按项目算"的优先级规则 + 都没有时报错）。把这段判断抽成一个共享函数：

```go
// detectScope 判断当前目录是 BrickKit 项目（有 brickkit.yaml）还是独立组件仓库
// （有 component.yaml、没有 brickkit.yaml）；两者都没有时返回错误。两者都有时按项目算——
// 这是 brickkit skills 一直以来的行为，lint 沿用同一条规则，不新发明一条。
func detectScope(opts *Options) (skills.Scope, config.Layout, error)
```

放进 `internal/cli/skills.go`（它是这段逻辑原来的家），`skillsInstaller` 与新的 `lint.go` 都调它。

- **项目模式**：先校验 `brickkit.yaml` 本身（`config.ParseConfigFile`，跟 `up` 用的是同一份逻辑）；
  通过之后，再枚举本地安装源（`sources[].type: local` 且启用的）目录下**每一个** `<scope>/<name>/component.yaml`
  （不管有没有被 `add` 过），逐个跑 `manifest.Parse`（含 `Validate`）+ `PropertyKeyWarnings`，
  并核对"目录名拼出来的组件 ID"与 `metadata.id` 一致（`add --local` 今天就是靠这一条把不一致的组件跳过的）。
  `brickkit.yaml` 自己没通过时，本地源在哪儿都不可信，所以跳过这一步并明说跳过了。
- **独立组件仓库模式**：只校验当前目录这一份 `component.yaml`，同样跑 `Parse` + `PropertyKeyWarnings`。

**枚举本地源文件的方式（`internal/source` 新增一个方法）。** 不走 `Client.Manifest`：它会把取到的 Manifest
写进 `.brickkit/manifests/` 缓存，而 `lint` 必须是纯只读；也不走 `LocalComponents`：它先过一遍"表头"筛选
（能解析出 id、版本精确），表头不合格的文件被当成"问题"单独返回，而这些正是 `lint` 要完整报告的对象，
且它只给一句话原因，报不出 `Validate` 那种逐字段的全部问题。所以新增：

```go
// LocalManifestFile 是本地安装源目录下的一份 component.yaml。
type LocalManifestFile struct {
    ID       string // 按目录名（<scope>/<name>）拼出来的组件 ID
    SourceID string // 提供它的安装源 id
    Path     string // 这份文件的完整路径
}

// LocalManifestFiles 列出所有启用的本地安装源里、活跃目录下的 component.yaml。
func (c *Client) LocalManifestFiles() ([]LocalManifestFile, error)
```

它与 `listComponents` 共用同一段"扫 `<scope>/<name>`、跳过点开头的目录、跳过非法组件 ID"的目录遍历
（从 `listComponents` 里抽出来，不复制第二份）。`.archived/` 因此同样不扫——与 `add --local` 一致：
归档目录里的是 `sync` 挪开的、暂时不用的那份，不是使用者此刻在编辑的文件。

`lint` 用 `source.New(layout, cfg, source.Options{})` 直接构造客户端，**不**走 `newSourceClient`：后者会先按
`installer.publicKeys` 去读公钥文件，而公钥文件缺失是 `up`/`add` 该报的事，不该让一条"离线校验 YAML"的命令因此失败。
`source.New` 本身不联网——三种安装源都是惰性的，只有真去取 Manifest 才会碰网络，而 `lint` 从不取。

### 3.4 明确不做

- 不解析依赖图，不检查 `servedBy` 指向的组件是否真实存在于项目里（那需要完整依赖图，天然要联网/读 manifest 缓存，
  跟"离线秒回"的定位不是一回事，留给 `up`/`add` 在生成阶段报）。
- 不校验市场/Git 源已缓存的组件（那些在 `add` 时已经校验过一次）——只管本地源目录下、使用者自己能编辑的文件。
- 不新增任何校验规则——`configSchema` 里作者自己声明的 `enum`/`minimum` 这类，§9.12 明确不校验值，`lint`
  同样不碰这条线。

### 3.5 输出、`--strict` 与退出码

- 报告写到 **stdout**（它是这条命令的产出，不是"命令失败了"的错误）：每个检查过的文件，干净的一行 `✅ <路径>`；
  有问题的直接打印它的 `clierr.Error` 块（错误 `❌`、警告 `⚠️`，块里已经带着文件路径与逐字段的全部问题，
  不再另起一行标题）；最后一行汇总"检查了几个文件、几个有错误、几条警告"。
- 不加 `--strict`：有错误 → 退出码 1，只有警告 → 退出码 0。
- 加 `--strict`：警告也算，退出码跟着变成 1——给 CI 门禁用。
- 失败时命令返回一个汇总错误，走 stderr 与 JSON 日志行的老路径。它用**新增的**错误码 `LINT_FAILED`，写进
  `docs/{en,zh}/06-architecture/10-error-codes.md`（`tests/docfields` 会拦住漏写）。不复用 `CONFIG_INVALID` /
  `MANIFEST_INVALID`：一次 lint 可以两者兼有，汇总只能带一个码；而 CI 脚本要区分的恰恰是
  "lint 查出了问题"和"配置读不出来"。
- 不新发明输出格式：块的排版就是 `clierr.Error.Format()`，跟 `up`/`add` 打印警告的方式一致。

## 4. JSON Schema 生成

### 4.1 做什么

从 `manifest.Manifest`（component.yaml）与 `config.Config`（brickkit.yaml）的 Go struct 通过反射生成两份
JSON Schema，落盘到仓库根目录新建的 `schemas/` 目录：`schemas/component.schema.json`、
`schemas/brickkit.schema.json`。使用者在自己的 YAML 文件顶部写一行
`# yaml-language-server: $schema=https://raw.githubusercontent.com/brickKit/brickKit/main/schemas/component.schema.json`
（或在编辑器里配置 `yaml.schemas` 映射），就能获得字段补全、类型提示、未知字段红线。

### 4.2 生成器

新增 `internal/schemagen` 包，反射 `manifest.Manifest`/`config.Config` 及其全部嵌套类型，产出 JSON Schema draft-07
（Red Hat YAML 扩展——VS Code 的 YAML 支持——认这个版本）：

- 每个字段的 JSON Schema `properties.<name>`：字段名取 yaml tag（去掉 `,omitempty` 等修饰符；`-` 的跳过）；
  类型按 Go 类型映射（`string`→`string`，`int`→`integer`，`float`→`number`，`bool`→`boolean`，slice→`array`，
  嵌套 struct→嵌套 `object`，`map[string]T`→`additionalProperties`，`any`→不加约束）。
- **必填字段**：yaml tag 没写 `,omitempty`、**不是 `bool`、不是指针**、且没有标 `jsonschema:"optional"` 的字段进 `required` 列表
  （指针天然可缺省——真实类型里的指针字段全都带 `omitempty`，所以"不是指针"目前不改变任何真实字段的结果，只是让规则自洽）。
  这条规则是逐个核对过 `manifest.Validate` / `config.Validate` 之后定的，不是只看了 `Metadata`：
  `bool` 必须排除，因为 `NetworkPolicy.enabled`、`Egress.enabled`、`ServiceAccount.enabled`、
  `ComponentDep.optional` 都没写 `omitempty`，却并不必填。规则由一份**手写的、按类型列出的必填集合**在测试里钉住
  （`TestRequiredFieldsMatchValidators`）——生成器哪天改了这条规则，测试会告诉你哪个类型的必填集合变了。
- **不必填的字段允许写成显式的 `null`**（`type` 写成 `["array", "null"]` 这种联合形式；`enum` 里补上 `null`）。
  原因：一个小节下面的条目全被注释掉时（`dependencies:` 后面只剩注释），YAML 把它读成 `null`，CLI 接受（解码成零值），
  schema 若拒绝就是编辑器对一份 CLI 认可的文件画红线——违反"schema 永远不比校验器更严"。必填字段不加：
  `port:` 写成 null 会解码成 0，校验器报"缺失"，schema 同样该拒绝。
- `additionalProperties: false`——镜像"Manifest 没有扩展字段机制，未知键直接拒绝"这条平台规则（AGENTS §6）。
  `map` 类型（`config`、`labels`、`configSchema.properties`……）里键是使用者自己定的，不受这条限制。
  **`map` 的值是 struct 时**（`configSchema.properties.<键>` 及其下的 `items`），`yamlcheck.Walk` 不往里下钻，
  `manifest.Parse` 对里面的多余键一声不吭——但那些键**不会生效**（`defualt` 拼错，组件就拿不到默认值），CLI 因此在
  作者自己听得到的地方警告（`PropertyKeyWarnings`：`lint` / `publish` / `add --local`）。schema 在这里仍然封闭：
  编辑器标红与 CLI 的警告说的是同一件事。这是一处**有意的**"schema 比 Parse 更严"，与下面 §4.5 列的另外两处一起，
  写进 `schemas_test.go` 顶部的已知例外清单，并在 `checkNode` 里为这两个节点开带注释的例外——让下一个人看得见这是决定而不是疏忽。
- **自定义解码逻辑的类型需要单独交代**：`manifest.ComponentDep` 既能写成字符串
  （`department/tree@1.0.0`）也能写成 `{id, optional}` 映射，反射看不出来。生成器里有一张小小的
  "类型 → 手写 schema"覆盖表，目前只有这一项；遇到 yaml.v3 会当作自定义解码来处理、却不在表里的类型——
  `UnmarshalYAML`（新旧两种签名都算）或 `encoding.TextUnmarshaler`——生成器直接报错
  （而不是生成一份悄悄错误的 schema）。同理，`,inline` 与 `time.Duration` 这两种 yaml.v3 会特殊对待的形状也报错，
  不去猜它们该长什么样。
- 递归类型不支持（目前两份 schema 里没有），遇到时生成器报错而不是无限展开——结构体、以及经由具名 slice / map 的递归都算。

### 4.3 富约束——新增一个小的 struct tag

纯反射只能拿到"字段存在、类型是什么、必填不必填"，拿不到 `deploy.target` 只能是 `docker`/`k8s`、
版本号要匹配 `x.y.z`、端口要在 1-65535 这类约束——而这些恰恰是"IDE 红线"最有价值的部分。
给以下这一小批字段（都是 Go 代码里已经有对应常量/校验规则的**封闭取值集合**）加一个新的 struct tag
`jsonschema:"..."`，生成器读这个 tag 补上对应的 JSON Schema 关键字：

| 字段 | tag 内容 | 依据 |
| --- | --- | --- |
| `config.Deploy.Target` | `enum=docker\|k8s` | `internal/config/validate.go`：`case TargetDocker, TargetK8s` |
| `config.Source.Type` | `enum=market\|git\|local` | `config.SourceType*` 常量；`source.newFetcher` 对其余值报错 |
| `manifest.Manifest.APIVersion` | `enum=brickkit/v1` | `manifest.APIVersion` |
| `manifest.Manifest.Kind` | `enum=Component` | `manifest.Kind` |
| `manifest.Metadata.Version`、`config.Component.Version` | `pattern=^[0-9]+[.][0-9]+[.][0-9]+$` | `manifest.IsExactVersion`（正则 `^\d+\.\d+\.\d+$`；tag 里写成不含反斜杠的等价形式）；`brickkit.yaml` 里 `components[].version` 是使用者最常敲版本号的地方，`^1.0.0` 在编辑器里就该红（AGENTS §9.2） |
| `manifest.Deployment.Type` | `enum=container` | `manifest.DeploymentTypeContainer` |
| `manifest.Deployment.Port` / `ExtraPort.Port` | `minimum=1,maximum=65535` | `manifest.MinPort`/`MaxPort` |
| `manifest.HealthCheck.Type` | `enum=http\|tcp\|none` | `manifest.HealthCheckHTTP/TCP/None` 常量 |
| `manifest.ResourceDep.Kind` / `config.Resource.Kind` | `enum=database\|cache\|mq\|storage\|search\|smtp` | `manifest.ResourceKinds` |
| `manifest.ConfigProperty.Type` | `enum=string\|integer\|number\|boolean\|array\|object` | `manifest.configSchemaTypes` |

（`apiVersion`/`kind`/`deployment.type`/`sources[].type` 四行是设计书初稿没有的：它们与其余各行是同一类——
封闭取值、`Validate` 里有对应的精确比较——而且正是使用者敲 `apiVersion: ` 之后最想让编辑器补全的东西。）

tag 语法：关键字之间用 `,` 分隔，`enum` 的取值之间用 `|` 分隔；取值与 `pattern` 里不能出现 `,`、`|`
（也就不用在 struct tag 里转义反斜杠）。另有一个不带 `=` 的关键字 `optional`：把一个没写 `omitempty` 的字段挪出 `required`
（目前只有 `manifest.ItemDef.Type` 用它——校验器从不检查 `items.type`，`items` 只是说明书）。
生成器不认识的关键字、重复的关键字、空的 `enum` 取值、关键字与字段类型对不上，一律直接报错，写错不会悄悄不生效。

**tag 是"额外的一份真相"，怎么防它与校验代码不一致。** 校验规则还在 `Validate` 里，tag 里抄了一份取值。
初稿把它标成"这个设计唯一没有自动防漂移的地方"。可以做得更好：`TestConstraintsAgreeWithValidators`
对上表每一行，拿一份真实合法的 component.yaml / brickkit.yaml，把那个字段依次改成"tag 说合法的每个取值"
（`manifest.Parse` / `config.ParseConfig` 必须通过）与"tag 说不合法的取值"（必须失败）。
校验代码改了而 tag 没跟着改，这个测试会红。

### 4.4 防漂移与落盘

新增 `internal/schemagen` 的一个测试：在内存里重新生成一遍两份 schema，跟 `schemas/` 目录下签入的文件做
字节比对，不一致就失败并提示"跑 `make generate-schemas`"——与 `tests/docfields` 同一个套路。
它同时挂进 `make lint`（新目标 `check-schemas`，跟 `check-doc-fields` 并列），不只是躲在 `make test` 里。

落盘由 `make generate-schemas` 触发，实际执行体是一个不进最终 `brickkit` 二进制的小工具
（`cmd/gen-schemas/main.go`，调用 `internal/schemagen` 的导出函数，写文件）。

### 4.5 文档

- `docs/{en,zh}/00-quick-start.md` 补一小节："给编辑器接上自动补全"，给出两份文件各自的 `$schema` 注释写法
  与 VS Code `yaml.schemas` 配置写法；并讲清一个已知的边界：`brickkit.yaml` 解析时会先展开 `${VAR}` 再校验，
  所以 `deploy.target: ${TARGET}` 这种写法 CLI 接受、schema 会标红——schema 校验的是**字面文本**，封闭取值的字段请写字面值。
  完整的"schema 比 CLI 更严"的已知边界共三处，文档里如实列出：① `${VAR}` 写进封闭取值的字段；② yaml.v3 会静默放过、schema 却标红的
  形状：不加引号的非字符串标量写进字符串字段（`project: 2024`、`password: 123456`——yaml.v3 一律照字面转成字符串），
  以及列表里的 `null` 元素（`tags: [null]`、`dependencies.components: [null]`——yaml.v3 静默丢掉）；保留它们是因为放宽会把类型提示的
  价值整个抹掉，而这些写法本来就是笔误或该加引号的值；③ `configSchema` 属性声明里的多余键
  （CLI 只警告，见 §4.2）。
- `07-component-yaml-reference.md`/`08-brickkit-yaml-reference.md` 顶部各加一句指向对应 schema 文件的链接。
- AGENTS §11.2 的仓库地图加 `schemas/`、`internal/schemagen/`、`cmd/gen-schemas/` 三行。

## 5. 跨两个命令的收尾工作

新增两个命令（`graph`、`lint`）会牵动：

- `AGENTS.md`/`AGENTS.zh.md` §8（命令表，数目从 14 变 16）、§11.2（仓库地图）。
- `docs/{en,zh}/06-architecture/09-cli-reference.md`：两个新命令各自的完整 flag 参考 + 真实生成输出。
- `scripts/check-cli-docs.py` 的命令数量断言（`COUNT_CLAIM`）会自动重新核对，不用改脚本本身，但要把
  所有写死"14 个命令"字样的地方找出来改成 16（`git grep -n "14 个命令\|14 commands"`）。
- `llms.txt`/`llms.zh.txt`：命令列表补两行。
- `docs/{en,zh}/08-troubleshooting.md`：如果 `lint`/`graph` 有值得记录的常见误用，顺手补一条（内容在实现阶段
  真跑出来再定，不在这里预先编）。
- `README.md`/`README.zh.md` 里那一行命令清单（"14 commands in total"）；`CHANGELOG.md` 的 `[Unreleased]`。
- 装进项目的 AI 助手技能（`internal/skills/assets/`）：`brickkit-component` 技能补一句"写完 component.yaml 先跑
  `brickkit lint`"——这正是独立组件仓库模式服务的场景。

## 6. 范围之外（本次明确不做）

- `brickkit graph` 的 HTML/SVG 输出（§1 已裁决）。
- `brickkit lint` 的跨文件引用检查（`servedBy` 目标是否存在于项目里）——留给 `up`/`add`。
- 校验 `configSchema` 里作者自己声明的 `enum`/`minimum` 这类值本身（§9.12 边界不变）。
- `graph`/`lint` 的 `--output <文件>` 参数（用 shell 重定向）。
- 除本文档 §4.3 列出的 5-8 个字段外，不给其余字段追加 `jsonschema` tag——按需再加，不预先覆盖所有字段。
