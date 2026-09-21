# CLI 多语言支持 · 全量迁移设计（子项目 2）

> 子项目 1（核心机制）已完成并推送：`internal/msgid`/`internal/i18n`/`internal/userconfig`
> 三个包、`clierr.Format()` 接入 i18n、`brickkit version`/`PROJECT_MISSING`/root `--help`
> 三个纵向切片、`brickkit lang`/`lang set` 命令。模式已经验证过，不再有新的架构风险；
> 这份文档只定"怎么把这个模式安全地套到剩下 560 处字符串上"，不是重新设计机制本身。

## 1. 真实规模

```
251  clierr.New/Newf/Warn/NewProblemSet 调用点（Message）
 62  WithDetail/WithDetailf/WithHint/WithTip 挂载的明细与建议
 40  cobra 命令的 Short:/Long:/Example: 字段（35 个命令文件）
210  internal/cli 里直接 opts.Printf/Println 的提示文字
 21  JSON 日志（stderr）的 message 字段（logging.* 20 处 + root.go 里通过 level(...) 间接调用的 1 处）
———
580  合计
```

按包分布：`internal/cli`（24 个文件含 clierr 调用 + 35 个命令文件 + 210 处 Printf，体量最大）、
`internal/source`（8）、`internal/k8s`（7）、`internal/config`/`internal/manifest`（各 4，`config`
里 `PROJECT_MISSING` 那一处已经在子项目 1 转换过）、`internal/compose`/`internal/security`
（各 3）、`internal/market`/`internal/inject`/`internal/engine`（各 2）、`internal/workspace`/
`internal/shell`/`internal/resolver`/`internal/deploy`/`internal/cascade`（各 1）。其余包不直接
打印给用户看（架构上人类可读输出统一走 `internal/cli`），不需要处理。

## 2. 已经定下的约定

- **不写逐文件逐字符串的实施计划文档**——模式已经验证过，机械性强，按包直接执行，每包测试
  过、`go build`/`go vet` 过就提交，不需要子项目 1 那种step-by-step TDD 计划。
- **测试断言继续硬编码翻译后的英文字面量**，不改成通过 `i18n.T(msgid.X)` 反算期望值——
  跟子项目 1 一致。理由：改文案的措辞永远只改 `catalog_en.go`/`catalog_zh.go`，从不碰调用点
  本身；但"目录里某个 key 对应的文字翻译错了"这类 bug，只有断言写死期望文字才能测出来——
  两边都从同一份目录算期望值，测试就失去了这层独立校验。

## 3. 本次要定的约定

### 3.0 日志的 message 也跟着语言走（2026-09-21 决定）

JSON 日志里的键名（`command`、`elapsed_ms`、`error_code`……）是机器读的稳定接口，任何语言下
都是英文，不进目录；只有 `message` 这句给人看的话跟着语言变，统一放在 `internal/msgid/log.go`。
脚本要按稳定标识分支，用 `error_code`，不用 `message`——这条承诺本来就在，所以 message 变
语言不新增风险。日志的 `error` 字段是 `clierr.Error()` 的单行摘要，随错误文案本身一起本地化。
这一项已经在第 2 批之后单独完成（21 处）。

### 3.1 `internal/msgid` 按来源包拆文件

560 个 key 不能都堆进一个 `msgid.go`。按"改哪个源文件，就开哪个 msgid 文件"拆分，文件名跟
源包对应：`internal/msgid/source.go`（对应 `internal/source/`）、`k8s.go`、`config.go`、
`manifest.go`、`compose.go`、`security.go`、`market.go`、`inject.go`、`engine.go`、
`workspace.go`、`shell.go`、`resolver.go`、`deploy.go`、`cascade.go`。`internal/cli` 体量大，
再按命令分组拆（比如 `cli_publish.go`、`cli_login.go`、`cli_remove.go`……跟对应的命令文件一一
对应）。都还是 `package msgid` 下的普通常量声明，拆文件纯粹是为了"改哪个源文件时只用打开
对应那一个 msgid 文件"，不影响引用方式。

key 的字符串值延续子项目 1 的点分写法，按"来源包.具体内容"命名，保证在全局唯一（所有 key
共享一个 map，靠 `TestCatalogParity` 兜底，但命名本身也要一眼看出来自哪里，方便排查）：
`"source.git.clone_failed"`、`"k8s.deployment.missing_image"`、`"cli.publish.not_logged_in"`。

