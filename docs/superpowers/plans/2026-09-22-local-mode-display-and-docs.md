# `mode: local` 展示层与文档（Plan 4d）Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 让 `graph`/`status`/`down` 对 `mode: local` 组件给出正确、不误导的展示；修一处顺带发现的"警告文案硬编码 mode: debug"真实 bug；补齐 `mode: local` 的用户向文档——一篇新教程、`brickkit.yaml` 参考文档、`check-guides` 的 `local` 层。

**Architecture:** `mode: local` 组件不生成容器（跟 `mode: debug` 一样），但它的运行态**只在跑着 `brickkit up` 的那个终端会话里**，`status`/`down`（可能在别的终端跑）没有直接手段知道它活不活着——唯一的跨终端信号是 Plan 2 已经建好的会话锁文件（`internal/sessionlock`，`config.Layout.SessionLockPath()`）。所以设计是：`status` 的表格（运行中/未在运行/未启动/本地调试）**完全不列 `mode: local` 组件**——它们不是容器，塞进这几张表任何一张都是错的；只在会话锁被持有时加**一行独立提示**。`down` 同理，在停完容器后加同一种提示——它物理上停不掉另一个终端里的裸进程，必须说清楚。`graph` 是纯声明式展示（不查运行态），给 `mode: local` 节点一个新样式/新标签，与 `mode: debug` 的"local debug"区分开。

**Tech Stack:** Go（`internal/cli`、`internal/compose`）、Markdown 文档（`docs/{en,zh}`）、Python（`scripts/check-guide-output.py`）、Bash（`scripts/check-guides.sh`）、TSV（`tests/guides/清单.tsv`）。

**Spec:** `docs/superpowers/specs/2026-09-21-mode-field-and-local-execution-design.md` §3（"status/down 跨终端可见性"）、§6.3（"graph/status 展示"）——四份计划共同的设计依据；本计划是其中"配套工作范围"里 `graph`/`status`/`down` 那部分 + 文档部分的落地。Plan 4a 执行结果里点名了这个遗留（"三处纯展示的 `ModeDebug` 判断故意没加 `ModeLocal`，留给 Plan 4d 一并改"）。

## Global Constraints

- 所有新增 CLI 输出走 `internal/msgid` + 两份 `internal/i18n/catalog_{en,zh}.go`，不写死字符串（`tests/i18nguard` 会拦）。
- 不碰 `internal/procsup`/`internal/sessionlock`/`internal/runcmd`——三个包已独立验证过，本计划只读它们已有的公开 API（`sessionlock.Inspect`、`config.Layout.SessionLockPath()`）。
- 不碰 `docs/{en,zh}/07-patterns/05-deployment-selection-guide.md`——另一个项目会先做一遍完整实操测试，根据反馈再更新（spec 文档开头的范围排除，Plan 4a/4b 沿用至今）。
- 教程里的输出块必须**逐字取自真实 CLI**（含中文提示）；命令行写法要过 `make check-cli-docs`（同一行 `brickkit <命令>` 之后的 `--参数` 会被当成这条命令的参数，容易和别的工具的参数混在一起，需要的话换行写）。
- 每个任务结束跑 `gofmt -l`、相关包的 `go test`，Task 10 之前先不要求全量 `make lint`（太慢，留到最后一次性把关）。
- 提交信息按本仓库既有习惯：说清楚改了什么、为什么，偏离计划的地方要点名。

---

## Task 1: 修一处真实 bug——本地组件的警告文案硬编码"mode: debug"

**背景：** 探索本计划时发现的真实问题，和 Plan 4b Task 6 挖出的三个 bug 是同一类：`internal/compose/local.go` 的 `localExposeWarnings`/`localLabelWarnings` 会对 `mode: debug` **与** `mode: local` 组件都触发（两者共用同一个 `p.locals` 桶），但生成的警告文案永远写死"On a mode: debug component..."——一个 `mode: local` 组件写了 `labels`/`expose` 时，警告会说谎，指错了字段该怎么改。

**Files:**
- Modify: `internal/compose/local.go`
- Modify: `internal/compose/local_test.go`
- Modify: `internal/compose/labels_test.go`
- Modify: `internal/msgid/compose.go`（`ComposeLocalFieldsIgnored`/`ComposeLocalLabelsIgnored` 两个常量所在文件，已用 `grep -rln "ComposeLocalFieldsIgnored" internal/msgid/` 确认）
- Modify: `internal/i18n/catalog_en.go`、`internal/i18n/catalog_zh.go`
- Modify: `docs/en/06-architecture/10-error-codes.md`、`docs/zh/06-architecture/10-error-codes.md`

**Interfaces:**
- Consumes：`config.Component.Mode`（已有字段，`config.ModeDebug`/`config.ModeLocal` 已有常量）。
- Produces：无新符号，`msgid.ComposeLocalFieldsIgnored`/`msgid.ComposeLocalLabelsIgnored` 的 i18n 模板参数从 1 个/0 个变成 2 个/1 个（模式在最前）。

- [ ] **Step 1: 写失败的测试——mode: local 组件的警告说的是 local，不是 debug**

在 `internal/compose/local_test.go`，紧挨着 `TestLocalComponentWithExposeIsWarned`（约第 1023 行开始那段）之后加：

```go
// mode: local 组件写了 expose 时，警告必须说 "mode: local"，不能沿用
// mode: debug 的硬编码文案——两种模式共用同一段判断逻辑（p.locals），
// 文案却从没跟着 Entry.Mode 走，是一处真实 bug（brickKit 反馈：Plan 4d
// 探索阶段用真实 mode: local 配置跑出来的）。
func TestLocalModeComponentWithExposeWarningNamesLocalNotDebug(t *testing.T) {
	b := newBuilder(t)
	b.component(simple("portal/user-frontend", "1.0.0", 80),
		config.Component{Mode: config.ModeLocal, Expose: true, ExposePort: 8888})

	result, err := b.build(compose.Options{})
	require.NoError(t, err)

	text := joinWarnings(result.Warnings)
	assert.Contains(t, text, "mode: local", "警告要说清楚是哪种模式")
	assert.NotContains(t, text, "mode: debug", "不能把 local 组件的警告说成 debug")
}

// 反过来，mode: debug 组件的警告仍然要说 "mode: debug"，不能被这次的参数化改坏。
func TestLocalDebugComponentWithExposeWarningStillNamesDebug(t *testing.T) {
	b := newBuilder(t)
	b.component(simple("portal/user-frontend", "1.0.0", 80),
		config.Component{Mode: config.ModeDebug, Expose: true, ExposePort: 8888})

	result, err := b.build(compose.Options{})
	require.NoError(t, err)

	text := joinWarnings(result.Warnings)
	assert.Contains(t, text, "mode: debug")
}
```

在 `internal/compose/labels_test.go`，紧挨着 `TestLocalComponentLabelsWarn`（第 121-138 行，`package compose_test`，与 `local_test.go` 同一个测试包，`newBuilder`/`simple`/`config` 都已经在这个文件的既有 import 里）之后加：

```go
// 同一处 bug，labels 那条警告版本——mode: local 组件写了 labels 时，
// 警告不该说成 mode: debug。
func TestLocalModeComponentLabelsWarningNamesLocalNotDebug(t *testing.T) {
	result := newBuilder(t).
		component(simple("erp/sales", "1.0.0", 8080), config.Component{
			Mode: config.ModeLocal,
			Labels: map[string]string{"traefik.enable": "true"},
		}).
		generate()

	var found string
	for _, w := range result.Warnings {
		if strings.Contains(w.Format(), "labels has no effect this run") {
			found = w.Format()
		}
	}
	require.NotEmpty(t, found, "%v", result.Warnings)
	assert.Contains(t, found, "mode: local")
	assert.NotContains(t, found, "mode: debug")
}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./internal/compose/ -run 'TestLocalModeComponentWithExposeWarningNamesLocalNotDebug|TestLocalDebugComponentWithExposeWarningStillNamesDebug|TestLocalModeComponentLabelsWarningNamesLocalNotDebug' -v`
Expected: `TestLocalModeComponentWithExposeWarningNamesLocalNotDebug` 与 `TestLocalModeComponentLabelsWarningNamesLocalNotDebug` FAIL（断言 `"mode: local"` 出现，实际文案还是硬编码的 `"mode: debug"`）；`TestLocalDebugComponentWithExposeWarningStillNamesDebug` PASS（现状本来就对 debug 成立）。

- [ ] **Step 3: 把模式参数化进这两条警告**

`internal/compose/local.go` 的 `localExposeWarnings`：

```go
func (p *plan) localExposeWarnings() []*clierr.Error {
	var out []*clierr.Error
	for _, l := range p.locals {
		if !l.Entry.Expose && l.Entry.ExposePort == 0 {
			continue
		}

		fields := "expose"
		if l.Entry.ExposePort > 0 {
			fields = "expose / exposePort"
		}
		w := clierr.Warn(clierr.CodeConfigInvalid,
			i18n.T(msgid.ComposeLocalFieldsIgnored, l.Entry.Mode, fields)).
			WithDetail(i18n.T(msgid.LabelComponent), refText(l.Ref)).
			WithDetail(i18n.T(msgid.LabelReason), i18n.T(msgid.ComposeLocalNoPortToMapDetail))
		if l.Entry.ExposePort > 0 {
			w = w.WithDetail(i18n.T(msgid.ComposeLabelExposePortWritten),
				i18n.T(msgid.ComposeExposePortWrittenDetail, l.Entry.ExposePort))
		}
		out = append(out, w.
			WithDetail(i18n.T(msgid.ComposeLabelActualAddress), i18n.T(msgid.ComposeActualAddressDetail, l.Port)).
			WithHint(
				i18n.T(msgid.ComposeHintListenOnPort),
				i18n.T(msgid.ComposeHintDropLocalForPorts),
			))
	}
	return out
}
```

（唯一的改动是 `i18n.T(msgid.ComposeLocalFieldsIgnored, fields)` → `i18n.T(msgid.ComposeLocalFieldsIgnored, l.Entry.Mode, fields)`，其余原样。）

`localLabelWarnings` 同理，唯一改动是 `i18n.T(msgid.ComposeLocalLabelsIgnored)` → `i18n.T(msgid.ComposeLocalLabelsIgnored, l.Entry.Mode)`。

- [ ] **Step 4: 更新 i18n 模板**

`internal/i18n/catalog_en.go` 第 393、401 行，现在的原文分别是 `"On a mode: debug component, %[1]s has no effect this run"` 与 `"On a mode: debug component, labels has no effect this run"`，改成：

```go
	msgid.ComposeLocalFieldsIgnored:                                        "On a mode: %[1]s component, %[2]s has no effect this run",
```

```go
	msgid.ComposeLocalLabelsIgnored:                                        "On a mode: %[1]s component, labels has no effect this run",
```

`internal/i18n/catalog_zh.go` 第 387、395 行，现在的原文分别是：

```go
	msgid.ComposeLocalFieldsIgnored:                                  "mode: debug 的组件上，%[1]s 本次不生效",
	msgid.ComposeLocalLabelsIgnored:                                  "mode: debug 的组件上，labels 本次不生效",
```

改成：

```go
	msgid.ComposeLocalFieldsIgnored:                                  "mode: %[1]s 的组件上，%[2]s 本次不生效",
	msgid.ComposeLocalLabelsIgnored:                                  "mode: %[1]s 的组件上，labels 本次不生效",
```

- [ ] **Step 5: 跑测试确认通过**

Run: `go test ./internal/compose/ -run 'TestLocalModeComponentWithExposeWarningNamesLocalNotDebug|TestLocalDebugComponentWithExposeWarningStillNamesDebug|TestLocalModeComponentLabelsWarningNamesLocalNotDebug' -v`
Expected: 三条全部 PASS。

Run: `go test ./internal/compose/... ./internal/i18n/...`
Expected: 全绿——尤其确认 `TestLocalComponentWithExposeIsWarned`/`TestLocalComponentWithExposeOnlyIsWarned`（第 1023 行附近那两条老测试）与 `TestLocalComponentLabelsWarn`（`labels_test.go` 第 121 行）没有因为参数顺序改变而断言错位（它们断言的是子串包含，不是完整字符串，理论上不受影响，但要跑一遍确认）。

- [ ] **Step 6: 更新 error-codes.md 的一行**

`docs/en/06-architecture/10-error-codes.md` 第 442 行左右，把：

```
| `On a mode: debug component, labels has no effect this run` | `CONFIG_INVALID` | A `mode: debug` component has no container to label. Remove `mode: debug` to get platform-managed labels back |
```

改成：

```
| `On a mode: <mode> component, labels has no effect this run` | `CONFIG_INVALID` | A `mode: debug` or `mode: local` component has no container to label. Remove `mode` (or switch it back to a container-generating value) to get platform-managed labels back |
```

（`<mode>` 是这份文档已有的占位符写法，`tests/docfields/errorcodes_test.go` 的 `docPlaceholder` 正则认得这个形状——执行时确认 `make lint` 里的 `check-doc-fields` 这一步跑过、不报"标题在源码里找不到"。）

`docs/zh/06-architecture/10-error-codes.md` 第 442 行，现在的原文是：

