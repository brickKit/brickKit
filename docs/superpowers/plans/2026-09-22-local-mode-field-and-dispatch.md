# `mode: local` 字段落地：Manifest `local:` 块 + 五态校验 + 安全的空生成 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 让 `mode: local` 从"不存在的取值"变成"能通过全部校验、生成阶段正确处理"的合法五态之一——`component.yaml` 新增可选的 `local:` 块（`language`/`runCommand`/`debugCommand`），`brickkit.yaml` 的 `Component.Mode` 从 `""/enabled/disable/debug` 四态扩展到五态。**本计划不启动任何进程**：`mode: local` 组件的效果是"不生成容器/Deployment、不生成迁移容器、被依赖它的组件正确地址寻址、`up`/`down`/`status`/`lint` 都不会因为它而报错或崩溃"——跟今天的 `mode: debug` 在生成层的效果完全对等，只是还没有人真的把裸进程拉起来（那是 Plan 4c 的事）。

**Architecture:** 这不是新建包，是给四类既有代码分别加一个新分支：① `internal/manifest` 新增一个可选顶层块的标准套路（照抄 `migration:` 的模式）；② `internal/config` 的 `Mode` 枚举与围绕它的六处校验（`validateComponentMode`/`validateComponentPorts`/`validateServedBy`×2/`validateReplicas`/`IsPinned`）——这六处全部只认 `Mode == ModeDebug` 的精确匹配，加 `ModeLocal` 就是在每一处并列加一个 `||` 分支，没有别的隐藏耦合；③ `internal/compose/compose.go` 里**唯一**一处决定"这个组件要不要生成容器"的分类点（`newPlan` 第 238 行），调研已确认它是黑名单式判断（默认生成、显式跳过），且下游的端口分配/`extra_hosts`/迁移跳过全部只读这次分类的结果（`p.locals` 成员关系），不重复查 `Mode`——改这一行，下游全部自动正确，包括"没写 `localPort` 就自动分配空闲端口"这条 spec §5 要求的行为（`assignHostPorts` 第 4 步本来就是为这天写的，注释里写着"延后项 P3"）；④ `internal/cli/up.go` 的 `collectTargets`——这是全仓库**唯一**一处会把组件的版本化服务名真的传给 `docker compose up`/`kubectl apply` 的地方，跟 compose 渲染器的判断是两份独立代码、历史上已经因为漏改这里导致过真机 `no such service` 崩溃（up.go 现有注释里记着这次教训），改这里是本计划里唯一"不改就会真机崩溃"的必做项。`internal/k8s` 完全不用碰：`mode: debug` 在 k8s 下已经被 ①②的校验层直接拒绝，`mode: local` 复用同一条拒绝规则（docker-only，物理原因跟 debug 一样——集群里的 Pod 够不到开发者自己机器上的进程），k8s 生成器从来没有为"裸进程"写过任何分支，也不需要开始写。

**Tech Stack:** 纯 Go 标准库改动（新增 `slices.Contains` 调用）+ 复用 Plan 3 已交付的 `internal/runcmd.Languages()` 做 `local.language` 的合法性校验（`internal/manifest` 新增对 `internal/runcmd` 的单向依赖，`runcmd` 本身零依赖，不会成环）。不新增任何第三方依赖。

**Spec:** `docs/superpowers/specs/2026-09-21-mode-field-and-local-execution-design.md` 的 §1（字段模型）、§1.1（Mode 是唯一真相）、§2（字段归属）、§5（端口覆盖机制——`internal/compose/local.go` 的 `assignHostPorts` 早就是按"自动分配 + 允许手动覆盖"两种情形通用实现的，本计划把 `mode: local` 接进同一套逻辑，`localPort` 覆盖与自动分配这两种情形**都**落地，不是只做一半）、§6.1（`PORT` 进保留变量精确匹配名单）。Plan 2 的"留给 Plan 4 的衔接点"、Plan 3 的"执行结果与遗留"是背景，不是本计划要实现的范围。

## 这份计划在整体拆分里的位置

四份大计划（Plan 1–4）之外，Plan 4 本身因为体量过大、覆盖多个相对独立的子系统，被进一步拆成几份子计划，按依赖顺序落地：

| 子计划 | 内容 | 状态 |
| --- | --- | --- |
| **本计划（Plan 4a）** | `component.yaml` 的 `local:` 块、`brickkit.yaml` 的 `mode: local` 五态校验、compose/up 生成层安全跳过、`PORT` 保留变量 | 待执行 |
| Plan 4b（待写） | `up` 前台监管模式：真正把 `runcmd`（Plan 3）+ `procsup`/`sessionlock`（Plan 2）接起来，含 `--crash-lines N`（已跟用户确认：只要这一个 flag，不加环境变量）、docker↔local 切换 | 待写，依赖本计划 |
| Plan 4c（待写） | 调试挂载怎么接（Go 的 `dlv attach`、Python 的 `debugpy` 包装、Java/Node 的已知限制——Plan 3 写作期间的真实实验已经推翻了 spec 原本"环境变量注入就够"的假设，需要单独设计） | 待写，依赖 Plan 4b |
| Plan 4d（待写） | `graph`/`status`/`down` 的展示层（labels/lock 文件提示）、`check-guides` 的 `local` 层、文档（AGENTS.md、`docs/{en,zh}`、CHANGELOG） | 待写，依赖 Plan 4b |

本计划交付后，`mode: local` 是一个**语义完整但还没有人真正启动进程**的合法值——跟 `mode: debug` 在生成层完全对等，唯一的区别是 debug 的"进程"由用户自己在 IDE 里启动，local 的"进程"目前还没有任何东西启动它（Plan 4b 的事）。这不是半成品：没有任何命令会因为写了 `mode: local` 而报错或生成错误的产物，只是这个组件暂时不会真的运行——跟 Plan 2/3 交付"没被接线的库"是同一种"完整但尚未激活"的状态，区别只是这次激活的是一个**字段值**而不是一个包。

## Global Constraints

- Go 版本下限 **1.22**；不新增第三方依赖。
- 仓库惯例：测试与被测代码同包；测试名英文、注释中文；提交信息中文；提交前跑**完整**的 `make lint`；`go test -race` 必须干净。
- 判断退出码：`make lint > /tmp/lint.log 2>&1; echo "exit=$?"` 再 `tail /tmp/lint.log`（zsh 没有 `${PIPESTATUS[0]}`）。
- 提交命令直接从 `git` 开始，别加 `cd` 前缀。
- **每一处 `Mode == ModeDebug` 的精确匹配都要考虑要不要并列加 `Mode == ModeLocal`**——这是本计划的核心工作模式，任务列表按"文件/子系统"分组，但检查方式统一：改之前先 `grep -rn "ModeDebug" internal/ market-server/` 确认没有漏改的第七处（本计划写作时已经调研过全部六处功能性判断点 + 三处纯展示判断点，见下面"设计决定"第 1 条，但代码可能在执行期间已经变化，改之前重新 grep 一遍成本很低）。
- **`internal/config` 与 `internal/manifest` 里任何 `jsonschema` tag 的取值列表，必须跟对应 `Validate`/校验函数里的 switch/case 取值保持同一份**——`schemas_test.go` 会核对，改一处要改另一处，这是仓库既有纪律（`config.go`/`types.go` 顶部注释里反复写明）。
- **文档范围**：本计划**不改** `AGENTS.md`、任何叙述"`mode: local` 怎么用"的指南/概念文档——`mode: local` 现在还不能真正运行任何东西，这类文档里不能先出现一个"看起来能用但试了没反应"的模式，留给 Plan 4d。**例外（Task 1 执行期间发现）**：`tests/docfields`（`make lint` 的一部分）强制要求 `docs/{en,zh}/06-architecture/07-component-yaml-reference.md` 的字段骨架表覆盖 `Manifest` 结构体的每一个字段——这是纯机械性的"这个字段存在、类型是什么"的事实陈述，跟 `migration.command` 这类字段一样，不涉及"怎么用 mode: local"，所以 `local.language`/`local.runCommand`/`local.debugCommand` 三行必须补进这张表（两种语言都要），否则 `make lint` 过不了。这条例外只覆盖这一张字段骨架表，不代表整个文档范围排除被推翻。
- `docs/{en,zh}/07-patterns/05-deployment-selection-guide.md` 依旧不改（另一个项目的实操反馈还没回来）。

