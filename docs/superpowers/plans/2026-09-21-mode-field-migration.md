# `mode` 字段迁移 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 把 `brickkit.yaml` 里 `enabled`（三态 `*bool`）与 `local: true` 合并成一个枚举字段 `mode`（未写/`enabled`/`disable`/`debug`——`local` 留给后续计划），迁移所有消费点只读这一个字段，泛化"冲突意图报错"规则，把 `local`/`debug`+k8s 的拒绝从 K8s 生成阶段挪到解析阶段，并把 `exposePort` 相关的一处过期文档改正。这份计划完成后系统整体行为跟今天等价（`mode: debug` ≡ 今天的 `local: true`，`mode: enabled`/`disable` ≡ 今天的 `enabled: true`/`false`），是后续三份计划（本地进程监管、语言探测、`mode: local` 集成）的地基。

**Architecture:** ① `internal/config/config.go` 的 `Component.Enabled *bool` 与 `Component.Local bool` 两个字段合并成 `Component.Mode string`（`LocalPort` 保留不动）。② `internal/config/validate.go` 新增 `mode` 枚举校验，并把今天在 `internal/k8s/k8s.go` 生成阶段才报的"`local`+k8s"错误挪到这里的解析阶段（现在扩成"`debug`+k8s"）。③ `internal/cascade/cascade.go` 的 `declSet` 从 `map[Ref]*bool` 改成 `map[Ref]string`（存 `Mode` 原始值），`pinned`/`disabled` 两个判定函数改成读 mode 值，覆盖 `enabled` 与 `debug` 两种"肯定要跑"的取值。④ `internal/compose/{compose,local}.go`、`internal/k8s/k8s.go`、`internal/cli/{up,graph,status,sync,restore,lifecycle}.go`、`internal/logging/logging.go` 里所有 `.Enabled`/`.Local` 读取点改读 `.Mode`。⑤ `schemas/*.json` 重新生成，`AGENTS.md`/`AGENTS.zh.md` 与 `docs/en(zh)/06-architecture/{07-component-yaml-reference,08-brickkit-yaml-reference}.md` 同步改写，仓库自己的示例 yaml/测试固件换成新字段，`08-brickkit-yaml-reference.md:89` 那行过期的 `exposePort` 文案顺手改正。

**Tech Stack:** Go 1.22，`testify`（`assert`/`require`），`gopkg.in/yaml.v3`，Python 3（`scripts/check-*.py`），Markdown 双语文档。

**Spec:** `docs/superpowers/specs/2026-09-21-mode-field-and-local-execution-design.md`（先读它——尤其是 §1 的历史教训与挪动决定、§8 已回收的方向）

## Global Constraints

- 代码注释、提交信息、文档正文用中文；标识符用英文。注释写"为什么"，不写"做了什么"。
- **`Mode` 必须是唯一真相**：完成后 `grep -rn "\.Enabled\b\|\.Local\b" internal/`（排除 `_test.go` 里对新 `Mode` 字符串值的字面量比较，以及 `internal/config/config.go`/`validate.go` 里定义 `Mode` 本身的代码）不应有命中。这是 Task 6 结束前必须跑的一条验收命令，见 Task 6 Step 5。
- **本计划不引入 `mode: local`**：`Mode` 的合法值这次只有 `""`（未写）/`"enabled"`/`"disable"`/`"debug"` 四个，`jsonschema:"enum=enabled|disable|debug"`（空值走 `omitempty` 自动允许 `null`/缺省，不需要把 `""` 写进 enum）。`local` 由后续计划（本地进程监管完成后）追加进枚举，本计划里出现 `mode: local` 一律按未知枚举值报错，这是当前阶段的正确行为。
- **不做旧字段兼容**：不保留 `enabled`/`local`/双写、不加过渡期。仓库自己的示例、测试固件、文档里的旧字段本次直接改成新字段。
- **每个任务结束前的验收标准，执行 Task 1-2 时发现原表述不现实，改成这样：** Go 要求整个包能编译才能跑包内任何测试，而这份计划的任务边界是"一次迁移一个包"（config → cascade → compose → k8s → cli），Task 3 完成之前 `internal/cascade` 就没法编译、Task 6 完成之前 `internal/cli` 就没法编译——这是任务拆分本身决定的，不是哪个任务没做完。所以**完整 `go build ./...` 与完整 `make lint` 全绿，只在 Task 6（`internal/cli` 迁移完，是最后一个还在读旧字段的包）结束时才作为验收标准**；Task 1-5 的验收标准是"这个任务touch到的包自己的 `go test` 全绿 + `go build ./...` 报错的包只剩计划里还没做到的那些（不能新增其它包的报错）"。另外——`make lint` 的 `check-doc-fields`（`go test ./tests/docfields/...`）不依赖编译，是纯静态文本核对（YAML 骨架实际拿去解析、文档提到的字段跟结构体反射结果比对），**这个从 Task 1 起就必须每个任务都保持绿**，因为它检查的是"文档说的话是不是真的"，不是"整个仓库能不能编译"——每个任务touch到哪个文件涉及的文档段落，就要跟着把那一段落改对，不能留到 Task 7 才补（Task 2 执行时就因为这个补了 `AGENTS.md`/`AGENTS.zh.md` 的骨架、`08-brickkit-yaml-reference.md` 的字段表、错误码文档三处，比计划原本分配给 Task 2 的范围大）。判断退出码——这台机器是 zsh，`${PIPESTATUS[0]}` 不存在，`make lint | grep` 会把失败看成成功，写成 `make lint > "$SCRATCH/lint.log" 2>&1; echo "exit=$?"` 再 `tail` 日志。
- 提交命令从 `git` 开头，不带 `cd` 前缀；提交信息末尾加一行 `Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>`。提交信息含中文全角引号时先写进文件再 `git commit -F <文件>`。
- zsh 会把 `--include=*.go` 当通配符展开，一律写成 `--include='*.go'`。
- 不碰仓库根目录未跟踪的 `改进计划.md`；若被 `check-docs`/`check-cli-docs` 拦住，临时 `mv` 到 scratchpad、跑完再 `mv` 回来。
- 文档：`docs/en`、`docs/zh` 各写一份，事实与结论必须一致，结构允许平行；**`07-patterns/05-deployment-selection-guide.md` 本次不改**（另一个项目会先做完整实操测试再反馈）。`AGENTS.md`/`AGENTS.zh.md` 是写给 AI 的压缩版。
- 新增/改动的用户可见文案一律走 `internal/msgid` + 两份 `catalog_*.go`，不写死字符串——`tests/i18nguard` 会拦。
- 不新增 `clierr.Code` 常量，复用 `CodeConfigInvalid`。**错误码文档要不要为一条新文案补行，执行 Task 2 时才搞清楚，跟原计划写的不一样：`docs/{en,zh}/06-architecture/10-error-codes.md` 只逐条收录直接经 `clierr.New`/`Newf`/`Warn`/`NewProblemSet` 调用产生的标题（`tests/docfields` 的 `sourceTitles` 只用 go/ast 扫这四个调用的第二个参数）。走 `ProblemSet.Add(field, reason)` 收集、最后由 `ProblemSet.Err()` 统一包成一个 `"brickkit.yaml failed validation"`/`"component.yaml 校验失败"` 的字段消息（`internal/config/validate.go`、`internal/manifest/validate.go` 里几乎所有校验都走这条路，包括这次新增的 `validateComponentMode`），不需要、也不应该在错误码文档里单独开一行——它已经被"Error: brickkit.yaml failed validation"那一行覆盖了（该行本身举的例子就是别的字段消息，同一个模式）。只有直接 `clierr.New(...)` 出来的独立错误（不经过某个 ProblemSet）才需要在文档里逐条登记。
- 改完 `internal/config/config.go`/`internal/manifest` 里的结构体后跑 `make generate-schemas`，把 `schemas/*.json` 的 diff 一起提交，`check-schemas` 会核对没漂移。
- 覆盖率门槛 92%（`./internal/...`，`make cover-check`）：新增代码要有测试。
- `market-server/` 是独立 module，本计划不碰它——但 Task 1 会顺手核对 `market-server/internal/validator/reserved.go` 是否有对应的 `enabled`/`local` 校验需要同步（若有，记录成后续计划的待办，不在本计划改，因为本计划范围明确是 CLI 侧字段合并）。