```
| `mode: debug 的组件上，labels 本次不生效` | `CONFIG_INVALID` | `mode: debug` 的组件没有容器可以挂标签。想让平台管标签，就去掉 `mode: debug` |
```

改成：

```
| `mode: <mode> 的组件上，labels 本次不生效` | `CONFIG_INVALID` | `mode: debug` 或 `mode: local` 的组件没有容器可以挂标签。去掉 `mode`（或换成会生成容器的取值）即可恢复由平台管理标签 |
```

- [ ] **Step 7: 提交**

```bash
git add internal/compose/local.go internal/compose/local_test.go internal/compose/labels_test.go internal/msgid/compose.go internal/i18n/catalog_en.go internal/i18n/catalog_zh.go docs/en/06-architecture/10-error-codes.md docs/zh/06-architecture/10-error-codes.md
git commit -m "$(cat <<'EOF'
修复：mode: local 组件的 expose/labels 警告不再硬编码"mode: debug"

localExposeWarnings/localLabelWarnings 对 mode: debug 与 mode: local 都会
触发（两者共用 p.locals），但生成的警告文案一直写死"On a mode: debug
component..."——mode: local 组件写了 expose/labels 时，警告会说谎，指错
字段该怎么改。两条 i18n 模板加一个 %[1]s 参数，从 l.Entry.Mode 取值。

探索 Plan 4d（展示层修复）时用真实 mode: local 配置跑出来的真实 bug，跟
Plan 4b Task 6 挖出的三个 bug 是同一类："两个各自合理的假设撞在一起，只有
真实运行时才现形"。

Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>
EOF
)"
```

---

## Task 2: `lifecycle.go`——`containerRefs` 排除 `mode: local`，`localRefs` 改名 `debugRefs`

**背景：** `containerRefs()` 目前只排除 `mode: debug`，没排除 `mode: local`——`status` 会把一个 `mode: local` 组件当成"该有容器"去引擎里查，查不到就归进"未在运行"（`v.failed`），而它压根不是失败，只是这次不是容器。`localRefs()` 这个名字现在只对 `mode: debug` 成立（本函数不动它的行为，只改名字，避免下一个任务里"local"这个词同时指"mode: debug 的展示"和"mode: local 的展示"两件事）。

**Files:**
- Modify: `internal/cli/lifecycle.go`
- Modify: `internal/cli/status.go`（改调用点 `p.localRefs()` → `p.debugRefs()`，行为不变，Task 3 才动 `status.go` 别的地方）
- Modify: `internal/cli/status_test.go`

**Interfaces:**
- Consumes：`config.ModeLocal`（已有常量）。
- Produces：`(p *project) debugRefs() []resolver.Ref`（原 `localRefs` 改名，签名不变）；`containerRefs()` 排除范围扩大，签名不变。

- [ ] **Step 1: 写失败的测试**

在 `internal/cli/status_test.go`，紧挨着 `TestStatusDoesNotReportLocalComponentAsDown`（约第 154 行）之后加：

```go
// containerRefs 不该把 mode: local 组件算进"要生成容器的组件"——跟
// mode: debug 一样，它没有容器，塞进这份列表只会让 status 把它错当成
// "该有容器却没查到"报出来。不直接调用私有方法——这个包里没有任何既有测试
// 直接构造 *project 调用 loadProject，一律走标准的 runIn/runWithEngine 命令
// 管线断言渲染出的文本，这条测试延续同一个惯例。
func TestStatusDoesNotReportModeLocalComponentAsNotRunning(t *testing.T) {
	comps := []comp{{ID: "demo/hello", Version: "1.0.0"}}
	f := addedProject(t, comps, "demo/hello@1.0.0")
	f.writeConfig(t, `components:
  - id: demo/hello
    version: 1.0.0
    mode: local
`)
	eng := newFakeEngine()

	r := statusOf(t, eng, f.Dir)

	require.Equal(t, clierr.ExitOK, r.code, r.stderr)
	assert.NotContains(t, r.stdout, "not created", "mode: local 组件不该被当成缺容器报出来")
}
```

（这条测试放进 `internal/cli/status_test.go`，不是新建 `lifecycle_test.go`——`containerRefs`/`debugRefs` 是私有方法，没有独立于 `status`/`down`/`graph` 的可观察行为，直接测私有方法既不匹配这个包的既有测试风格，也测不出真正有意义的东西；它的行为完全由 Task 3 的黑盒测试覆盖。`debugRefs`（原 `localRefs`）的改名不改变行为，靠现有的 `TestStatusShowsLocalComponents`/`TestStatusDoesNotReportLocalComponentAsDown`（`status_test.go` 第 140/154 行，`mode: debug` 场景）在 Step 5 原样跑一遍确认没有被改名带坏，不需要新写。`statusOf`/`newFakeEngine`/`clierr` 都是 `status_test.go` 已有的 import，不需要新建测试文件也不需要新增 import。）

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./internal/cli/ -run 'TestStatusDoesNotReportModeLocalComponentAsNotRunning' -v`
Expected: FAIL（`containerRefs()` 目前会把 `mode: local` 的 `demo/hello` 算进去，`status` 在引擎里查不到它，把它归进"未在运行"，输出里会出现 `"not created"`）。

- [ ] **Step 3: 改 `lifecycle.go`**

```go
// containerRefs 返回本次**由本项目**生成容器的组件，按启动顺序。
//
// `mode: debug`/`mode: local` 都要排除掉：debug 在依赖图里、也"在跑"，但跑在
// 开发者的 IDE 里；local 也在依赖图里、也"在跑"，但跑在 brickkit 自己前台监管
// 的裸进程里——本项目对这两者都不生成任何容器（003 §4.4、005 §3）。`status`
// 把它们各自单列一节汇报（debug 走"本地调试"表；local 走会话锁提示，见
// Task 3），不混在"未在运行"里——那会让人以为它们出问题了。
func (p *project) containerRefs() []resolver.Ref {
	var out []resolver.Ref
	for _, ref := range p.componentRefs() {
		mode := p.entry(ref).Mode
		if mode != config.ModeDebug && mode != config.ModeLocal {
			out = append(out, ref)
		}
	}
	return out
}

// debugRefs 返回 mode: debug 且本次会启动的组件（原名 localRefs——改名是因为
// "local"这个词现在同时可能指 mode: debug 的展示与 mode: local 的展示，两者
// 走的是完全不同的表：debugRefs 只喂给"本地调试"表，mode: local 组件永远不会
// 出现在这个函数的返回值里，见 status.go 的 renderLocalDebug）。
func (p *project) debugRefs() []resolver.Ref {
	var out []resolver.Ref
	for _, ref := range p.componentRefs() {
		if p.entry(ref).Mode == config.ModeDebug {
			out = append(out, ref)
		}
	}
	return out
}
```

- [ ] **Step 4: 改 `status.go` 的调用点**

`internal/cli/status.go` 第 152 行 `v.local = p.localRefs()` 改成 `v.local = p.debugRefs()`。（`componentView.local` 字段名先不动——Task 3 会重新审视它的语义。）

- [ ] **Step 5: 跑测试确认通过**

Run: `go test ./internal/cli/ -run 'TestStatusDoesNotReportModeLocalComponentAsNotRunning' -v`
Expected: PASS。

Run: `go test ./internal/cli/ -run 'TestStatusShowsLocalComponents|TestStatusDoesNotReportLocalComponentAsDown' -v`
Expected: 两条既有的 `mode: debug` 测试仍然 PASS——确认 `localRefs`→`debugRefs` 改名没有连带改坏行为。

Run: `go test ./internal/cli/`
Expected: 全绿——`down_test.go`/`graph_test.go` 里没有任何地方直接调用过 `localRefs`（它是 `project` 的私有方法，只在 `status.go` 内部用），改名不应该牵动其他文件；但要跑一遍完整包确认。

- [ ] **Step 6: 提交**

```bash
git add internal/cli/lifecycle.go internal/cli/status.go internal/cli/status_test.go
git commit -m "$(cat <<'EOF'
修复：containerRefs 排除 mode: local，localRefs 改名 debugRefs

containerRefs 只排除了 mode: debug，没排除 mode: local——status 会把一个
mode: local 组件当成"该有容器"去引擎里查，查不到就归进"未在运行"，而它
压根不是失败，只是这次不是容器（Plan 4a 执行结果点名的遗留："三处纯展示
的 ModeDebug 判断故意没加 ModeLocal"）。localRefs 改名 debugRefs：这个
函数只对 mode: debug 成立，"local"这个名字现在会跟 mode: local 的展示
（下一个提交）混在一起，改名先把两者从命名上分开。

Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>
EOF
)"
```

---

## Task 3: `status.go`——`mode: local` 不进容器表，会话锁存在时加一行提示

**背景：** spec §6.3："`status` 表格只展示 docker/k8s 真实运行态；锁文件存在时加一行提示，只针对 `local`。" 现在 `mode: local` 组件（Task 2 之后）已经不会被塞进"未在运行"了，但也完全没有任何展示——一个声明了 `mode: local` 的组件，`status` 现在对它完全沉默，使用者会以为它"消失了"。这个任务补上"锁文件存在时加一行提示"这半句。

**Files:**
- Modify: `internal/cli/status.go`
- Modify: `internal/cli/status_test.go`
- Modify: `internal/msgid/cli_status.go`
- Modify: `internal/i18n/catalog_en.go`、`internal/i18n/catalog_zh.go`

**Interfaces:**
- Consumes：`sessionlock.Inspect(path string) (info sessionlock.Info, held bool, err error)`（已有）、`config.Layout.SessionLockPath() string`（已有，Plan 4b Task 1）、`config.Component.Mode`/`config.ModeLocal`（已有）。
- Produces：`renderLocalModeSessionHint(opts *Options, layout config.Layout, cfg *config.Config)`（新函数，`runStatus` 调用）。

- [ ] **Step 1: 写失败的测试**

在 `internal/cli/status_test.go`，紧挨着 Task 2 新加的 `TestStatusDoesNotReportModeLocalComponentAsNotRunning` 之后加：

```go
// ============================================================
// mode: local 的会话锁提示
// ============================================================

// mode: local 组件没有容器，不该出现在运行中/未在运行任何一张表里——
// 塞进"未在运行"会让使用者误以为它出了问题，而它可能压根没打算这次跑起来。
func TestStatusDoesNotListModeLocalComponentInAnyTable(t *testing.T) {
	comps := []comp{{ID: "demo/hello", Version: "1.0.0"}}
	f := addedProject(t, comps, "demo/hello@1.0.0")
	f.writeConfig(t, `components:
  - id: demo/hello
    version: 1.0.0
    mode: local
`)
	eng := newFakeEngine()

	r := statusOf(t, eng, f.Dir)

	require.Equal(t, clierr.ExitOK, r.code, r.stderr)
	assert.NotContains(t, r.stdout, "demo/hello")
}

// 会话锁被持有时（模拟"另一个终端正在跑 brickkit up"），status 要打一行
// 指向信息，点名 PID——这是使用者唯一能从这个终端知道"那边有 local 会话
// 在跑"的办法。
func TestStatusShowsHintWhenLocalSessionIsRunning(t *testing.T) {
	comps := []comp{{ID: "demo/hello", Version: "1.0.0"}}
	f := addedProject(t, comps, "demo/hello@1.0.0")
	f.writeConfig(t, `components:
  - id: demo/hello
    version: 1.0.0
    mode: local
`)
	layout := config.NewLayout(f.Dir, "")
	held, err := sessionlock.Acquire(layout.SessionLockPath())
	require.NoError(t, err)
	defer func() { _ = held.Release() }()
	eng := newFakeEngine()

	r := statusOf(t, eng, f.Dir)

	require.Equal(t, clierr.ExitOK, r.code, r.stderr)
	assert.Contains(t, r.stdout, strconv.Itoa(os.Getpid()), "要点名持有者的 PID")
}

// 没有会话在跑时，不该冒出这条提示——沉默才是"没有事发生"的正确信号。
func TestStatusShowsNoHintWhenNoLocalSessionIsRunning(t *testing.T) {
	comps := []comp{{ID: "demo/hello", Version: "1.0.0"}}
	f := addedProject(t, comps, "demo/hello@1.0.0")
	f.writeConfig(t, `components:
  - id: demo/hello
    version: 1.0.0
    mode: local
`)
	eng := newFakeEngine()

	r := statusOf(t, eng, f.Dir)

	require.Equal(t, clierr.ExitOK, r.code, r.stderr)
	assert.NotContains(t, r.stdout, "session")
}