---

## 设计决定（本计划执行前，基于对现有代码的调研做出的取舍）

1. **`mode: local` 复用 `mode: debug` 在生成层的全部既有基础设施，不新建"local 专属"的分类桶。** 调研已确认：`internal/compose/compose.go` 的 `newPlan`（第 230-266 行）是 compose 包内**唯一**一次"这个组件要不要生成容器"的判断，判断结果落进 `p.locals`（不生成）/`p.served`（servedBy）/`p.components`（生成）三个桶之一，下游的 `internal/compose/local.go`（`extraHostsOf`、`assignHostPorts`、`rewriteEndpointsForLocalDependencies`）全部只读 `p.locals`/`p.localPort` 的成员关系，**没有一处**重复检查 `entry.Mode == ModeDebug`。这意味着只要把 `mode: local` 的组件也分流进 `p.locals`，端口分配（含"没写 `localPort` 就自动分配空闲端口，默认用组件自己声明的主端口"——`assignHostPorts` 第 4 步，注释明确写着"005 §4.6，延后项 P3"，就是为今天这个任务预留的）、`extra_hosts` → `host-gateway` 寻址、迁移容器跳过（因为根本没进 `p.components`，不是靠额外判断挡住的），全部自动正确，不需要新写任何下游逻辑。`internal/inject`（算 `*_ENDPOINT` 初始值）本来就不区分 Mode，对所有依赖一视同仁地算出 `http://<服务名>:<容器端口>`，真正的地址修正（服务名解析、端口改写）发生在 compose 这一层，`inject` 不用动。
2. **`internal/k8s` 不需要新增任何代码。** `mode: debug` 在 `deploy.target: k8s` 下今天已经被 `internal/config/validate.go` 的 `validateComponentMode` 在解析阶段直接拒绝，k8s 生成器（`internal/k8s/k8s.go` 的 `newPlan`）从未为"裸进程"写过跳过分支——走到生成阶段的配置里保证没有 `mode: debug`，同理也保证没有 `mode: local`（只要 `validateComponentMode` 把两者一起挡住）。`mode: local` 需要跟 `mode: debug` 一样 docker-only 的物理原因完全相同：集群里的 Pod 够不到开发者/brickkit 所在机器上的裸进程。这不是"偷懒少做一件事"，是让 `internal/k8s` 保持"从没写过本不需要写的代码"这个已经成立的事实。
3. **`internal/cli/up.go` 的 `collectTargets` 是本计划里唯一"漏改就会真机崩溃"的点，必须最先确认、最后再核对一遍。** 这个函数独立于 compose 渲染器、手工复制了同一条件（它自己的注释写明了历史教训：漏掉 `servedBy` 那一半时，组件的服务名混进 `docker compose up` 的目标列表，而生成的 compose 文件里根本没有这个 service，真机执行报 `no such service`，整个命令失败）。`mode: local` 面临一模一样的风险，必须在这里也加判断，且**这是全仓库唯一一处**把具体组件的服务名传给底层引擎执行的地方（`down`/`status` 对引擎的调用都是项目级/命名空间级操作，不带具体 service 名，不存在同类风险——已核实）。
4. **`ConfigModeInvalid` 等五条现有错误文案的文字本身要跟着改，不只是校验逻辑。** `ConfigModeInvalid` 的中英文文案把 "enabled/disable/debug" 直接拼死在句子里（不是从代码动态生成的列表），`ConfigLocalPortNeedsLocal`/`ConfigServedByWithLocal`/`ConfigServedByTargetLocal`/`ConfigReplicasWithLocal` 四条也都在文字里点名"mode: debug"。这五条如果只改校验代码、不改文案，用户会看到一句跟实际校验规则对不上的错误提示（比如写 `mode: local` 撞见冲突时，报错却说"不能跟 mode: debug 同时声明"，看着像是校验器认错了字段）。
5. **`schemagen/schemas_test.go` 里已经有一行专门测 `mode` 字段的用例，而且它现在把 `"local"` 当作"保留值，现在写就该被拒绝"在测**（注释原文："local 是留给以后的保留值：现在写它必须被拒绝，而不是悄悄当成别的意思"）。这条测试不是要新增，是要把 `"local"` 从 `invalid` 数组移到 `valid` 数组，并且把那句现在会自相矛盾的注释一并改掉——这是本计划里一个容易漏掉、但漏掉就会被现有测试直接拦下来的点（先跑一遍现有测试确认它确实会因为这行断言而失败，是 Task 2 的第一步）。
6. **`local.language` 的合法性校验直接复用 `internal/runcmd.Languages()`，不在 `internal/manifest` 里另开一份语言清单。** Plan 3 交付的 `internal/runcmd` 包导出了 `Languages() []string`（固定顺序：go/rust/dotnet/node/java/python/ruby），`internal/manifest` 对它的依赖是单向的（`runcmd` 零内部依赖），不会成环。两处清单如果分开维护，迟早会在新增第八种语言时漏改一处。
7. **`local:` 块三个字段全部可选，不设"至少写一个"的约束。** spec §4 说"语言字段基本免填"、"两个字段（`runCommand`/`debugCommand`）都只作为自动探测失败/歧义时的手动覆盖出口"——一个组件完全不写 `local:` 块（或写一个空的 `local: {}`）是完全合法的常态，意味着"全靠自动探测"，不是需要被拦下来的不完整声明。

## File Structure