## File Structure

| 文件 | 职责 | 任务 |
| --- | --- | --- |
| `internal/config/config.go` | `Component.Mode string` 取代 `Enabled`/`Local`；`Mode*` 常量；`IsDisabled`/新增 `IsPinned` 读 `Mode` | 1 |
| `internal/config/validate.go` | `mode` 枚举校验；`debug`+k8s 拒绝（从 k8s.go 挪来）；`validateReplicas`/`validateServedBy` 的 `.Local` 判断改 `.Mode` | 2 |
| `internal/cascade/cascade.go` | `declSet` 从 `map[Ref]*bool` 改 `map[Ref]string`；`pinned`/`disabled` 读 mode 值 | 3 |
| `internal/compose/local.go`、`internal/compose/compose.go` | `.Local` 判断改 `.Mode == config.ModeDebug`（工作负载跳过条件、`extraHostsOf`、`localEnvFile`、`localExposeWarnings`、`localLabelWarnings`、端口分配） | 4 |
| `internal/k8s/k8s.go` | 删除 `localNotSupported`（校验已前移到 Task 2），核对没有其它 `.Local` 读取点 | 5 |
| `internal/cli/up.go`（`collectTargets`）、`graph.go`、`status.go`、`sync.go`、`restore.go`、`lifecycle.go`、`internal/logging/logging.go` | `.Local`/`.Enabled` 读取点改 `.Mode` | 6 |
| `internal/msgid/config.go`、`internal/i18n/catalog_{en,zh}.go` | `mode` 相关新文案 | 2 |
| `docs/{en,zh}/06-architecture/10-error-codes.md` | 新文案登记 | 2 |
| `schemas/component.schema.json`、`schemas/brickkit.schema.json` | 重新生成 | 7 |
| `AGENTS.md`、`AGENTS.zh.md`、`docs/{en,zh}/06-architecture/{07-component-yaml-reference,08-brickkit-yaml-reference}.md` | `mode` 字段文档、`exposePort` 过期文案修正 | 7 |
| 仓库自身示例 yaml（`docs/03-guide/`、`tests/components/`、`tests/guides/` 引用到的固定 yaml）| 旧字段换新字段 | 7 |

---

## Task 1: `Component.Mode` 字段定义 ✅ 已完成（实际范围比计划大，见下）

**执行记录（写计划时没预料到，执行时才发现）：** Go 要求整个包能编译才能跑包内任何一个测试，"只改数据结构、放着消费方编译失败"在 `internal/config` 包内部是做不到的——`internal/config/validate.go`、`internal/config/edit.go`、以及本包内一大批 `_test.go` 文件都在同一个包里引用旧字段。实际执行时把这些也一并处理了（仍然是纯迁移读取字段，不是新逻辑）：

1. `validate.go` 的 `validateComponentPorts`/`validateServedBy`/`validateReplicas` 里读 `item.Local` 的四处改读 `item.Mode == ModeDebug`（Task 2 会在这基础上再加 mode 枚举合法性校验与 debug+k8s 拒绝，不用重做这四处）。
2. `internal/config/edit.go` 的 `SetComponentEnabled(id, version string, enabled bool)`/`ClearComponentEnabled` ——这是计划最初完全没有列出的一块：`brickkit restore` 用它在 YAML 节点层面读写 `enabled` 字段，改成了 `SetComponentMode(id, version, mode string)`/`ClearComponentMode`，操作字符串而不是 bool（`mode` 的零值 `""` 本身就表达"没写"，不再需要 `*bool` 的指针语义）。**调用方 `internal/cli/restore.go` 还没跟着改**（`enabledChange`/`restorePlan`/`sameEnabled`/`applyEnabled`/`writeEnabled`/`printEnabledChanges`/`showEnabled`/`toEnabled` 整套 `*bool` 机制），这是 Task 6 的工作，Task 6 执行时直接调用这里新增的 `SetComponentMode`/`ClearComponentMode`，不要重新设计这两个方法。
3. 保留变量文档文案修正：`ConfigServedByWithLocal`/`ConfigServedByTargetLocal`/`ConfigLocalPortNeedsLocal`/`ConfigReplicasWithLocal` 四条 msgid 的文案里原本硬编码着 `"local: true"` 字样——**计划里判断错了**，以为"文案本身不用改，只是判断条件换了读取字段"，实际这四句话本身就在向用户展示字段名，字段改名后文案是错的，必须改成 `"mode: debug"`。执行任何一个后续任务时，如果又见到某条 msgid 文案里硬编码着旧字段名，都要用同样的标准去查——不能想当然假设"逻辑变了、文案不用变"。
4. `internal/compose/{compose,local}.go`、`internal/k8s/k8s.go`、`internal/cli/*.go` 的 `msgid` 目录里还有大约 20+ 条文案硬编码着 `"local: true"`（`graph`/`status`/`up` 的 `--help` 长文本、compose 的 `local: true` 组件警告、K8s 拒绝错误等），这些留给 Task 4-6 按各自触及的文件处理，不在 Task 1 范围内改（因为它们在别的包，Task 1 不需要碰那些包就能让 `internal/config` 自己编译通过）。

**原计划描述（背景，仍然成立）：** `internal/config/config.go` 的 `Component` 结构体（现有第 246-291 行附近）目前有 `Enabled *bool`（三态）与 `Local bool`（二态）两个独立字段。合并成 `Mode string`：空字符串等价于今天的 `Enabled == nil`（跟随上层，走容器），`"enabled"` 等价于今天的 `Enabled != nil && *Enabled == true`，`"disable"` 等价于今天的 `*Enabled == false`，`"debug"` 等价于今天的 `Local == true`。`LocalPort` 不变。

**验收结果**：`go test ./internal/config/... -count=1` 137 个测试全部通过；`go build ./...` 现在只在 `internal/cascade/cascade.go:289`（`c.Enabled undefined`）报错，符合预期——下一个未编译的包正是 Task 3 要处理的 `internal/cascade`。

**Files:**
- Modify: `internal/config/config.go`
- Test: `internal/config/config_test.go`

**Interfaces:**
- Produces（Task 2-6 依赖）：
  - `type Mode = string`（沿用 `Component.Mode` 的类型，不单独定义新类型，避免和 `yaml.v3`/`jsonschema` 反射打交道时多一层转换）
  - 常量：`ModeEnabled = "enabled"`、`ModeDisable = "disable"`、`ModeDebug = "debug"`（都在 `internal/config` 包内）
  - `func (c Component) IsDisabled() bool`（保留原方法名与语义，内部改读 `Mode == ModeDisable`）
  - `func (c Component) IsPinned() bool`（新增，`Mode == ModeEnabled || Mode == ModeDebug`——"肯定要跑"的判定，供 Task 3 的 cascade 与 Task 2 的冲突校验复用，避免两处各写一份判断条件）
  - **额外产出（执行时才发现需要，见下面的执行记录）**：`internal/config/edit.go` 的 `func (e *Edit) SetComponentMode(id, version, mode string) bool`、`func (e *Edit) ClearComponentMode(id, version string) bool`——Task 6 迁移 `internal/cli/restore.go` 时直接调用，不要重新实现。

- [ ] **Step 1: 写失败测试**

在 `internal/config/config_test.go` 末尾追加：

```go
func TestComponentModeHelpers(t *testing.T) {
	cases := []struct {
		name       string
		mode       string
		isDisabled bool
		isPinned   bool
	}{
		{"未写跟随上层", "", false, false},
		{"enabled 钉住", ModeEnabled, false, true},
		{"disable 关闭", ModeDisable, true, false},
		{"debug 钉住", ModeDebug, false, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := Component{Mode: tc.mode}
			assert.Equal(t, tc.isDisabled, c.IsDisabled())
			assert.Equal(t, tc.isPinned, c.IsPinned())
		})
	}
}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./internal/config/ -run TestComponentModeHelpers -v`
Expected: 编译失败（`ModeEnabled`/`ModeDisable`/`ModeDebug`/`IsPinned` 未定义），或 `Component` 结构体里没有 `Mode` 字段报错。

- [ ] **Step 3: 改结构体与方法**

在 `internal/config/config.go` 里，找到 `Component` 结构体定义（`Enabled *bool` 与 `Local bool` 两行），替换：