// 项目里压根没有 mode: local 组件时，不该去碰会话锁文件——没有意义，
// 也避免每次 status 都多一次无谓的文件系统访问。
func TestStatusSkipsSessionCheckWhenNoModeLocalComponent(t *testing.T) {
	f, eng := startedProject(t)

	r := statusOf(t, eng, f.Dir)

	require.Equal(t, clierr.ExitOK, r.code, r.stderr)
	assert.NotContains(t, r.stdout, "session")
}
```

在 `status_test.go` 顶部的 import 块加 `"os"`、`"strconv"`、`"github.com/brickkit/brickkit/internal/config"`、`"github.com/brickkit/brickkit/internal/sessionlock"`（先读一遍现有 import 块，只加缺的）。

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./internal/cli/ -run 'TestStatusDoesNotListModeLocalComponentInAnyTable|TestStatusShowsHintWhenLocalSessionIsRunning|TestStatusShowsNoHintWhenNoLocalSessionIsRunning|TestStatusSkipsSessionCheckWhenNoModeLocalComponent' -v`
Expected: `TestStatusDoesNotListModeLocalComponentInAnyTable` FAIL（`mode: local` 组件此刻会落进 `degradedView`/`resolvedView` 的 `default` 分支，归进"未启动"表，带着 `demo/hello` 字样）；其余三条也 FAIL（`renderLocalModeSessionHint` 还不存在，没有任何输出提到 "session"/PID）。

- [ ] **Step 3: 在 `resolvedView`/`degradedView` 里排除 `mode: local`**

`internal/cli/status.go` 的 `resolvedView`（第 134-154 行附近）：把

```go
	for _, ref := range p.containerRefs() {
```

保持不变（Task 2 已经让 `containerRefs()` 排除了 `mode: local`，这一段循环因此已经不会把它算进 running/failed）。再往下：

```go
	if p.states != nil {
		for _, c := range p.states.Components {
			if c.State != cascade.StateRunning {
				v.skipped = append(v.skipped, statusRow{ref: c.Ref, text: c.Reason})
			}
		}
	}
```

这一段要加判断——`p.states.Components` 遍历的是**全部**声明过的组件（不管 mode），一个 `mode: local` 且这次会启动的组件，`c.State == cascade.StateRunning`，不会进 `v.skipped`；但一个 `mode: local` 且**这次不会启动**（比如被上层关掉）的组件，现在会被塞进 `v.skipped`——这是对的，"这次不启动"这件事跟"是不是容器"无关，`mode: local` 组件被跳过时依然该出现在"未启动"表里，跟 `mode: debug`/普通容器组件一视同仁。**这一段不用改**，原样保留。

`degradedView`（第 168-189 行附近）：

```go
	case c.Mode == config.ModeDebug:
		// mode: debug 组件本来就不会出现在引擎里，"查不到"是它的正常状态
		v.local = append(v.local, ref)
```

改成：

```go
	case c.Mode == config.ModeDebug:
		// mode: debug 组件本来就不会出现在引擎里，"查不到"是它的正常状态
		v.local = append(v.local, ref)
	case c.Mode == config.ModeLocal:
		// mode: local 组件同样不会出现在引擎里，但它不走"本地调试"表（那是
		// mode: debug 专属的话术），也不走 skipped（降级路径判不出它这次
		// 该不该跑）——干脆不进任何一张表，跟 mode: local 唯一的展示手段
		// （会话锁提示）保持一致，见 renderLocalModeSessionHint
```

（这个 `case` 分支没有任何 `v.xxx = append(...)`，是故意的——降级路径下 `mode: local` 组件直接被跳过，不进任何一张表。）

- [ ] **Step 4: 写 `renderLocalModeSessionHint`**

在 `status.go`，紧挨着 `renderLocalDebug` 函数之后加：

```go
// renderLocalModeSessionHint 在项目有 mode: local 组件、且会话锁被持有时，
// 打一行指向信息——这是使用者从**别的终端**唯一能知道"那边有个 local 会话
// 在跑"的办法（spec §3：status/down 跨终端可见性，复用同一把锁文件）。
//
// 项目里没有 mode: local 组件时，压根不去碰锁文件：没有意义，也避免每次
// status 都多一次无谓的文件系统访问。
func renderLocalModeSessionHint(opts *Options, layout config.Layout, cfg *config.Config) {
	if !anyModeLocal(cfg.Components) {
		return
	}
	info, held, err := sessionlock.Inspect(layout.SessionLockPath())
	if err != nil || !held {
		// 读不出来（罕见的 I/O 错误）跟"没有会话"一视同仁：这条提示本来就是
		// 锦上添花，不该因为一次读锁文件失败就让 status 的其余输出也报错
		return
	}
	opts.Printf("\n%s\n", i18n.T(msgid.CliStatusLocalSessionRunning, info.PID))
}
```

（`anyModeLocal` 是 Plan 4b Task 1 在 `internal/cli/up_local.go` 里已经定义好的 `func anyModeLocal(components []config.Component) bool`，同包内直接复用，不需要重新定义。）

在 `runStatus` 函数里（`renderResourceStatus` 那一行之后，函数最后 `return nil` 之前）加一行调用：

```go
	renderResourceStatus(ctx, opts, p)
	renderLocalModeSessionHint(opts, p.layout, p.cfg)
	return nil
```

- [ ] **Step 5: 新增 msgid + i18n**

`internal/msgid/cli_status.go` 有两个 const 块——第一个是主常量块（以 `CliStatusLong` 结尾），第二个是"手工处理的条目"（只有 `CliStatusReasonUnknown` 一项）。新常量是普通的自动生成风格，加进**第一个**块的末尾（`CliStatusLong` 那一行之后、块的闭合 `)` 之前），不要加进第二个块：

```go
	CliStatusLocalSessionRunning = "cli.status.local_session_running"
```

`internal/i18n/catalog_en.go`：

```go
	msgid.CliStatusLocalSessionRunning: "💡 This project has a local session running (PID %[1]d) — go to that terminal, or Ctrl+C it there",
```

`internal/i18n/catalog_zh.go`：

```go
	msgid.CliStatusLocalSessionRunning: "💡 这个项目有一个本地会话在跑（PID %[1]d）——去那个终端看，或者在那边 Ctrl+C",
```

（插入位置：两份 catalog 文件都按 `internal/msgid/cli_status.go` 里常量声明的相对顺序排列——`CliStatusLong`（`catalog_en.go` 第 1193 行）与 `CliStatusReasonUnknown`（第 1196 行）之间插入新条目，`catalog_zh.go` 同理，先 `grep -n "CliStatusLong\|CliStatusReasonUnknown" internal/i18n/catalog_zh.go` 确认对应行号。）

- [ ] **Step 6: 跑测试确认通过**

Run: `go test ./internal/cli/ -run 'TestStatusDoesNotListModeLocalComponentInAnyTable|TestStatusShowsHintWhenLocalSessionIsRunning|TestStatusShowsNoHintWhenNoLocalSessionIsRunning|TestStatusSkipsSessionCheckWhenNoModeLocalComponent' -v`
Expected: 四条全部 PASS。

Run: `go test ./internal/cli/`
Expected: 全绿。

Run: `go test -race ./internal/cli/ -run 'TestStatus' -count=3`
Expected: 干净（`sessionlock.Acquire`/`Inspect` 涉及文件锁，值得跑几次确认没有偶发问题）。

- [ ] **Step 7: 提交**

```bash
git add internal/cli/status.go internal/cli/status_test.go internal/msgid/cli_status.go internal/i18n/catalog_en.go internal/i18n/catalog_zh.go
git commit -m "$(cat <<'EOF'
新增：status 对 mode: local 的正确展示——不进容器表，会话锁存在时加提示

mode: local 组件（Task 2 让 containerRefs 排除它之后）现在不会再被误报成
"未在运行"，但也完全没有任何展示——status 对它彻底沉默，使用者会以为它
"消失了"。按 spec §3/§6.3："status 表格只展示 docker/k8s 真实运行态；
锁文件存在时加一行提示，只针对 local。" degradedView 里新增一个空 case
（mode: local 不进任何一张表）；renderLocalModeSessionHint 在会话锁被
持有时打一行指向信息，点名 PID；项目里没有 mode: local 组件时不碰锁文件。

Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>
EOF
)"
```

---

## Task 4: `down.go`——同一种会话锁提示

**背景：** `down` 停的是容器，物理上停不掉另一个终端里的裸进程——如果不说清楚，使用者会以为 `down` 之后"一切都停了"，而 `mode: local` 进程还在别的终端跑着。跟 `status` 复用同一把锁、同一种提示。

**Files:**
- Modify: `internal/cli/down.go`
- Modify: `internal/cli/down_test.go`

**Interfaces:**
- Consumes：Task 3 的 `renderLocalModeSessionHint`（直接复用，不重新定义）、`loadConfig`（已有，`down.go` 已经在用）。
- Produces：无新符号。

- [ ] **Step 1: 写失败的测试**

在 `internal/cli/down_test.go`，紧挨着 `TestDownTellsThatDataIsKept`（约第 61 行）之后加：

```go
// down 停的是容器，停不掉另一个终端里的 mode: local 裸进程——不说清楚，
// 使用者会以为 down 之后"一切都停了"。
func TestDownShowsHintWhenLocalSessionIsRunning(t *testing.T) {
	comps := []comp{{ID: "demo/hello", Version: "1.0.0"}}
	f := addedProject(t, comps, "demo/hello@1.0.0")
	f.writeConfig(t, `components:
  - id: demo/hello
    version: 1.0.0
    mode: local
`)
	layout := config.NewLayout(f.Dir, "")
	held, err := sessionlock.Acquire(layout.SessionLockPath())
	require.NoError(t, err)
	defer func() { _ = held.Release() }()
	eng := newFakeEngine()

	r := runWithEngine(t, eng, f.Dir, "down")

	require.Equal(t, clierr.ExitOK, r.code, r.stderr)
	assert.Contains(t, r.stdout, strconv.Itoa(os.Getpid()))
}

// 没有会话在跑时，down 照常，不冒出这条提示。
func TestDownShowsNoHintWhenNoLocalSessionIsRunning(t *testing.T) {
	f, eng := startedProject(t)

	r := runWithEngine(t, eng, f.Dir, "down")

	require.Equal(t, clierr.ExitOK, r.code, r.stderr)
	assert.NotContains(t, r.stdout, "session")
}
```

`down_test.go` 现有 import 块已经有 `"os"`；再加 `"strconv"`、`"github.com/brickkit/brickkit/internal/config"`、`"github.com/brickkit/brickkit/internal/sessionlock"` 这三个缺的。

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./internal/cli/ -run 'TestDownShowsHintWhenLocalSessionIsRunning|TestDownShowsNoHintWhenNoLocalSessionIsRunning' -v`
Expected: `TestDownShowsHintWhenLocalSessionIsRunning` FAIL（`down` 完全不打印 PID）；`TestDownShowsNoHintWhenNoLocalSessionIsRunning` PASS（现状本来就不打印，这条测试先跑绿，是为了在 Step 3 之后仍然保持绿——确认改动没有引入误报）。

- [ ] **Step 3: 在 `runDown` 里加一行调用**

`internal/cli/down.go` 的 `runDown`，在 `renderDownResult(...)` 之后、`logging.Info(...)` 之前加：

```go
	renderDownResult(opts, p.cfg.Deploy.Target == config.TargetK8s, running, probed)
	renderLocalModeSessionHint(opts, p.layout, p.cfg)
	logging.Info(i18n.T(msgid.LogProjectStopped), "project", p.cfg.Project, "stopped", running)
```

（`p` 在这里是 `loadConfig(opts)` 返回的 `*project`，已经有 `layout`/`cfg` 两个字段，`renderLocalModeSessionHint` 是 Task 3 在 `status.go` 里定义的包内函数，同包直接调用，不需要 import。）

- [ ] **Step 4: 跑测试确认通过**

Run: `go test ./internal/cli/ -run 'TestDownShowsHintWhenLocalSessionIsRunning|TestDownShowsNoHintWhenNoLocalSessionIsRunning' -v`
Expected: 两条都 PASS。

Run: `go test ./internal/cli/`
Expected: 全绿。

- [ ] **Step 5: 提交**

```bash
git add internal/cli/down.go internal/cli/down_test.go
git commit -m "$(cat <<'EOF'
新增：down 对 mode: local 会话的同一种提示

down 停的是容器，物理上停不掉另一个终端里的 mode: local 裸进程——不说
清楚，使用者会以为 down 之后"一切都停了"。直接复用 Task 3 在 status.go
里定义的 renderLocalModeSessionHint，同一把锁、同一句话（spec §3："
status/down 跨终端可见性：复用同一把锁文件"）。

Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>
EOF
)"
```

---

## Task 5: `graph.go`——给 `mode: local` 节点一个新样式/新标签

**背景：** spec §6.3："`graph` 给 local/debug 节点都加标签（纯展示），复用 `internal/cli/graph.go` 已有的 `classDef`/`class` 机制新增一个样式。" `graph` 不查运行态（跟 `status`/`down` 不同），是纯声明式展示——`mode: local` 节点该有自己的标签和颜色，不能跟 `mode: debug` 的"local debug"标签混在一起（那句话意味着"你自己在 IDE 里启动"，对 `mode: local` 是假的，这正是 Plan 4b Task 6 已经在别处修过的同一种错误）。

**Files:**
- Modify: `internal/cli/graph.go`
- Modify: `internal/cli/graph_test.go`
- Modify: `internal/msgid/cli_graph.go`
- Modify: `internal/i18n/catalog_en.go`、`internal/i18n/catalog_zh.go`

**Interfaces:**
- Consumes：`config.ModeLocal`（已有）。
- Produces：新增常量 `classManaged = "managed"`；`mermaidClassDefs` 多一项；`declare` 闭包多一个分支。

- [ ] **Step 1: 写失败的测试**

在 `internal/cli/graph_test.go`，紧挨着 `TestGraphLocalDebugWithoutLocalPortShowsNoPort`（约第 168 行）之后加：

```go
// mode: local 节点要有自己的标签和样式，不能跟 mode: debug 的
// "local debug"标签混在一起——那句话意味着"你自己在 IDE 里启动"，
// 对 mode: local 是假的（brickkit 自己拉起它）。
func TestGraphMarksModeLocalComponent(t *testing.T) {
	f := graphProject(t, `components:
  - id: demo/hello
    version: 1.0.0
    mode: local
resources: []
`, comp{ID: "demo/hello", Version: "1.0.0"})

	r := runIn(t, f.Dir, "graph")
	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)
	requirePureMermaid(t, r.stdout)
	assert.NotContains(t, r.stdout, "local debug", "不能沿用 mode: debug 的标签措辞")
	assert.Contains(t, r.stdout, "    classDef managed ", "要有一个新样式，不是复用 classLocal")
	assert.Contains(t, r.stdout, "    class demo_hello_1_0_0 managed\n")
}