- Modify: `internal/manifest/types.go`——新增 `Local` 结构体 + `Manifest.Local` 字段
- Modify: `internal/manifest/validate.go`——新增 `validateLocal`，`Validate()` 里挂一行调用
- Modify: `internal/manifest/validate_test.go`（或同目录下现有的校验测试文件——执行时先 `grep -n "func TestValidate" internal/manifest/*_test.go` 确认放在哪个文件最合适）——`local:` 块的校验测试
- Modify: `internal/msgid/manifest.go`——新增 `ManifestLocalLanguageInvalid`
- Modify: `internal/msgid/config.go`——新增 `ConfigModeLocal`（如果需要新增常量；主要是复用已有五条常量，改文案不改名字）
- Modify: `internal/i18n/catalog_en.go`、`internal/i18n/catalog_zh.go`——新增一条 + 更新五条既有文案
- Modify: `internal/config/config.go`——新增 `ModeLocal` 常量、更新 `jsonschema` enum、更新 `IsPinned()`、更新相关注释
- Modify: `internal/config/validate.go`——`validateComponentMode`/`validateComponentPorts`/`validateServedBy`（两处）/`validateReplicas` 五处扩展
- Modify: `internal/config/config_test.go`——`TestModeFourStates`（改名或新增五态版本）、`TestComponentModeHelpers`、`TestValidateComponentMode` 三处补充用例
- Modify: `internal/schemagen/schemas_test.go`——`mode` 字段那条 `constraintCase` 的 `valid`/`invalid` 调整 + 注释改写
- Modify: `schemas/component.schema.json`、`schemas/brickkit.schema.json`——`make generate-schemas` 重新生成
- Modify: `internal/compose/compose.go`——`newPlan` 第 238 行扩展
- Modify: `internal/compose/compose_test.go`（或新建一个更聚焦的测试文件，执行时判断）——`mode: local` 不生成容器、依赖方拿到正确地址的测试
- Modify: `internal/cli/up.go`——`collectTargets` 扩展
- Modify: `internal/cli/up_dryrun_test.go`（或新建测试文件）——`mode: local` 不出现在 `docker compose up` 目标列表里的测试
- Modify: `internal/inject/reserved.go`——`reservedExact` 加 `"PORT"`
- Modify: `market-server/internal/validator/reserved.go`——同步加 `"PORT"`
- Modify: `internal/inject/reserved_test.go`（或对应测试文件）、`market-server/internal/validator/reserved_test.go`——新增 `PORT` 的冲突测试

---

## Task 1: `component.yaml` 的 `local:` 块

**背景：** 给 Manifest 新增一个跟 `migration:` 平级的可选顶层块，三个字段全部可选（`language`/`runCommand`/`debugCommand`）。校验规则很薄：`language` 若给出必须是 `internal/runcmd.Languages()` 里的一个，`runCommand`/`debugCommand` 若给出必须是非空字符串数组（数组本身可以不写，写了就不能有空字符串元素——照抄 `validateMigration` 的模式）。

**Files:**
- Modify: `internal/manifest/types.go`
- Modify: `internal/manifest/validate.go`
- Modify: `internal/msgid/manifest.go`
- Modify: `internal/i18n/catalog_en.go`、`internal/i18n/catalog_zh.go`
- Modify: `internal/manifest` 下合适的 `*_test.go`
- Modify: `schemas/component.schema.json`（`make generate-schemas` 生成）
- Modify: `docs/en/06-architecture/07-component-yaml-reference.md`、`docs/zh/06-architecture/07-component-yaml-reference.md`——`tests/docfields` 强制要求字段骨架表覆盖每个 Manifest 字段（见上面"文档范围"的例外说明）

**Interfaces:**
- Consumes：`internal/runcmd.Languages() []string`（Plan 3 已交付，零依赖）。
- Produces：`manifest.Local{Language, RunCommand, DebugCommand}`、`Manifest.Local *Local`——Plan 4b 从这个字段转译出 `runcmd.Hints{Language, RunCommand}` 时会用到。

- [ ] **Step 1: 在 `internal/manifest/types.go` 里新增 `Local` 结构体**

在 `Migration` 结构体（第 256-259 行）后面加：

```go
// Local 是组件在本机（mode: local/debug）的启动方式声明，跟 migration/healthCheck/
// deployment 平级（005 §2）。三个字段全部可选——不写时完全依赖自动探测；写了才是
// "探测失败/歧义时的手动覆盖出口"，不是必须完整声明的配置块。
type Local struct {
	// Language 是 internal/runcmd.Languages() 里的一个；多数情况能自动识别，
	// 只在探测出歧义（比如同一目录里 go.mod 和 package.json 都在）时才需要手写。
	Language string `yaml:"language,omitempty"`
	// RunCommand 是探测失败时的手动覆盖：Argv[0] 含路径分隔符时相对组件目录。
	RunCommand []string `yaml:"runCommand,omitempty"`
	// DebugCommand 是调试挂载探测失败时的手动覆盖（005 §4，调试挂载具体怎么接由
	// 后续计划设计——Plan 3 期间用真实进程做实验，推翻了 spec 原本"环境变量注入
	// 就够"的假设，见 docs/superpowers/plans/2026-09-22-run-command-detection.md
	// 的"设计决定"第 1 条）。
	DebugCommand []string `yaml:"debugCommand,omitempty"`
}
```

在 `Manifest` 结构体（第 98-112 行）里，`Migration *Migration` 那一行后面加一行：

```go
	Local        *Local        `yaml:"local,omitempty"`
```

（对齐现有字段的空格宽度，跟 `HealthCheck` 那一列对齐。）

- [ ] **Step 2: 新增 msgid 常量**

`internal/msgid/manifest.go`，在 `ManifestMigrationCommandMissing` 常量附近加：

```go
	ManifestLocalLanguageInvalid = "manifest.local_language_invalid"
```

- [ ] **Step 3: 两份 catalog 补译文**

`internal/i18n/catalog_en.go`：

```go
	msgid.ManifestLocalLanguageInvalid: "not a supported language (supported: %[2]s), got %[1]q",
```

`internal/i18n/catalog_zh.go`：

```go
	msgid.ManifestLocalLanguageInvalid: "不是受支持的语言（支持：%[2]s），实际是 %[1]q",
```

（`%[1]q` 是用户写的值，`%[2]s` 是 `strings.Join(runcmd.Languages(), "/")` 拼出来的清单——两个参数都要传，参照 `i18n.T` 现有的多参数调用写法，比如 `msgid.ConfigComponentDuplicate` 那一类。）

- [ ] **Step 4: 写 `validateLocal`**

`internal/manifest/validate.go` 顶部 import 块加 `"slices"` 和 `"github.com/brickkit/brickkit/internal/runcmd"`。在 `validateMigration`（第 383-396 行）后面加：

```go
func (m *Manifest) validateLocal(p *clierr.ProblemSet) {
	if m.Local == nil {
		return
	}
	if m.Local.Language != "" && !slices.Contains(runcmd.Languages(), m.Local.Language) {
		p.Add("local.language", i18n.T(msgid.ManifestLocalLanguageInvalid,
			m.Local.Language, strings.Join(runcmd.Languages(), "/")))
	}
	for i, arg := range m.Local.RunCommand {
		if strings.TrimSpace(arg) == "" {
			p.Missing(fmt.Sprintf("local.runCommand[%d]", i))
		}
	}
	for i, arg := range m.Local.DebugCommand {
		if strings.TrimSpace(arg) == "" {
			p.Missing(fmt.Sprintf("local.debugCommand[%d]", i))
		}
	}
}
```

在 `Validate()`（第 57-80 行）里，`m.validateMigration(p)` 后面加一行 `m.validateLocal(p)`。

- [ ] **Step 5: 写测试**

先 `grep -n "func TestValidate\|func TestManifest" internal/manifest/*_test.go` 确认现有校验测试放在哪个文件（多半是 `validate_test.go` 或按分块拆开的文件，比如可能已经有 `migration_test.go` 风格的独立文件），把新测试加进同一种组织方式里。至少覆盖：