```go
// 改动前（删除）：
// // Enabled 是三态字段：nil=默认开启可被级联 / true=钉住 / false=显式关闭。
// Enabled   *bool `yaml:"enabled,omitempty"`
// Local     bool  `yaml:"local,omitempty"`

// 改动后：
// Mode 取代了 Enabled/Local 两个字段（004 § mode 字段迁移）：
// ""（未写）= 跟随上层，走容器；"enabled" = 钉住，走容器；
// "disable" = 钉住不跑；"debug" = 裸进程，用户自己启动（原 local: true）。
// 校验器负责按 deploy.target 决定这四个取值里哪些合法（k8s 下只认前三个）。
Mode string `yaml:"mode,omitempty" jsonschema:"enum=enabled|disable|debug"`
```

保留 `LocalPort int` 不动。新增常量（放在 `Component` 结构体定义之前）：

```go
const (
	ModeEnabled = "enabled"
	ModeDisable = "disable"
	ModeDebug   = "debug"
)
```

替换 `IsDisabled` 方法（原第 314 行附近），并新增 `IsPinned`：

```go
// IsDisabled 表示这个组件被钉死不跑（mode: disable）。
func (c Component) IsDisabled() bool { return c.Mode == ModeDisable }

// IsPinned 表示这个组件被钉死一定要跑：mode: enabled 或 mode: debug 都算——
// 写 debug 就是要盯着它调试，跟 enabled 一样不能被 cascade 判定为"没人需要就不跑"。
func (c Component) IsPinned() bool { return c.Mode == ModeEnabled || c.Mode == ModeDebug }
```

- [ ] **Step 4: 跑测试确认通过**

Run: `go test ./internal/config/ -run TestComponentModeHelpers -v`
Expected: PASS（此时仓库其余包会编译失败，这是预期状态，Task 2-6 逐步修复）

- [ ] **Step 5: 提交**

```bash
git add internal/config/config.go internal/config/config_test.go
git commit -m "$(cat <<'EOF'
重构：Component.Enabled/Local 合并成 Mode 字段（第一步：数据结构）

Mode 取代 Enabled（三态 enabled/disable）与 Local（二态 debug），LocalPort
不变。本提交只改数据结构与 IsDisabled/IsPinned 两个辅助方法，其余包的消费点
留到后续任务逐一迁移，本提交后仓库暂时无法整体编译，是预期状态。

Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>
EOF
)"
```

---

## Task 2: `mode` 校验规则（含 k8s 拒绝前移） ✅ 已完成（`8b4ba8e`）

**背景：** 新增 `validateComponentMode`，校验 `mode` 的取值合法性（只认 `""`/`enabled`/`disable`/`debug` 四个字符串，其它值报错，提示合法取值），并把今天在 `internal/k8s/k8s.go` 生成阶段才报的 `local`+k8s 错误（`localNotSupported`）挪到这里、扩成 `debug`+k8s（`local` 这次还不存在，不用管）。同时把 `internal/config/validate.go` 里 `validateReplicas`（现有第 569-584 行附近）与 `validateServedBy`（现有第 271-320 行附近）里读 `item.Local` 的两处，改成读 `item.Mode == ModeDebug`。

**Files:**
- Modify: `internal/config/validate.go`
- Modify: `internal/msgid/config.go`（新增两个 msgid）
- Modify: `internal/i18n/catalog_en.go`、`internal/i18n/catalog_zh.go`
- Modify: `docs/en/06-architecture/10-error-codes.md`、`docs/zh/06-architecture/10-error-codes.md`
- Test: `internal/config/validate_test.go`

**Interfaces:**
- Consumes：Task 1 的 `Component.Mode`、`ModeEnabled`/`ModeDisable`/`ModeDebug`。
- Produces（Task 5 依赖，用于确认 `internal/k8s/k8s.go` 的 `localNotSupported` 可以安全删除）：`validateComponentMode` 在解析阶段（`config.ParseConfigFile`/`ParseConfig` 的校验流程内）对 `mode: debug` + `deploy.target: k8s` 报 `CodeConfigInvalid`。

- [ ] **Step 1: 写失败测试**

在 `internal/config/validate_test.go` 里找到现有的 table-driven 校验测试（参照 Task 2 §Q2 提到的 `{"5.10", "deploy.target 非法值", ...}` 这类条目风格），追加：

```go
func TestValidateComponentMode(t *testing.T) {
	cases := []struct {
		name    string
		yaml    string
		wantErr []string // 期望错误信息里包含的子串，nil 表示应该通过
	}{
		{
			name: "mode 非法取值报错",
			yaml: "project: p\ndeploy:\n  target: docker\ncomponents:\n  - id: a/b\n    version: 1.0.0\n    mode: bogus\n",
			wantErr: []string{"mode", "enabled", "disable", "debug"},
		},
		{
			name: "mode: debug 配 docker 合法",
			yaml: "project: p\ndeploy:\n  target: docker\ncomponents:\n  - id: a/b\n    version: 1.0.0\n    mode: debug\n",
			wantErr: nil,
		},
		{
			name: "mode: debug 配 k8s 报错",
			yaml: "project: p\ndeploy:\n  target: k8s\ncomponents:\n  - id: a/b\n    version: 1.0.0\n    mode: debug\n",
			wantErr: []string{"mode", "k8s"},
		},
		{
			name: "mode: enabled 配 k8s 合法",
			yaml: "project: p\ndeploy:\n  target: k8s\ncomponents:\n  - id: a/b\n    version: 1.0.0\n    mode: enabled\n",
			wantErr: nil,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ParseConfig([]byte(tc.yaml), "brickkit.yaml")
			if tc.wantErr == nil {
				require.NoError(t, err)
				return
			}
			require.Error(t, err)
			for _, sub := range tc.wantErr {
				assert.Contains(t, err.Error(), sub)
			}
		})
	}
}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./internal/config/ -run TestValidateComponentMode -v`
Expected: FAIL（`mode: bogus` 与 `mode: debug` + k8s 现在都不会报错，因为校验函数还不存在）

- [ ] **Step 3: 加 msgid 与双语文案**

`internal/msgid/config.go` 新增（放在文件里其它 `Config*` 常量旁边，保持字母序或现有排列习惯）：

```go
ConfigModeInvalid       = "config.mode_invalid"
ConfigModeK8sUnsupported = "config.mode_k8s_unsupported"
```

`internal/i18n/catalog_en.go` 新增：

```go
msgid.ConfigModeInvalid:        "mode must be one of enabled/disable/debug (or omitted), got %[1]q",
msgid.ConfigModeK8sUnsupported: "mode: %[1]s is only supported with deploy.target: docker (got k8s) — a cluster Pod cannot reach a process on the developer's own machine",
```

`internal/i18n/catalog_zh.go` 新增：

```go
msgid.ConfigModeInvalid:        "mode 只能是 enabled/disable/debug 之一（或不写），实际是 %[1]q",
msgid.ConfigModeK8sUnsupported: "mode: %[1]s 只支持 deploy.target: docker（现在是 k8s）——集群里的 Pod 连不到开发者自己机器上的进程",
```

- [ ] **Step 4: 实现校验函数**

在 `internal/config/validate.go` 里，找到组件级校验的入口（`validateComponents` 遍历每个 `Component` 调用一系列 `validateXxx` 的地方，参照 `validateComponentPorts`/`validateReplicas`/`validateServedBy` 的调用方式），新增并接入：

```go
// validateComponentMode 校验 mode 的取值合法性，以及 mode: debug 与
// deploy.target: k8s 的组合——这条检查以前在 K8s 生成阶段才报（internal/k8s/k8s.go
// 的 localNotSupported），这次挪到解析阶段：不需要依赖图，跟 validateComponentPorts
// 已经在做的 deploy.target 组合校验是同一类检查，挪过来后 `brickkit lint` 就能
// 拿到这个错误，不用等真正生成部署文件。
func (c *Config) validateComponentMode(p *clierr.ProblemSet, field string, item Component) {
	switch item.Mode {
	case "", ModeEnabled, ModeDisable, ModeDebug:
		// 合法取值
	default:
		p.Add(field+".mode", i18n.T(msgid.ConfigModeInvalid, item.Mode))
		return
	}
	if item.Mode == ModeDebug && c.Deploy.Target == TargetK8s {
		p.Add(field+".mode", i18n.T(msgid.ConfigModeK8sUnsupported, item.Mode))
	}
}
```