// mode: local 跟 mode: debug 一样是"钉住"的——上层全被关掉，跟着上层走的
// 组件本该跟着被级联跳过，但 mode: local 不跟着上层走，永远不会被置灰。
// 只搭一条两跳的依赖链（demo/web 关掉 → demo/hello 是它唯一的依赖）：
// 没有 mode: local 这个钉子的话，demo/hello 上面没人需要它，会被跟着关掉，
// 跟 classLineOf(r.stdout, "disabled") 里现在断言它不在的结果矛盾——这条
// 测试的意义正在于验证"钉住"确实推翻了这条默认的级联规则。
func TestGraphModeLocalComponentIsPinnedAndNeverGreyedOut(t *testing.T) {
	f := graphProject(t, `components:
  - id: demo/web
    version: 1.0.0
    mode: disable
  - id: demo/hello
    version: 1.0.0
    mode: local
resources: []
`,
		comp{ID: "demo/hello", Version: "1.0.0"},
		comp{ID: "demo/web", Version: "1.0.0", Requires: []string{"demo/hello@1.0.0"}},
	)

	r := runIn(t, f.Dir, "graph")
	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)

	disabled := classLineOf(r.stdout, "disabled")
	require.NotEmpty(t, disabled, r.stdout)
	assert.Contains(t, disabled, "demo_web_1_0_0", "web 自己被关")
	assert.NotContains(t, disabled, "demo_hello_1_0_0", "mode: local 被钉住，不会被级联跳过")
	assert.Contains(t, r.stdout, "    class demo_hello_1_0_0 managed\n")
}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./internal/cli/ -run 'TestGraphMarksModeLocalComponent|TestGraphModeLocalComponentIsPinnedAndNeverGreyedOut' -v`
Expected: 两条都 FAIL（`mode: local` 节点目前没有任何特殊标签/样式，`declare` 闭包里没有对应分支）。

- [ ] **Step 3: 加新样式常量**

`internal/cli/graph.go` 第 96-99 行附近：

```go
	classDisabled = "disabled"
	classLocal    = "local"
	classManaged  = "managed"
	classMissing  = "missing"
)
```

（`classLocal` 这个名字历史上就是给 `mode: debug` 用的——`local: true` 是 `mode: debug` 改名前的旧字段名，这个常量名字从那时候留到现在，没有跟着改名。这次不重命名它，只新增 `classManaged` 给 `mode: local`：改 `classLocal` 的名字会牵动它在 Mermaid 输出里的实际取值（"local"这个字符串本身，不只是 Go 里的标识符），对已经生成过 `.mmd` 文件、拿它们做 git diff 的人是一次没有功能收益的纯churn，不做。）

`mermaidClassDefs`：

```go
var mermaidClassDefs = []struct{ name, style string }{
	{classDisabled, "fill:#eee,stroke:#999,color:#999"},
	{classLocal, "fill:#e6f2ff,stroke:#3673a8"},
	{classManaged, "fill:#e6ffe6,stroke:#2e8b57"},
	{classMissing, "fill:#fff4e5,stroke:#c77700,stroke-dasharray:4 3"},
}
```

- [ ] **Step 4: 在 `declare` 闭包里加 `mode: local` 分支**

`internal/cli/graph.go` 的 `declare` 闭包（第 152-174 行附近），在 `if entry := entries[ref]; entry.Mode == config.ModeDebug { ... }` 这个 `if` 块之后（**不是**内部，是同一层级的新 `if`），加：

```go
		if entry := entries[ref]; entry.Mode == config.ModeLocal {
			label += i18n.T(msgid.CliGraphBrManagedLocally)
			if entry.LocalPort > 0 {
				// mode: local 也接受 localPort 作为"固定端口"的手动覆盖
				// （005 §5：默认自动分配，只有想固定端口时才手动指定）
				label += fmt.Sprintf(" :%d", entry.LocalPort)
			}
			if running {
				tag(classManaged, ref)
			}
		}
```

（这一段要接在原有 `mode: debug` 的 `if` 块**之后**、`if !running { tag(classDisabled, ref) }` **之前**——两个 `if` 互斥（一个组件的 `Mode` 不可能同时是 `debug` 又是 `local`），顺序不影响正确性，但保持"先处理具体标签，再处理置灰"这个既有结构。执行时打开 `graph.go` 实际看一眼当前第 152-174 行的确切文本再落笔，不要凭这里的描述直接假设行号没有偏移——Task 2-4 没有改过 `graph.go`，但保险起见还是要读一遍。）

- [ ] **Step 5: 新增 msgid + i18n**

`internal/msgid/cli_graph.go`：

```go
	CliGraphBrManagedLocally                 = "cli.graph.br_managed_locally"
```

`internal/i18n/catalog_en.go`：

```go
	msgid.CliGraphBrManagedLocally: "<br/>managed locally",
```

`internal/i18n/catalog_zh.go`：

```go
	msgid.CliGraphBrManagedLocally: "<br/>托管本地",
```

（插入位置：`internal/msgid/cli_graph.go` 第 11-12 行 `CliGraphBrLocalDebug`/`CliGraphSubgraphMembersShell` 之间；`catalog_en.go` 第 801-802 行、`catalog_zh.go` 第 795-796 行同样两行之间——三处都紧邻插入，保持三个文件常量声明顺序一致。）

- [ ] **Step 6: 跑测试确认通过**

Run: `go test ./internal/cli/ -run 'TestGraphMarksModeLocalComponent|TestGraphModeLocalComponentIsPinnedAndNeverGreyedOut' -v`
Expected: 两条都 PASS。

Run: `go test ./internal/cli/ -run TestGraph`
Expected: 全绿——尤其确认 `TestGraphMarksLocalDebugComponent`/`TestGraphDebugComponentIsPinnedAndNeverGreyedOut`（`mode: debug` 那两条老测试）没有被新分支影响。

Run: `go test ./internal/cli/`
Expected: 全绿。

- [ ] **Step 7: 提交**

```bash
git add internal/cli/graph.go internal/cli/graph_test.go internal/msgid/cli_graph.go internal/i18n/catalog_en.go internal/i18n/catalog_zh.go
git commit -m "$(cat <<'EOF'
新增：graph 给 mode: local 节点一个独立的样式与标签

mode: local 节点此前完全没有特殊展示，会被当成普通容器组件画——图上看
不出它其实是裸进程。新增 classManaged 样式（不复用 classLocal：那个
常量名字是 mode: debug 改名前的历史遗留，它在 Mermaid 输出里的实际取值
"local"不改，避免给已生成的 .mmd 文件带来没有功能收益的 diff）与
"managed locally"标签，跟 mode: debug 的"local debug"标签区分开——后者
意味着"你自己在 IDE 里启动"，对 mode: local 是假的。

Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>
EOF
)"
```

---

## Task 6: `08-brickkit-yaml-reference.md`——补上 `mode: local` 的字段文档

**背景：** 这份参考文档的 `mode` 字段行、`localPort` 字段行、"Mutual exclusions"一节，目前只字未提 `mode: local`——只写了 `enabled`/`disable`/`debug` 三个值，而 `internal/config/validate.go` 里 `mode: local` 跟 `mode: debug` 共享几乎所有互斥规则（k8s 目标拒绝、`servedBy` 互斥、`replicas` 互斥、`localPort` 需要其中一个），文档完全没跟上。

**Files:**
- Modify: `docs/en/06-architecture/08-brickkit-yaml-reference.md`
- Modify: `docs/zh/06-architecture/08-brickkit-yaml-reference.md`

**Interfaces:**
- 纯文档任务，无代码接口。校验依据：`internal/config/validate.go` 的 `validateComponentMode`（第 227-238 行）、`validateComponentPorts`（第 240-283 行）、`validateServedBy`（第 290-339 行）、`replicas` 校验（第 590-603 行附近，函数名执行时用 `grep -n "ConfigReplicasWithLocal" internal/config/validate.go` 定位）——这次探索阶段已经逐条读过，下面的文案改动照这些行号对应的真实校验逻辑写，不是凭空写文档。

- [ ] **Step 1: 改 `mode` 字段行**

`docs/en/06-architecture/08-brickkit-yaml-reference.md` 第 83 行，把：

```
| `components[].mode` | string | no | one of `enabled` / `disable` / `debug`, or omitted — omitted follows the top (AGENTS.md §5.4), `enabled` pins it on, `disable` pins it off, `debug` pins it on *and* runs it as a bare process on your machine (see the mutual-exclusion notes below) — only `docker` targets accept `debug`, rejected at parse time under `k8s` |
```

改成：

```
| `components[].mode` | string | no | one of `enabled` / `disable` / `debug` / `local`, or omitted — omitted follows the top (AGENTS.md §5.4), `enabled` pins it on, `disable` pins it off, `debug` pins it on *and* runs it as a bare process **you** start yourself, `local` pins it on *and* runs it as a bare process **BrickKit** starts and supervises itself (see the mutual-exclusion notes below) — only `docker` targets accept `debug`/`local`, both rejected at parse time under `k8s` |
```

- [ ] **Step 2: 改 `localPort` 字段行**

第 84 行，把：

```
| `components[].localPort` | int | required *if* `mode: debug` and you want a fixed host port; otherwise omit | only legal when `mode: debug` is also set — present without it is rejected, not ignored; `1`–`65535`; must be unique across every component's `localPort` in the project |
```

改成：

```
| `components[].localPort` | int | no | only legal when `mode: debug` or `mode: local` is also set — present without either is rejected, not ignored; `1`–`65535`; must be unique across every component's `localPort` in the project. Under `mode: debug` it's pure routing information (your own process decides what port it listens on; this only tells BrickKit where to route to). Under `mode: local`, BrickKit assigns a free port automatically by default — this field pins a specific one, the one case `mode: debug` can't offer since BrickKit doesn't control that process's startup |
```

（第 84 行原本"required *if*"这个措辞其实一直不准确——`localPort` 从来不是必填，写不写完全可选，改动顺手把这处措辞也订正了。）

- [ ] **Step 3: 改"Mutual exclusions"一节**

第 97-106 行，把整节替换成：

```markdown
### Mutual exclusions among `components[].mode: debug`/`local` / `.servedBy` / `.replicas`

These describe different, incompatible ideas about where a component's process actually runs, and the validator rejects every pairwise combination:

- **`mode: debug`/`local` + `servedBy`** — rejected outright, for either mode: `debug`/`local` both mean "this component runs on your machine, outside any container"; `servedBy` means "this component's code is already compiled into another component's image." A component can't simultaneously not be containerized and be merged into someone else's container.
- **`servedBy` chains** — a component can't declare `servedBy` pointing at a target that *itself* declares `servedBy` ("a shell can't be served by another shell"), and a component can't be the target of a `servedBy` from someone else while also being `mode: debug` or `mode: local` itself — a shell has to be reachable from the container/cluster network, which a process running on a developer's own machine structurally can't provide, regardless of who started that process.
- **`servedBy` self-reference** — `components[].servedBy` can't equal that same entry's own `id@version`.
- **`replicas` + `mode: debug`/`local`** — rejected for either mode: both mean the component runs as a single process on your machine; a replica count describes multiple Pods, which has no meaning for a process that isn't a Pod at all.