```go
func TestValidateLocal(t *testing.T) {
	base := func(local string) string {
		return "apiVersion: brickkit/v1\nkind: Component\n" +
			"metadata:\n  id: a/b\n  name: n\n  version: 1.0.0\n  description: d\n" +
			"deployment:\n  type: container\n  image: img:1\n  port: 8080\n" +
			"healthCheck:\n  type: http\n  path: /healthz\n" +
			local
	}

	t.Run("不写 local 块是合法的", func(t *testing.T) {
		_, err := Parse([]byte(base("")), "component.yaml")
		require.NoError(t, err)
	})

	t.Run("空 local 块是合法的", func(t *testing.T) {
		_, err := Parse([]byte(base("local: {}\n")), "component.yaml")
		require.NoError(t, err)
	})

	t.Run("language 合法时通过", func(t *testing.T) {
		_, err := Parse([]byte(base("local:\n  language: python\n")), "component.yaml")
		require.NoError(t, err)
	})

	t.Run("language 不在支持列表里报错", func(t *testing.T) {
		_, err := Parse([]byte(base("local:\n  language: cobol\n")), "component.yaml")
		require.Error(t, err)
		assert.Contains(t, clierr.As(err).Format(), "local.language")
		assert.Contains(t, clierr.As(err).Format(), "cobol")
	})

	t.Run("runCommand 里的空字符串报错", func(t *testing.T) {
		_, err := Parse([]byte(base("local:\n  runCommand: [\"go\", \"\", \"run\"]\n")), "component.yaml")
		require.Error(t, err)
		assert.Contains(t, clierr.As(err).Format(), "local.runCommand[1]")
	})

	t.Run("debugCommand 里的空字符串报错", func(t *testing.T) {
		_, err := Parse([]byte(base("local:\n  debugCommand: [\"\"]\n")), "component.yaml")
		require.Error(t, err)
		assert.Contains(t, clierr.As(err).Format(), "local.debugCommand[0]")
	})

	t.Run("runCommand 与 debugCommand 都写也合法", func(t *testing.T) {
		_, err := Parse([]byte(base(
			"local:\n  language: go\n  runCommand: [\"go\", \"run\", \".\"]\n  debugCommand: [\"dlv\", \"debug\", \".\"]\n")),
			"component.yaml")
		require.NoError(t, err)
	})
}
```

（具体的 YAML 拼接方式要跟着 `internal/manifest` 现有测试的写法走——执行时先看一眼同目录别的测试怎么构造最小合法 Manifest，多半已经有一个 helper 函数，照抄它而不是每个测试自己拼字符串。）

- [ ] **Step 6: 跑测试**

Run: `go test ./internal/manifest/ -v -run TestValidateLocal`
Expected: 全部 `PASS`。

Run: `go test ./internal/manifest/`
Expected: `PASS`（确认没有破坏既有测试）。

- [ ] **Step 7: 重新生成并核对 JSON Schema**

Run: `make generate-schemas`
Expected: `schemas/component.schema.json` 多出一个 `local` 属性块（三个子字段全 `omitempty`，大概率 `"required"` 键不出现，形状可以参照同一个文件里 `migration` 块的样子）。

Run: `go test ./internal/schemagen/`
Expected: `PASS`（防漂移测试确认生成结果与刚生成的文件一致）。

- [ ] **Step 8: gofmt + 提交**

```bash
gofmt -l internal/manifest internal/msgid internal/i18n
git add internal/manifest/types.go internal/manifest/validate.go internal/msgid/manifest.go internal/i18n/catalog_en.go internal/i18n/catalog_zh.go schemas/component.schema.json internal/manifest/*_test.go
git commit -m "$(cat <<'EOF'
新增：component.yaml 的 local: 块——language/runCommand/debugCommand

跟 migration/healthCheck/deployment 平级的新顶层块，三个字段全部可选：不写时
完全依赖 Plan 3 交付的 internal/runcmd 自动探测，写了才是探测失败/歧义时的
手动覆盖出口。language 的合法性直接复用 runcmd.Languages()，不在 manifest
包里另开一份语言清单——两份清单迟早会在新增语言时漏改一处。

Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>
EOF
)"
```

---

## Task 2: `brickkit.yaml` 的 `mode: local` 五态校验

**背景：** `Component.Mode` 今天只认 `""/enabled/disable/debug` 四个值，围绕它的六处判断（`validateComponentMode`、`validateComponentPorts`、`validateServedBy` 两处、`validateReplicas`、`IsPinned`）全部是对 `ModeDebug` 的精确匹配，没有任何反向/黑名单写法——这意味着加 `ModeLocal` 就是在每一处并列加一个条件，没有别的隐藏耦合。五条现有错误文案的文字本身硬编码了"debug"，也要跟着改。`schemagen/schemas_test.go` 里已经有一行测试把 `"local"` 当"保留值，现在写就该被拒绝"在测——这次要把它从 `invalid` 移到 `valid`。

**Files:**
- Modify: `internal/config/config.go`
- Modify: `internal/config/validate.go`
- Modify: `internal/msgid/config.go`（如需要新增常量）
- Modify: `internal/i18n/catalog_en.go`、`internal/i18n/catalog_zh.go`
- Modify: `internal/config/config_test.go`
- Modify: `internal/schemagen/schemas_test.go`
- Modify: `schemas/brickkit.schema.json`

**Interfaces:**
- Consumes：无新依赖。
- Produces：`config.ModeLocal`、扩展后的 `Component.IsPinned()`——Task 3（compose.go）、Task 4（up.go）直接消费这两个符号。

- [ ] **Step 1: 先确认现有测试真的会因为"local 现在被当保留值拒绝"而失败**

Run: `go test ./internal/schemagen/ -v -run TestConstraints 2>&1 | grep -A5 "components\[0\].mode"`

（测试函数名执行时用 `grep -n "func Test" internal/schemagen/schemas_test.go` 确认——这条断言不一定单独成一个 `Test` 函数，可能是表驱动的一部分，找到能单独跑这一条 case 的方式。）

Expected：这一步只是确认起点，不要求任何特定输出，只是让执行者亲眼看到这条用例目前把 `"local"` 当无效值在测，心里有数后面 Step 6 要去翻转它。

- [ ] **Step 2: `config.go` 加 `ModeLocal` 常量与相关注释**

第 242-246 行：

```go
const (
	ModeEnabled = "enabled"
	ModeDisable = "disable"
	ModeDebug   = "debug"
	ModeLocal   = "local"
)
```

第 255-259 行的注释与 tag 改成：

```go
	// Mode 取代了 Enabled/Local 两个字段（mode 字段迁移设计）：
	// ""（未写）= 跟随上层，走容器；"enabled" = 钉住，走容器；
	// "disable" = 钉住不跑；"debug" = 裸进程，用户自己启动；
	// "local" = 裸进程，brickkit 自己拉起（Plan 4b 起才真正启动，本计划只保证
	// 这个取值本身合法、生成阶段安全跳过）。
	// 校验器负责按 deploy.target 决定这五个取值里哪些合法（k8s 下只认前三个）。
	Mode      string `yaml:"mode,omitempty" jsonschema:"enum=enabled|disable|debug|local"`
```

