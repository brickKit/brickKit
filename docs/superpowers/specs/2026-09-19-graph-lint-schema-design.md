# `brickkit graph` / `brickkit lint` / JSON Schema：设计

> 来源：仓库根目录未跟踪的 `改进计划.md`（第二次更新后的版本，"依赖拓扑可视化与结构级 Manifest Linter"）。
> 那份文件是一次性的、会被删除；本文档自足，删掉它不损失任何依据。

## 1. 背景与范围裁决

`改进计划.md` 提了两条提案：`brickkit graph`（依赖拓扑可视化）与 `brickkit lint`（结构级校验 + JSON Schema）。
逐条对着仓库核实后的结论（细节见 §2-§4，这里先给结论）：

| 提案原话 | 裁决 | 为什么 |
| --- | --- | --- |
| `brickkit graph`：Mermaid + HTML/SVG 两种输出 | **只做 Mermaid，砍掉 HTML/SVG** | Mermaid 在 GitHub、VS Code 里已经原生可交互渲染；自己再造一个 HTML/SVG 渲染器是重新发明已经免费拿到的东西，直接违反"平台极简"（每一个额外渲染器都是永久维护成本） |
| `brickkit lint`：四类结构错误 + 跨文件引用检查 | **四类错误的校验逻辑本来就已经存在，只是没有独立入口；跨文件引用检查明确不做** | 实测确认 `manifest.Validate`/`config.Validate` 早就覆盖了拼写、类型、保留变量冲突、版本格式；真正缺的是"不用 `add`/`up` 就能单独跑一遍"这个入口。跨文件引用（`servedBy` 目标是否真实存在）需要完整依赖图，天然要联网，跟"离线秒回"的定位不是一回事，留给 `up`/`add` |
| JSON Schema：随便写一份 | **从 Go struct 反射生成，不手写** | 手写的 schema 是仓库里从没有过的一种"容易悄悄过期"的东西，跟 `tests/docfields` 已经在防的问题是同一类；生成 + 防漂移测试才是"架构正确的完整方案" |

三处都已经过 `AskUserQuestion` 得到确认：graph 只做 Mermaid；lint 按收窄范围；schema 用 struct tag 表达
enum/pattern 这类约束（而不是纯反射，纯反射拿不到 `deploy.target` 只能是 `docker`/`k8s` 这类约束，而这恰恰是
提案最想要的"IDE 红线"效果）。

## 2. `brickkit graph`

### 2.1 做什么

新增命令 `brickkit graph`（连同 `brickkit lint`，命令总数从 14 个变成 16 个），把当前项目的依赖拓扑渲染成 Mermaid 代码，
打印到 stdout。数据来源与 `up --dry-run` 完全相同（`resolver.Graph` + `cascade.Result` + `shell.Group`），
但跳过 `up` 才需要的东西：镜像权限检查、迁移展示、引擎解析、生成部署文件。

### 2.2 复用而不是重复计算——抽出 `resolveTopology`

`internal/cli/up.go` 的 `buildUpPlan` 开头三步（`resolver.New(...).ResolveConfig` → `cascade.Compute` →
`shell.Resolve`）是 `graph` 唯一需要的东西。把这三步抽成一个新函数，放进新文件 `internal/cli/topology.go`：

```go
// resolveTopology 算出一个项目的依赖图、级联状态与 servedBy 分组——
// up 与 graph 共用的部分，到这里为止两者需要的信息完全一致。
func resolveTopology(
    ctx context.Context, opts *Options, layout config.Layout, cfg *config.Config, ignoreServedBy bool,
) (*resolver.Graph, *cascade.Result, []shell.Group, error)
```

`buildUpPlan` 改成调用 `resolveTopology`，不再自己内联这三步——这是一处真实的、值得顺手做的重构（不是新引入的抽象）：
两处各写一遍"解析依赖图 → 算级联 → 算 servedBy 分组"，以后这三步里任何一步的调用方式变了（比如 `shell.Resolve`
需要的参数变化），都要记得同步改两处，是一个真实存在、没必要留着的维护负担。

### 2.3 输出

- 只有 Mermaid（`graph TD`），只写 stdout，**没有 `--output <文件>` 参数**——要存文件用 `brickkit graph > graph.mmd`，
  平台不需要为"写文件"这件事再开一个关。
- 支持 `--ignore-served-by`（与 `up` 同名同义，因为共用 `resolveTopology`，成本接近零）。
- 支持全局 `--config`（root 的 persistent flag，新命令自动继承，不用额外接线）。

### 2.4 Mermaid 具体形状

- **节点 ID**：复用 `manifest.ServiceName(id, version)`（已经是合法的 Mermaid 标识符：小写、`/`/`.` 都换成 `-`），
  不新发明一套命名规则。