`localPort` without `mode: debug` or `mode: local`, and `exposePort`/`tlsSecret` without `expose: true`, follow the same "written but inert" philosophy as `healthCheck.startPeriodSeconds` under `type: none` on the component side ([07-component-yaml-reference.md](07-component-yaml-reference.md)) — rejected at parse time rather than silently doing nothing, because a config value that's simply ignored is exactly the failure mode this whole platform tries hardest to avoid.
```

- [ ] **Step 4: 中文版同步**

`docs/zh/06-architecture/08-brickkit-yaml-reference.md` 第 83 行，现在的原文：

```
| `components[].mode` | string | 否 | `enabled`/`disable`/`debug` 三选一，或不写——不写＝跟着上层走（AGENTS.zh.md §5.4），`enabled`＝钉住一定跑，`disable`＝钉住一定不跑，`debug`＝钉住一定跑，**并且**在你自己机器上跑成裸进程（见下方互斥说明）——只有 `docker` 目标接受 `debug`，`k8s` 下在解析阶段就拒绝 |
```

改成：

```
| `components[].mode` | string | 否 | `enabled`/`disable`/`debug`/`local` 四选一，或不写——不写＝跟着上层走（AGENTS.zh.md §5.4），`enabled`＝钉住一定跑，`disable`＝钉住一定不跑，`debug`＝钉住一定跑、**并且**在你自己机器上跑成**你自己启动**的裸进程，`local`＝钉住一定跑、并且在你自己机器上跑成**brickkit 自己启动并监管**的裸进程（见下方互斥说明）——只有 `docker` 目标接受 `debug`/`local`，`k8s` 下两者都在解析阶段就拒绝 |
```

第 84 行，现在的原文：

```
| `components[].localPort` | int | 只有想要固定宿主机端口时才需要写，前提是 `mode: debug` | 只有在同时写了 `mode: debug` 时才合法——单独写会被拒绝，不是悄悄忽略；`1`–`65535`；在项目里全部组件的 `localPort` 之间必须唯一 |
```

改成：

```
| `components[].localPort` | int | 否 | 只有在同时写了 `mode: debug` 或 `mode: local` 时才合法——单独写会被拒绝，不是悄悄忽略；`1`–`65535`；在项目里全部组件的 `localPort` 之间必须唯一。在 `mode: debug` 下它纯粹是路由信息（进程听哪个端口是你自己的进程自己决定的，这里只是告诉 brickkit 该往哪路由）；在 `mode: local` 下 brickkit 默认自动分配一个空闲端口，这个字段是"固定某个端口"的手动覆盖——这是 `mode: debug` 做不到的，因为 brickkit 根本不掌控那个进程的启动 |
```

（原文"只有想要固定宿主机端口时才需要写，前提是 mode: debug"这一处"是否必填"的措辞一直不准确——顺手订正成"否"，跟英文版 Task 6 Step 2 同步。）

第 97-106 行"互斥"一节，标题与正文整段改成：

```markdown
### `components[].mode: debug`/`local` / `.servedBy` / `.replicas` 之间的互斥

这几个字段描述的是几种不同、互不相容的"这个组件的进程到底跑在哪"的设想，校验器把每一对组合都拦了下来：

- **`mode: debug`/`local` + `servedBy`**——两种 mode 都直接拒绝：`debug`/`local` 的意思都是"这个组件跑在你自己机器上，脱离任何容器"；`servedBy` 的意思是"这个组件的代码已经编进了另一个组件的镜像里"。一个组件不可能同时"没有被容器化"又"被合并进了别人的容器"。
- **`servedBy` 链式嵌套**——一个组件的 `servedBy` 不能指向一个自己也声明了 `servedBy` 的目标（"外壳不能被另一个外壳收编"），一个组件也不能一边是别的组件 `servedBy` 的目标、一边自己又是 `mode: debug` 或 `mode: local`——外壳必须能从容器/集群网络里被访问到，而一个跑在开发者自己机器上的进程在结构上做不到这一点，不管这个进程是谁启动的。
- **`servedBy` 自引用**——`components[].servedBy` 不能等于这一条自己的 `id@version`。
- **`replicas` + `mode: debug`/`local`**——两种 mode 都拒绝：都意味着这个组件在你自己机器上是单个进程；副本数描述的是多个 Pod，对一个根本不是 Pod 的进程毫无意义。

`localPort` 不带 `mode: debug` 或 `mode: local`、`exposePort`/`tlsSecret` 不带 `expose: true`，跟组件那侧 `healthCheck.startPeriodSeconds` 在 `type: none` 下的处理是同一种哲学（[07-component-yaml-reference.md](07-component-yaml-reference.md)）——在解析阶段就拒绝，不是悄悄什么都不做，因为"写了配置却被悄悄忽略"正是这整个平台最想避免的那类失败。
```

- [ ] **Step 5: 校验**

Run: `go test ./tests/docfields/...`
Expected: 全绿（这几处改动不涉及错误码标题，`principles_test.go`/`reference_test.go` 那类检查不受影响，但要跑一遍确认没有间接触发别的断言）。

手动读一遍改完的两份文档，确认：① 中英文结构一一对应（`make lint` 的 `check-docs-bilingual` 会做更严格的检查，这里先自己过一眼）；② 没有遗漏"only `docker` targets accept `debug`"这类只提了一个 mode 的旧措辞（`grep -n "mode: debug" docs/en/06-architecture/08-brickkit-yaml-reference.md` 确认剩下的每一处"只提 debug 不提 local"都是**有意的**——比如 `AGENTS.md §5.6`/`06-signing-and-trust.md` 之类跟这次改动无关的引用不用动，只改本文件自己正文里两者本该对称出现的地方）。

- [ ] **Step 6: 提交**

```bash
git add docs/en/06-architecture/08-brickkit-yaml-reference.md docs/zh/06-architecture/08-brickkit-yaml-reference.md
git commit -m "$(cat <<'EOF'
文档：08-brickkit-yaml-reference.md 补齐 mode: local

mode 字段行、localPort 字段行、"Mutual exclusions"一节此前只字未提
mode: local——而 internal/config/validate.go 里 mode: local 跟
mode: debug 共享几乎所有互斥规则（k8s 目标拒绝、servedBy 互斥、replicas
互斥、localPort 需要其中一个），文档完全没跟上。顺手订正了 localPort
"required if"这处一直不准确的措辞（它从来不是必填）。

Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>
EOF
)"
```

---

## Task 7: 教程序列改号——为新教程腾出编号

**背景：** 新教程要插在 `03-local-debugging.md`之后，题目讲 `mode: local`。按仓库既有约定（编号 = 阅读顺序，en/zh 两棵树名字完全一致），后面 `04-kubernetes.md` 到 `13-multi-project-sharing.md` 十篇要整体后移一号，变成 `05` 到 `14`。这个任务只做"腾位置"，不写新教程正文——新教程正文是 Task 8。

**Files:**
- Create: 一次性脚本（写到仓库外的 scratchpad，不提交），执行完删除
- Modify（`git mv` + 内容更新）：`docs/en/03-guide/04-kubernetes.md` → `05-kubernetes.md`（以此类推到 `13-multi-project-sharing.md` → `14-multi-project-sharing.md`），`docs/zh/03-guide/` 同名十篇
- Modify: `docs/en/03-guide/README.md`、`docs/zh/03-guide/README.md`
- Modify: `llms.txt`、`llms.zh.txt`
- Modify: `AGENTS.md`、`AGENTS.zh.md`
- Modify: `README.md`、`docs/README.md`
- Modify: `scripts/check-guide-output.py`

**Interfaces:**
- 纯文档/脚本任务。真相来源：`git grep -rn "0[4-9]-\|1[0-3]-" docs/en/03-guide/ docs/zh/03-guide/ llms.txt llms.zh.txt AGENTS.md AGENTS.zh.md README.md docs/README.md scripts/check-guide-output.py` 在 Step 1 执行时重新跑一遍，得到的才是权威清单——下面列的文件只是探索阶段找到的，执行时必须重新核实，不要假设这份清单没有遗漏。

- [ ] **Step 1: 建立"旧路径→新路径"映射，核实完整的引用清单**

在 scratchpad 里（不是仓库内）建一个 Python 脚本 `renumber_guides.py`：

```python
#!/usr/bin/env python3
"""把 docs/{en,zh}/03-guide/ 下 04-13 整体后移一号到 05-14，
同步所有引用它们的地方。一次性脚本，跑完即弃。"""
import re
import subprocess
from pathlib import Path

ROOT = Path("/home/zhijie/Desktop/github/brickKit/.claude/worktrees/mode-field-migration")

# 旧编号 -> 新编号，按数字从大到小处理（先挪 13->14 再挪 12->13...），
# 避免 12->13 的产物又被 11->12 的规则误伤。
OLD_TO_NEW = {n: n + 1 for n in range(13, 3, -1)}  # 13->14, 12->13, ..., 4->5

# 每个旧编号对应的文件名主体（不含编号前缀），en/zh 共用同一个主体。
STEMS = {
    4: "kubernetes",
    5: "upgrades-and-versions",
    6: "assemble-and-break",
    7: "consuming-artifacts",
    8: "component-source",
    9: "marketplace",
    10: "signing",
    11: "build-your-own",
    12: "network-policy",
    13: "multi-project-sharing",
}

def old_name(n: int) -> str:
    return f"{n:02d}-{STEMS[n]}.md"

def new_name(n: int) -> str:
    return f"{OLD_TO_NEW[n]:02d}-{STEMS[n]}.md"

# Step A: git mv，从大号到小号，两棵树都做
for n in sorted(OLD_TO_NEW, reverse=True):
    for lang in ("en", "zh"):
        src = ROOT / "docs" / lang / "03-guide" / old_name(n)
        dst = ROOT / "docs" / lang / "03-guide" / new_name(n)
        subprocess.run(["git", "mv", str(src), str(dst)], cwd=ROOT, check=True)

print("git mv 完成，接下来手动处理内容里的编号引用——这一步只挪文件名。")
```

Run（在 scratchpad 目录）：`python3 renumber_guides.py`

Expected：`git status` 显示 20 个 `R`（10 篇 × 2 种语言，全部识别为 rename，不是"删除+新增"——如果识别成两条独立记录，说明改动幅度太大让 Git 认不出来是同一个文件，这一步只是纯改名不该发生这种情况）。

- [ ] **Step 2: 核实完整的引用清单**

Run（在仓库根目录）：

```bash
git grep -n -E "0[4-9]-(kubernetes|upgrades-and-versions|assemble-and-break|consuming-artifacts|component-source|marketplace|signing|build-your-own|network-policy)|1[0-3]-(build-your-own|network-policy|multi-project-sharing)" -- ':!docs/archive' ':!*.jsonl'
```

（这条命令找的是"文件名里带旧编号"的所有引用——链接、`check-guide-output.py` 的 `"file":` 值、`.brickkit`/技能资源里写死的路径。`docs/archive/` 是历史归档，不属于这次改号范围，排除；`.jsonl` 是会话记录，排除。）

Run：

```bash
git grep -n -E "第 ?(4|5|6|7|8|9|10|11|12|13) ?篇|Article (4|5|6|7|8|9|10|11|12|13)|guide (4|5|6|7|8|9|10|11|12|13)|tutorial (4|5|6|7|8|9|10|11|12|13)" -- ':!docs/archive' ':!*.jsonl'
```

（这条找的是"正文里用序数词指向某一篇"的引用，不含在文件名里——比如"见第 8 篇"这种写法，不会被上一条命令的文件名匹配抓到。）

Run：

```bash
git grep -n -E "13 篇|13 articles|13-article|12-part|12 篇" -- ':!docs/archive' ':!*.jsonl'
```

（这条找"写死教程总数"的地方——`AGENTS.md §12`、两份根 README、`docs/README.md`。**执行时会发现 `docs/README.md` 里英文写的是"12-part"而中文写的是"13 篇"——这是一处独立于本次改号的、此前就存在的中英文不同步，顺手一起修成"14-part"/"14 篇"，不是这次改号引入的新问题。**）

把三条命令的完整输出存成一份清单（scratchpad 里存一份文本文件，不提交），作为 Step 3-6 要挨个改的权威列表——**不要凭本计划文档里列出的文件清单去改，以这三条命令跑出来的真实结果为准**，本计划写作时探索到的清单（`docs/{en,zh}/03-guide/README.md`、`llms.txt`、`llms.zh.txt`、`AGENTS.md` 第 522/1179/1213 行附近、`AGENTS.zh.md` 对应位置、`README.md` 第 505 行、`docs/README.md` 第 34/47/53/90 行附近、`scripts/check-guide-output.py` 里 `"file": "05-upgrades-and-versions.md"` 这类值）只是参考起点，可能有遗漏或者行号已经偏移。

- [ ] **Step 3: 改每篇文章自己的 H1 编号与"下一篇"链**

对 Step 1 挪动后的 20 个文件（`docs/{en,zh}/03-guide/05-*.md` 到 `14-*.md`），逐篇打开：
- H1 标题的编号（`# 4. Deploy to Kubernetes` → `# 5. Deploy to Kubernetes`，其余同理）。
- 文首/文末"下一篇"链接（如果有——先读一遍这十篇现在是怎么写"下一篇"的，`grep -n "Article\|下一篇\|Next" docs/en/03-guide/05-kubernetes.md` 之类，确认写法后统一改）。
- **`13-multi-project-sharing.md`（改号后是 `14-`）文首那句"这是系列最后一篇"**——保持它仍然是最后一篇，不用动这句话本身，但确认它引用的"上一篇"编号（如果有）也跟着改了。
- 每篇开头如果有"前面每一篇……"这类对系列整体的断言（`09-marketplace.md`/改号后 `10-` 那篇，记录里提到过"前面每一篇装的 demo/hello 都来自 local 源"这类话——**读一遍确认改号后这句话是不是还成立，不成立就要改内容，不只是改编号**），逐篇读开头核对，不要只做字符串替换。

- [ ] **Step 4: 改教程索引、`llms.txt`、AGENTS.md、README**