第 323-325 行：

```go
// IsPinned 表示这个组件被钉死一定要跑：mode: enabled、mode: debug、mode: local
// 都算——不管有没有别的组件依赖它，这三种写法都是"我就是要它跑"的明确意图，
// 不能被 cascade 判定为"没人需要就不跑"。
func (c Component) IsPinned() bool {
	return c.Mode == ModeEnabled || c.Mode == ModeDebug || c.Mode == ModeLocal
}
```

- [ ] **Step 3: `validate.go` 的五处扩展**

`validateComponentMode`（第 227-238 行）：

```go
func (c *Config) validateComponentMode(p *clierr.ProblemSet, field string, item Component) {
	switch item.Mode {
	case "", ModeEnabled, ModeDisable, ModeDebug, ModeLocal:
		// 合法取值
	default:
		p.Add(field+".mode", i18n.T(msgid.ConfigModeInvalid, item.Mode))
		return
	}
	if (item.Mode == ModeDebug || item.Mode == ModeLocal) && c.Deploy.Target == TargetK8s {
		p.Add(field+".mode", i18n.T(msgid.ConfigModeK8sUnsupported, item.Mode))
	}
}
```

`validateComponentPorts`（第 247 行那一个 `case`）：

```go
		case item.Mode != ModeDebug && item.Mode != ModeLocal:
```

`validateServedBy`（第 307 行与第 335 行，两处都要改）：

```go
		if item.Mode == ModeDebug || item.Mode == ModeLocal {
```

```go
		if (item.Mode == ModeDebug || item.Mode == ModeLocal) && servedByTarget[item.Ref()] {
```

`validateReplicas`（第 600 行）：

```go
	if item.Mode == ModeDebug || item.Mode == ModeLocal {
```

- [ ] **Step 4: 更新五条错误文案的文字**

`internal/i18n/catalog_en.go`：

```go
	msgid.ConfigModeInvalid:                "must be one of enabled/disable/debug/local (or omitted), got %[1]q",
	msgid.ConfigLocalPortNeedsLocal:        "only takes effect with mode: debug or mode: local; declare one of them, or remove this field",
	msgid.ConfigServedByWithLocal:          "cannot be declared together with mode: debug or mode: local — both mean the workload does not live in a container the platform generates, while servedBy means the code is already baked into another shell's image; the intents contradict each other",
	msgid.ConfigServedByTargetLocal:        "%[1]s is pointed at by another component's servedBy, so it cannot also be mode: debug or mode: local (a shell has to be reachable inside the cluster/container network, which a bare process on the developer's machine cannot be)",
	msgid.ConfigReplicasWithLocal:          "cannot be declared together with mode: debug or mode: local: both mean this component runs as a single bare process, not a set of container replicas",
```

`internal/i18n/catalog_zh.go`：

```go
	msgid.ConfigModeInvalid:                "只能是 enabled/disable/debug/local 之一（或不写），实际是 %[1]q",
	msgid.ConfigLocalPortNeedsLocal:        "只在 mode: debug 或 mode: local 时生效，请一并声明其中之一或删除该字段",
	msgid.ConfigServedByWithLocal:          "不能跟 mode: debug 或 mode: local 同时声明——两者都是「工作负载不在平台生成的容器里」，而 servedBy 是代码已经打进另一个外壳镜像，两种意图互相矛盾",
	msgid.ConfigServedByTargetLocal:        "%[1]s 被别的组件 servedBy 指向，不能同时是 mode: debug 或 mode: local（外壳要能在集群/容器网络里被访问到，开发者本机上的裸进程做不到这件事）",
	msgid.ConfigReplicasWithLocal:          "不能与 mode: debug 或 mode: local 同时声明：两者都是这个组件跑成单个裸进程，不是一组容器副本",
```

（这五条都是**替换**已有的 map 条目值，不是新增 key——用 Edit 按现有文本精确替换，不要整段重写附近的 map。）

- [ ] **Step 5: 更新 `config_test.go` 的三处测试**

`TestModeFourStates`（第 206-245 行）改成五态，在 YAML 里加第五个组件、加对应断言：

```go
func TestModeFourStates(t *testing.T) {
	c, err := ParseConfig([]byte(`
project: my-project
deploy:
  target: docker
components:
  - id: a/pinned
    version: 1.0.0
    mode: enabled
  - id: b/default
    version: 1.0.0
  - id: c/disabled
    version: 1.0.0
    mode: disable
  - id: d/debug
    version: 1.0.0
    mode: debug
  - id: e/local
    version: 1.0.0
    mode: local
resources: []
`), "brickkit.yaml")
	require.NoError(t, err)

	pinned, dflt, disabled, debug, local :=
		c.Components[0], c.Components[1], c.Components[2], c.Components[3], c.Components[4]

	assert.Equal(t, ModeEnabled, pinned.Mode)
	assert.True(t, pinned.IsPinned(), "mode: enabled → 一定跑")
	assert.False(t, pinned.IsDisabled())

	assert.Equal(t, "", dflt.Mode, "不写 mode → 空字符串，不是 disable")
	assert.False(t, dflt.IsDisabled(), "没写不等于关掉")
	assert.False(t, dflt.IsPinned(), "没写不等于钉住")

	assert.Equal(t, ModeDisable, disabled.Mode)
	assert.True(t, disabled.IsDisabled(), "mode: disable → 一定不跑")

	assert.Equal(t, ModeDebug, debug.Mode)
	assert.True(t, debug.IsPinned(), "mode: debug → 一定跑（要盯着它调试）")
	assert.False(t, debug.IsDisabled())

	assert.Equal(t, ModeLocal, local.Mode)
	assert.True(t, local.IsPinned(), "mode: local → 一定跑（brickkit 自己拉起）")
	assert.False(t, local.IsDisabled())
}
```

（函数名保留 `TestModeFourStates` 还是改成 `TestModeFiveStates`：改名，"四态"这个名字在测五态时会误导读者——用 Edit 把函数名和函数体一起换掉，注意搜索仓库里有没有别的地方按名字引用这个测试函数。）

`TestComponentModeHelpers`（第 995-1014 行）表里加一行：

```go
		{"local 钉住", ModeLocal, false, true},
```

`TestValidateComponentMode`（第 952-993 行）的 `cases` 里加两行：

```go
		{
			name:    "mode: local 配 docker 合法",
			yaml:    "project: p\ndeploy:\n  target: docker\ncomponents:\n  - id: a/b\n    version: 1.0.0\n    mode: local\n",
			wantErr: nil,
		},
		{
			name:    "mode: local 配 k8s 报错",
			yaml:    "project: p\ndeploy:\n  target: k8s\ncomponents:\n  - id: a/b\n    version: 1.0.0\n    mode: local\n",
			wantErr: []string{"mode", "k8s"},
		},
```

同时把第一条用例（"mode 非法取值报错"）的 `wantErr` 从 `[]string{"mode", "enabled", "disable", "debug"}` 改成 `[]string{"mode", "enabled", "disable", "debug", "local"}`（文案里现在也列出了 local）。

- [ ] **Step 6: 翻转 `schemagen/schemas_test.go` 里 `mode` 那条用例**