- **节点标签**：原始的 `id@version`；`local: true` 的节点标签追加一行 `本地调试 :<port>`（Mermaid 标签内
  `<br/>` 换行）；`servedBy` 成员标签不特殊处理（它已经被包进所属外壳的 `subgraph` 里，参见下一条）。
- **边**：强依赖（`resolver.Node.Requires`）实线 `-->`；弱依赖（`resolver.Node.Optional`）虚线 `-.->`。
  边总是画出来（不管对方这次有没有实际启动）——图要展示的是**声明的结构**，"启动与否"用节点样式表达（见下一条），
  两件事分开表达，别混在一起。
- **样式（`classDef` + `class`，不逐节点写 `style`）**：
  - `classDef disabled fill:#eee,stroke:#999,color:#999;`——套给 `cascade.Result` 里没有运行的组件。
  - `classDef local fill:#e6f2ff,stroke:#3673a8;`——套给 `local: true` 的组件。
  - 一个组件可能同时是 `disabled` 又不可能是 `local`（`local: true` 就在跑，二者互斥，不需要处理"两个 class 一起套"
    的情况）。
- **`servedBy` 外壳分组**：一个 `subgraph <外壳的服务名>["外壳：<外壳 id@version>"] ... end` 包住这个外壳收编的全部成员节点。
  外壳自己的节点画在 `subgraph` 外面（它是独立的容器，收编的成员才没有自己的容器）。

### 2.5 不做什么

- 不做 HTML/SVG（§1 已裁决）。
- 不做"只画某个组件为中心的局部子图"这类过滤参数——v1 就是"画出这次 `up` 会考虑的全部拓扑"，
  过滤是明显的 YAGNI（没有人问过这个）。
- 不新增任何生成逻辑或状态——纯读取 `resolveTopology` 已经算出来的结果。

## 3. `brickkit lint`

### 3.1 做什么

新增命令 `brickkit lint`，对着当前目录**离线**（不联网、不需要 Docker/K8s）跑一遍结构校验，输出错误/警告列表，
退出码 0（干净）或 1（有结构性错误，或 `--strict` 下有警告）。

**校验的四类错误全部复用已有逻辑，一条新规则都不增加：**

| 类别 | 复用的现成逻辑 |
| --- | --- |
| 必填字段缺失、类型错误、端口范围、版本号格式 | `manifest.Parse` + `Manifest.Validate`（component.yaml）、`config.ParseConfig` + `Config.Validate`（brickkit.yaml） |
| `configSchema.properties.<key>` 拼写笔误（如 `defualt:`） | `manifest.PropertyKeyWarnings`（今天只在 `publish`/`add --local` 时跑） |
| 保留变量冲突 | 已经是 `Validate`/注入阶段警告的一部分 |

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
func detectScope(opts *Options) (scope skills.Scope, layout config.Layout, err error)
```

放进 `internal/cli/skills.go`（它是这段逻辑原来的家），`skillsInstaller` 与新的 `lint.go` 都调它。

- **项目模式**：校验 `brickkit.yaml` 本身（`config.ParseConfigFile` + `Validate`，跟 `up` 用的是同一份逻辑）；
  再扫描本地安装源目录（`sources[].type: local` 声明的路径）下**每一个** `component.yaml`（不管有没有被
  `add` 过），逐个跑 `manifest.Parse` + `Validate` + `PropertyKeyWarnings`。
- **独立组件仓库模式**：只校验这一份 `component.yaml`，同样跑 `Parse` + `Validate` + `PropertyKeyWarnings`。

### 3.4 明确不做

- 不解析依赖图，不检查 `servedBy` 指向的组件是否真实存在于项目里（那需要完整依赖图，天然要联网/读 manifest 缓存，
  跟"离线秒回"的定位不是一回事，留给 `up`/`add` 在生成阶段报）。
- 不校验市场/Git 源已缓存的组件（那些在 `add` 时已经校验过一次）——只管本地源目录下、使用者自己能编辑的文件。
- 不新增任何校验规则——`configSchema` 里作者自己声明的 `enum`/`minimum` 这类，§9.12 明确不校验值，`lint`
  同样不碰这条线。

### 3.5 `--strict` 与退出码

- 不加 `--strict`：结构性错误（必填缺失、类型错误、未知字段……）退出码 1；警告（`PropertyKeyWarnings` 一类）
  只打印，退出码 0。
- 加 `--strict`：警告也算错误，退出码跟着变成 1——给 CI 门禁用。
- 输出格式复用现有的 `clierr.Error`/`renderWarnings` 风格，跟其余命令保持一致，不新发明一套格式。

## 4. JSON Schema 生成

### 4.1 做什么

从 `manifest.Manifest`（component.yaml）与 `config.Config`（brickkit.yaml）的 Go struct 通过反射生成两份
JSON Schema，落盘到仓库根目录新建的 `schemas/` 目录：`schemas/component.schema.json`、
`schemas/brickkit.schema.json`。使用者在自己的 YAML 文件顶部写一行
`# yaml-language-server: $schema=https://raw.githubusercontent.com/brickKit/brickKit/main/schemas/component.schema.json`
（或在编辑器里配置 `yaml.schemas` 映射），就能获得字段补全、类型提示、未知字段红线。