### 3.2 不建额外的抽取/生成脚本

考虑过写一个脚本从 `clierr.New(...)` 调用点自动抠出候选中文字符串、生成 key 骨架，减少手工
转录。放弃：真正费工夫的部分是"想清楚英文怎么措辞"和"把调用点正确改成 `i18n.T(...)`"，这两
步脚本都帮不上——脚本只能省下"复制中文原文"这一步，价值有限，还多一份要维护的一次性工具。
按包直接读源文件、逐条改，用子项目 1 已经验证过的 grep 技术做"这一批改完了没有"的核对
（改前改后各数一次剩余中文字面量，应该归零）。

### 3.3 分批顺序：小包先行，`internal/cli` 最后、且拆成多批

```
第 1 批  cascade + deploy + resolver + shell + workspace（各 1 处，合并一批）
第 2 批  market + engine + inject（各 2 处，合并一批）
第 3 批  compose + security（各 3 处，合并一批）
第 4 批  manifest（4 处）
第 5 批  config（剩下 3 处，PROJECT_MISSING 已转换）
第 6 批  k8s（7 处）
第 7 批  source（8 处）
第 8 批起 internal/cli：按命令文件分组，体量最大，拆成若干批（比如每批 3-5 个命令文件），
         每批同时处理该文件的 clierr 调用、Short/Long/Example、直接 Printf
```

每一批：改源码 → 加 msgid 常量 + 两份目录 → 改测试断言 → `go build && go test ./...`
（受影响的包）→ 用 grep 确认这批文件里的用户可见中文字面量归零 → 提交一次。不需要每批都跑
完整 `make lint`（太慢），但每处理完一个"大批"（k8s、source、以及 cli 的每个子批）跑一次
`go test ./internal/... ./tests/...` 确认没有牵连其他包（子项目 1 的教训：改共用渲染函数会
波及全代码库，这次虽然大多是各包独立的调用点，不太会有这类牵连，但 `tests/docfields` 的
`sourceTitles()` 是全代码库扫描，值得时不时跑一下）。全部批次做完后跑一次完整 `make lint`
收尾，跟子项目 1 的 Task 9 一样。

### 3.4 `check-guide-output.py` 的 `BRICKKIT_LANG=zh` 钉法不用动

子项目 1 里给它钉了 `BRICKKIT_LANG=zh`，因为 13 篇教程的快照全部是中文。这次迁移只是把
"中文写死在源码里"换成"中文写在 `catalog_zh.go` 里、通过 `i18n.T` 查出来"——**内容完全不
变**，所以钉住中文继续能让"真实输出"对上教程快照，不需要在这次迁移期间碰它。真正需要重新
设计它的时机是子项目 3（docs/en 配真实英文快照、docs/zh 配真实中文快照，检查逻辑不再要求
两边逐字相同）。

## 4. 执行中发现的补充约定（2026-09-21）

前三批一直按"clierr 调用点"数工作量，第 4 批盘点全仓库时发现这个口径漏了东西。下面是
修正后的约定，已经按它补做过一批（"补漏"提交）。

### 4.1 "直接当数据显示给用户"的中文，跟 clierr 文案一样要迁移

不经过 `clierr`、直接被 `Printf` 或当明细文字显示出来的中文同样是用户可见的：cascade 的
`Reason`（"启动（顶层）"）、workspace 的删除风险说明、shell 的端口占用方、resolver 的资源
绑定提示、gitrepo 的错误、`fmt.Errorf` 里的中文。统一用 `i18n.T` 在**产生它的地方**转换；
只有一个坑——**不能在包级 `var`/`const` 里调 `i18n.T`**（包初始化时语言还没确定），要么
改成函数，要么改成惰性取文案的类型（`gitrepo.ErrNotRepo` 就是后者）。

### 4.2 两类中文不进消息目录

- **标识常量**：`inject.Source*`、`shell.SourceServed` 只用来在代码里比较、分流，从不显示，
  改成语言中立的英文值（`"platform"`、`"endpoint"`……）。**判断标准**：这个值有没有被打印给
  使用者。有 → 标识和显示分开（值取中立英文，显示走 `i18n.T`，`skills.State*` 会这样处理）；
  没有 → 只改值。