把 `"local"` 从 `invalid` 数组移到 `valid` 数组，并且改写那句现在会自相矛盾的注释：

```go
	// mode 不必填，schema 的 enum 因此带着 null（显式写 null 等于没写）。
	// "" 在 yaml 里与没写无法区分，校验器放行它，而 schema 不该把 "" 当成一个可选的取值
	// 推荐给人——所以点名成 validatorOnly。
	// debug/local 只在 docker 下合法，基准里的 k8s 要换掉，component 上也得有一行 mode 才有落脚点。
	name: "components[0].mode", doc: "project", schemaPath: "components[]/mode",
	baseline: strings.Replace(strings.Replace(baselineProject, "target: k8s", "target: docker", 1),
		"    version: 1.0.0\n", "    version: 1.0.0\n    mode: enabled\n", 1),
	dataPath: []any{"components", 0, "mode"}, errField: "components[0].mode",
	valid:         []any{config.ModeEnabled, config.ModeDisable, config.ModeDebug, config.ModeLocal, nil},
	validatorOnly: []any{""},
	invalid:       []any{"Enabled", "disabled", "debugging", "docker"},
```

- [ ] **Step 7: 跑测试**

Run: `go test ./internal/config/ -v -run 'TestModeFourStates|TestModeFiveStates|TestComponentModeHelpers|TestValidateComponentMode'`
Expected: 全部 `PASS`。

Run: `go test ./internal/config/`
Expected: `PASS`。

- [ ] **Step 8: 重新生成 schema 并跑 schemagen 测试**

Run: `make generate-schemas && go test ./internal/schemagen/`
Expected: `schemas/brickkit.schema.json` 的 `mode` enum 多出 `local`；测试 `PASS`。

- [ ] **Step 9: gofmt + 提交**

```bash
gofmt -l internal/config internal/i18n internal/schemagen
git add internal/config/config.go internal/config/validate.go internal/config/config_test.go internal/i18n/catalog_en.go internal/i18n/catalog_zh.go internal/schemagen/schemas_test.go schemas/brickkit.schema.json
git commit -m "$(cat <<'EOF'
新增：brickkit.yaml 的 mode: local——五态校验、IsPinned、与 servedBy/replicas 互斥

围绕 Mode 的六处判断（validateComponentMode、validateComponentPorts、
validateServedBy 两处、validateReplicas、IsPinned）全部只认 ModeDebug 的精确
匹配，加 ModeLocal 是并列加条件，没有隐藏耦合。docker-only 的限制复用
mode: debug 已有的那条规则——物理原因相同，k8s 生成器不需要新写任何代码。
五条现有错误文案原本把"debug"硬编码进文字，一并更新；schemagen 测试里原本
把"local"当保留值在测"现在写就该被拒绝"，这次翻成合法值。

Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>
EOF
)"
```

---

## Task 3: `internal/compose` 安全跳过——不生成容器，依赖方拿到正确地址

**背景：** compose 包内唯一的分类点（`newPlan` 第 238 行）目前只认 `entry.Mode == config.ModeDebug` 就跳过生成容器、分流进 `p.locals`。加一个 `|| entry.Mode == config.ModeLocal` 并列条件，下游的端口分配（含"没写 `localPort` 就自动用组件声明的主端口"这条本来就通用的逻辑）、`extra_hosts`、迁移跳过全部自动正确——这是 Task 3 的核心断言，要用真实测试证明，不能只凭代码读起来像会这样就相信。

**Files:**
- Modify: `internal/compose/compose.go`
- Modify: `internal/compose/compose_test.go`

**Interfaces:**
- Consumes：Task 2 的 `config.ModeLocal`。
- Produces：无新符号——这一步只是给既有分类逻辑加一个分支。

- [ ] **Step 1: 扩展 `newPlan` 的分类判断**

第 238 行：

```go
		if entry.Mode == config.ModeDebug || entry.Mode == config.ModeLocal {
```

第 239-240 行的注释一并更新：

```go
			// 12.7 / 13.1 / Plan 4a：mode: debug 与 mode: local 的组件都不生成容器——
			// 前者在宿主机 IDE 里跑，后者由 brickkit 自己拉起裸进程（Plan 4b 起才
			// 真正启动），但两者都仍然是"启动中"的组件，依赖方要能找到它。
```

- [ ] **Step 2: 写测试证明"不生成容器"**

在 `internal/compose/compose_test.go` 里找到测 `mode: debug` 不生成 service 的现成测试（`grep -n "ModeDebug" internal/compose/compose_test.go` 找到第 733 行附近那条，读一遍它的断言写法），照同样的结构为 `mode: local` 写一条：

```go
func TestModeLocalDoesNotGenerateAService(t *testing.T) {
	b := newBuilder(t)
	b.component(simple("people/basic", "1.0.0", 8080), config.Component{Mode: config.ModeLocal})

	assert.NotContains(t, servicesOf(t, b.parsed()), "people-basic-1-0-0")
}
```

- [ ] **Step 3: 写测试证明依赖方拿到正确的地址与 `extra_hosts`（不写 `localPort`，验证自动分配）**

照抄 `TestMigrationServiceInheritsRewrittenEnvironment`（第 387-403 行）与 `TestMigrationServiceGetsTheSameExtraHosts`（第 407-422 行）的结构，但这次故意**不写** `LocalPort`，断言拿到的是组件自己声明的主端口（8080）而不是 0 或别的默认值：

```go
func TestModeLocalDependentsGetAnAutoAssignedPortAndExtraHosts(t *testing.T) {
	b := newBuilder(t)
	b.component(dependsOn(simple("erp/backend", "1.0.0", 8080), "people/basic", "1.0.0"),
		config.Component{})
	b.component(simple("people/basic", "1.0.0", 8080), config.Component{Mode: config.ModeLocal})

	doc := b.parsed()
	env := envOf(t, serviceOf(t, doc, "erp-backend-1-0-0"))

	assert.Equal(t, "http://people-basic-1-0-0:8080", env["PEOPLE_BASIC_ENDPOINT"],
		"没写 localPort，自动分配的端口应该就是组件自己声明的主端口")
	assert.Contains(t, extraHostsOf(t, serviceOf(t, doc, "erp-backend-1-0-0")),
		"people-basic-1-0-0:host-gateway")
}
```

- [ ] **Step 4: 写测试证明显式 `localPort` 覆盖对 `mode: local` 也生效**

Task 2 已经把 `validateComponentPorts` 的 `case item.Mode != ModeDebug:` 扩展成也接受 `ModeLocal`，`assignHostPorts` 第 2 步（"使用者钦定的 localPort"）本来就不区分 debug/local，只看 `l.Entry.LocalPort != 0`。这条测试证明这条路径对 `mode: local` 也是通的：

```go
func TestModeLocalRespectsAnExplicitLocalPortOverride(t *testing.T) {
	b := newBuilder(t)
	b.component(dependsOn(simple("erp/backend", "1.0.0", 8080), "people/basic", "1.0.0"),
		config.Component{})
	b.component(simple("people/basic", "1.0.0", 8080),
		config.Component{Mode: config.ModeLocal, LocalPort: 9000})

	env := envOf(t, serviceOf(t, b.parsed(), "erp-backend-1-0-0"))

	assert.Equal(t, "http://people-basic-1-0-0:9000", env["PEOPLE_BASIC_ENDPOINT"],
		"写了 localPort 就该用它，不该被自动分配覆盖")
}
```