### 4.2 生成器

新增 `internal/schemagen` 包，反射 `manifest.Manifest`/`config.Config` 及其全部嵌套类型，产出：

- 每个字段的 JSON Schema `properties.<name>`：字段名取 yaml tag（去掉 `,omitempty` 等修饰符）；
  类型按 Go 类型映射（`string`→`string`，`int`→`integer`，`bool`→`boolean`，slice→`array`，
  嵌套 struct→嵌套 `object`，`map[string]T`→`additionalProperties`）。
- **必填字段**：yaml tag 没写 `,omitempty` 的字段进 `required` 列表（已核实这是仓库现有的约定——
  `Metadata.ID`/`Name`/`Version` 等真正必填的字段确实都没有 `,omitempty`）。
- `additionalProperties: false`——镜像"Manifest 没有扩展字段机制，未知键直接拒绝"这条平台规则（AGENTS §6）。

### 4.3 富约束——新增一个小的 struct tag

纯反射只能拿到"字段存在、类型是什么、必填不必填"，拿不到 `deploy.target` 只能是 `docker`/`k8s`、
版本号要匹配 `x.y.z`、端口要在 1-65535 这类约束——而这些恰恰是"IDE 红线"最有价值的部分。
给以下这一小批字段（预计 5-8 个，都是已经在 Go 代码里能找到对应常量/校验规则的）加一个新的 struct tag
`jsonschema:"..."`，生成器读这个 tag 补上对应的 JSON Schema 关键字：

| 字段 | tag 内容 | 依据 |
| --- | --- | --- |
| `config.Deploy.Target` | `enum=docker,k8s` | `internal/config/validate.go`：`case TargetDocker, TargetK8s` |
| `manifest.Metadata.Version` | `pattern=^\d+\.\d+\.\d+$` | `manifest.IsExactVersion` |
| `manifest.Deployment.Port` / `ExtraPort.Port` | `minimum=1,maximum=65535` | `internal/config/validate.go` 的 `MinPort`/`MaxPort` |
| `manifest.HealthCheck.Type` | `enum=http,tcp,none` | `manifest.HealthCheckHTTP/TCP/None` 常量 |
| `manifest.ResourceDep.Kind` / `config.Resource.Kind` | `enum=database,cache,mq,storage,search,smtp` | `manifest.ResourceKinds` |
| `manifest.ConfigProperty.Type` | `enum=string,integer,number,boolean,array,object` | AGENTS §6 骨架里列的合法类型 |

这是一个新的标注约定：以后给这些"已知取值范围"的字段加校验规则时，要记得同步这个 tag（防漂移测试会在
tag 与代码实际校验规则不一致时不会自动发现——tag 是"额外的一份真相"，需要人在改校验逻辑时想起来同步；
这是这个设计唯一没有自动防漂移的地方，值得在生成器的包注释里显著标出来）。

### 4.4 防漂移

新增 `internal/schemagen` 的一个测试：在内存里重新生成一遍两份 schema，跟 `schemas/` 目录下签入的文件做
字节比对，不一致就失败并提示"跑 `make generate-schemas`"——与 `tests/docfields` 同一个套路。

落盘由 `make generate-schemas` 触发，实际执行体是一个不进最终 `brickkit` 二进制的小工具
（`cmd/gen-schemas/main.go`，调用 `internal/schemagen` 的导出函数，写文件）。

### 4.5 文档

- `docs/{en,zh}/00-quick-start.md` 补一小节："给编辑器接上自动补全"，给出两份文件各自的 `$schema` 注释写法
  与 VS Code `yaml.schemas` 配置写法。
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

## 6. 范围之外（本次明确不做）

- `brickkit graph` 的 HTML/SVG 输出（§1 已裁决）。
- `brickkit lint` 的跨文件引用检查（`servedBy` 目标是否存在于项目里）——留给 `up`/`add`。
- 校验 `configSchema` 里作者自己声明的 `enum`/`minimum` 这类值本身（§9.12 边界不变）。
- `graph`/`lint` 的 `--output <文件>` 参数（用 shell 重定向）。
- 除本文档 §4.3 列出的 5-8 个字段外，不给其余字段追加 `jsonschema` tag——按需再加，不预先覆盖所有字段。