`docs/en/03-guide/README.md`：把表格第 4-13 行（`04 | [Deploy to Kubernetes]...` 到 `13 | [Multi-project sharing]...`）的编号列改成 `05`-`14`，链接目标文件名同步改；在原第 3 行（`03 | [Debug a component locally]...`）之后插入新的第 4 行（Task 8 写好新教程后回来填标题/学到什么/还需要什么这三列，先占位写 `TBD——Task 8 完成后回填`，Task 8 结束时必须把这个占位替换成真实内容，不能遗留）。

`docs/zh/03-guide/README.md` 同步。

`llms.txt`：把 `Guide article 4` 到 `Guide article 13` 的条目改成 `Guide article 5` 到 `Guide article 14`，链接路径同步改；在原 `Guide article 3` 条目之后插入新条目（同样先占位，Task 8 回填真实的一句话描述）。`llms.zh.txt` 若有独立的对应条目（它是独立撰写的索引，不是 `llms.txt` 的翻译——执行时先确认这个文件是否也列了教程条目，若有，同样改号+占位插入）。

`AGENTS.md`：第 1213 行附近"13 articles"改成"14 articles"；第 522、1179 行附近具体指向 `08-component-source.md`/`07-consuming-artifacts.md` 这类文件的链接，凡是编号在 4-13 之间的都要改成新编号（这两行原本指向的是 `08-component-source.md`（改号后 `09-`）——执行时用 Step 2 的 grep 结果核实，不要只改这两处，可能还有别的）。`AGENTS.zh.md` 同步。

`README.md`（根目录）：第 505 行附近"13 articles"改成"14 articles"；顺手用 Step 2 的 grep 结果确认这份文件里有没有具体指向某一篇教程的链接（比如"接下来去哪"这类表格），一并改号。

`docs/README.md`：第 34、47 行附近具体指向 `11-build-your-own.md`（改号后 `12-`）、`05-upgrades-and-versions.md`（改号后 `06-`）的链接改号；第 53 行"12-part"改成"14-part"（这是本任务发现的、此前就存在的中英文不同步，一并修正，不是新引入的编号问题）；第 90 行"13 篇"改成"14 篇"。

- [ ] **Step 5: 改 `check-guide-output.py` 的场景**

对 Step 2 grep 出的每一条 `"file": "0X-xxx.md"`（`X` 在 4-13 之间），把编号改成对应的新编号（`"file": "05-upgrades-and-versions.md"` → `"file": "06-upgrades-and-versions.md"`，以此类推）。**不要改 `"file": "03-local-debugging.md"` 这类编号不在 4-13 范围内的场景**——那些不受这次改号影响。

- [ ] **Step 6: 确认没有残留，跑校验**

Run: `git grep -n -E "0[4-9]-(kubernetes|upgrades-and-versions|assemble-and-break|consuming-artifacts|component-source|marketplace|signing|build-your-own|network-policy)|1[0-3]-(build-your-own|network-policy|multi-project-sharing)" -- ':!docs/archive' ':!*.jsonl'`
Expected: 空——Step 2 找到的每一处都已经改掉（占位符 `TBD——Task 8 完成后回填` 除外，那是有意留给下一个任务的）。

Run: `git status --porcelain`
Expected: 20 个 `R `（rename）+ 若干 `M`（修改内容的文件），没有任何 `D`（不该有文件被真的删掉）或 `??`（不该有孤立的新文件——`git mv` 已经处理了移动本身）。

Run: `go test ./tests/docfields/... ./tests/i18nguard/...`
Expected: 全绿（这一步还没有新增/删除任何 msgid，i18nguard 不该受影响；docfields 里如果有校验文档路径存在性的测试，这是第一次真正验证改号本身有没有漏改）。

（**这个任务结束时，教程索引与 `llms.txt` 里还留着 Task 8 要回填的占位符**——这是刻意的：Task 7 只负责"腾位置"，新教程的真实内容要等 Task 8 真跑一遍 CLI 才能写，不能在这里编。）

- [ ] **Step 7: 提交**

```bash
git add -A
git commit -m "$(cat <<'EOF'
文档：教程序列改号，为 mode: local 的新教程腾出第 4 篇的位置

04-kubernetes.md 到 13-multi-project-sharing.md 十篇（en/zh 各一份）整体
后移一号到 05-14；教程索引、llms.txt/llms.zh.txt、AGENTS.md/AGENTS.zh.md、
两份 README、check-guide-output.py 的场景 file 值同步改号。

顺手修正一处此前就存在、与这次改号无关的中英文不同步：docs/README.md
英文写"12-part"、中文写"13 篇"（应为当时的教程总数，两边没对齐）。

教程索引与 llms.txt 里留了占位符给下一个任务——新教程的真实内容要跑一遍
真实 CLI 才能写，不在这个只负责"腾位置"的提交里编。

Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>
EOF
)"
```

---

## Task 8: 新教程——`04-local-execution.md`

**背景：** Task 7 已经腾出第 4 篇的位置。这篇教程要讲清楚 `mode: local`：`brickkit` 自己探测启动命令、自己拉起进程、自己监管，跟 `mode: debug`（用户自己在 IDE 里启动）并排放在一起对比着讲最清楚——正好接在 `03-local-debugging.md` 后面，读者刚学完 `mode: debug` 的心智模型，最容易理解两者的区别。

**Files:**
- Create: `docs/en/03-guide/04-local-execution.md`
- Create: `docs/zh/03-guide/04-local-execution.md`
- Modify: `docs/en/03-guide/README.md`、`docs/zh/03-guide/README.md`（把 Task 7 留的占位符换成真实内容）
- Modify: `llms.txt`、`llms.zh.txt`（同上）
- Modify: `docs/en/03-guide/05-kubernetes.md`（原 `04-`，改号后的第一句"前置阅读"如果指向第 3 篇，要确认还成立；新增教程插进来后，这篇如果有"上一篇学到了 mode: debug"这类衔接句，读一遍确认还准确，不准确要改）

**Interfaces:**
- 纯文档任务。示例组件复用现有的 `demo/hello`/`demo/caller`（跟 03/07 两篇教程一致），照"guide 教程用最简单的模拟组件"的既有约定：`demo/hello` 只需要打印一行、监听一个端口，不需要真实业务逻辑。

- [ ] **Step 1: 手动真机搭一遍教程要展示的完整流程**

在 `/tmp` 或 scratchpad 下（不是仓库内）：

```bash
brickkit init hello-local --no-skills
brickkit new demo/hello
```

用 Write 工具写 `components/demo/hello/go.mod`（`module example.com/hello` + `go 1.22`）与 `main.go`（读 `PORT` 环境变量、监听、打印一行"demo/hello listening on :$PORT"）——比 Plan 4b 手动验证时用的固件还要再简单一点，不需要 `/healthz`，因为这篇教程的重点是"它自己启动了"，不是健康检查。

```bash
brickkit add --local
```

编辑 `brickkit.yaml`，把 `demo/hello` 的条目加上 `mode: local`。

依次真跑，逐字记录输出（不要凭记忆默写，每一步都要真的执行）：

```bash
brickkit up --dry-run          # 展示探测出的命令
brickkit up                    # 真启动，看到实时输出、"已监听端口"
```

在**另一个终端**（同一个项目目录）跑：

```bash
brickkit status                # 应该看到"这个项目有一个本地会话在跑"的提示
brickkit graph                 # 应该看到新的 "managed locally" 标签
brickkit down                  # 应该看到同样的会话锁提示（down 停不掉它）
```

回到第一个终端，`Ctrl+C`，记录体面退出的输出。

（这一步的产物是一份记满真实命令输出的笔记——scratchpad 里存一份，作为 Step 2-3 写文档时的唯一事实来源。**不要在没有真跑的情况下凭对 CLI 代码的理解去猜输出长什么样**——本计划前面每个任务的教训都是"教程会撞出 CLI/文档的真问题"，这一步就是那个撞真问题的机会。）

- [ ] **Step 2: 写英文版 `04-local-execution.md`**

结构参照 `03-local-debugging.md` 的写法（先读一遍那篇的完整结构：开头一段点明这篇要证明什么、"The setup"一节给出跟上一篇比只改了什么、真实命令+真实输出块、一段解释"背后发生了什么"、结尾一节小结）。这篇要覆盖：

1. 开头一段：`mode: debug` 是"你自己启动、brickkit 只管地址"；`mode: local` 是"brickkit 自己探测、自己启动、自己监管"——同一个"裸进程"大类下两种截然不同的责任划分，适合并排讲。
2. `brickkit.yaml` 里 `demo/hello` 从 Article 2 的普通配置改成 `mode: local` 的一行 diff（照 03 篇的写法）。
3. `brickkit up --dry-run` 的真实输出（Step 1 记录的），点出"看看会发生什么"这条命令对 `mode: local` 也成立——探测出的命令会被打印出来。
4. `brickkit up`（真启动）的真实输出，点出实时的带前缀输出、"已监听端口"这一行。
5. 另开一个终端跑 `status`/`graph`/`down` 的真实输出，讲清楚**为什么**：会话锁是唯一的跨终端信号，`down` 停不掉它——这不是 bug，是设计（对应 Task 3/4/5 刚做的这几处修复背后的道理，读者应该能从这篇教程反推出"为什么 status 要专门处理这个"）。
6. `Ctrl+C` 的真实输出，一段话讲清"不需要跨会话存活"这条设计决定（本地进程只在这个终端会话期间存在）。
7. 结尾一节，指向 `--crash-lines` 这个旗位（一句话提一下"进程崩了会怎样"，不用在这篇教程里真的演示崩溃——那是排障向的内容，属于 `08-troubleshooting.md` 的范围，不是"跑通一遍"这篇教程该做的）。

- [ ] **Step 3: 写中文版 `04-local-execution.md`**

不是逐句翻译——参照现有中文教程（比如 `docs/zh/03-guide/03-local-debugging.md`）的行文风格，内容结构跟英文版一一对应（`check-docs-bilingual` 会校验结构镜像），但中文提示文字直接用 `BRICKKIT_LANG=zh` 跑出来的真实输出（Step 1 如果只记录了英文输出，这一步要用中文环境变量重新跑一遍相同的流程，拿到真实的中文 CLI 输出，不能自己翻译"📋 组件状态计算"这类提示——**必须是真实 CLI 打印出来的原文**，照 `check-guide-output.py` 的既有要求）。

- [ ] **Step 4: 回填 Task 7 留的占位符**

`docs/en/03-guide/README.md`：把之前插入的占位行换成：

```
| 04 | [Run a component locally, hands-off](04-local-execution.md) | `mode: local`: BrickKit detects the start command, launches the process, and supervises it itself — `status`/`graph`/`down` all learn to recognize it | — |
```

（标题与"学到什么"这两列照 Step 2 实际写出的文章内容调整措辞，上面只是起点，不是必须逐字照抄。）

`docs/zh/03-guide/README.md` 同步，中文措辞。

`llms.txt`：

```
- [Run a component locally, hands-off](https://raw.githubusercontent.com/brickKit/brickKit/main/docs/en/03-guide/04-local-execution.md): Guide article 4 — mode: local end to end: BrickKit detects the start command, launches the process, supervises it, and status/graph/down all learn to show it correctly, proven with a real Go process and real cross-terminal session-lock output.
```

`llms.zh.txt` 同步（若该文件确实独立列了教程条目——Task 7 Step 4 已经确认过这一点）。

- [ ] **Step 5: 检查第 5 篇的衔接句**

`docs/en/03-guide/05-kubernetes.md`（原 `04-kubernetes.md`）：读一遍开头几段，确认有没有"上一篇你学到了 X"这类衔接句、以及文首的"前置阅读"链接——如果这类句子原本是接在 `03-local-debugging.md` 后面写的（比如"you just watched one component run outside a container"），现在中间插了一篇新教程，这句话如果指的是"上一篇"（现在的第 4 篇，`mode: local`）而不是第 3 篇（`mode: debug`），要确认逻辑仍然连贯，不连贯就改写这一两句话，不是整篇重写。`docs/zh/03-guide/05-kubernetes.md` 同步核对。

- [ ] **Step 6: 全面校验**

Run: `make check-cli-docs`
Expected: 通过（新文章里写命令行的地方，同一行 `brickkit <命令>` 之后的参数要挂在正确命令后面，或换行写——照 Global Constraints 里的提醒检查一遍新文章）。

Run: `make check-guide-output`（Makefile 第 227-228 行：这个目标就是 `python3 scripts/check-guide-output.py`，先 `build-cli`）
Expected: 新文章暂时不会被这个脚本检查到（场景要 Task 9 才加），跑这一步的目的是确认**没有把现有场景跑坏**——尤其是 Task 7 改过 `"file"` 值的那些场景，全绿。

手动通读一遍两份新文档，确认：① 每个输出块的第一行是不是一个能唯一定位的锚点（教程惯例，`check-guide-output.py` 靠这个匹配）；② 中英文结构一一对应；③ 没有编出任何一行没有真的跑出来过的 CLI 输出。

- [ ] **Step 7: 提交**