- [ ] **Step 5: 写测试证明迁移容器不会为 `mode: local` 组件生成**

```go
func TestModeLocalDoesNotGenerateAMigrationService(t *testing.T) {
	b := newBuilder(t)
	b.component(withMigration(simple("people/basic", "1.0.0", 8080)), config.Component{Mode: config.ModeLocal})

	assert.NotContains(t, servicesOf(t, b.parsed()), "people-basic-1-0-0-migration")
}
```

（`withMigration`/`simple`/`dependsOn`/`newBuilder`/`servicesOf`/`serviceOf`/`envOf`/`extraHostsOf` 都是 `compose_test.go` 里已有的测试构造帮助函数——执行时确认函数签名，上面几段代码按现有用法直接抄。）

- [ ] **Step 6: 跑测试**

Run: `go test ./internal/compose/ -v -run TestModeLocal`
Expected: 全部 `PASS`。

Run: `go test ./internal/compose/`
Expected: `PASS`。

- [ ] **Step 7: gofmt + 提交**

```bash
gofmt -l internal/compose
git add internal/compose/compose.go internal/compose/compose_test.go
git commit -m "$(cat <<'EOF'
新增：compose 生成层对 mode: local 的处理——不生成容器，复用既有本地组件基础设施

compose 包内唯一的分类点（newPlan 第 238 行）原本只认 mode: debug，加一个并列
条件分流进同一个 p.locals 桶。下游的端口自动分配（没写 localPort 时用组件自己
声明的主端口）、extra_hosts → host-gateway 寻址、迁移容器跳过全部只读这次分类
的结果，不需要新写任何下游逻辑——三条新测试直接验证这个断言，不只是信任代码
读起来像会这样。

Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>
EOF
)"
```

---

## Task 4: `internal/cli/up.go` 安全跳过——避免真机 `no such service`

**背景：** `collectTargets` 是全仓库唯一一处把组件的版本化服务名真的传给 `docker compose up`/`kubectl apply` 的地方，跟 compose 渲染器的判断是两份独立代码，历史上漏改过 `servedBy` 那一半、真机报过 `no such service`（函数自己的注释记着这次教训）。这是本计划里唯一"漏改就会真机崩溃"的点。

**Files:**
- Modify: `internal/cli/up.go`
- Modify: `internal/cli` 下合适的测试文件（`collectTargets`/`upPlan` 今天没有独立的单元测试——这个任务要顺手补上第一条，不是可选项）

**Interfaces:**
- Consumes：Task 2 的 `config.ModeLocal`。
- Produces：无新符号。

- [ ] **Step 1: 扩展 `collectTargets`**

第 472 行：

```go
		if c.Mode == config.ModeDebug || c.Mode == config.ModeLocal || c.ServedBy != "" {
```

第 461-468 行的函数注释一并更新（把"两者都没有自己的容器"改成"三者都没有自己的容器"，把判断条件的引用也加上 `|| entry.Mode == config.ModeLocal`）。

- [ ] **Step 2: 补一条 `collectTargets` 的单元测试**

先 `grep -n "func TestCollectTargets\|func (p \*upPlan) collectTargets" internal/cli/up.go internal/cli/*_test.go`，确认 `upPlan`/`collectTargets` 需要哪些字段才能在测试里独立构造（不依赖真实 Docker/K8s——这个函数只读 `p.cfg.Components` 和 `order.Steps`，理论上可以脱离引擎纯构造）。写一条测试，断言一个 `mode: local` 组件不出现在最终传给引擎的 service 列表里：

```go
func TestCollectTargetsExcludesModeLocal(t *testing.T) {
	// 构造一个含 mode: local 组件的最小 upPlan，调 collectTargets，
	// 断言结果里的 service 列表（或者 noWorkload 集合）排除了这个组件——
	// 具体断言哪个字段，取决于 collectTargets 把结果存在 upPlan 的哪个成员上，
	// 执行时读一遍 up.go 里 collectTargets 前后的代码确认。
}
```

（这条测试的具体构造方式要看 `upPlan` 的完整定义——它大概率需要一个 `resolver.Plan`/`resolver.Graph` 才能跑，执行时先读 `internal/cli/up.go` 里 `upPlan` 结构体定义和 `collectTargets` 的完整调用上下文，找到一种不依赖真实文件系统/Docker 的最小构造方式；如果确实构造不出脱离引擎的最小实例，退而求其次在 `up_dryrun_test.go` 现有的 dry-run 集成测试框架里加一条用例，断言 `--dry-run` 的输出或者生成的 compose 文件里，一个 `mode: local` 组件既不出现在 compose 文件的 services 里，`up --dry-run` 本身也不报错——用 `internal/cli/up_dryrun_test.go` 里已有的测试作为写法参照。）

- [ ] **Step 3: 跑测试**

Run: `go test ./internal/cli/ -v -run 'CollectTargets|ModeLocal'`
Expected: 全部 `PASS`。

Run: `go test ./internal/cli/`
Expected: `PASS`。

- [ ] **Step 4: gofmt + 提交**

```bash
gofmt -l internal/cli
git add internal/cli/up.go internal/cli/*_test.go
git commit -m "$(cat <<'EOF'
新增：up 命令跳过 mode: local 组件，避免真机 no such service

collectTargets 是全仓库唯一一处把组件服务名传给底层引擎执行的地方，跟 compose
渲染器的判断是两份独立代码——历史上漏改过 servedBy 那一半、真机报过 no such
service（函数自己的注释记着这次教训）。mode: local 面临同样的风险，这次一并
加判断，并补上这个函数第一条独立于真实引擎的单元测试。

Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>
EOF
)"
```

---

## Task 5: `PORT` 保留变量

**背景：** spec §6.1 要求 `PORT`（裸名字，不加组件前缀）进保留变量精确匹配名单——很多语言/框架社区本来就读 `PORT` 决定监听端口，通用性决定它必须进保留名单，避免撞组件自己 `configSchema` 里的同名 key。这条规则由 CLI 侧（`internal/inject/reserved.go`）与市场侧（`market-server/internal/validator/reserved.go`）两份代码镜像维护。**这一步只注册保留名，不涉及"谁在什么时候真的往环境变量里写 `PORT=<端口号>`"——那是 Plan 4b 启动本地进程时才会做的事，本计划的效果只是"如果组件自己的 `configSchema` 里出现一个叫 `port` 的 key，`brickkit up`/市场发布校验会警告/拒绝"，跟其余五个已有保留前缀（`DATABASE_`/`REDIS_`/`MQ_`/`STORAGE_`/`SEARCH_`/`SMTP_`）与四个精确匹配名字（`COMPONENT_ID` 等）享受同一套现成机制。**

**Files:**
- Modify: `internal/inject/reserved.go`
- Modify: `market-server/internal/validator/reserved.go`
- Modify: 两边对应的测试文件

**Interfaces:**
- Consumes：无。
- Produces：无新符号——这一步是给已有的 `reservedExact` 切片加一个元素。

- [ ] **Step 1: CLI 侧加 `PORT`**

`internal/inject/reserved.go`：