在 `validateComponents`（或等价的组件遍历函数，与 `validateComponentPorts`/`validateReplicas`/`validateServedBy` 同一处调用）里加一行：

```go
c.validateComponentMode(p, field, item)
```

- [ ] **Step 5: 迁移 `validateReplicas`/`validateServedBy` 里的 `.Local` 判断**

`internal/config/validate.go` 的 `validateReplicas`（现有约第 569-584 行）：

```go
// 改动前：
// if item.Local {
// 	p.Add(name, i18n.T(msgid.ConfigReplicasWithLocal))
// }

// 改动后：
if item.Mode == ModeDebug {
	p.Add(name, i18n.T(msgid.ConfigReplicasWithLocal))
}
```

`validateServedBy`（现有约第 271-320 行）两处 `item.Local` 同理改成 `item.Mode == ModeDebug`（msgid 常量 `ConfigServedByWithLocal`/`ConfigServedByTargetLocal` 保持不变，只是判断条件换了读取字段，文案本身不用改——`mode: debug` 依然是"本地调试"这同一个概念，只是换了个字段名承载）。

- [ ] **Step 6: 跑测试确认通过**

Run: `go test ./internal/config/ -run 'TestValidateComponentMode|TestValidateReplicas|TestValidateServedBy' -v`
Expected: PASS

- [x] **Step 7: 登记错误文案到错误码文档 —— 执行时发现这一步是错的，不需要做**

`validateComponentMode` 是通过 `p.Add(field, reason)`（`ProblemSet`）报的，最终统一包进 `"Error: brickkit.yaml failed validation"` 这一条顶层错误（`internal/config/parse.go` 的 `newConfigProblems`），不是直接 `clierr.New(...)`。`tests/docfields` 的 `TestErrorCodesDocTitlesExistInSource` 只用 go/ast 扫描直接 `clierr.New`/`Newf`/`Warn`/`NewProblemSet` 调用的第二个参数——`ProblemSet.Add` 收集的字段消息永远不会以这种形式出现，所以**不需要、也不应该**给这两条消息单独加错误码文档行，它们已经被现有的 "Error: brickkit.yaml failed validation" 那一行覆盖（该行本身举的例子就是另一个字段的校验消息）。实际验收标准是跑 `go test ./tests/docfields/... -count=1`，不需要手动编辑 `10-error-codes.md`。**这条经验已经回写进 Global Constraints，Task 3-6 遇到同类"要不要给新校验消息登记错误码文档"的问题时按同一个判断标准处理：直接 `clierr.New` 才登记，`ProblemSet.Add` 不登记。**

- [ ] **Step 8: 提交**

```bash
git add internal/config/validate.go internal/config/validate_test.go internal/msgid/config.go internal/i18n/catalog_en.go internal/i18n/catalog_zh.go docs/en/06-architecture/10-error-codes.md docs/zh/06-architecture/10-error-codes.md
git commit -m "$(cat <<'EOF'
新增：mode 字段的枚举校验，debug+k8s 拒绝从生成阶段挪到解析阶段

新增 validateComponentMode：校验 mode 只能是 enabled/disable/debug（或不写），
并把 mode: debug 撞 deploy.target: k8s 的报错从 internal/k8s/k8s.go 的生成阶段
挪到这里的解析阶段——brickkit lint 现在就能捕获这个错误，不用等到真正生成部署
文件。validateReplicas/validateServedBy 里原本读 item.Local 的两处一并改读
item.Mode == ModeDebug。

Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>
EOF
)"
```

---

## Task 3: cascade 泛化（`declSet` 改读 `Mode`） ✅ 已完成（`c65bc0a`）

**背景：** `internal/cascade/cascade.go` 的 `declSet`（现有 `map[resolver.Ref]*bool`）、`declarations(cfg)`、`pinned(ref)`、`disabled(ref)` 四处都要从"三态 `*bool`"改成读 `Mode` 字符串。语义映射：`pinned` 现在要覆盖 `enabled` 与 `debug` 两个取值（用 Task 1 新增的 `Component.IsPinned()`），`disabled` 覆盖 `disable`（用 `Component.IsDisabled()`）。`disabledDependencyError`（冲突报错，现有第 315-338 行）不需要改——它已经是"钉住的组件撞上被关掉的强依赖就报错"这个通用逻辑，改的只是"钉住"（`pinned`）的判定范围，从"只有 `enabled: true`"扩到"`enabled` 或 `debug`"，报错文本和触发条件的代码结构完全不用动。

**Files:**
- Modify: `internal/cascade/cascade.go`
- Test: `internal/cascade/cascade_test.go`

**Interfaces:**
- Consumes：Task 1 的 `Component.IsPinned()`/`IsDisabled()`。
- Produces（Task 4-6 依赖）：`cascade.Compute` 的对外签名不变（`Compute(cfg *config.Config, graph *resolver.Graph) (*Result, error)`），只是内部判定逻辑变了，调用方不需要跟着改。

- [ ] **Step 1: 写失败测试（新增"debug 钉住"场景）**

在 `internal/cascade/cascade_test.go` 里找到现有覆盖"`enabled: true` 撞上必需依赖被禁用要报错"的用例，参照它的结构追加一个用 `mode: debug` 触发同一冲突的用例：

```go
func TestComputeDebugPinnedConflictsWithDisabledDependency(t *testing.T) {
	cfg := &config.Config{
		Components: []config.Component{
			{ID: "a", Version: "1.0.0", Mode: config.ModeDebug},
			{ID: "b", Version: "1.0.0", Mode: config.ModeDisable},
		},
	}
	graph := &resolver.Graph{
		Nodes: []*resolver.Node{
			{Ref: resolver.Ref{ID: "a", Version: "1.0.0"}, Requires: []resolver.Ref{{ID: "b", Version: "1.0.0"}}},
			{Ref: resolver.Ref{ID: "b", Version: "1.0.0"}, Dependents: []resolver.Ref{{ID: "a", Version: "1.0.0"}}},
		},
	}
	_, err := Compute(cfg, graph)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "a@1.0.0")
	assert.Contains(t, err.Error(), "b@1.0.0")
}

func TestComputeDebugKeepsRunningEvenIfNotNeeded(t *testing.T) {
	// mode: debug 的组件即便没有顶层依赖它，也要强制运行——跟 enabled: true 一样。
	cfg := &config.Config{
		Components: []config.Component{
			{ID: "a", Version: "1.0.0", Mode: config.ModeDebug},
		},
	}
	graph := &resolver.Graph{
		Nodes: []*resolver.Node{
			{Ref: resolver.Ref{ID: "a", Version: "1.0.0"}}, // 没有 Dependents，非顶层也非被依赖
		},
	}
	result, err := Compute(cfg, graph)
	require.NoError(t, err)
	assert.True(t, result.IsRunning(resolver.Ref{ID: "a", Version: "1.0.0"}))
}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./internal/cascade/ -run 'TestComputeDebugPinnedConflictsWithDisabledDependency|TestComputeDebugKeepsRunningEvenIfNotNeeded' -v`
Expected: FAIL（`mode: debug` 现在不会被当成"钉住"，两个用例都得不到期望结果）

- [ ] **Step 3: 改 `declSet` 与两个判定函数**

在 `internal/cascade/cascade.go` 里，把（现有约第 281-302 行）：

```go
// 改动前：
// type declSet map[resolver.Ref]*bool
// func declarations(cfg *config.Config) declSet {
// 	out := declSet{}
// 	for _, c := range cfg.Components {
// 		out[resolver.Ref{ID: c.ID, Version: c.Version}] = c.Enabled
// 	}
// 	return out
// }
// func (d declSet) pinned(ref resolver.Ref) bool {
// 	enabled, ok := d[ref]
// 	return ok && enabled != nil && *enabled
// }
```

改成：