```bash
git add docs/en/03-guide/04-local-execution.md docs/zh/03-guide/04-local-execution.md docs/en/03-guide/README.md docs/zh/03-guide/README.md docs/en/03-guide/05-kubernetes.md docs/zh/03-guide/05-kubernetes.md llms.txt llms.zh.txt
git commit -m "$(cat <<'EOF'
新增：教程第 4 篇——mode: local 端到端

用真实 Go 固件、真实 brickkit up 走一遍 mode: local 的完整闭环：dry-run
探测出的命令、真启动的实时输出、另一个终端跑 status/graph/down 看到的
会话锁提示、Ctrl+C 的体面退出。跟第 3 篇（mode: debug）并排对比着讲，
读者刚学完"你自己启动"的心智模型，最容易理解"brickkit 自己启动"差在
哪——这也是 Task 3/4/5 那几处展示修复背后的道理，读者能从这篇教程反推
出"为什么 status 要专门处理这个"。

所有输出块逐字取自真实 CLI（英文与中文环境各跑一遍），回填了 Task 7
在教程索引与 llms.txt 里留的占位符。

Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>
EOF
)"
```

---

## Task 9: `check-guides` 新增 `local` 层

**背景：** spec §7："`mode: local` 的真实启动验证走 `scripts/check-guides.sh` + `tests/guides/清单.tsv`（不在 CI 里，`make check-guides` 手动跑），照 `core`/`docker`/`k8s` 三层的模式新增一层 `local`。"

**Files:**
- Modify: `scripts/check-guides.sh`
- Modify: `tests/guides/清单.tsv`
- Modify: `scripts/check-guide-output.py`（给 Task 8 的新教程加场景，Task 8 Step 6 提到过这一步留到这里）

**Interfaces:**
- 纯脚本/配置任务。真相来源是 `scripts/check-guides.sh` 本身（177 行）——已经完整读过：`tests/guides/清单.tsv` 的真实列格式是 `tier`（`core`/`docker`/`k8s`，不是"篇"/教程编号——第 4 行的文件头注释"列：篇 / ..."是一处与实现脱节的旧文案，这次不顺带修，不在本计划范围内）/ `what`（这一步在验什么）/ `_env`（第 120 行 `read -r tier what _env cmd expect` 里这一列读进 `_env` 但从没被用过，两条现有数据行都写 `-`，跟着写 `-` 就行）/ `cmd`（真的会被当成 `brickkit` 命令执行，除非等于某个被硬编码识别的 `!xxx` 夹具标记）/ `expect`（必须出现在输出里的关键词，`cmd` 是夹具标记时这一列没有意义，写 `-`）。**这是单个共享项目**（`$PROJ`，第 36 行起）：所有层的所有行按 TSV 文件顺序对同一个项目连续执行，不是每条各自独立——`core` 层的头两行已经 `init` 了项目并 `add demo/caller@1.0.0`（连带递归拉到 `demo/hello@1.0.0`），`local` 层的行排在文件末尾时，`demo/hello`/`demo/caller` 已经在 `brickkit.yaml` 里、都没有任何 `mode` 字段。`tests/components/demo-hello`（脚本第 39-42 行会把它复制进 `$PROJ/components/demo/hello`）本来就带着真实的 `go.mod`/`main.go`（是 Docker 层也在用的同一份夹具，`docker image inspect brickkit-demo/hello:1.0.0` 那步证实了它是可构建镜像的真实组件），`mode: local` 的探测不需要为这个任务另造固件。

- [ ] **Step 1: 给 `check-guides.sh` 加 `local` 层的环境探测**

`local` 层需要的环境：Go 工具链（`runcmd.Detect` 探测 `go.mod`/`main.go`，不需要真的执行 `go run .`——这次只验 `up --dry-run`，dry-run 从不启动进程）。不需要 Docker（`mode: local` 组件本身不生成容器；这次要跑的两行也不涉及 `demo/caller` 的数据库依赖，不会触碰到 `docker` 层已经做过的 `bind_pg`）。

在 `have_docker`/`have_k8s` 探测那几行（第 49-54 行）附近加：

```bash
have_go=0; command -v go >/dev/null 2>&1 && have_go=1
```

`tier_ok` 函数（第 56-63 行）加 `local` 分支：

```bash
tier_ok() {
	case "$1" in
		core) return 0 ;;
		docker) [[ $have_docker -eq 1 && $have_images -eq 1 ]] ;;
		k8s) [[ $have_k8s -eq 1 ]] ;;
		local) [[ $have_go -eq 1 ]] ;;
		*) return 1 ;;
	esac
}
```

`tier_why` 函数（第 109-114 行）加对应分支：

```bash
tier_why() {
	case "$1" in
		docker) [[ $have_docker -eq 0 ]] && echo "没有可用的 Docker" || echo "缺组件镜像（brickkit-demo/hello:1.0.0 等，见 docs/archive/guide/00-准备.md）" ;;
		k8s) echo "minikube 没在跑" ;;
		local) echo "没有可用的 go 工具链" ;;
	esac
}
```

- [ ] **Step 2: 加一个把 `demo/hello` 改成 `mode: local` 的夹具函数**

设置 `mode: local` 没有对应的 `brickkit` 命令（跟 `!bind-pg` 需要真起一个 postgres 容器同理，属于"把项目推到某个状态"的夹具步骤，不是要验证的命令本身）。照 `bind_pg`（第 77-107 行）的写法，在它之后加一个新函数：

```bash
# local_mode 把 demo/hello 改成 mode: local——04 层要用它去触发真实的启动
# 命令探测。跟 bind_pg 一样，这不是要验证的 brickkit 命令，是把项目推到
# 某个状态的夹具步骤。
local_mode() {
	python3 - "$PROJ/brickkit.yaml" <<-'PYEOF'
		import sys
		path = sys.argv[1]
		body = open(path, encoding="utf-8").read()
		old = "  - id: demo/hello\n    version: 1.0.0\n"
		if old not in body:
			sys.exit(1)
		open(path, "w", encoding="utf-8").write(body.replace(old, old + "    mode: local\n", 1))
	PYEOF
}
```

在主循环里 `!bind-pg` 的判断分支（第 137-141 行）之后加一段同构的判断：

```bash
	if [[ "$cmd" == "!local-mode" ]]; then
		local_mode && printf "  ✅ [%s] %s\n" "$tier" "$what" && pass=$((pass + 1)) \
			|| { printf "  ❌ [%s] %s\n" "$tier" "$what"; fail=$((fail + 1)); }
		continue
	fi
```

- [ ] **Step 3: 在清单 TSV 里加 `local` 层的两行**

`tests/guides/清单.tsv` 文件末尾（`docker` 层最后一行之后）加两行，制表符分隔：

```
local	把 demo/hello 改成 mode: local（夹具，不比对输出）	-	!local-mode	-
local	dry-run 探测出真实启动命令	-	up --dry-run	go run .
```

（第一行是夹具步骤，跟现有 `docker 绑定资源（夹具，不比对输出） - !bind-pg -` 那一行是同一种写法——`expect` 列写 `-`，因为 `!local-mode` 分支根本不检查输出。第二行才是真正验证的东西：`up --dry-run` 的输出里必须出现 `go run .`——这正是 `runcmd` 对 `tests/components/demo-hello` 的真实探测结果，Step 5 会真的跑一遍确认。）

- [ ] **Step 4: 给 `check-guide-output.py` 加场景**

在 `scripts/check-guide-output.py` 的场景列表里，紧挨着 `"03 mode: debug 的 dry-run 画面"` 那条场景（第 152-164 行附近）之后加：

```python
    {
        "what": "04 mode: local 的 dry-run 画面",
        "reset": True,
        "run": ["init hello-world --no-skills",
                "!copy-into components/demo/hello demo-hello",
                "add --local",
                "!local-mode demo/hello"],
        "file": "04-local-execution.md",
        "check": ("up --dry-run",
                  {"zh": "📋 组件状态计算：",
                   "en": "📋 Component state calculation:"}, 0),
    },
```

既有命令里没有能把一个组件改成 `mode: local` 的（现有的是 `!copy-into`/`!set-version`/`!disable`/`!pin`/`!local-debug`/`!add-second-version`/`!bind-resource`/`!make-remotes`/`!git-sources`/`!clear-mode`/`!git-init`/`!drop-components-ignore`/`!append`/`!git`——`add --local` 之后组件条目里不带任何 `mode` 字段，`!disable`/`!pin`/`!local-debug` 分别只会写 `mode: disable`/`mode: enabled`/`mode: debug` + `localPort`，见 `scripts/check-guide-output.py` 第 518-546 行的 `disable`/`pin`/`local_debug` 三个函数）。照 `disable`/`pin` 的写法（第 518-535 行）加一个新函数与对应的 `!` 分支：

```python
def local_mode(proj, component_id):
    """把某个组件改成 mode: local（04 的场景：brickkit 自己探测、自己启动）。"""
    path = os.path.join(proj, "brickkit.yaml")
    s = open(path, encoding="utf-8").read()
    old = f"  - id: {component_id}\n    version: 1.0.0\n"
    if old not in s:
        sys.exit(f"❌ 配置里找不到 {component_id}，无法改成 mode: local")
    open(path, "w", encoding="utf-8").write(s.replace(old, old + "    mode: local\n", 1))
```

在 `run_case`（第 750-785 行附近），紧接在 `elif step.startswith("!local-debug "):` 那个分支（第 760-762 行）之后、`elif step.startswith("!add-second-version "):`（第 763 行）之前插入：

```python
            elif step.startswith("!local-mode "):
                local_mode(proj, step.split(None, 1)[1])
```

- [ ] **Step 5: 变异测试新场景**

按 memory 里记录的既有做法："加完要做变异测试：篡改一行中文、一行英文，确认守卫分别报'对不上真实输出'和'中英不一致'，再还原。"

对刚加的场景，临时把 `check` 元组里 `"zh"` 那一行改错一个字，跑：

```bash
python3 scripts/check-guide-output.py
```

Expected: 报"对不上真实输出"类的错误，指向这个场景。改回来。

再临时把 `"en"` 那一行改成跟 `"zh"` 不对应的内容（模拟中英文本该一致却不一致），跑同样的命令，Expected: 报"中英不一致"类的错误。改回来。

- [ ] **Step 6: 真跑一遍 `check-guides`**

Run: `make check-guides`
Expected: `local` 层被识别并运行（这台机器一直在用 `go test`，`have_go` 会是 1），新加的两行都 ✅——第一行 `!local-mode` 把 `demo/hello` 改成 `mode: local`，第二行 `up --dry-run` 的输出里出现 `go run .`。

- [ ] **Step 7: 提交**

```bash
git add scripts/check-guides.sh tests/guides/清单.tsv scripts/check-guide-output.py
git commit -m "$(cat <<'EOF'
新增：check-guides 的 local 层——真实验证 mode: local 教程

照 core/docker/k8s 三层的既有模式加第四层 local（只需要 go 工具链，不需要
Docker——mode: local 组件本身不生成容器）。tests/guides/清单.tsv 加两行：
一条夹具步骤（把 demo/hello 改成 mode: local，复用 Docker 层也在用的同一份
真实 go.mod/main.go 固件）+ 一条真正验证 dry-run 探测出 go run . 的场景；
check-guide-output.py 也加对应的输出比对场景，做过变异测试确认守卫真的会
因为中英文不对/跟真实输出不符而报错。

Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>
EOF
)"
```

---

## Task 10: 全量验证与收尾

**背景：** 跑一遍全仓库测试、`-race`、完整 `make lint`、手动真机走一遍教程，确认九个任务互相之间没有破坏，回写执行结果。

**Files:**
- Modify: `docs/superpowers/plans/2026-09-22-local-mode-display-and-docs.md`（本文件）

- [ ] **Step 1: 全仓库测试**

Run: `go test ./...`（`brickkit` 与 `market-server` 两个 module 各跑一遍）
Expected: 全部 `ok`。

- [ ] **Step 2: `-race`**

Run: `go test -race ./internal/cli/ ./internal/compose/ -count=1`
Expected: 干净——这次触碰了 `status`/`down` 里新的 `sessionlock.Inspect` 调用，值得专门确认。

- [ ] **Step 3: 交叉编译确认**

Run: `make check-cross-build`
Expected: 退出码 0（本计划没有引入任何平台特定代码）。

- [ ] **Step 4: 完整 `make lint`**

Run: `make lint > /tmp/lint-plan4d.log 2>&1; echo "exit=$?"`
Expected: `exit=0`。重点确认：① `check-doc-fields`（Task 1 改过 error-codes.md 的一行、Task 6 改过 brickkit-yaml-reference.md）；② `check-docs-bilingual`（Task 6/8 的中英文镜像）；③ `check-cli-docs`（Task 8 新教程里的命令行写法）；④ `check-guide-output`（全部场景，含 Task 9 新加的）；⑤ `check-doc-tree`（Task 7 的改号有没有漏改哪个索引）；⑥ 覆盖率门槛。

- [ ] **Step 5: 手动真机走一遍第 4 篇教程（不是单元测试，是眼见为实）**