```go
var (
	reservedExact  = []string{"COMPONENT_ID", "COMPONENT_VERSION", "BRICKKIT_SERVED_MEMBERS", "BRICKKIT_SERVED_MEMBERS_CONFIG", "PORT"}
	reservedSuffix = []string{"_ENDPOINT"}
	reservedPrefix = []string{"DATABASE_", "REDIS_", "MQ_", "STORAGE_", "SEARCH_", "SMTP_"}
)
```

- [ ] **Step 2: 市场侧同步加 `PORT`**

`market-server/internal/validator/reserved.go`，同样在 `reservedExact` 里追加 `"PORT"`。

- [ ] **Step 3: 写测试**

先 `grep -n "func Test" internal/inject/reserved_test.go` 找到已有测试对 `COMPONENT_ID` 之类精确匹配保留名的测试写法（大概率是一条表驱动测试，输入一个跟保留名同名的 configSchema key，断言警告/跳过注入），照同样的写法为 `PORT` 加一行用例或一条独立测试。`market-server/internal/validator/reserved_test.go` 同理。

- [ ] **Step 4: 跑测试**

Run: `go test ./internal/inject/`
Expected: `PASS`。

Run: `cd market-server && go test ./internal/validator/`
Expected: `PASS`（沙箱如果不允许带 `cd` 的复合命令，改成两条分开的命令，或者用 `go test github.com/brickkit/market-server/internal/validator`——执行时按仓库既有的市场侧测试跑法来，不确定就先 `grep` Makefile 里 `test-market` 目标怎么跑的）。

- [ ] **Step 5: gofmt + 提交**

```bash
gofmt -l internal/inject market-server/internal/validator
git add internal/inject/reserved.go internal/inject/reserved_test.go market-server/internal/validator/reserved.go market-server/internal/validator/reserved_test.go
git commit -m "$(cat <<'EOF'
新增：PORT 进保留变量精确匹配名单（CLI 与市场侧镜像维护）

很多语言/框架社区本来就读 PORT 决定监听端口，通用名字必须进保留名单，避免撞
组件自己 configSchema 里的同名 key——这条规则跟 COMPONENT_ID 等四个既有精确
匹配名字用的是同一套机制。这一步只注册保留名，真正往环境变量里写 PORT=<端口
号> 是 Plan 4b 启动本地进程时才做的事。

Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>
EOF
)"
```

---

## Task 6: 全量验证与收尾

**背景：** 前五个任务各自验证了自己那一块，这个任务跑一遍全仓库测试 + 完整 `make lint`，确认没有相互破坏，并回写执行结果。

**Files:**
- Modify: `docs/superpowers/plans/2026-09-22-local-mode-field-and-dispatch.md`（本文件）——收尾时回写执行结果

- [ ] **Step 1: 全仓库测试**

Run: `go test ./...`
Expected: 全部 `ok`，包括 `internal/manifest`、`internal/config`、`internal/compose`、`internal/cli`、`internal/inject`、`internal/schemagen`、`market-server/...`。

- [ ] **Step 2: `-race`**

Run: `go test -race ./internal/manifest/ ./internal/config/ ./internal/compose/ ./internal/cli/ ./internal/inject/`
Expected: 干净。

- [ ] **Step 3: 交叉编译确认**

Run: `make check-cross-build`
Expected: 退出码 0（本计划没有引入任何平台相关代码，这一步是例行确认）。

- [ ] **Step 4: 完整 `make lint`**

Run: `make lint > /tmp/lint.log 2>&1; echo "exit=$?"`
Expected: `exit=0`。

- [ ] **Step 5: 交叉核对"每一处 `ModeDebug` 都考虑过要不要加 `ModeLocal`"**

Run: `grep -rn "ModeDebug" internal/ market-server/ --include=*.go | grep -v _test.go`
Expected: 对照"设计决定"第 1 条与执行期间发现的清单，逐行确认每一处要么已经加了 `ModeLocal`、要么有明确理由不需要加（比如 `internal/cli/graph.go`/`status.go`/`lifecycle.go` 的展示层——这些留给 Plan 4d，本计划不动，但要在这一步明确记下"看过了，故意不动，理由是什么"，不是漏看）。

- [ ] **Step 6: 回写执行结果，提交**

在本文件末尾追加"执行结果与遗留"一节，记录：六个任务的提交哈希、`go test ./...`/`-race`/`make lint` 的真实结果、Step 5 交叉核对的清单（哪些展示层的点确认过但故意没动，留给 Plan 4d）、任何偏离计划之处。

```bash
git add docs/superpowers/plans/2026-09-22-local-mode-field-and-dispatch.md
git commit -m "$(cat <<'EOF'
文档：Plan 4a（mode: local 字段落地）的执行结果回写进计划

六个任务全部完成：local: 块、五态校验、compose 安全跳过、up.go 避免 no such
service、PORT 保留变量、全量验证。mode: local 现在是语义完整但还没有人真正
启动进程的合法值，跟 mode: debug 在生成层完全对等。

Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>
EOF
)"
```

---

## Self-Review Checklist（执行完 6 个任务后逐条核对）

- [ ] `go test ./...` 全绿（含 `market-server/...`）
- [ ] `go test -race` 对本计划触碰的五个包干净
- [ ] `make check-cross-build` 退出码 0
- [ ] `make lint` 完整跑一遍，退出码 0
- [ ] `local:` 块三个字段全部可选，空 `local: {}` 与完全不写都合法
- [ ] `mode: local` 通过 docker 下的校验，k8s 下被拒绝（复用 `mode: debug` 的同一条规则，`internal/k8s` 零改动）
- [ ] `IsPinned()`、`validateComponentPorts`、`validateServedBy`（两处）、`validateReplicas` 全部把 `ModeLocal` 并列进 `ModeDebug` 的判断
- [ ] 五条现有错误文案（`ConfigModeInvalid`/`ConfigLocalPortNeedsLocal`/`ConfigServedByWithLocal`/`ConfigServedByTargetLocal`/`ConfigReplicasWithLocal`）文字本身提到了 `mode: local`，不再只提 `mode: debug`
- [ ] `schemagen/schemas_test.go` 里 `mode` 字段那条用例：`"local"` 在 `valid` 数组里，不在 `invalid` 数组里，注释不再自相矛盾
- [ ] `internal/compose` 里一个 `mode: local` 组件不生成容器、不生成迁移容器，依赖方拿到正确的 `extra_hosts` 与自动分配的端口（有测试证明，不是凭读代码相信）
- [ ] `internal/cli/up.go` 的 `collectTargets` 排除了 `mode: local`，有独立于真实引擎的测试覆盖
- [ ] `PORT` 在 CLI 侧与市场侧的 `reservedExact` 里都出现
- [ ] `AGENTS.md`、`docs/en/`、`docs/zh/` 没有任何改动（Plan 4d 的事）
- [ ] `internal/k8s` 目录下没有任何改动

## Execution Handoff

Plan complete and saved to `docs/superpowers/plans/2026-09-22-local-mode-field-and-dispatch.md`. Two execution options:

1. **Subagent-Driven (recommended)** - I dispatch a fresh subagent per task, review between tasks, fast iteration
2. **Inline Execution** - Execute tasks in this session using executing-plans, batch execution with checkpoints

选哪种？