```go
// declSet 是 brickkit.yaml 里对各组件的 mode 声明（未写/enabled/disable/debug）。
// 存组件本身（不只是 mode 字符串），因为 pinned/disabled 要复用 Component 上
// 已经写好的 IsPinned/IsDisabled，避免这里和 config 包各自维护一份判定逻辑——
// mode 取代 Enabled 时留下的教训就是"改判定规则要动两处，只有一处会被测到"，
// 这次把判定逻辑唯一地放在 Component 方法上，declSet 只做查找。
type declSet map[resolver.Ref]config.Component

func declarations(cfg *config.Config) declSet {
	out := declSet{}
	for _, c := range cfg.Components {
		out[resolver.Ref{ID: c.ID, Version: c.Version}] = c
	}
	return out
}

func (d declSet) pinned(ref resolver.Ref) bool {
	c, ok := d[ref]
	return ok && c.IsPinned()
}

func (d declSet) disabled(ref resolver.Ref) bool {
	c, ok := d[ref]
	return ok && c.IsDisabled()
}
```

（`disabled` 方法若原本已存在于文件别处、不是内联在 `declSet` 定义旁边，找到它原来的实现一并替换成上面这版，保持只有一份 `disabled` 定义。）

- [ ] **Step 4: 跑测试确认通过**

Run: `go test ./internal/cascade/... -v`
Expected: PASS（包括本任务新增的两个用例和文件里原有的全部用例——尤其是原本覆盖 `enabled: true` 冲突场景的用例，改完 `declSet` 后应该继续通过，证明这次改动没有破坏原有语义）

- [ ] **Step 5: 提交**

```bash
git add internal/cascade/cascade.go internal/cascade/cascade_test.go
git commit -m "$(cat <<'EOF'
重构：cascade 的 declSet 改存 Component、pinned/disabled 复用 IsPinned/IsDisabled

declSet 从 map[Ref]*bool 改成 map[Ref]config.Component，pinned() 与 disabled()
不再自己解读三态 bool，改成调用 Component.IsPinned()/IsDisabled()——判定逻辑
唯一地放在 Task 1 新增的这两个方法上，避免 cascade 包和 config 包各自维护一份
"什么算钉住"的规则。mode: debug 现在跟 enabled 一样会被 cascade 当作钉住：
必需依赖被 disable 时同样报冲突错误，没有上层依赖它时也照样强制运行。

Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>
EOF
)"
```

---

## Task 4: `internal/compose` 迁移 ✅ 已完成（`bf0a7c1`）

**背景：** `internal/compose/local.go`、`internal/compose/compose.go` 里所有读 `.Local`（`config.Component.Local`）的地方改成 `.Mode == config.ModeDebug`：工作负载是否生成的判断、`extraHostsOf`（`host-gateway` 映射）、`localEnvFile`/`localEnvFiles`（`local-debug.<service>.env` 生成）、`localExposeWarnings`/`localLabelWarnings`（警告不报错的字段检查）、端口分配（`assignHostPorts`）。这些函数内部逻辑不变，只是判断条件从读一个 bool 字段换成比较字符串。

**Files:**
- Modify: `internal/compose/local.go`
- Modify: `internal/compose/compose.go`
- Test: `internal/compose/local_test.go`、`internal/compose/compose_test.go`

**Interfaces:**
- Consumes：Task 1 的 `config.ModeDebug`。
- Produces：无新接口，纯迁移，对外行为不变（`mode: debug` 的组件生成的 compose 文件、`local-debug.env`、警告文案应该跟今天 `local: true` 生成的完全一致，只是触发字段换了名字）。

- [ ] **Step 1: 跑现有测试确认基线**

Run: `go test ./internal/compose/... -v 2>&1 | tee "$SCRATCH/compose-before.log"`

先确认在改动前，现有覆盖 `local: true` 行为的测试（生成 `extra_hosts`、`local-debug.env`、`localExposeWarnings` 等）都是绿的——这些测试目前应该还在用旧的 `Local: true` 构造测试数据，本任务的改动会让它们编译失败，Step 2 逐个改成 `Mode: config.ModeDebug` 构造数据，不改断言本身（因为行为不该变）。

- [ ] **Step 2: 全局替换测试数据构造**

`internal/compose/local_test.go`、`internal/compose/compose_test.go` 里所有 `config.Component{..., Local: true, ...}` 的测试数据构造，改成 `config.Component{..., Mode: config.ModeDebug, ...}`：

```bash
grep -rln "Local: *true" internal/compose/*_test.go
```

对每个命中的文件，把 `Local: true` 替换成 `Mode: config.ModeDebug`（逐个文件手动确认替换点，不要用无差别的全局 `sed`，因为要确认每处确实是在构造 `config.Component` 而不是别的同名字段）。

- [ ] **Step 3: 跑测试确认失败（预期是编译错误）**

Run: `go test ./internal/compose/... 2>&1 | head -50`
Expected: 编译失败，报 `Local` 字段不存在（因为 `internal/compose/local.go`/`compose.go` 的生产代码还没改）

- [ ] **Step 4: 迁移生产代码**

`internal/compose/local.go` 里所有形如 `c.Entry.Local`/`item.Local`/`s.Entry.Local`（具体变量名以实际代码为准，统一模式是"某个 `config.Component` 值的 `.Local` 字段"）的读取，改成 `.Mode == config.ModeDebug`。重点覆盖：

- 工作负载跳过条件（决定"这个组件要不要生成容器"的地方，`internal/compose/compose.go` 里与 `up.go` 的 `collectTargets` 共享同一条件——**本任务只改 `compose.go` 这一侧，`up.go` 那一侧留给 Task 6，改完 Task 6 之后两处必须严格一致，否则会复现"servedBy 成员混进启动列表导致 no such service"同类故障，Task 6 的验收步骤会包含"两处条件字符串完全一致"的检查**）：
  ```go
  // 改动前： if c.Entry.Local || c.Entry.ServedBy != "" { ... }
  // 改动后：
  if c.Entry.Mode == config.ModeDebug || c.Entry.ServedBy != "" { ... }
  ```
- `extraHostsOf`（`internal/compose/local.go`）里判断"这个依赖是不是 local"的地方（原本靠 `p.localPort[service]` 这张表判断，这张表的填充逻辑本身不依赖 `.Local` 字段，不用改；但函数里如果还有单独的 `.Local` 读取，一并改）。
- `localExposeWarnings`/`localLabelWarnings`：判断"这个组件是不是本地调试模式、要不要检查它写没写 `expose`/`labels` 之类惰性字段"的条件。
- `assignHostPorts`：判断"这个组件要不要分配本地端口"的条件。

- [ ] **Step 5: 跑测试确认通过**

Run: `go test ./internal/compose/... -v 2>&1 | tee "$SCRATCH/compose-after.log"`
Expected: PASS，且 `diff "$SCRATCH/compose-before.log" "$SCRATCH/compose-after.log"` 里除了测试名称/字段名相关的行，断言结果（PASS/FAIL 的分布）应该完全一致——证明行为没有变化，只是换了触发字段。

- [ ] **Step 6: 提交**

```bash
git add internal/compose/local.go internal/compose/compose.go internal/compose/local_test.go internal/compose/compose_test.go
git commit -m "$(cat <<'EOF'
重构：internal/compose 的 Local 判断改读 Mode == ModeDebug

工作负载跳过条件、extra_hosts 生成、local-debug.env 生成、localExposeWarnings/
localLabelWarnings、本地端口分配——全部从读 config.Component.Local 改成读
config.Component.Mode == config.ModeDebug。纯迁移，mode: debug 组件生成的
compose 文件、local-debug.env、警告文案跟改动前的 local: true 完全一致。

Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>
EOF
)"
```

---

## Task 5: `internal/k8s` 迁移（删除生成阶段的 `local`+k8s 拒绝） ✅ 已完成（`7ef9eb9`）

**背景：** Task 2 已经把 `debug`+k8s 的拒绝挪到了 `internal/config/validate.go` 的解析阶段，`internal/k8s/k8s.go` 里原来的 `localNotSupported` 生成阶段检查现在是死代码（在它执行到之前，解析阶段已经报错退出了）——删除它，并核对 `internal/k8s/k8s.go`、`internal/k8s/servedby.go` 里还有没有其它读 `.Local`/`.Enabled` 的地方（研究阶段没有找到别的，但改动前要用 grep 核实一遍，避免遗漏）。

**Files:**
- Modify: `internal/k8s/k8s.go`
- Test: `internal/k8s/k8s_test.go`

**Interfaces:**
- Consumes：Task 2 产出的解析阶段拒绝（本任务的测试要证明"删掉生成阶段检查之后，`mode: debug` + k8s 依然在更早的阶段被挡住，行为对用户来说没有退化，只是报错时机提前了"）。