- **开发者工具**：`internal/schemagen` 和 `cmd/gen-schemas` 只在 `make generate-schemas`
  时由开发者运行，报错读者是维护者不是使用者，保持中文，验收的 grep 排除它们。

### 4.3 生成文件的头注释与多行文案

`docker-compose.yaml`、`local-debug.*.env`、`brickkit new` 骨架里的说明注释都是用户会打开
看的文件，要跟着语言变。做法：一条目录文案就是一整段（内部用 `\n` 分行），由
`deploy.CommentBanner` / `manifest.commentBlock` 按行加 `# ` 前缀——各语言自己决定要几行，
排版只在一处定义。英文骨架里写进 YAML 值的占位（`description: TODO …`）**不能带冒号**，
否则会写出非法 YAML。

### 4.4 否定断言会空转

测试里 `assert.NotContains(x, "<中文短语>")` 在文案改成英文之后**永远通过**，等于那条检查
悄悄消失了（第 3 批就抓到两处：deploy 第 1 批起就在空转的"容器里连不上"，和 publish 的
"不会生效"）。每一批迁移后都要做两件事：把否定断言里的短语同步改成英文；再扫一遍——中文
短语只要已经出现在 `catalog_zh.go` 里，含它的否定断言就要人工过目（子串巧合的除外，比如
断言的是 cli 尚未迁移的输出）。全部迁移完成后，任何还留着中文短语的否定断言都是空转，
一律要清掉。

## 5. 验收

- 全仓库（`internal/`、`cmd/`，排除 §4.2 说的开发者工具）不再有用户可见的硬编码中文字面量——
  用 §1 的四类 grep 分别归零来确认，不是"看起来差不多"；另外扫一遍 §4.1 那类直接展示的文案。
- §4.4 说的否定断言清扫做完，没有空转的检查留下。
- `go build ./...`、`go vet ./...`、`go test ./...`、完整 `make lint` 全部通过。
- 默认（英文）与 `BRICKKIT_LANG=zh` 两条路径，用真实二进制各跑一遍受影响的命令，确认输出
  完整、没有中英夹杂。

## 6. 完成情况（2026-09-21）

**子项目 2 已完成。** 消息目录现有 1394 条（中英对等，`TestCatalogParity` 守着），
`internal/msgid` 按来源包拆成 53 个文件；`internal/`、`cmd/` 里用户可见的中文字面量已经清零
（唯一保留的是 §4.2 说的开发者工具，以及 `root.go` 里仅 zh 时才套用的 cobra 中文模板）。

实际做下来和 §3.3 的分批计划有出入，记在这里：

- 前三批按 `clierr` 调用点计数，漏掉了**不经过 clierr、直接当数据显示的中文**（§4.1），
  盘点后补了一批；`internal/yamlcheck`、`internal/clierr`、`internal/gitrepo`、
  `internal/skills` 也要迁，最初的包清单里没有它们。
- `internal/cli` 约 970 行，手工逐条改写太慢也太容易出错，改成**一次性 AST 工具**
  （`go/parser` 抽取字符串字面量、按调用点上下文改写成 `i18n.T(...)`；事后放进了仓库的 `tools/i18n/`，
  用法见那里的 README）：
  机械部分自动化，人工只写英文措辞。工具处理 `Printf` 的前后换行、`Errorf` 的 `%w`、
  参数里嵌套另一处中文、复用已有的同中文同英文 key。做不了的（包级常量、比较用的字面量）
  仍手工处理。
- manifest / config / compose / k8s / source 之间措辞一致的文案（校验问题、IO 错误、
  servedBy 警告标题、生成文件头注释……）抽成共享 key，并消除了两处重复实现
  （`yamlcheck.KindName`、`internal/yamlcomment`）。
- 生成的文件（compose / K8s 清单头注释、`brickkit.yaml` 与 `component.yaml` 骨架、
  `.gitignore` 注释、pre-commit hook 的说明注释）跟着语言走；`.gitignore` 是否已含某段
  只看规则行，换语言重跑不会重复追加。

验收（§5）的落实：

- 用户可见中文字面量清零 → `tests/i18nguard` 的 `TestNoHardcodedChineseInProductionCode`
  用 AST 守着，白名单每项写了理由；
- 否定断言清扫（§4.4）→ 同一个包的 `TestNoChineseNegativeAssertionsInTests`。迁移过程中共
  抓到并修掉三十多处空转或改错的否定断言；