不是重新搭一遍固件——直接照 `docs/en/03-guide/04-local-execution.md` 写的步骤，在一个干净的临时目录里，从头跟着教程操作一遍：`init` → `new` → 写真实 `main.go` → `add --local` → 改 `mode: local` → `up --dry-run` → `up` → 另一个终端 `status`/`graph`/`down` → 回第一个终端 `Ctrl+C`。

确认每一步终端上看到的输出跟教程里写的**逐字**一致——这一步的意义是验证"教程写的是真的"，跟 Task 8 Step 1 的"记录真实输出"不是同一件事（那一步是采集素材，这一步是照着成品文档复核）。

- [ ] **Step 6: 回写执行结果，提交**

在本文件末尾追加"执行结果与遗留"一节，记录：十个任务的提交哈希、手动真机复核的结果、任何偏离计划之处、留给未来的清单（如果有）。

```bash
git add docs/superpowers/plans/2026-09-22-local-mode-display-and-docs.md
git commit -m "$(cat <<'EOF'
文档：Plan 4d（mode: local 展示层与文档）的执行结果回写进计划

十个任务全部完成：status/graph/down 对 mode: local 的正确展示、一处
"警告文案硬编码 mode: debug"的真实 bug、brickkit-yaml-reference.md 补齐、
教程序列改号、新教程、check-guides 的 local 层。

Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>
EOF
)"
```

---

## Self-Review Checklist（执行完 10 个任务后逐条核对）

- [ ] `go test ./...` 全绿；`go test -race` 对 `internal/cli`/`internal/compose` 干净
- [ ] `make check-cross-build`、完整 `make lint` 都是 `exit=0`
- [ ] `status`/`down` 对 `mode: local` 组件既不报"未在运行"，也不彻底沉默——会话锁存在时有提示，不存在时安静
- [ ] `graph` 给 `mode: local` 一个独立于 `mode: debug` 的标签与样式
- [ ] `internal/compose/local.go` 的两条警告不再硬编码"mode: debug"
- [ ] `08-brickkit-yaml-reference.md` 中英文都覆盖了 `mode: local` 的字段行与互斥规则
- [ ] 教程序列改号后，`git grep` 旧编号引用为空；`check-guide-output.py` 全部场景（含新加的）通过，且真的做过变异测试
- [ ] 新教程的每一行输出都取自真实 CLI，不是编出来的
- [ ] `check-guides` 的 `local` 层能被正确识别、跑通或响亮跳过
- [ ] 至少手动走完一遍新教程的真机流程（Task 10 Step 5），不是只看单元测试绿灯
- [ ] `internal/procsup`/`internal/sessionlock`/`internal/runcmd` 三个包本身没有被本计划修改一行
- [ ] `docs/{en,zh}/07-patterns/05-deployment-selection-guide.md` 没有任何改动

## Execution Handoff

Plan complete and saved to `docs/superpowers/plans/2026-09-22-local-mode-display-and-docs.md`. Two execution options:

1. **Subagent-Driven (recommended)** - I dispatch a fresh subagent per task, review between tasks, fast iteration
2. **Inline Execution** - Execute tasks in this session using executing-plans, batch execution with checkpoints

选哪种？

---

## 执行结果与遗留（2026-09-22）

十个任务全部完成，Inline Execution 跑完。提交哈希（时间顺序）：

| 任务 | 提交 | 一句话 |
| --- | --- | --- |
| 计划本身 | `9e85437` | Plan 4d 十个任务，写作时自查改过 3 处真实错误 |
| Task 1 | `d33ef04` | `expose`/`labels` 警告不再硬编码 `mode: debug`——四处硬编码，不是两处 |
| Task 2 | `7b62435` | `containerRefs` 排除 `mode: local`，`localRefs` 改名 `debugRefs` |
| Task 3 | `0826d84` | `status` 排除 `mode: local` 出容器表、会话锁存在时加提示 |
| Task 4 | `997b61a` | `down` 同一种会话锁提示 |
| Task 5 | `5d1eca6` | `graph` 给 `mode: local` 独立标签与样式 |
| Task 6 | `f470eb8` | `08-brickkit-yaml-reference.md` 补齐 `mode: local` 字段行与互斥规则 |
| Task 7 | `dc13c36` | 教程序列改号（04-13 → 05-14），为新教程腾出第 4 篇 |
| Task 8 | `254f48f` | 新教程第一版（后被 Task 9 发现固件不对，整篇重写） |
| Task 9 | `f5f0acc` | `check-guides` 加 `local` 层；新教程改用真实共享固件重写 |
| Task 10 | 本提交 | 全量验证、手动真机复核、回写本节 |

### Task 10 六步验证结果

1. **`go test ./...`**：`brickkit` 与 `market-server` 两个 module 全绿。
2. **`go test -race ./internal/cli/ ./internal/compose/ -count=1`**：干净，20.7s + 1.8s。
3. **`make check-cross-build`**：`exit=0`，linux/darwin/windows 三个目标都编得过。
4. **完整 `make lint`**：`exit=0`。唯一的 `❌` 是已知、不计入退出码的一条——`up --crash-lines` 没写进任何文档，这是等用户确认要不要写进 CLI 参考文档的既有待办（不属于 Plan 4d 范围），不是本计划引入的新问题。
5. **手动真机复核第 4 篇教程**：在仓库根目录直接建了 `hello-local/`（不是另起固件——照教程原文一字不差地敲：`mkdir hello-local && cd hello-local`、`brickkit init hello-local`（没加 `--no-skills`，教程原文就没加）、`mkdir -p components/demo`、`cp -r ../tests/components/demo-hello components/demo/hello`、`brickkit add --local`、手改 `mode: local`、`up --dry-run`、`up`、另一个终端 `status`/`graph`/`down`、回第一个终端 `Ctrl+C`）。**每一步的真实输出跟教程里写的逐字一致**（`PID`、JSON 日志里的 `time`/`elapsed_ms` 这类天然易变的字段除外——教程里这些字段要么用真实抓下来的例子，要么按既有惯例写 `"..."`，都不要求逐字复现）。复核完 `rm -rf hello-local`，`git status --porcelain` 确认干净。
6. 本节。

### 偏离计划之处

- **Task 1 比计划预期多改了两处**：计划草稿只预料到 `ComposeLocalFieldsIgnored`/`ComposeLocalLabelsIgnored` 两条消息体硬编码了 "mode: debug"，实际测试跑起来才发现对应的两条 Hint（`ComposeHintDropLocalForPorts`/`ComposeHintDropLocalForLabels`）也独立硬编码了同一个词——四处，不是两处。
- **Task 5 的 `TestGraphModeLocalComponentIsPinnedAndNeverGreyedOut` 改写**：计划草稿里的版本用两个互不相关的组件，测不出级联钉住；执行时改成 `demo/web`（disabled）→`demo/hello`（`mode: local`）的真实依赖链才真正验证到这条断言。
- **Task 6 修了一个真实 panic**：`tests/docfields/reference_test.go` 的 `documentedPaths` 解析器在" 裸 `` `local` `` 紧跟在 `` `.servedBy` `` 这类点前缀续接词前面"时会 panic（`previous[:strings.LastIndex(previous, ".")]` 假设前一个 token 一定带点）。绕开写法是把 "Mutual exclusions" 一节里的 `` `local` `` 展开成完整的 `` `components[].mode: local` ``——检查器本身这处脆弱没有修，判定不在这次任务范围内。
- **Task 7 范围比计划文档描述的更大**：
  - 除了文件名字符串替换（脚本自动做的）之外，正文里"Article N"/"第 N 篇"/"guide N"这类指位置的措辞、每篇文章自己的 H1 编号、`llms.txt`/`llms.zh.txt` 的 "Guide article N" 标签、两份 README 表格的编号列，全部逐处人工核对并改正——这部分工作量比计划草稿预估的大，因为"文件名"和"指位置的措辞"是两类完全独立的引用，前者能脚本化，后者不能。
  - `fix_refs.py` 脚本第一次跑漏了一个排除项：它的排除列表只有 `docs/superpowers/plans/` 和 `docs/archive/`，没有 `docs/superpowers/specs/`，误改了一份 2026-09-19 的历史 spec 文档，`git checkout` 撤回，脚本本身这处排除列表没有回头补（脚本是一次性用完即弃的工具，不会再跑第二遍）。
  - 装上 `golangci-lint`（这台机器原来没装，此前 `make lint` 一直在悄悄退化成 `go vet`）后跑出两处真实问题，一并修了：`internal/procsup/supervisor.go` 里本会话早前为 Plan 4c 调查加的 `Printf` 方法没检查 `fmt.Fprintf` 的返回值（errcheck）；`internal/procsup/prefix_test.go` 三处 `Write(fmt.Sprintf(...))` 换成更地道的 `fmt.Fprintf`（staticcheck QF1012）。**这两处修改让 Self-Review Checklist 里"`internal/procsup`/`internal/sessionlock`/`internal/runcmd` 三个包本身没有被本计划修改一行"这条没有严格成立**——但这不是本计划自己的设计改动，是跑 `make lint` 时才第一次暴露出来的、与 Plan 4d 无关的历史 lint 债务，行为不变，只是让返回值检查过关。
- **Task 8 的第一版教程后来被 Task 9 整篇重写**：Task 8 Step 1 按计划原文的指示，手写了一份最小固件（`go.mod` + 读 `PORT` 的 `main.go`，`version: 0.1.0`）来走查真实输出。给 Task 9 的 `check-guide-output.py` 写确定性场景时才发现：这个系列所有其它场景统一复用 `tests/components/demo-hello`（这份真实夹具版本是 `1.0.0`，`main.go` 用 `slog` 打 JSON 结构化日志，不是一行 `fmt.Printf`）——教程手写的那份固件跟被验证的东西根本不是同一个组件。照"guide 教程用最简单的模拟组件"的既有约定（复用现成夹具，不另起一个），改用 `tests/components/demo-hello`，中英文各重新真跑一遍完整流程，用真实捕获的输出（`demo-hello-1-0-0`、真实 JSON 日志行）整篇重写。这也带出一处顺手的真实发现：`main.go` 在收到 `SIGINT`/`SIGTERM` 后会打一行 `component exited` 的日志再退出——比手写固件的"静默退出"更能说明"BrickKit 是在体面地要求它停下"这件事，写进了"停掉它"一节。
- **Task 9 发现并修了两处真实 bug/遗留**：
  - `check-guides.sh` 的 `local_mode()` 函数：Python heredoc 用 `<<-'PYEOF'`，`if`/`sys.exit(1)` 两行全用 tab 缩进——`<<-` 会剥掉每行开头的所有 tab，Python 那层相对缩进跟着被剥没，报 `IndentationError`。`bind_pg` 之前没撞上是因为它的 Python 代码没有任何需要相对缩进的语句。改成 `sys.exit(1)` 那行用空格撑住相对缩进（`<<-` 只剥 tab，不剥空格），`make check-guides` 真的跑通验证过。
  - `check-guide-output.py` 里 `"05"`–`"08"` 开头的十几条场景 `what` 标签，对应的是 Task 7 改号前的教程编号——Task 7 的 grep 只扫了正文措辞和文件名引用，没扫到这批脚本内部纯数字前缀的标识字符串。按各自的 `file` 字段核实后统一改成 `06`/`07`/`08`/`09`。

### Self-Review Checklist 逐条核对

- [x] `go test ./...` 全绿；`go test -race` 对 `internal/cli`/`internal/compose` 干净
- [x] `make check-cross-build`、完整 `make lint` 都是 `exit=0`
- [x] `status`/`down` 对 `mode: local` 组件既不报"未在运行"，也不彻底沉默——会话锁存在时有提示，不存在时安静
- [x] `graph` 给 `mode: local` 一个独立于 `mode: debug` 的标签与样式
- [x] `internal/compose/local.go` 的两条警告不再硬编码"mode: debug"（实际是四处硬编码，见上）
- [x] `08-brickkit-yaml-reference.md` 中英文都覆盖了 `mode: local` 的字段行与互斥规则
- [x] 教程序列改号后，`git grep` 旧编号引用为空；`check-guide-output.py` 全部场景（含新加的）通过，且真的做过变异测试
- [x] 新教程的每一行输出都取自真实 CLI，不是编出来的（Task 8 第一版不是，Task 9 发现后整篇重写过）
- [x] `check-guides` 的 `local` 层能被正确识别、跑通或响亮跳过
- [x] 至少手动走完一遍新教程的真机流程（Task 10 Step 5），不是只看单元测试绿灯
- [ ] `internal/procsup`/`internal/sessionlock`/`internal/runcmd` 三个包本身没有被本计划修改一行——**未严格成立**：`internal/procsup/supervisor.go`、`internal/procsup/prefix_test.go` 各改了几行，理由见上（golangci-lint 装上后才暴露的历史 lint 债务，非本计划设计改动，行为不变）
- [x] `docs/{en,zh}/07-patterns/05-deployment-selection-guide.md` 没有任何改动

### 遗留

无遗留任务。`--crash-lines` 的文档化（是否要写进 `docs/en/06-architecture/09-cli-reference.md` 之类的参考文档）仍然是此前就有的、等用户确认的待办，跟 Plan 4d 无关，不在这次的范围内。

分支 `worktree-mode-field-migration` 仍未合并、未推送——是否合并、何时合并是用户的决定，不在本计划范围内。