- [ ] **Step 1: 核实没有遗漏的 `.Local`/`.Enabled` 读取点**

Run: `grep -rn "\.Local\b\|\.Enabled\b" internal/k8s/`

如果除了 `localNotSupported` 相关代码外还有命中，记下具体位置，本任务一并处理（改成读 `.Mode`）；如果没有其它命中，直接进入 Step 2。

- [ ] **Step 2: 写测试，证明端到端行为不退化**

在 `internal/k8s/k8s_test.go`（或等价的集成测试文件）里，找到原本覆盖 `local: true` + k8s 报错的测试用例（如果存在，是覆盖 `localNotSupported` 那条路径的），**不要直接删除它**——改写成验证"这条路径现在走不到，因为更早的解析阶段已经报错"：

```go
func TestModeDebugWithK8sTargetRejectedAtParseNotGeneration(t *testing.T) {
	// mode: debug + deploy.target: k8s 现在应该在 config.ParseConfig 阶段就报错
	// （Task 2），k8s.Generate 根本不会被调用到——这条测试证明这一点，同时也是
	// 删除 internal/k8s/k8s.go 里生成阶段的 localNotSupported 检查是安全的依据。
	yaml := "project: p\ndeploy:\n  target: k8s\ncomponents:\n  - id: a/b\n    version: 1.0.0\n    mode: debug\n"
	_, err := config.ParseConfig([]byte(yaml), "brickkit.yaml")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "mode")
}
```

- [ ] **Step 3: 删除生成阶段检查**

在 `internal/k8s/k8s.go` 里，删除 `localNotSupported` 函数本身，以及调用它的那一行（原第 379 行附近，`return nil, localNotSupported(locals)` 那段逻辑，包括收集 `locals` 切片的代码，如果这段收集逻辑只为这一个检查服务）。同时删除对应的 msgid `K8sLocalNotSupported` 在 `internal/msgid/k8s.go` 里的定义，以及两份 catalog 里的文案（先用 `grep -rn "K8sLocalNotSupported" internal/` 确认没有其它引用再删，避免残留死引用导致编译错误或者 `TestCatalogParity` 报"catalog 里有 msgid 用不到"之类的警告，如果这个仓库的 i18n 守卫会检查"未使用的 msgid"，删除时要一并处理）。

- [ ] **Step 4: 跑测试确认通过**

Run: `go test ./internal/k8s/... -v`
Expected: PASS

- [ ] **Step 5: 提交**

```bash
git add internal/k8s/k8s.go internal/k8s/k8s_test.go internal/msgid/k8s.go internal/i18n/catalog_en.go internal/i18n/catalog_zh.go
git commit -m "$(cat <<'EOF'
删除：K8s 生成阶段的 local/debug 拒绝检查（已在 Task 2 挪到解析阶段）

localNotSupported 现在是死代码——mode: debug + deploy.target: k8s 在
config.ParseConfig 阶段就会报错，走不到 K8s 生成这一步。新增测试证明这一点，
删除生成阶段的检查与相关 msgid。

Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>
EOF
)"
```

---

## Task 6: `internal/cli` 与 `internal/logging` 迁移 ✅ 已完成（`744e557`）

**背景：** 这是本计划改动面最大的一个任务——`internal/cli/up.go`（`collectTargets`）、`graph.go`、`status.go`、`sync.go`、`restore.go`、`lifecycle.go`，以及 `internal/logging/logging.go`，全部要把读 `.Local`/`.Enabled` 的地方改成读 `.Mode`。这些文件里对"local"的语义引用（比如 `graph.go` 的 `classLocal` 样式、`status.go` 的 `renderLocalDebug`）**本任务只做字段迁移，不改视觉/文案**——`mode: debug` 组件的 `graph`/`status` 输出应该跟今天 `local: true` 组件的输出完全一致，只是内部判断条件换了。

**Files:**
- Modify: `internal/cli/up.go`（`collectTargets`，现有第 459-498 行附近）
- Modify: `internal/cli/graph.go`（`declare` 闭包，现有第 152-174 行附近）
- Modify: `internal/cli/status.go`（`componentView`/`buildView`/`resolvedView`/`degradedView`，现有第 116-189 行附近）
- Modify: `internal/cli/sync.go`、`internal/cli/restore.go`、`internal/cli/lifecycle.go`
- Modify: `internal/logging/logging.go`
- Test: 对应的 `*_test.go`

**Interfaces:**
- Consumes：Task 1 的 `config.ModeDebug`。
- Produces：无新接口。

- [ ] **Step 1: 列出完整改动面**

Run: `grep -rn "\.Local\b\|\.Enabled\b\|\.IsDisabled(\|c\.Local\|item\.Local" internal/cli/ internal/logging/ --include='*.go' | grep -v _test.go`

把输出结果整理成一份清单（写进 `$SCRATCH/task6-todo.txt` 供本任务对照，不提交），逐个文件处理，处理完一个从清单划掉一个。

- [ ] **Step 2: 迁移 `up.go` 的 `collectTargets`**

```go
// 改动前： if c.Local || c.ServedBy != "" { continue }  // 大意，具体变量名以实际代码为准
// 改动后：
if c.Mode == config.ModeDebug || c.ServedBy != "" { continue }
```

**关键验收点**：这个条件字符串必须跟 Task 4 里改过的 `internal/compose/compose.go` 那一处**逐字一致**（`c.Mode == config.ModeDebug || c.ServedBy != ""` 或语义完全等价的写法），跑：

```bash
grep -n "Mode == config.ModeDebug\|Mode == ModeDebug" internal/cli/up.go internal/compose/compose.go
```

两处的判断条件必须同时存在且逻辑等价——历史上这两处条件不一致导致过"servedBy 成员混进启动列表、docker compose 报 no such service"的故障，这次迁移不能重现。

- [ ] **Step 3: 迁移 `graph.go`**

`declare` 闭包里原本判断 `entry.Local` 的地方（决定要不要给节点加"本地调试"标签、要不要打 `classLocal` 样式）：

```go
// 改动前： if entry.Local { ... }
// 改动后：
if entry.Mode == config.ModeDebug { ... }
```

标签文案（`msgid.CliGraphBrLocalDebug`，"本地调试"）与样式类（`classLocal`）都不改，只改触发条件。

- [ ] **Step 4: 迁移 `status.go`/`sync.go`/`restore.go`/`lifecycle.go`/`logging.go`**

`status.go` 里 `componentView.local []resolver.Ref` 的填充条件（`resolvedView`/`degradedView` 里判断某个组件该不该归进 `v.local` 桶）：

```go
// 改动前： if c.Local { view.local = append(view.local, ref) }
// 改动后：
if c.Mode == config.ModeDebug { view.local = append(view.local, ref) }
```

`sync.go`/`lifecycle.go`/`logging.go` 里的命中点按同样模式逐一替换（具体位置以 Step 1 的清单为准，每改一个文件跑一次该文件对应的测试，不要攒到最后一次性跑全部——出错时更容易定位是哪一步引入的）。