- 默认（英文）与 `BRICKKIT_LANG=zh` 两条路径都用真实二进制跑过 init / new / add --local /
  lint / graph / up --dry-run / status / sync / skills / restore --check / remove / lang，
  以及未知命令、非法项目名、未登录等错误路径：英文输出零中文，中文输出与迁移前一致；
- `go build ./...`、`go vet ./...`、`go test ./...`、完整 `make lint` 全部通过，
  `internal` 覆盖率 93.3%（门槛 92%）。

## 7. 子项目 3（文档工具链）完成情况（2026-09-21）

**已完成。** 做了什么、为什么这样做：

- **英文单复数**（默认换成英文之后"1 components"成了看得见的毛病）：`i18n.TN(id, n, args...)` 在
  n==1 且目录里有 `id+".one"` 时选单数形式，否则回落到 id；`i18n.Count(id, n)` 给"数字加
  名词"的短语（`msgid.Count*` 一族：组件/资源/文件/项/警告/清单），句子里只留一个 `%s` 接它。
  跟着动词或代词变的整句（"1 file needs refreshing"、"its source"）走 `TN`。中文不写 `.one`
  条目。三道守卫：`TestCatalogParity` 放行单数条目、`TestPluralOneKeysComplementTheirBase`
  要求单数与"其他"形式用同一组参数、`tests/i18nguard` 要求 TN / Count 用到的每个 key 在英文
  目录里都有单数形式且没有孤儿单数条目。
- **教程输出核对**（`scripts/check-guide-output.py`）：34 个场景对 en、zh 各跑一遍，各对各的
  真实输出，不再要求两棵树抄同一份中文；夹具改写不依赖 `init` 写下的本地化注释。
- **docs/en 换成真实英文输出**：`tools/i18n/docs_outputs.py` 反译了 661 行，其余手改。docs/en
  现在一个中文字符都没有——`scripts/check-docs-bilingual.py` 永久守着（白名单只有 `brickkit lang`
  示例里刻意展示中文输出的那三行）。
- **错误码文档**按文档所在语言分别解析标题（`tests/docfields`），docs/en 引英文标题。
- **新增守卫 `TestDocOutputLinesConformToCatalog`**：所有文档围栏块里以符号开头的输出行必须
  匹配该文档语言的消息目录。它当场抓到了 `version` 输出两棵树都过期、一句 "use its IP"
  对真实的 "write its IP" 这类没有任何东西发现的漂移。
- `brickkit lang` 写进了 09-cli-reference、00-quick-start、AGENTS.md / AGENTS.zh.md
  （标题改成 16 commands + version + lang）、README、llms。`check-cli-docs.py` 现在也核对英文
  文档里的 "N commands"，并且行内代码里的命令只认自己那段代码（英文文档没有"第一个中文字符"
  这个边界）。
- `scripts/check-guides.sh` + `tests/guides/清单.tsv`（不在 make lint 里，所以之前没人发现它
  已经因为默认语言变英文而全部失败）固定 `BRICKKIT_LANG=en` 跑，关键词换成英文。
- demo/hello、demo/caller 两个教程夹具的运行时输出改成英文（响应文案、日志、默认问候语
  `Hello`、openapi summary），教程里的配置覆盖示例改用 `Howdy`。

**没做、也不属于文档工具链的三件事**（留给后续，见项目记忆里的记录）：

1. **AI 助手技能资产是纯中文的**（`internal/skills/assets/` 下 5 个文件，约 30KB）。
   `brickkit init` 会把它们装进每个新项目，英文用户拿到的是中文的 AGENTS.md 与技能。
   这需要一个产品决定，因为技能文件被提交进项目仓库，而语言偏好是机器级的：若按机器的
   语言装，两个语言不同的队友会互相把提交进仓库的文件改来改去。见记忆里的两个方案。
2. **市场服务端（`market-server`，另一个 Go module）的报错文案是中文**，CLI 把它原样接在
   `Reason` 等标签后面。要国际化得让 CLI 带上 `Accept-Language` 之类的东西，是服务端的子项目。
3. 其余 8 个夹具组件（部门树、权限、erp 等）的运行时中文输出没动——它们只在架构文档里出现
   寥寥几处，组件用什么语言打日志是组件自己的事；docs/en 引用它们的输出时是翻译后加了说明。