**`restore.go` 的迁移范围比其它文件大，单独说清楚（Task 1 执行时发现的，`internal/config/edit.go` 那一侧的新方法已经在 Task 1 提交里落地了）**：整个文件是围绕 `enabledChange{from, to *bool}`/`restorePlan`/`sameEnabled`/`applyEnabled`/`writeEnabled`/`printEnabledChanges`/`showEnabled`/`toEnabled` 这一整套 `*bool` 三态比较机制写的，不是简单的字段读取替换。要改成：
- `enabledChange` 的 `from`/`to` 从 `*bool` 改成 `string`——不需要指针，`""` 本身就表达"没写"（跟 `Component.Mode` 自己的语义一致）。
- `sameEnabled(a, b *bool) bool` 这个辅助函数可以整个删掉：两个 `Mode` 字符串直接用 `==` 比较就行，不再需要专门处理"nil 与 false 不是一回事"这种指针特例。
- `restorePlan` 里 `headEnabled := make(map[string]*bool...)` 改成 `headMode := make(map[string]string...)`，取值改成 `c.Mode`，`sameEnabled(c.Enabled, want)` 那行直接改成 `c.Mode == want`。
- `applyEnabled` 里 `cfg.Components[i].Enabled = ch.to` 改成 `cfg.Components[i].Mode = ch.to`。
- `writeEnabled` 里 `if ch.to == nil { edit.ClearComponentEnabled(...) } else { edit.SetComponentEnabled(ch.id, ch.version, *ch.to) }` 改成 `if ch.to == "" { edit.ClearComponentMode(ch.id, ch.version) } else { edit.SetComponentMode(ch.id, ch.version, ch.to) }`——`ClearComponentMode`/`SetComponentMode` 已经在 `internal/config/edit.go` 里了，直接调用，不要重新实现。
- `showEnabled(v *bool)`/`toEnabled(v *bool)` 改成 `showMode(v string)`/`toMode(v string)`：`v == ""` 时走"没写"/"删除该字段"的文案分支，否则直接返回 `v` 本身（不再需要 `strconv.FormatBool`）。
- 相应地把 `internal/cli/restore_test.go` 里构造 `enabledChange{...}`/`config.Component{Enabled: ...}` 的测试数据统一改成 `Mode: string` 的写法（用跟 Task 4 Step 2 一样的办法：先 `grep -n "Enabled:\|\*bool\|&enabled\|sameEnabled"` 定位，逐个替换，不要无差别 `sed`）。
- 文件头部那句包注释（"本文件实现 brickkit restore：把 brickkit.yaml 的 enabled 与组件源码结构还原到最后一次提交"）与函数上那些提到"enabled"的注释一并改成"mode"。

- [ ] **Step 5: 全量迁移验收**

Run:
```bash
grep -rn "\.Enabled\b\|\.Local\b" internal/ --include='*.go' | grep -v _test.go | grep -v "internal/config/config.go" | grep -v "internal/config/validate.go"
```

Expected: 空输出。如果还有命中，回到 Step 1-4 补漏（`internal/manifest`、`internal/resolver` 等包如果有命中也要处理，虽然研究阶段没有发现，但这条命令是最终裁决标准，不是研究报告）。

- [ ] **Step 6: 跑全仓库测试**

Run: `go test ./internal/... -count=1 2>&1 | tee "$SCRATCH/task6-full.log"; echo "exit=$?"`
Expected: `exit=0`，全部包测试通过。

- [ ] **Step 7: 提交**

```bash
git add internal/cli internal/logging
git commit -m "$(cat <<'EOF'
重构：internal/cli 与 internal/logging 的 Local/Enabled 判断改读 Mode

collectTargets、graph 节点标签、status 的 local 分桶、sync/restore/lifecycle
里的判断条件全部迁移到 Mode。collectTargets 与 internal/compose/compose.go 的
工作负载跳过条件保持逐字一致（历史上两处不一致导致过 servedBy 成员混进启动
列表的故障）。grep -rn ".Enabled\b|.Local\b" internal/ 现在只在 config 包定义
Mode 本身的地方命中，迁移完整。

Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>
EOF
)"
```

---

## Task 7: Schema 重新生成、文档同步、仓库自身示例迁移 ✅ 已完成（Schema 随 `744e557`，文档随 `5cb411b`）

**背景：** 前六个任务改完代码后，`schemas/*.json` 需要重新生成；`AGENTS.md`/`AGENTS.zh.md` 与 `docs/{en,zh}/06-architecture/{07-component-yaml-reference,08-brickkit-yaml-reference}.md` 里描述 `enabled`/`local`/`localPort` 的段落要同步改写成 `mode`；`08-brickkit-yaml-reference.md:89` 那句"`exposePort`+k8s 目前没有任何东西会挡它"是过期文案（研究阶段确认 `warnTargetOnlyFields` 早就在挡了），顺手改正；仓库自己引用到 `enabled`/`local: true`/`localPort` 的示例 yaml（`docs/03-guide/` 教程、`tests/components/`、`tests/guides/` 涉及的固定 yaml）要换成新字段，保证 `make check-guides`/`check-guide-output` 这类真机验证不会因为示例还在用旧字段而失败。

**Files:**
- Modify: `internal/config/config.go`（若 Task 1-6 过程中还有遗留的 `jsonschema` tag 细节要调整）
- Modify: `schemas/component.schema.json`、`schemas/brickkit.schema.json`（生成产物）
- Modify: `AGENTS.md`、`AGENTS.zh.md`
- Modify: `docs/en/06-architecture/07-component-yaml-reference.md`、`docs/zh/06-architecture/07-component-yaml-reference.md`
- Modify: `docs/en/06-architecture/08-brickkit-yaml-reference.md`、`docs/zh/06-architecture/08-brickkit-yaml-reference.md`
- Modify: 仓库内引用 `enabled`/`local: true`/`localPort` 的示例 yaml（先 grep 定位，见 Step 3）

- [ ] **Step 1: 重新生成 JSON Schema**

Run: `make generate-schemas`

核对 diff：`git diff schemas/` 里 `component.schema.json`/`brickkit.schema.json` 应该出现 `mode` 字段的 `enum: [enabled, disable, debug]`，且不再出现 `enabled`/`local`（`localPort` 应该还在，不受影响）。

- [ ] **Step 2: 跑 schema 一致性检查**

Run: `go test ./internal/schemagen/ -count=1 -v`
Expected: PASS（`check-schemas` 背后跑的就是这个测试，确认 schema 里的约束跟真实校验器一致）

- [ ] **Step 3: 定位并迁移仓库自身的示例 yaml**

Run:
```bash
grep -rln "enabled:\|local: *true\|localPort:" docs/ tests/ --include='*.yaml' --include='*.yml' --include='*.md'
```

对每个命中文件：
- `.yaml`/`.yml` 固定配置文件：`enabled: true` → `mode: enabled`；`enabled: false` → `mode: disable`；`local: true` → `mode: debug`（`localPort` 不变，跟 `mode: debug` 搭配一起写）。
- `.md` 教程文档里内嵌的 yaml 代码块与对应的 CLI 输出说明：同样替换，注意如果文档里有"写 `enabled: false` 表示关闭"这类文字说明，措辞也要跟着改成"写 `mode: disable`"。

- [ ] **Step 4: 改写 `AGENTS.md`/`AGENTS.zh.md`**

`AGENTS.md` 里以下几处需要改写（对照当前内容定位，改写成反映 `mode` 字段的版本，`AGENTS.zh.md` 做同等改写）：
- §3 术语表：`强依赖`/`弱依赖`那一带没有涉及 `enabled`，不用改；但如果术语表里有单独一行讲 `enabled`/`local: true` 的，要改成讲 `mode`。
- §5.4"`enabled`：top-down inheritance"整节标题与内容，改写成"`mode`：包含 enabled/disable 两态的 top-down inheritance"，取值表格从"未写/true/false"三行改成"未写/enabled/disable/debug"四行（本计划范围内 `local` 还不存在，不要提前写进去，避免文档说了一个这次还没实现的能力）。
- §5.6"Local debugging (`local: true`)"整节标题与内容里所有 `local: true` 的写法改成 `mode: debug`。
- §6 `component.yaml` 字段骨架：如果里面举例引用了 `enabled`/`local`，改成 `mode` 的例子。
- §7 `brickkit.yaml` 字段骨架：`enabled: true`/`local: false`/`localPort: 8081` 那几行改成 `mode: debug`（或按场景改成合适的取值）+ `localPort: 8081`。
- §10"讨论要点"表格里提到 `enabled`/`local: true` 的行，措辞同步。

- [ ] **Step 5: 改写 component.yaml / brickkit.yaml 参考文档**

`docs/en/06-architecture/07-component-yaml-reference.md`、`docs/zh/...`：这两份文档目前不直接涉及 `enabled`/`local`（那是 `brickkit.yaml` 的字段，不是 `component.yaml` 的），核实一遍确认没有需要改的地方即可（如果确认没有，这一步不产生任何 diff，属于正常结果，不是遗漏）。

`docs/en/06-architecture/08-brickkit-yaml-reference.md`、`docs/zh/...`：
- 把描述 `enabled`/`local`/`localPort` 字段的表格行改写成描述 `mode`/`localPort`。
- 第 89 行附近关于 `exposePort` 的那句过期文案，改成：

  ```
  英文版改成大意：`components[].exposePort` only takes effect under
  `deploy.target: docker`; writing it alongside `deploy.target: k8s` is
  accepted at parse time and triggers a warning at `up` time (including
  `--dry-run`) via `warnTargetOnlyFields` — not silently ignored.
  ```

  中文版对应改写，两边结论保持一致（这句话原本说"没有任何东西会挡它"，现在要明确说"是警告，不是静默失效"）。

- [ ] **Step 6: 跑完整校验**

Run: `make lint > "$SCRATCH/task7-lint.log" 2>&1; echo "exit=$?"`

Expected: `exit=0`。如果 `check-docs-bilingual`/`check-guide-output`/`check-doc-fields` 任何一个红，看日志定位具体哪个文件哪一行，回到 Step 3-5 补上遗漏（常见遗漏：某个 `.md` 教程文件的"预期输出"代码块里还留着旧字段名，或者中英文两份文档改动不同步）。

- [ ] **Step 7: 跑 `make check-guides`（如果本机有 Docker）**

Run: `make check-guides 2>&1 | tee "$SCRATCH/check-guides.log"`

如果本机没有 Docker/K8s，`docker`/`k8s` 两层会"响亮跳过"（输出里会明确写跳过原因），`core` 层必须通过——`core` 层覆盖了不需要 Docker 的部分，包括新的 `mode` 字段解析，这一层跑通足以证明本次迁移在示例层面是自洽的。

- [ ] **Step 8: 提交**

```bash
git add schemas/ AGENTS.md AGENTS.zh.md docs/
git commit -m "$(cat <<'EOF'
文档：mode 字段迁移的收尾——重新生成 Schema，AGENTS.md 与字段参考文档同步

component.yaml/brickkit.yaml 的 JSON Schema 重新生成，反映 enabled/local 合并
成 mode 后的新枚举。AGENTS.md/AGENTS.zh.md 的 §5.4/§5.6/§6/§7 与
08-brickkit-yaml-reference.md 同步改写；顺手修正 08-brickkit-yaml-reference.md
里一句关于 exposePort+k8s 的过期文案（早就有警告在挡，不是"没人挡"）。仓库自身
教程与固件里的 enabled/local: true/localPort 示例换成 mode 的写法。
deployment-selection-guide.md 本次不改（另一个项目会先做完整实操测试）。

Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>
EOF
)"
```

---

## Self-Review Checklist（执行完 7 个任务后逐条核对）

- [x] `grep -rn "\.Enabled\b\|\.Local\b" internal/` 除 `internal/config/{config,validate}.go` 外无命中。——剩下的命中都是别的结构体上同名的字段（`Source.Enabled`、`NetworkPolicy.Enabled`、`ServiceAccount.Enabled`、`slog.Logger.Enabled()`），与 `Component` 无关。
- [x] `go test ./internal/... -count=1` 全绿，覆盖率不低于 92%。——`go test ./...` 全绿，`make lint` 报覆盖率 93.3%。
- [x] `make lint` 全绿。——`exit=0`。这台机器没装 golangci-lint，Makefile 回退到 `go vet`；合并前在装了它的环境再跑一次全量。
- [x] `mode: debug` + `deploy.target: k8s` 在 `brickkit lint`（不生成任何文件）阶段就报错，不需要跑到 `up`/`up --dry-run`。——真二进制验证过，退出码 1，`components[0].mode: "debug" is only supported with deploy.target: docker (got k8s) ——…`。
- [x] `mode: debug` 组件生成的 `docker-compose.yaml`/`local-debug.<service>.env`/`extra_hosts` 跟迁移前的 `local: true` 逐字节一致（Task 4 Step 5 的 diff 对照）。——用迁移前的 `4327af0` 编出旧二进制、同一份项目各跑一次 `up --dry-run`：`docker-compose.yaml` 逐字节一致；`local-debug.*.env` 只有头部注释一行从 `(local: true)` 变成 `(mode: debug)`；stdout 只有理由文案的差别，外加那条预期内的新语义——`starting (demo/caller needs it)` 变成 `starting (mode: debug)`（debug 现在是钉住的）。
- [x] `internal/cli/up.go` 的 `collectTargets` 与 `internal/compose/compose.go` 的工作负载跳过条件逐字一致（Task 6 Step 2）。——`c.Mode == config.ModeDebug || c.ServedBy != ""` 对 compose 里 `entry.Mode == config.ModeDebug` 分支加紧随其后的 servedBy 分支，注释里互相指着，防的是上次真机反馈的 `no_such_service` 那类漂移。
- [x] `schemas/*.json` 已重新生成并随本次改动一起提交。
- [x] `AGENTS.md`/`AGENTS.zh.md`、`08-brickkit-yaml-reference.md`（含 `exposePort` 那处文案修正）已同步，`deployment-selection-guide.md` 未改动。
- [x] 没有为旧字段保留任何兼容/双写逻辑。——旧的 `enabled`/`local` 现在是未知字段：`brickkit lint` 报 `unknown field`。

## 执行结果与遗留（供 Plan 2–4 参考）

七个任务的提交依次是 `183563f`（Task 1）→ `8b4ba8e` → `c65bc0a` → `bf0a7c1` → `7ef9eb9` → `744e557`（Task 6，含重新生成的 Schema）→ `5cb411b`（Task 7 文档）。执行中撞到、计划里没写、后续计划要知道的几件事：

- **debug 是"钉住"的，这是语义变化，不只是改名。** 旧的"local 一视同仁、cascade 从不读 local"那两条测试（`TestGraphLocalClassOnlyAppliesToRunningComponents`、`TestSyncTreatsLocalComponentsTheSame`）是按旧语义写的，按新语义重写了：graph 里 debug 组件不会被置灰，sync 不会把它归档，强依赖被关掉时报错。Plan 4 加 `mode: local` 时同样要回答"它是不是钉住的"——设计上 local 与 debug 只差"谁启动进程"，答案应该一样。
- **schemagen 的约束表多了两个字段。** `constraintCase` 新增 `baseline`（这一行需要换掉文档基准，因为 `debug` 只在 docker 下合法而 project 基准写的是 k8s）与 `validatorOnly`（校验器接受、schema 故意不列的取值——`mode: ""` 在 yaml 里与没写无法区分）。Plan 4 往枚举里加 `local` 时，`valid` 里要跟着加，`local` 同样是仅限 docker。
- **`scripts/check-guide-output.py` 的辅助步骤改写 mode。** `!disable` / `!pin` / `!local-debug` 现在写 `mode: disable` / `mode: enabled` / `mode: debug`，`!clear-enabled` 改名 `!clear-mode`。新增教程场景时照这个写。
- **lint 不检查的"生成阶段才查"的组合规则，现在的例子是 `expose: true` 组件需要 `ingressController`**（要知道最终哪些组件会跑起来）；原来举的 `local: true` + k8s 已经前移到解析阶段。`09-cli-reference.md`、AGENTS.md 的 lint 行、CHANGELOG 都按这个改过。
- **故意没动的：**
  - `deployment-selection-guide.md`（中英）——按约定等另一个项目实操之后再改，所以它现在仍写着 `local: true` 与"生成阶段拒绝"。`AGENTS.md` §10/§11 与 `llms*.txt` 里对它的一句话描述**已经**用 `mode: debug`、"解析阶段拒绝"，等那份指南更新后要核对两边对得上。
  - msgid 常量名里带 `Enabled` / `Local` 的（如 `CliUpWriteEnabledTrueForThe`、`ConfigServedByWithLocal`）——它们是不透明的键，文案已经改对，改名只是搅动。
  - `internal/compose` 注释里泛指"宿主机上跑的组件"的"local 组件"这个词——刻意留着，Plan 4 的 `mode: local` 落在同一批代码路径上，这个泛称正好覆盖两种。
- **顺手修正的过期说法**：`exposePort` 在 k8s 下"悄悄不生效、没有任何东西拦住"（`warnTargetOnlyFields` 一直在警告），改动涉及参考文档、AGENTS.md §7/§11、`llms*.txt`。

## Execution Handoff

计划已保存到 `docs/superpowers/plans/2026-09-21-mode-field-migration.md`。两种执行方式：

**1. Subagent-Driven（推荐）**——每个任务派一个新的子代理去做，任务之间做审查，迭代更快
**2. Inline Execution**——在当前会话里按任务顺序执行，批量执行、有检查点

已选择 Inline Execution，七个任务全部完成，见上面的执行结果。
