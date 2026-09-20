# CLI 多语言支持 · 核心机制 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 给 BrickKit CLI 建立一套消息目录 + 全局语言配置机制，默认英语，`brickkit lang set en|zh` 切换，并在一个不碰另外 80 个命令文件的纵向切片（`version`、`PROJECT_MISSING` 错误、root `--help`）上验证机制可行。

**Architecture:** 新增两个零依赖的叶子包——`internal/msgid`（消息 ID 常量）与 `internal/i18n`（编译期内嵌的 en/zh 目录 + 查表 + 语言解析），再加一个新的机器级配置包 `internal/userconfig`（BrickKit 第一次引入的跨项目状态）。`internal/clierr` 与 `internal/cli` 消费这两个包；`clierr` 只为自己的渲染骨架（"建议："标签、明细行的"："分隔符）接入 i18n，调用点自己的 `Message`/`Detail.Value` 内容仍由调用方传入。

**Tech Stack:** Go 1.22，`spf13/cobra`，`stretchr/testify`；不引入任何新的第三方依赖。

**Spec:** [docs/superpowers/specs/2026-09-20-cli-i18n-design.md](../specs/2026-09-20-cli-i18n-design.md)（子项目 1：核心机制）

## Global Constraints

- 语言只有两种：`en`（默认）、`zh`。目录是编译期内嵌的 Go map，不做按需下载（spec §2 已否决）。
- 语言解析优先级：`BRICKKIT_LANG` 环境变量 > 全局配置文件（`brickkit lang set` 写入）> 默认英语。不做 `--lang` 命令行参数（spec §3.3）。
- 全局配置文件路径由 `os.UserConfigDir()/brickkit/config.json` 决定，跨平台正确；测试通过 `BRICKKIT_USERCONFIG_DIR` 环境变量覆盖，避免读到/写到跑测试的人自己机器上真实的全局配置。
- `brickkit lang` / `brickkit lang set` 跟 `version` 一样是 CLI 自身命令：不分组、不计入"16 个业务命令"这个数字（`scripts/check-cli-docs.py` 的统计需要同步排除，否则命令数对不上会让 `make lint` 失败）。
- 本计划只改三个已有点：`internal/cli/version.go`、`internal/config/parse.go` 里的 `PROJECT_MISSING`、root 命令的 `--help` 模板切换。其余 80+ 命令文件与 resolver/compose/k8s/inject/cascade/workspace/security/manifest/config 里的中文字面量，以及 `docs/en/06-architecture/10-error-codes.md`、`scripts/check-guide-output.py`、AGENTS.md 命令集标题，全部留给未来独立立项的子项目 2/3，不在本计划范围内。
- 带参数的目录文案一律用 Go `fmt` 的位置 verb（`%[1]s`），不用裸 `%s`。
- `clierr.Error` 的 `Message`/`Detail.Value`/`Hints`/`Tips` 内容仍由调用点传入（已翻译好的字符串），`clierr` 包本身不知道"这是第几种错误"，只知道怎么排版。
- 每个任务结束都要跑 `go build ./...` 与相关包的 `go test`，最后一个任务跑一次完整 `make lint`。

---

## 计划执行中发现的两个必须处理的连带问题

写这份计划、逐个读了要改的代码和测试之后，发现两处光靠"翻译一下文字"补不上、必须一起修的地方，都已经编进下面的任务里，这里先说明白，免得执行时以为是自己改错了：

1. **`internal/clierr/clierr.go` 的 `Format()` 自己就硬编码了中文标点**——明细行的全角冒号"："、"建议："这个标签本身，都不是某条具体错误的文案，而是渲染骨架自带的。这两处不翻译，`PROJECT_MISSING` 就算翻成英文也会渲染成 `Path：brickkit.yaml`（英文标签 + 全角中文冒号）和硬编码的中文"建议："。Task 2 专门修这个。
2. **`tests/docfields/errorcodes_test.go` 靠 `go/ast` 静态解析 `clierr.New(...)` 调用的第二个参数来核对错误码文档标题**，只认得字符串字面量和字符串拼接，认不出 `i18n.T(msgid.X)` 这种函数调用。Task 6 转换 `PROJECT_MISSING` 之后，这条测试会先变红（预期之内，不是改错了），同一个任务里把它修好——教会它把 `i18n.T(msgid.X)` 还原成中文目录里的真实文案（`docs/{en,zh}/10-error-codes.md` 现在两侧都还照抄中文原文，子项目 3 才会改这个约定，所以这里统一按 zh 目录解析，跟文档现状对齐）。这个修法是一次性的：子项目 2 以后每转换一条消息，这条测试都会自动认得，不需要再回来改。

---

### Task 1: `internal/msgid` + `internal/i18n` 核心（目录、查表）

**Files:**
- Create: `internal/msgid/msgid.go`
- Create: `internal/i18n/i18n.go`
- Create: `internal/i18n/catalog_en.go`
- Create: `internal/i18n/catalog_zh.go`
- Create: `internal/i18n/i18n_test.go`

**Interfaces:**
- Produces: `msgid.DetailLine`、`msgid.HintLabelSingle`、`msgid.HintLabelMulti`（`string` 常量，Task 2 消费）
- Produces: `i18n.Lang`（`type Lang string`）、`i18n.EN`、`i18n.ZH`、`i18n.Current() Lang`、`i18n.SetCurrent(Lang)`、`i18n.T(id string, args ...any) string`、`i18n.CatalogFor(Lang) map[string]string`

- [ ] **Step 1: 写 `internal/msgid/msgid.go`**

```go
// Package msgid 声明 CLI 消息目录的 key。每个常量对应 internal/i18n 两份
// 目录（en/zh）里的一条文案；常量值本身（不是常量名）就是查表用的 key。
package msgid

const (
	// clierr 渲染骨架用的 key：跟"这是第几种错误"无关，只跟 Format() 的
	// 排版本身有关（明细行的 分隔符、"建议："这个标签）。
	DetailLine      = "detail.line"
	HintLabelSingle = "hint.label.single"
	HintLabelMulti  = "hint.label.multi"
)
```

- [ ] **Step 2: 写 `internal/i18n/catalog_en.go`**

```go
package i18n

import "github.com/brickkit/brickkit/internal/msgid"

var en = map[string]string{
	msgid.DetailLine:      "%[1]s: %[2]s",
	msgid.HintLabelSingle: "Suggestion: %[1]s",
	msgid.HintLabelMulti:  "Suggestions:",
}
```

- [ ] **Step 3: 写 `internal/i18n/catalog_zh.go`**

```go
package i18n

import "github.com/brickkit/brickkit/internal/msgid"

var zh = map[string]string{
	msgid.DetailLine:      "%[1]s：%[2]s",
	msgid.HintLabelSingle: "建议：%[1]s",
	msgid.HintLabelMulti:  "建议：",
}
```

- [ ] **Step 4: 先写测试 `internal/i18n/i18n_test.go`**

```go
package i18n

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/brickkit/brickkit/internal/msgid"
)

func TestCurrentDefaultsToEnglish(t *testing.T) {
	assert.Equal(t, EN, Current())
}

func TestSetCurrentChangesLookupLanguage(t *testing.T) {
	prev := Current()
	defer SetCurrent(prev)

	SetCurrent(ZH)
	assert.Equal(t, "建议：", T(msgid.HintLabelMulti))

	SetCurrent(EN)
	assert.Equal(t, "Suggestions:", T(msgid.HintLabelMulti))
}

func TestTInterpolatesPositionalArgs(t *testing.T) {
	prev := Current()
	defer SetCurrent(prev)

	SetCurrent(EN)
	assert.Equal(t, "Path: brickkit.yaml", T(msgid.DetailLine, "Path", "brickkit.yaml"))

	SetCurrent(ZH)
	assert.Equal(t, "路径：brickkit.yaml", T(msgid.DetailLine, "路径", "brickkit.yaml"))
}

func TestCatalogForReturnsIndependentSnapshot(t *testing.T) {
	prev := Current()
	defer SetCurrent(prev)
	SetCurrent(ZH)

	snap := CatalogFor(ZH)
	snap[msgid.HintLabelMulti] = "被改了"
	assert.Equal(t, "建议：", T(msgid.HintLabelMulti), "CatalogFor 返回的必须是拷贝，不能让调用方改到真的目录")
}

// TestCatalogParity 是双语完整性的门槛：msgid 里声明的每个 key，en/zh
// 两份目录都必须有，缺一个就是半成品——不允许运行时才发现某句话是空字符串。
func TestCatalogParity(t *testing.T) {
	for key := range en {
		if _, ok := zh[key]; !ok {
			t.Errorf("zh 目录缺少 key：%s", key)
		}
	}
	for key := range zh {
		if _, ok := en[key]; !ok {
			t.Errorf("en 目录缺少 key：%s", key)
		}
	}
}
```

- [ ] **Step 5: 跑测试确认失败**

Run: `go test ./internal/i18n/... -v`
Expected: 编译失败（`i18n.go` 还不存在，`Current`/`SetCurrent`/`T`/`CatalogFor`/`EN`/`ZH` 未定义）

- [ ] **Step 6: 写 `internal/i18n/i18n.go` 让测试通过**

```go
// Package i18n 是 BrickKit CLI 的消息目录：给一个 internal/msgid 里声明的
// key，返回当前语言的文案。只管"文字选哪种语言"，不掺和错误怎么渲染
// （那是 internal/clierr 的事），也不掺和 cobra（那是 internal/cli 的事）。
package i18n

import "fmt"

// Lang 是已支持的语言。
type Lang string

const (
	EN Lang = "en"
	ZH Lang = "zh"
)

// current 是当前进程的语言。默认 EN；CLI 每次构建命令树时都会重新解析
// 并设置一次（跟 internal/logging 的 SetLevel 是同一种"进程级全局状态，
// 但每次入口调用都重新初始化"的用法），所以测试不需要手动复位。
var current = EN

// SetCurrent 设置当前进程使用的语言。
func SetCurrent(l Lang) {
	current = l
}

// Current 返回当前生效的语言。
func Current() Lang {
	return current
}

func catalogFor(l Lang) map[string]string {
	if l == ZH {
		return zh
	}
	return en
}

// T 返回 id 对应的当前语言文案，用 args 做位置参数插值
// （目录里的动词一律是 %[1]s 这种位置 verb，因为中英文语序经常不同）。
//
// id 必须是 internal/msgid 里声明的常量。两份目录都缺失同一个 key 会被
// TestCatalogParity 在测试期拦住，正常运行不会走到"查不到"这条分支；
// 万一真的走到了（比如新增 key 时漏了一份目录、测试又没跑），直接暴露
// 问题而不是悄悄回落到另一种语言——这是这个项目一贯的态度（§9.13）。
func T(id string, args ...any) string {
	text, ok := catalogFor(current)[id]
	if !ok {
		return fmt.Sprintf("!missing-i18n-key:%s!", id)
	}
	if len(args) == 0 {
		return text
	}
	return fmt.Sprintf(text, args...)
}

// CatalogFor 返回给定语言目录的只读快照，供工具类代码使用
// （tests/docfields 核对错误码文档标题要用到），不用于运行时查文案——
// 运行时一律用 T()。返回值是拷贝，调用方改它不会影响真正的目录。
func CatalogFor(l Lang) map[string]string {
	src := catalogFor(l)
	out := make(map[string]string, len(src))
	for k, v := range src {
		out[k] = v
	}
	return out
}
```

- [ ] **Step 7: 跑测试确认通过**

Run: `go test ./internal/i18n/... -v`
Expected: PASS（全部 5 个测试）

- [ ] **Step 8: 跑 `go vet` 与 build 确认没有破坏其他地方**

Run: `go build ./... && go vet ./...`
Expected: 无输出、无错误

- [ ] **Step 9: Commit**

```bash
git add internal/msgid internal/i18n
git commit -m "$(cat <<'EOF'
新增：internal/msgid + internal/i18n 消息目录核心

编译期内嵌的 en/zh 两份目录、T() 查表 + 位置 verb 插值、双语完整性
lint（TestCatalogParity）。只搭机制，还没有任何调用点消费它。

Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>
EOF
)"
```

---

### Task 2: 给 `clierr.Format()` 接入 i18n（明细分隔符 + 建议标签）

**Files:**
- Modify: `internal/clierr/clierr.go`
- Modify: `internal/clierr/clierr_test.go`

**Interfaces:**
- Consumes: `i18n.T`、`msgid.DetailLine`、`msgid.HintLabelSingle`、`msgid.HintLabelMulti`（Task 1）
- Produces: 无新增导出符号；`(*Error).Format()` 的渲染结果从此按 `i18n.Current()` 变化

- [ ] **Step 1: 改 `internal/clierr/clierr_test.go` 里三处断言，改成英文默认值**

把：

```go
func TestFormatFullBlock(t *testing.T) {
	err := New(CodeDependencyMissing, "错误：强依赖缺失").
		WithDetail("组件", "erp/backend@1.0.0").
		WithDetail("缺失依赖", "authorization/rbac@1.0.0").
		WithDetail("原因", "该组件在所有安装源中均未找到").
		WithHint("检查安装源配置（brickkit.yaml → sources）", "确认组件是否已发布到市场")

	got := err.Format()
	want := "❌ 错误：强依赖缺失\n" +
		"   组件：erp/backend@1.0.0\n" +
		"   缺失依赖：authorization/rbac@1.0.0\n" +
		"   原因：该组件在所有安装源中均未找到\n" +
		"   建议：\n" +
		"   1. 检查安装源配置（brickkit.yaml → sources）\n" +
		"   2. 确认组件是否已发布到市场\n"
	assert.Equal(t, want, got)
}

func TestFormatSingleHintIsInline(t *testing.T) {
	err := New(CodePortConflict, "错误：expose 端口冲突").
		WithHint("在 brickkit.yaml 中为其中一个组件添加 exposePort 字段")
	assert.Contains(t, err.Format(), "   建议：在 brickkit.yaml 中为其中一个组件添加 exposePort 字段\n")
	assert.NotContains(t, err.Format(), "   1. ")
}
```

改成：

```go
func TestFormatFullBlock(t *testing.T) {
	err := New(CodeDependencyMissing, "错误：强依赖缺失").
		WithDetail("组件", "erp/backend@1.0.0").
		WithDetail("缺失依赖", "authorization/rbac@1.0.0").
		WithDetail("原因", "该组件在所有安装源中均未找到").
		WithHint("检查安装源配置（brickkit.yaml → sources）", "确认组件是否已发布到市场")

	got := err.Format()
	want := "❌ 错误：强依赖缺失\n" +
		"   组件: erp/backend@1.0.0\n" +
		"   缺失依赖: authorization/rbac@1.0.0\n" +
		"   原因: 该组件在所有安装源中均未找到\n" +
		"   Suggestions:\n" +
		"   1. 检查安装源配置（brickkit.yaml → sources）\n" +
		"   2. 确认组件是否已发布到市场\n"
	assert.Equal(t, want, got)
}

func TestFormatSingleHintIsInline(t *testing.T) {
	err := New(CodePortConflict, "错误：expose 端口冲突").
		WithHint("在 brickkit.yaml 中为其中一个组件添加 exposePort 字段")
	assert.Contains(t, err.Format(), "   Suggestion: 在 brickkit.yaml 中为其中一个组件添加 exposePort 字段\n")
	assert.NotContains(t, err.Format(), "   1. ")
}
```

再把 `TestRenderWritesAndReturnsExitCode` 里的：

```go
	assert.Contains(t, buf.String(), "建议：")
```

改成：

```go
	assert.Contains(t, buf.String(), "Suggestion:")
```

（`Message`/`Detail` 的 `Key`/`Value`、`Hints` 的具体文字仍然是测试自己传进去的中文字面量，保持不变——`clierr` 不翻译调用点内容，这几个断言里唯一因为语言默认值改变而变化的，只有分隔符"："→": "和"建议："标签→"Suggestion(s):"。）

- [ ] **Step 2: 跑测试确认失败（还没改 clierr.go）**

Run: `go test ./internal/clierr/... -run 'TestFormatFullBlock|TestFormatSingleHintIsInline|TestRenderWritesAndReturnsExitCode' -v`
Expected: 三个都 FAIL（实际输出仍是"："和"建议："，因为 `Format()` 还没接 i18n）

- [ ] **Step 3: 改 `internal/clierr/clierr.go` 的 `Format()`**

在文件顶部 import 里加两行：

```go
import (
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/brickkit/brickkit/internal/i18n"
	"github.com/brickkit/brickkit/internal/msgid"
)
```

把 `Format()` 方法体：

```go
func (e *Error) Format() string {
	symbol := "❌"
	if e.Warning {
		symbol = "⚠️"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%s %s\n", symbol, e.Message)
	for _, d := range e.Details {
		fmt.Fprintf(&b, "   %s：%s\n", d.Key, d.Value)
	}
	switch len(e.Hints) {
	case 0:
	case 1:
		fmt.Fprintf(&b, "   建议：%s\n", e.Hints[0])
	default:
		b.WriteString("   建议：\n")
		for i, h := range e.Hints {
			fmt.Fprintf(&b, "   %d. %s\n", i+1, h)
		}
	}
	for _, t := range e.Tips {
		fmt.Fprintf(&b, "   💡 %s\n", t)
	}
	return b.String()
}
```

改成：

```go
func (e *Error) Format() string {
	symbol := "❌"
	if e.Warning {
		symbol = "⚠️"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%s %s\n", symbol, e.Message)
	for _, d := range e.Details {
		fmt.Fprintf(&b, "   %s\n", i18n.T(msgid.DetailLine, d.Key, d.Value))
	}
	switch len(e.Hints) {
	case 0:
	case 1:
		fmt.Fprintf(&b, "   %s\n", i18n.T(msgid.HintLabelSingle, e.Hints[0]))
	default:
		b.WriteString("   " + i18n.T(msgid.HintLabelMulti) + "\n")
		for i, h := range e.Hints {
			fmt.Fprintf(&b, "   %d. %s\n", i+1, h)
		}
	}
	for _, t := range e.Tips {
		fmt.Fprintf(&b, "   💡 %s\n", t)
	}
	return b.String()
}
```

- [ ] **Step 4: 跑测试确认通过**

Run: `go test ./internal/clierr/... -v`
Expected: PASS（全部测试，含刚改的三个）

- [ ] **Step 5: 跑一次全仓库测试确认没有牵连别的包**

Run: `go build ./... && go test ./internal/... 2>&1 | tail -40`
Expected: 全部 `ok`（`internal/clierr` 被几乎所有包间接使用，这一步确认没有别处也硬编码断言了"建议："）

- [ ] **Step 6: Commit**

```bash
git add internal/clierr
git commit -m "$(cat <<'EOF'
改进：clierr.Format() 的渲染骨架接入 i18n

明细行的分隔符、"建议："这个标签本身是 Format() 自带的排版，不是任何
一条具体错误的文案；不翻这两处，其余文案翻得再对也会渲染出中英夹杂
的输出。调用点自己的 Message/Detail.Value/Hints 内容不变。

Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>
EOF
)"
```

---

### Task 3: `internal/userconfig`（全局语言偏好存储）

**Files:**
- Create: `internal/userconfig/userconfig.go`
- Create: `internal/userconfig/userconfig_test.go`

**Interfaces:**
- Produces: `userconfig.Config{Lang string}`、`userconfig.Dir() (string, error)`、`userconfig.Path() (string, error)`、`userconfig.Load() (*Config, error)`、`userconfig.Save(*Config) error`、`userconfig.EnvDirOverride`（常量，测试专用覆盖）

- [ ] **Step 1: 先写测试 `internal/userconfig/userconfig_test.go`**

```go
package userconfig

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDirRespectsEnvOverride(t *testing.T) {
	t.Setenv(EnvDirOverride, "/tmp/example-brickkit-config")
	dir, err := Dir()
	require.NoError(t, err)
	assert.Equal(t, "/tmp/example-brickkit-config", dir)
}

func TestLoadWithoutFileReturnsEmptyConfigNotError(t *testing.T) {
	t.Setenv(EnvDirOverride, t.TempDir())
	cfg, err := Load()
	require.NoError(t, err)
	assert.Equal(t, "", cfg.Lang)
}

func TestSaveThenLoadRoundTrips(t *testing.T) {
	t.Setenv(EnvDirOverride, t.TempDir())

	require.NoError(t, Save(&Config{Lang: "zh"}))

	cfg, err := Load()
	require.NoError(t, err)
	assert.Equal(t, "zh", cfg.Lang)
}

func TestSaveOverwritesPreviousValue(t *testing.T) {
	t.Setenv(EnvDirOverride, t.TempDir())

	require.NoError(t, Save(&Config{Lang: "zh"}))
	require.NoError(t, Save(&Config{Lang: "en"}))

	cfg, err := Load()
	require.NoError(t, err)
	assert.Equal(t, "en", cfg.Lang)
}

func TestLoadRejectsMalformedJSON(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(EnvDirOverride, dir)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "config.json"), []byte("{not json"), 0o644))

	_, err := Load()
	assert.Error(t, err)
}

func TestSaveCreatesDirIfMissing(t *testing.T) {
	base := t.TempDir()
	nested := filepath.Join(base, "does", "not", "exist", "yet")
	t.Setenv(EnvDirOverride, nested)

	require.NoError(t, Save(&Config{Lang: "en"}))

	cfg, err := Load()
	require.NoError(t, err)
	assert.Equal(t, "en", cfg.Lang)
}

func TestPathIsInsideDir(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(EnvDirOverride, dir)

	path, err := Path()
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(dir, "config.json"), path)
}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./internal/userconfig/... -v`
Expected: 编译失败（包还不存在）

- [ ] **Step 3: 写 `internal/userconfig/userconfig.go`**

```go
// Package userconfig 管理 BrickKit CLI 第一份"项目之外"的状态：机器级、
// 跨项目的用户偏好（目前只有显示语言一项）。
//
// BrickKit 到目前为止的所有状态都长在某个项目的 .brickkit/ 目录里
// （internal/config.Layout 的每条路径都相对项目 Root 推导）。但语言偏好
// 天然是机器级的——brickkit init、brickkit version、brickkit login 这些
// 命令本来就可能在还没有项目、或不在任何项目目录里执行，语言偏好没有
// 项目目录可以寄存。
package userconfig

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// EnvDirOverride 覆盖 Dir() 的返回值，仅供测试使用——避免测试读到/写到
// 跑测试的人自己机器上真实的全局配置（这是 BrickKit 第一次引入跨项目
// 状态，需要这样一个隔离开关；其余状态都在某个项目的 .brickkit/ 里，
// 测试用 t.TempDir() 当项目根目录就已经隔离了，不需要它）。
const EnvDirOverride = "BRICKKIT_USERCONFIG_DIR"

const fileName = "config.json"

// Config 是全局配置文件的内容。
type Config struct {
	Lang string `json:"lang"`
}

// Dir 返回全局配置目录：跨平台正确（Linux 走 XDG_CONFIG_HOME 或
// ~/.config，macOS 走 ~/Library/Application Support），可用
// EnvDirOverride 覆盖。
func Dir() (string, error) {
	if v := os.Getenv(EnvDirOverride); v != "" {
		return v, nil
	}
	base, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "brickkit"), nil
}

// Path 返回全局配置文件的完整路径。
func Path() (string, error) {
	dir, err := Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, fileName), nil
}

// Load 读取全局配置。文件不存在时返回空值而不是错误——还没设置过语言
// 等价于"用默认值"，跟 internal/source.LoadCredentials 的"未登录不是
// 错误"是同一个道理。
func Load() (*Config, error) {
	path, err := Path()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	switch {
	case os.IsNotExist(err):
		return &Config{}, nil
	case err != nil:
		return nil, err
	}
	var c Config
	if err := json.Unmarshal(data, &c); err != nil {
		return nil, err
	}
	return &c, nil
}

// Save 写入全局配置。先写临时文件再 rename——写到一半失败不会把已有的
// 有效配置毁掉，跟 internal/source/credentials.go 的写法一致。语言偏好
// 不是密钥，文件权限用 0644（credentials.go 的 0600 是因为那是明文
// Token，这里不需要）。
func Save(c *Config) error {
	dir, err := Dir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')

	path := filepath.Join(dir, fileName)
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}
```

- [ ] **Step 4: 跑测试确认通过**

Run: `go test ./internal/userconfig/... -v`
Expected: PASS（全部 7 个测试）

- [ ] **Step 5: Commit**

```bash
git add internal/userconfig
git commit -m "$(cat <<'EOF'
新增：internal/userconfig 全局语言偏好存储

BrickKit 第一次引入项目之外的状态：os.UserConfigDir()/brickkit/config.json，
原子写入，测试用 BRICKKIT_USERCONFIG_DIR 隔离。还没接进语言解析逻辑。

Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>
EOF
)"
```

---

### Task 4: `internal/i18n.Resolve()`（优先级解析）

**Files:**
- Create: `internal/i18n/resolve.go`
- Create: `internal/i18n/resolve_test.go`

**Interfaces:**
- Consumes: `userconfig.Load()`、`userconfig.EnvDirOverride`（Task 3）
- Produces: `i18n.Source`（`SourceEnv`/`SourceConfig`/`SourceDefault`）、`i18n.Resolve() (Lang, Source)`、`i18n.ParseLang(string) (Lang, bool)`、`i18n.SupportedLangs() []Lang`、`i18n.LangNames() []string`、`i18n.EnvLang`（常量）

- [ ] **Step 1: 先写测试 `internal/i18n/resolve_test.go`**

```go
package i18n

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/brickkit/brickkit/internal/userconfig"
)

func TestParseLangAcceptsKnownValues(t *testing.T) {
	l, ok := ParseLang("en")
	assert.True(t, ok)
	assert.Equal(t, EN, l)

	l, ok = ParseLang(" ZH ")
	assert.True(t, ok)
	assert.Equal(t, ZH, l, "大小写与前后空白都要容错")
}

func TestParseLangRejectsUnknownValue(t *testing.T) {
	_, ok := ParseLang("fr")
	assert.False(t, ok)
}

func TestSupportedLangsAndNames(t *testing.T) {
	assert.Equal(t, []Lang{EN, ZH}, SupportedLangs())
	assert.Equal(t, []string{"en", "zh"}, LangNames())
}

func TestResolveDefaultsToEnglishWhenNothingConfigured(t *testing.T) {
	t.Setenv(EnvLang, "")
	t.Setenv(userconfig.EnvDirOverride, t.TempDir())

	lang, source := Resolve()
	assert.Equal(t, EN, lang)
	assert.Equal(t, SourceDefault, source)
}

func TestResolveEnvVarTakesPriorityOverConfig(t *testing.T) {
	t.Setenv(userconfig.EnvDirOverride, t.TempDir())
	require.NoError(t, userconfig.Save(&userconfig.Config{Lang: "zh"}))
	t.Setenv(EnvLang, "en")

	lang, source := Resolve()
	assert.Equal(t, EN, lang)
	assert.Equal(t, SourceEnv, source)
}

func TestResolveFallsBackToConfigWhenEnvUnset(t *testing.T) {
	t.Setenv(EnvLang, "")
	t.Setenv(userconfig.EnvDirOverride, t.TempDir())
	require.NoError(t, userconfig.Save(&userconfig.Config{Lang: "zh"}))

	lang, source := Resolve()
	assert.Equal(t, ZH, lang)
	assert.Equal(t, SourceConfig, source)
}

func TestResolveIgnoresUnsupportedEnvValueAndFallsThrough(t *testing.T) {
	t.Setenv(userconfig.EnvDirOverride, t.TempDir())
	require.NoError(t, userconfig.Save(&userconfig.Config{Lang: "zh"}))
	t.Setenv(EnvLang, "fr")

	lang, source := Resolve()
	assert.Equal(t, ZH, lang, "不认识的 BRICKKIT_LANG 值当作没设置，往下一层找，不阻断启动")
	assert.Equal(t, SourceConfig, source)
}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./internal/i18n/... -run 'TestParseLang|TestSupportedLangs|TestResolve' -v`
Expected: 编译失败（`Resolve`/`ParseLang`/`SupportedLangs`/`LangNames`/`Source*`/`EnvLang` 未定义）

- [ ] **Step 3: 写 `internal/i18n/resolve.go`**

```go
package i18n

import (
	"os"
	"strings"

	"github.com/brickkit/brickkit/internal/userconfig"
)

// EnvLang 是覆盖当前语言的环境变量，优先级最高——适合 CI 与一次性调用
// （BRICKKIT_LANG=en brickkit up），不需要 --lang 命令行参数：语言必须
// 在 cobra 搭命令树之前就确定，命令行参数要等 cobra 解析完才能拿到值。
const EnvLang = "BRICKKIT_LANG"

// Source 标记一次语言解析结果来自哪一层，供 `brickkit lang` 展示给用户。
type Source string

const (
	SourceEnv     Source = "env"
	SourceConfig  Source = "config"
	SourceDefault Source = "default"
)

// ParseLang 校验字符串是否是已支持的语言，容忍大小写与前后空白。
func ParseLang(s string) (Lang, bool) {
	switch Lang(strings.ToLower(strings.TrimSpace(s))) {
	case EN:
		return EN, true
	case ZH:
		return ZH, true
	default:
		return "", false
	}
}

// SupportedLangs 返回全部已支持语言，顺序固定。
func SupportedLangs() []Lang {
	return []Lang{EN, ZH}
}

// LangNames 是 SupportedLangs 的字符串形式，用于拼错误提示。
func LangNames() []string {
	langs := SupportedLangs()
	names := make([]string, len(langs))
	for i, l := range langs {
		names[i] = string(l)
	}
	return names
}

// Resolve 按优先级解析当前应该使用的语言：
// BRICKKIT_LANG 环境变量 > 全局配置文件（brickkit lang set 写入）> 默认英语。
//
// BRICKKIT_LANG 或全局配置文件里的值如果不是已支持的语言，当作没设置、
// 继续往下一层找——这不是"静默吞掉一个会导致悄悄用错服务的坑"（§9.13
// 那一类），只是退回默认语言，用户会立刻从看到的文字里发现，代价很小，
// 换来的是不需要在命令树建好之前就有能力中断整个进程去报一个用法错误。
func Resolve() (Lang, Source) {
	if v := strings.TrimSpace(os.Getenv(EnvLang)); v != "" {
		if l, ok := ParseLang(v); ok {
			return l, SourceEnv
		}
	}
	if cfg, err := userconfig.Load(); err == nil && cfg != nil {
		if l, ok := ParseLang(cfg.Lang); ok {
			return l, SourceConfig
		}
	}
	return EN, SourceDefault
}
```

- [ ] **Step 4: 跑测试确认通过**

Run: `go test ./internal/i18n/... -v`
Expected: PASS（`internal/i18n` 全部测试，含 Task 1 与本任务的）

- [ ] **Step 5: Commit**

```bash
git add internal/i18n
git commit -m "$(cat <<'EOF'
新增：internal/i18n.Resolve() 语言优先级解析

BRICKKIT_LANG 环境变量 > 全局配置文件 > 默认英语；不认识的值当作没
设置、往下找，不阻断启动。还没接进 CLI 命令树。

Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>
EOF
)"
```

---

### Task 5: 纵向切片①——`brickkit version` 走 i18n

**Files:**
- Modify: `internal/i18n/catalog_en.go`
- Modify: `internal/i18n/catalog_zh.go`
- Modify: `internal/msgid/msgid.go`
- Modify: `internal/cli/version.go`
- Modify: `internal/cli/cli_test.go`
- Modify: `internal/cli/execute_test.go`

**Interfaces:**
- Consumes: `i18n.T`（Task 1）
- Produces: 无新增导出符号；`brickkit version` 的三行输出（含 `--verbose` 追加的两行）从此按语言变化，`"BrickKit CLI %s"` 这一行不变（纯产品名+版本号，不含任何语言相关词）

- [ ] **Step 1: 在 `internal/msgid/msgid.go` 追加常量**

```go
	// internal/cli/version.go
	VersionManifestLine  = "version.manifest_line"
	VersionTargetsLine   = "version.targets_line"
	VersionCommitLine    = "version.commit_line"
	VersionBuildDateLine = "version.build_date_line"
```

- [ ] **Step 2: 在 `internal/i18n/catalog_en.go` 的 map 里追加**

```go
	msgid.VersionManifestLine:  "Supported Manifest version: %[1]s",
	msgid.VersionTargetsLine:   "Supported deploy targets: %[1]s",
	msgid.VersionCommitLine:    "Git commit: %[1]s",
	msgid.VersionBuildDateLine: "Build date: %[1]s",
```

- [ ] **Step 3: 在 `internal/i18n/catalog_zh.go` 的 map 里追加**

```go
	msgid.VersionManifestLine:  "支持 Manifest 版本：%[1]s",
	msgid.VersionTargetsLine:   "支持部署目标：%[1]s",
	msgid.VersionCommitLine:    "Git commit：%[1]s",
	msgid.VersionBuildDateLine: "构建时间：%[1]s",
```

- [ ] **Step 4: 跑 `TestCatalogParity` 确认两份目录仍然对齐**

Run: `go test ./internal/i18n/... -run TestCatalogParity -v`
Expected: PASS

- [ ] **Step 5: 改 `internal/cli/cli_test.go` 与 `internal/cli/execute_test.go` 的断言（先改测试，确认它们此刻会失败）**

把 `cli_test.go` 里：

```go
func TestVersionCommand(t *testing.T) {
	r := run(t, "version")
	assert.Equal(t, clierr.ExitOK, r.code)
	assert.Contains(t, r.stdout, "BrickKit CLI v")
	assert.Contains(t, r.stdout, "支持 Manifest 版本：brickkit/v1")
	assert.Contains(t, r.stdout, "支持部署目标：docker, k8s")
}

func TestVersionVerboseAddsBuildInfo(t *testing.T) {
	r := run(t, "version", "--verbose")
	assert.Equal(t, clierr.ExitOK, r.code)
	assert.Contains(t, r.stdout, "Git commit：")
	assert.Contains(t, r.stdout, "构建时间：")
}
```

改成：

```go
func TestVersionCommand(t *testing.T) {
	r := run(t, "version")
	assert.Equal(t, clierr.ExitOK, r.code)
	assert.Contains(t, r.stdout, "BrickKit CLI v")
	assert.Contains(t, r.stdout, "Supported Manifest version: brickkit/v1")
	assert.Contains(t, r.stdout, "Supported deploy targets: docker, k8s")
}

func TestVersionVerboseAddsBuildInfo(t *testing.T) {
	r := run(t, "version", "--verbose")
	assert.Equal(t, clierr.ExitOK, r.code)
	assert.Contains(t, r.stdout, "Git commit:")
	assert.Contains(t, r.stdout, "Build date:")
}
```

把 `execute_test.go` 里的：

```go
	// --log-level off 让 stderr 不产生日志，避免污染测试输出。
	os.Args = []string{"brickkit", "version", "--log-level", "off"}
	code := Execute()

	require.NoError(t, w.Close())
	out, err := io.ReadAll(r)
	require.NoError(t, err)
	require.NoError(t, r.Close())

	assert.Equal(t, clierr.ExitOK, code)
	assert.Contains(t, string(out), "BrickKit CLI v")
	assert.Contains(t, string(out), "支持部署目标：docker, k8s")
}
```

改成（多加两行环境隔离，避免这条测试的结果取决于跑测试的机器上是否真的设过 `BRICKKIT_LANG` 或跑过 `brickkit lang set`）：

```go
	// 隔离语言解析：这条测试走的是真实 Execute()，会真的调用
	// i18n.Resolve()——不隔离的话，结果取决于跑测试的人自己的机器上
	// 是否设了 BRICKKIT_LANG、或者是否真的执行过 brickkit lang set。
	t.Setenv("BRICKKIT_LANG", "")
	t.Setenv(userconfig.EnvDirOverride, t.TempDir())

	// --log-level off 让 stderr 不产生日志，避免污染测试输出。
	os.Args = []string{"brickkit", "version", "--log-level", "off"}
	code := Execute()

	require.NoError(t, w.Close())
	out, err := io.ReadAll(r)
	require.NoError(t, err)
	require.NoError(t, r.Close())

	assert.Equal(t, clierr.ExitOK, code)
	assert.Contains(t, string(out), "BrickKit CLI v")
	assert.Contains(t, string(out), "Supported deploy targets: docker, k8s")
}
```

并在 `execute_test.go` 的 import 块里加上：

```go
	"github.com/brickkit/brickkit/internal/userconfig"
```

- [ ] **Step 6: 跑测试确认失败（还没改 version.go）**

Run: `go test ./internal/cli/... -run 'TestVersionCommand|TestVersionVerboseAddsBuildInfo|TestExecuteReadsOSArgs' -v`
Expected: 三个都 FAIL（实际输出仍是中文）

- [ ] **Step 7: 改 `internal/cli/version.go`**

把：

```go
package cli

import (
	"github.com/spf13/cobra"

	"github.com/brickkit/brickkit/internal/version"
)

// newVersionCommand 实现 brickkit version（004 §11.3）。
//
// 输出格式严格对齐设计书：
//
//	BrickKit CLI v1.0.0
//	支持 Manifest 版本：brickkit/v1
//	支持部署目标：docker, k8s
func newVersionCommand(opts *Options) *cobra.Command {
	var verbose bool

	cmd := &cobra.Command{
		Use:   "version",
		Short: "查看 CLI 版本、支持的 Manifest 版本与部署目标",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			opts.Printf("BrickKit CLI %s\n", version.Display())
			opts.Printf("支持 Manifest 版本：%s\n", version.ManifestAPIVersion)
			opts.Printf("支持部署目标：%s\n", version.SupportedTargets())
			if verbose {
				opts.Printf("Git commit：%s\n", version.Commit)
				opts.Printf("构建时间：%s\n", version.BuildDate)
			}
			return nil
		},
	}

	cmd.Flags().BoolVarP(&verbose, "verbose", "v", false, "额外输出 Git commit 与构建时间")
	return cmd
}
```

改成：

```go
package cli

import (
	"github.com/spf13/cobra"

	"github.com/brickkit/brickkit/internal/i18n"
	"github.com/brickkit/brickkit/internal/msgid"
	"github.com/brickkit/brickkit/internal/version"
)

// newVersionCommand 实现 brickkit version（004 §11.3）。
//
// 输出格式严格对齐设计书（中文示例，英文按 internal/i18n 目录对应变化）：
//
//	BrickKit CLI v1.0.0
//	支持 Manifest 版本：brickkit/v1
//	支持部署目标：docker, k8s
//
// "BrickKit CLI %s" 这一行不接 i18n：纯产品名 + 版本号，不含任何语言
// 相关的词。Short 与 --verbose 的参数说明仍是硬编码中文，留给子项目 2
// （全量迁移）处理——本命令只转换实际打印给用户看的三行输出。
func newVersionCommand(opts *Options) *cobra.Command {
	var verbose bool

	cmd := &cobra.Command{
		Use:   "version",
		Short: "查看 CLI 版本、支持的 Manifest 版本与部署目标",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			opts.Printf("BrickKit CLI %s\n", version.Display())
			opts.Printf("%s\n", i18n.T(msgid.VersionManifestLine, version.ManifestAPIVersion))
			opts.Printf("%s\n", i18n.T(msgid.VersionTargetsLine, version.SupportedTargets()))
			if verbose {
				opts.Printf("%s\n", i18n.T(msgid.VersionCommitLine, version.Commit))
				opts.Printf("%s\n", i18n.T(msgid.VersionBuildDateLine, version.BuildDate))
			}
			return nil
		},
	}

	cmd.Flags().BoolVarP(&verbose, "verbose", "v", false, "额外输出 Git commit 与构建时间")
	return cmd
}
```

- [ ] **Step 8: 跑测试确认通过**

Run: `go test ./internal/cli/... -run 'TestVersionCommand|TestVersionVerboseAddsBuildInfo|TestExecuteReadsOSArgs' -v`
Expected: PASS（三个）

- [ ] **Step 9: 跑真实二进制确认真的是英文**

Run: `go build -o /tmp/brickkit-i18n-check ./cmd/brickkit && /tmp/brickkit-i18n-check version --verbose --log-level off`
Expected: 四行输出全是英文（`BrickKit CLI v...` / `Supported Manifest version: brickkit/v1` / `Supported deploy targets: docker, k8s` / `Git commit: ...` / `Build date: ...`）

- [ ] **Step 10: Commit**

```bash
git add internal/msgid internal/i18n internal/cli/version.go internal/cli/cli_test.go internal/cli/execute_test.go
git commit -m "$(cat <<'EOF'
改进：brickkit version 走 i18n 目录，默认英文输出

纵向切片①：纯 Printf 输出这一种文案形态。"BrickKit CLI %s" 那一行
不需要翻译（纯产品名+版本号）。Short/--verbose 参数说明仍是中文，
留给全量迁移。

Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>
EOF
)"
```

---

### Task 6: 纵向切片②——`PROJECT_MISSING` 走 i18n，并教会错误码文档核对测试认得它

**Files:**
- Modify: `internal/msgid/msgid.go`
- Modify: `internal/i18n/catalog_en.go`
- Modify: `internal/i18n/catalog_zh.go`
- Modify: `internal/config/parse.go`
- Modify: `internal/config/config_test.go`
- Modify: `tests/docfields/errorcodes_test.go`

**Interfaces:**
- Consumes: `i18n.T`、`i18n.CatalogFor`（Task 1）
- Produces: `msgid.ProjectMissing`、`msgid.LabelPath`、`msgid.ProjectMissingHintInit`、`msgid.ProjectMissingHintConfig`；`tests/docfields` 内部新增 `msgidKeys(t)`、`i18nCallTitle(...)` 两个测试专用 helper（仅在该包内使用）

- [ ] **Step 1: 在 `internal/msgid/msgid.go` 追加常量**

```go
	// internal/config/parse.go：PROJECT_MISSING
	ProjectMissing           = "project.missing"
	LabelPath                = "label.path"
	ProjectMissingHintInit   = "project.missing.hint.init"
	ProjectMissingHintConfig = "project.missing.hint.config"
```

- [ ] **Step 2: 在 `internal/i18n/catalog_en.go` 的 map 里追加**

```go
	msgid.ProjectMissing:           "Error: project config file not found",
	msgid.LabelPath:                "Path",
	msgid.ProjectMissingHintInit:   "Run brickkit init <project-name> inside the project directory to initialize it",
	msgid.ProjectMissingHintConfig: "Or point --config at the correct config file path",
```

- [ ] **Step 3: 在 `internal/i18n/catalog_zh.go` 的 map 里追加（文字必须跟 `parse.go` 现在的原文一字不差——docs/en、docs/zh 的 `10-error-codes.md` 都还照抄着这句中文，本任务不碰文档，靠这里的文字对齐让 Step 8 的文档核对测试继续通过）**

```go
	msgid.ProjectMissing:           "错误：项目配置文件不存在",
	msgid.LabelPath:                "路径",
	msgid.ProjectMissingHintInit:   "在项目目录中执行 brickkit init <项目名称> 初始化项目",
	msgid.ProjectMissingHintConfig: "或用 --config 指定正确的配置文件路径",
```

- [ ] **Step 4: 跑 `TestCatalogParity` 确认对齐**

Run: `go test ./internal/i18n/... -run TestCatalogParity -v`
Expected: PASS

- [ ] **Step 5: 改 `internal/config/config_test.go` 里的断言（先改测试）**

把 `TestParseConfigFileNotExist`：

```go
func TestParseConfigFileNotExist(t *testing.T) {
	_, err := ParseConfigFile(filepath.Join("testdata", "nope.yaml"))
	require.Error(t, err)

	out := clierr.As(err).Format()
	assert.Contains(t, out, "不存在")
	assert.Contains(t, out, "brickkit init", "应提示先初始化项目")
}
```

改成：

```go
func TestParseConfigFileNotExist(t *testing.T) {
	_, err := ParseConfigFile(filepath.Join("testdata", "nope.yaml"))
	require.Error(t, err)

	out := clierr.As(err).Format()
	assert.Contains(t, out, "not found")
	assert.Contains(t, out, "brickkit init", "应提示先初始化项目")
}
```

（`internal/config/edge_test.go` 里的 `TestParseConfigFileMissingUsesProjectMissingCode` 只断言 `clierr.CodeProjectMissing` 这个 Code，不断言文案内容，不用改。）

- [ ] **Step 6: 跑测试确认失败（还没改 parse.go）**

Run: `go test ./internal/config/... -run TestParseConfigFileNotExist -v`
Expected: FAIL（实际输出仍是中文"不存在"）

- [ ] **Step 7: 改 `internal/config/parse.go`**

在 import 块里加：

```go
	"github.com/brickkit/brickkit/internal/i18n"
	"github.com/brickkit/brickkit/internal/msgid"
```

把：

```go
	case os.IsNotExist(err):
		return nil, clierr.New(clierr.CodeProjectMissing, "错误：项目配置文件不存在").
			WithDetail("路径", path).
			WithHint(
				"在项目目录中执行 brickkit init <项目名称> 初始化项目",
				"或用 --config 指定正确的配置文件路径",
			).WithCause(err)
```

改成：

```go
	case os.IsNotExist(err):
		return nil, clierr.New(clierr.CodeProjectMissing, i18n.T(msgid.ProjectMissing)).
			WithDetail(i18n.T(msgid.LabelPath), path).
			WithHint(
				i18n.T(msgid.ProjectMissingHintInit),
				i18n.T(msgid.ProjectMissingHintConfig),
			).WithCause(err)
```

- [ ] **Step 8: 跑测试——`internal/config` 应该转绿，`tests/docfields` 应该转红（预期之内）**

Run: `go test ./internal/config/... -run TestParseConfigFileNotExist -v`
Expected: PASS

Run: `go test ./tests/docfields/... -run TestErrorCodesDocTitlesExistInSource -v`
Expected: **FAIL**——`docs/en/06-architecture/10-error-codes.md` 与 `docs/zh/06-architecture/10-error-codes.md` 里的 `` `错误：项目配置文件不存在` `` 这个标题，现在源码里找不到对应的字符串字面量了（变成了 `i18n.T(msgid.ProjectMissing)` 这个函数调用，`go/ast` 的字面量提取器认不出来）。这是本任务开头说明的第二个连带问题，下面几步来修。

- [ ] **Step 9: 先写测试，证明"还原 `i18n.T(msgid.X)` 调用"这件事本身是对的**

在 `tests/docfields/errorcodes_test.go` 的 import 块里加：

```go
	"github.com/brickkit/brickkit/internal/i18n"
```

在文件末尾追加：

```go
func TestI18nCallTitleResolvesKnownMessage(t *testing.T) {
	msgidToKey := msgidKeys(t)
	zhCatalog := i18n.CatalogFor(i18n.ZH)

	fset := token.NewFileSet()
	expr, err := parser.ParseExprFrom(fset, "", `i18n.T(msgid.ProjectMissing)`, 0)
	require.NoError(t, err)
	call, ok := expr.(*ast.CallExpr)
	require.True(t, ok)

	title, ok := i18nCallTitle(call, msgidToKey, zhCatalog)
	require.True(t, ok)
	assert.Equal(t, "错误：项目配置文件不存在", title)
}

func TestI18nCallTitleRejectsUnrelatedCalls(t *testing.T) {
	msgidToKey := msgidKeys(t)
	zhCatalog := i18n.CatalogFor(i18n.ZH)

	fset := token.NewFileSet()
	expr, err := parser.ParseExprFrom(fset, "", `fmt.Sprintf("x")`, 0)
	require.NoError(t, err)
	call, ok := expr.(*ast.CallExpr)
	require.True(t, ok)

	_, ok = i18nCallTitle(call, msgidToKey, zhCatalog)
	assert.False(t, ok, "不是 i18n.T(msgid.X) 形状的调用要直接放行返回 false，不能误判")
}
```

- [ ] **Step 10: 跑测试确认编译失败**

Run: `go test ./tests/docfields/... -run TestI18nCallTitle -v`
Expected: 编译失败（`msgidKeys`/`i18nCallTitle` 未定义）

- [ ] **Step 11: 在 `tests/docfields/errorcodes_test.go` 里实现 `msgidKeys` 与 `i18nCallTitle`，并接进 `sourceTitles`**

在 `clierrCodes` 函数后面追加：

```go
// msgidKeys 从 internal/msgid 源码里取出常量名 → key 字符串值的映射
// （msgid.ProjectMissing -> "project.missing"），用来把 i18n.T(msgid.X)
// 调用还原成它实际查到的文案。跟 clierrCodes() 是同一个手法：真相来自
// 源码本身，不是又抄一份清单。
func msgidKeys(t *testing.T) map[string]string {
	t.Helper()
	dir := filepath.Join(repoRoot, "internal", "msgid")
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)

	re := regexp.MustCompile(`(?m)^\s*(\w+)\s*=\s*"([^"]+)"`)
	out := map[string]string{}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") || strings.HasSuffix(entry.Name(), "_test.go") {
			continue
		}
		body, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		require.NoError(t, err)
		for _, m := range re.FindAllStringSubmatch(string(body), -1) {
			out[m[1]] = m[2]
		}
	}
	return out
}

// i18nCallTitle 把 i18n.T(msgid.Name, ...) 形状的调用还原成它在 zh 目录里
// 的实际文案。错误码文档现在两侧（docs/en 与 docs/zh）都还照抄同一句
// 中文原文（子项目 3 才会改成两侧各自的真实语言），所以这里统一按 zh
// 目录解析，跟文档现状对齐；子项目 3 改变文档约定之后，这里要跟着改成
// 按文档所在的语言分别解析。
func i18nCallTitle(call *ast.CallExpr, msgidToKey map[string]string, zhCatalog map[string]string) (string, bool) {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return "", false
	}
	pkg, ok := sel.X.(*ast.Ident)
	if !ok || pkg.Name != "i18n" || sel.Sel.Name != "T" || len(call.Args) < 1 {
		return "", false
	}
	idSel, ok := call.Args[0].(*ast.SelectorExpr)
	if !ok {
		return "", false
	}
	idPkg, ok := idSel.X.(*ast.Ident)
	if !ok || idPkg.Name != "msgid" {
		return "", false
	}
	key, ok := msgidToKey[idSel.Sel.Name]
	if !ok {
		return "", false
	}
	text, ok := zhCatalog[key]
	return text, ok
}
```

再改 `sourceTitles`：把

```go
func sourceTitles(t *testing.T) []string {
	t.Helper()
	fset := token.NewFileSet()
	var out []string
	for _, dir := range []string{"internal", "cmd"} {
		err := filepath.WalkDir(filepath.Join(repoRoot, dir), func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return err
			}
			file, err := parser.ParseFile(fset, path, nil, 0)
			require.NoError(t, err, path)
			ast.Inspect(file, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok || len(call.Args) < 2 {
					return true
				}
				sel, ok := call.Fun.(*ast.SelectorExpr)
				if !ok {
					return true
				}
				pkg, ok := sel.X.(*ast.Ident)
				if !ok || pkg.Name != "clierr" {
					return true
				}
				switch sel.Sel.Name {
				case "New", "Newf", "Warn", "NewProblemSet":
				default:
					return true
				}
				if title, ok := titleLiteral(call.Args[1]); ok {
					// 源码标题里本来就带尖括号的（"init <项目名称>"）也归一成占位符，
					// 与文档那一侧的归一化对称。
					title = formatVerb.ReplaceAllString(title, titlePlaceholder)
					out = append(out, docPlaceholder.ReplaceAllString(title, titlePlaceholder))
				}
				return true
			})
			return nil
		})
		require.NoError(t, err)
	}
	return out
}
```

改成：

```go
func sourceTitles(t *testing.T) []string {
	t.Helper()
	msgidToKey := msgidKeys(t)
	zhCatalog := i18n.CatalogFor(i18n.ZH)
	fset := token.NewFileSet()
	var out []string
	for _, dir := range []string{"internal", "cmd"} {
		err := filepath.WalkDir(filepath.Join(repoRoot, dir), func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return err
			}
			file, err := parser.ParseFile(fset, path, nil, 0)
			require.NoError(t, err, path)
			ast.Inspect(file, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok || len(call.Args) < 2 {
					return true
				}
				sel, ok := call.Fun.(*ast.SelectorExpr)
				if !ok {
					return true
				}
				pkg, ok := sel.X.(*ast.Ident)
				if !ok || pkg.Name != "clierr" {
					return true
				}
				switch sel.Sel.Name {
				case "New", "Newf", "Warn", "NewProblemSet":
				default:
					return true
				}
				title, ok := titleLiteral(call.Args[1])
				if !ok {
					if inner, isCall := call.Args[1].(*ast.CallExpr); isCall {
						title, ok = i18nCallTitle(inner, msgidToKey, zhCatalog)
					}
				}
				if ok {
					// 源码标题里本来就带尖括号的（"init <项目名称>"）也归一成占位符，
					// 与文档那一侧的归一化对称。
					title = formatVerb.ReplaceAllString(title, titlePlaceholder)
					out = append(out, docPlaceholder.ReplaceAllString(title, titlePlaceholder))
				}
				return true
			})
			return nil
		})
		require.NoError(t, err)
	}
	return out
}
```

- [ ] **Step 12: 顺手修 `formatVerb` 正则，让它认得位置 verb（`%[1]s`）**

本任务转换的 `PROJECT_MISSING` 没有参数，不会触发这个问题；但目录里其他带参数的 key（Task 5 已经加了好几个）一旦被某条错误引用并写进文档，现在的 `formatVerb` 正则会漏判——它认不出 `%[1]s` 这种显式参数索引写法，只认 `%s`/`%d` 这种传统写法。趁着这次已经在改这个文件，一并修掉，子项目 2 转换带参数的消息时就不用再回来碰这个正则。

把：

```go
	formatVerb = regexp.MustCompile(`%[-+# 0-9.]*[a-zA-Z]`)
```

改成：

```go
	formatVerb = regexp.MustCompile(`%(?:\[\d+\])?[-+# 0-9.]*[a-zA-Z]`)
```

在 `TestErrorTitleMatcher` 测试函数末尾追加一个子测试（跟着已有测试的写法走，具体断言方式以该测试当前的写法为准，加一行验证 `formatVerb.ReplaceAllString("%[1]s 已启动", titlePlaceholder+" 已启动")` 能正确把 `%[1]s` 换成占位符）：

```go
func TestFormatVerbMatchesPositionalVerbs(t *testing.T) {
	assert.Equal(t, titlePlaceholder+" 已启动", formatVerb.ReplaceAllString("%[1]s 已启动", titlePlaceholder))
	assert.Equal(t, titlePlaceholder+" 个", formatVerb.ReplaceAllString("%d 个", titlePlaceholder))
}
```

- [ ] **Step 13: 跑全部 docfields 测试确认恢复全绿**

Run: `go test ./tests/docfields/... -v`
Expected: PASS（全部，包括 `TestErrorCodesDocCoversEveryCode`、`TestErrorCodesDocTitlesExistInSource`、新增的 `TestI18nCallTitleResolvesKnownMessage`、`TestI18nCallTitleRejectsUnrelatedCalls`、`TestFormatVerbMatchesPositionalVerbs`）

- [ ] **Step 14: 跑真实二进制确认 `brickkit up` 在空目录下报英文错误**

Run: `mkdir -p /tmp/brickkit-i18n-empty && cd /tmp/brickkit-i18n-empty && /tmp/brickkit-i18n-check up --log-level off; cd -`
Expected: stderr 输出形如：

```
❌ Error: project config file not found
   Path: brickkit.yaml
   Suggestions:
   1. Run brickkit init <project-name> inside the project directory to initialize it
   2. Or point --config at the correct config file path
```

- [ ] **Step 15: Commit**

```bash
git add internal/msgid internal/i18n internal/config tests/docfields
git commit -m "$(cat <<'EOF'
改进：PROJECT_MISSING 走 i18n；docfields 错误码标题核对认得 i18n.T 调用

纵向切片②：clierr.Error 结构化输出这一种文案形态。tests/docfields 靠
go/ast 静态解析 clierr.New(...) 的第二个参数核对错误码文档标题，原本
只认字符串字面量——教会它把 i18n.T(msgid.X) 还原成 zh 目录里的文案
（文档现在两侧都还照抄中文原文，子项目 3 才会改这个约定）。顺手修了
formatVerb 正则，让它认得 %[1]s 这种位置 verb，子项目 2 转换带参数的
消息时不用再回来碰。

Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>
EOF
)"
```

---

### Task 7: 纵向切片③——root 命令 `--help` 默认英文，中文模板按语言切换

**Files:**
- Modify: `internal/cli/root.go`
- Modify: `internal/cli/cli_test.go`

**Interfaces:**
- Consumes: `i18n.Resolve()`、`i18n.SetCurrent()`、`i18n.Current()`（Task 1、4）
- Produces: `NewRootCommand` 现在每次调用都会重新解析并设置当前语言（跟 `internal/logging.Init` 在每次 `Run()` 都重新初始化是同一种用法），`run()` 测试 helper 隔离全局配置目录

- [ ] **Step 1: 改 `internal/cli/cli_test.go` 的共享测试 helper `run()`，隔离全局配置目录**

把：

```go
// run 在隔离的缓冲区与临时目录上执行一次 CLI。
//
// WorkDir 必须指向临时目录：否则会写到测试进程的当前目录（源码目录）里去。
func run(t *testing.T, args ...string) result {
	t.Helper()
	var out, errBuf bytes.Buffer
	opts := &Options{
		WorkDir:    t.TempDir(),
		ConfigPath: DefaultConfigFile,
		LogLevel:   logging.LevelInfo,
		Stdout:     &out,
		Stderr:     &errBuf,
	}
	code := Run(NewRootCommand(opts), opts, args)
	return result{stdout: out.String(), stderr: errBuf.String(), code: code}
}
```

改成：

```go
// run 在隔离的缓冲区与临时目录上执行一次 CLI。
//
// WorkDir 必须指向临时目录：否则会写到测试进程的当前目录（源码目录）里去。
//
// 顺手隔离全局语言配置目录：NewRootCommand 每次调用都会重新解析语言
// （见 root.go），如果不隔离，测试结果会取决于跑测试的人自己的机器上
// 是否真的执行过 brickkit lang set。只在调用方还没有自己设置过的情况下
// 才覆盖——需要跨多次 run() 调用验证"设置后持久生效"的测试，可以在
// 调用 run() 之前自己先 t.Setenv(userconfig.EnvDirOverride, ...)，
// 这里就不会覆盖掉。
func run(t *testing.T, args ...string) result {
	t.Helper()
	if os.Getenv(userconfig.EnvDirOverride) == "" {
		t.Setenv(userconfig.EnvDirOverride, t.TempDir())
	}
	var out, errBuf bytes.Buffer
	opts := &Options{
		WorkDir:    t.TempDir(),
		ConfigPath: DefaultConfigFile,
		LogLevel:   logging.LevelInfo,
		Stdout:     &out,
		Stderr:     &errBuf,
	}
	code := Run(NewRootCommand(opts), opts, args)
	return result{stdout: out.String(), stderr: errBuf.String(), code: code}
}
```

并在 `cli_test.go` 的 import 块里加上：

```go
	"os"

	"github.com/brickkit/brickkit/internal/userconfig"
```

再把 `TestNoArgsPrintsHelp` 与 `TestEachSubcommandHelp` 里的 `"用法："` 改成 `"Usage:"`：

```go
func TestNoArgsPrintsHelp(t *testing.T) {
	r := run(t)
	assert.Equal(t, clierr.ExitOK, r.code)
	assert.Contains(t, r.stdout, "Usage:")
	for _, name := range allCommands {
		assert.Contains(t, r.stdout, name)
	}
}
```

```go
func TestEachSubcommandHelp(t *testing.T) {
	for _, name := range allCommands {
		t.Run(name, func(t *testing.T) {
			r := run(t, name, "--help")
			assert.Equal(t, clierr.ExitOK, r.code)
			assert.Contains(t, r.stdout, "Usage:")
			assert.Contains(t, r.stdout, "brickkit "+name)
			assert.NotEmpty(t, strings.TrimSpace(r.stdout))
		})
	}
}
```

（`TestRootHelpListsAllCommands` 只检查每个命令的裸名字如 `"init"`、`"up"` 是否出现在 `--help` 里，这些名字来自 cobra 的 `Use` 字段、不受语言影响，不用改。）

新增一个测试证明中文路径也还工作：

```go
func TestRootHelpIsLocalizedWhenBrickkitLangIsZH(t *testing.T) {
	t.Setenv("BRICKKIT_LANG", "zh")
	r := run(t, "--help")
	assert.Equal(t, clierr.ExitOK, r.code)
	assert.Contains(t, r.stdout, "用法：")
}
```

- [ ] **Step 2: 跑测试确认失败（还没改 root.go）**

Run: `go test ./internal/cli/... -run 'TestNoArgsPrintsHelp|TestEachSubcommandHelp|TestRootHelpIsLocalizedWhenBrickkitLangIsZH' -v`
Expected: `TestNoArgsPrintsHelp`、`TestEachSubcommandHelp` FAIL（现在还是无条件中文模板）；`TestRootHelpIsLocalizedWhenBrickkitLangIsZH` PASS（现在本来就是中文，凑巧先绿，Step 4 之后应该仍然绿）

- [ ] **Step 3: 改 `internal/cli/root.go`**

在 import 块里加：

```go
	"github.com/brickkit/brickkit/internal/i18n"
```

把 `NewRootCommand` 开头：

```go
func NewRootCommand(opts *Options) *cobra.Command {
	if opts == nil {
		opts = NewOptions()
	}

	root := &cobra.Command{
```

改成：

```go
func NewRootCommand(opts *Options) *cobra.Command {
	if opts == nil {
		opts = NewOptions()
	}

	// 每次都重新解析并设置当前语言（跟 internal/logging.Init 在每次
	// Run() 都重新初始化是同一种用法）：命令树里每个子命令的 Short/Long
	// 是在这里构建时就写死的字符串，语言必须先确定下来。
	lang, _ := i18n.Resolve()
	i18n.SetCurrent(lang)

	root := &cobra.Command{
```

把函数末尾：

```go
	localize(root)
	return root
}
```

改成：

```go
	if lang == i18n.ZH {
		localize(root)
	}
	return root
}
```

- [ ] **Step 4: 跑测试确认通过**

Run: `go test ./internal/cli/... -run 'TestNoArgsPrintsHelp|TestEachSubcommandHelp|TestRootHelpIsLocalizedWhenBrickkitLangIsZH' -v`
Expected: PASS（三个）

- [ ] **Step 5: 跑整个 `internal/cli` 包的测试，确认没有牵连其他用到 `run()`/`NewRootCommand` 的测试**

Run: `go test ./internal/cli/... -v 2>&1 | tail -80`
Expected: 全部 PASS

- [ ] **Step 6: 跑真实二进制分别验证两种语言的 `--help`**

Run: `/tmp/brickkit-i18n-check --help | head -5`
Expected: 第一行是 `Usage:`（cobra 原生英文模板）

Run: `BRICKKIT_LANG=zh /tmp/brickkit-i18n-check --help | head -5`
Expected: 第一行是 `用法：`（现有的中文模板，行为不变）

- [ ] **Step 7: Commit**

```bash
git add internal/cli/root.go internal/cli/cli_test.go
git commit -m "$(cat <<'EOF'
改进：root 命令 --help 默认走 cobra 原生英文模板

纵向切片③：cobra 帮助模板这一种文案形态。英文模式什么都不用做——
cobra 自带的默认模板本来就是英文的；中文模式原样保留现有的
usageTemplate/localize()，只是从"无条件调用"改成"语言=zh 才调用"。
NewRootCommand 现在每次构建命令树都会重新解析语言，跟 logging.Init
每次 Run() 都重新初始化是同一种用法，run() 测试 helper 相应地隔离了
全局配置目录。

Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>
EOF
)"
```

---

### Task 8: `brickkit lang` / `brickkit lang set` 命令

**Files:**
- Modify: `internal/msgid/msgid.go`
- Modify: `internal/i18n/catalog_en.go`
- Modify: `internal/i18n/catalog_zh.go`
- Create: `internal/cli/lang.go`
- Create: `internal/cli/lang_test.go`
- Modify: `internal/cli/root.go`
- Modify: `scripts/check-cli-docs.py`

**Interfaces:**
- Consumes: `i18n.Resolve`、`i18n.ParseLang`、`i18n.SupportedLangs`、`i18n.SetCurrent`（Task 1、4）、`userconfig.Save`、`userconfig.Path`（Task 3）
- Produces: `newLangCommand(opts *Options) *cobra.Command`（挂进 `root.AddCommand`）

- [ ] **Step 1: 在 `internal/msgid/msgid.go` 追加常量**

```go
	// internal/cli/lang.go
	LangCmdShort      = "lang.cmd.short"
	LangSetCmdShort   = "lang.set_cmd.short"
	LangCurrentLine   = "lang.current_line"
	LangSourceEnv     = "lang.source.env"
	LangSourceConfig  = "lang.source.config"
	LangSourceDefault = "lang.source.default"
	LangSetSuccess    = "lang.set.success"
	LangSetWriteFailed = "lang.set.write_failed"
	LangInvalidValue  = "lang.invalid_value"
```

- [ ] **Step 2: 在 `internal/i18n/catalog_en.go` 的 map 里追加**

```go
	msgid.LangCmdShort:       "Show the CLI's current display language",
	msgid.LangSetCmdShort:    "Set the CLI's display language",
	msgid.LangCurrentLine:    "Current language: %[1]s (source: %[2]s)",
	msgid.LangSourceEnv:      "BRICKKIT_LANG environment variable",
	msgid.LangSourceConfig:   "global config file",
	msgid.LangSourceDefault:  "default",
	msgid.LangSetSuccess:     "Language set to %[1]s",
	msgid.LangSetWriteFailed: "Failed to save language preference",
	msgid.LangInvalidValue:   "Unsupported language: %[1]s (supported: %[2]s)",
```

- [ ] **Step 3: 在 `internal/i18n/catalog_zh.go` 的 map 里追加**

```go
	msgid.LangCmdShort:       "查看 CLI 当前的显示语言",
	msgid.LangSetCmdShort:    "设置 CLI 的显示语言",
	msgid.LangCurrentLine:    "当前语言：%[1]s（来源：%[2]s）",
	msgid.LangSourceEnv:      "BRICKKIT_LANG 环境变量",
	msgid.LangSourceConfig:   "全局配置文件",
	msgid.LangSourceDefault:  "默认值",
	msgid.LangSetSuccess:     "语言已设为 %[1]s",
	msgid.LangSetWriteFailed: "写入语言偏好失败",
	msgid.LangInvalidValue:   "不支持的语言：%[1]s（支持：%[2]s）",
```

- [ ] **Step 4: 跑 `TestCatalogParity` 确认对齐**

Run: `go test ./internal/i18n/... -run TestCatalogParity -v`
Expected: PASS

- [ ] **Step 5: 先写测试 `internal/cli/lang_test.go`**

```go
package cli

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/userconfig"
)

func TestLangShowsDefaultWhenNothingConfigured(t *testing.T) {
	r := run(t, "lang")
	assert.Equal(t, clierr.ExitOK, r.code)
	assert.Contains(t, r.stdout, "en")
	assert.Contains(t, r.stdout, "default")
}

func TestLangReflectsEnvOverride(t *testing.T) {
	t.Setenv("BRICKKIT_LANG", "zh")
	r := run(t, "lang")
	assert.Equal(t, clierr.ExitOK, r.code)
	assert.Contains(t, r.stdout, "zh")
}

func TestLangSetPersistsAcrossInvocations(t *testing.T) {
	// 用同一个全局配置目录跑两次调用，验证"设置后下次调用还生效"；
	// run() 只在调用方还没设置过 EnvDirOverride 时才会自己隔离一个，
	// 这里先设置好，run() 就不会覆盖它。
	t.Setenv(userconfig.EnvDirOverride, t.TempDir())

	setResult := run(t, "lang", "set", "zh")
	assert.Equal(t, clierr.ExitOK, setResult.code)
	assert.Contains(t, setResult.stdout, "zh")

	showResult := run(t, "lang")
	assert.Equal(t, clierr.ExitOK, showResult.code)
	assert.Contains(t, showResult.stdout, "zh")
	assert.Contains(t, showResult.stdout, "global config file")
}

func TestLangSetRejectsUnsupportedValue(t *testing.T) {
	r := run(t, "lang", "set", "fr")
	assert.Equal(t, clierr.ExitUsage, r.code)
	assert.Contains(t, r.stderr, "fr")
	assert.Contains(t, r.stderr, "en")
	assert.Contains(t, r.stderr, "zh")
}

func TestLangSetRequiresExactlyOneArg(t *testing.T) {
	r := run(t, "lang", "set")
	assert.Equal(t, clierr.ExitUsage, r.code)
}
```

- [ ] **Step 6: 跑测试确认失败**

Run: `go test ./internal/cli/... -run TestLang -v`
Expected: FAIL（`brickkit lang` 命令还不存在，会报 unknown command）

- [ ] **Step 7: 写 `internal/cli/lang.go`**

```go
package cli

import (
	"strings"

	"github.com/spf13/cobra"

	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/i18n"
	"github.com/brickkit/brickkit/internal/msgid"
	"github.com/brickkit/brickkit/internal/userconfig"
)

// newLangCommand 实现 brickkit lang 与 brickkit lang set（跟 version 一样
// 是 CLI 自身命令：不分组、不计入"业务命令"数）。
func newLangCommand(opts *Options) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "lang",
		Short: i18n.T(msgid.LangCmdShort),
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runLangShow(opts)
		},
	}
	cmd.AddCommand(&cobra.Command{
		Use:   "set <en|zh>",
		Short: i18n.T(msgid.LangSetCmdShort),
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runLangSet(opts, args[0])
		},
	})
	return cmd
}

func runLangShow(opts *Options) error {
	lang, source := i18n.Resolve()
	opts.Printf("%s\n", i18n.T(msgid.LangCurrentLine, string(lang), langSourceLabel(source)))
	return nil
}

func runLangSet(opts *Options, value string) error {
	lang, ok := i18n.ParseLang(value)
	if !ok {
		return clierr.Newf(clierr.CodeInvalidArgument, i18n.T(msgid.LangInvalidValue, value, langNamesJoined())).
			WithExit(clierr.ExitUsage)
	}

	if err := userconfig.Save(&userconfig.Config{Lang: string(lang)}); err != nil {
		e := clierr.New(clierr.CodeInternal, i18n.T(msgid.LangSetWriteFailed)).WithCause(err)
		if path, pathErr := userconfig.Path(); pathErr == nil {
			e = e.WithDetail(i18n.T(msgid.LabelPath), path)
		}
		return e
	}

	i18n.SetCurrent(lang)
	opts.Printf("✅ %s\n", i18n.T(msgid.LangSetSuccess, string(lang)))
	return nil
}

func langSourceLabel(s i18n.Source) string {
	switch s {
	case i18n.SourceEnv:
		return i18n.T(msgid.LangSourceEnv)
	case i18n.SourceConfig:
		return i18n.T(msgid.LangSourceConfig)
	default:
		return i18n.T(msgid.LangSourceDefault)
	}
}

func langNamesJoined() string {
	return strings.Join(i18n.LangNames(), ", ")
}
```

- [ ] **Step 8: 把 `newLangCommand(opts)` 加进 `internal/cli/root.go` 的命令列表**

把 `root.AddCommand(...)` 里的：

```go
	root.AddCommand(
		newInitCommand(opts),
		newSkillsCommand(opts),
		newGraphCommand(opts),
		newLintCommand(opts),
		newNewCommand(opts),
		newAddCommand(opts),
		newRemoveCommand(opts),
		newFetchCommand(opts),
		newSyncCommand(opts),
		newRestoreCommand(opts),
		newUpCommand(opts),
		newDownCommand(opts),
		newStatusCommand(opts),
		newLoginCommand(opts),
		newLogoutCommand(opts),
		newPublishCommand(opts),
		newVersionCommand(opts),
	)
```

改成（`lang` 紧跟在 `version` 后面，两者都是"不分组"的 CLI 自身命令）：

```go
	root.AddCommand(
		newInitCommand(opts),
		newSkillsCommand(opts),
		newGraphCommand(opts),
		newLintCommand(opts),
		newNewCommand(opts),
		newAddCommand(opts),
		newRemoveCommand(opts),
		newFetchCommand(opts),
		newSyncCommand(opts),
		newRestoreCommand(opts),
		newUpCommand(opts),
		newDownCommand(opts),
		newStatusCommand(opts),
		newLoginCommand(opts),
		newLogoutCommand(opts),
		newPublishCommand(opts),
		newVersionCommand(opts),
		newLangCommand(opts),
	)
```

- [ ] **Step 9: 跑测试确认通过**

Run: `go test ./internal/cli/... -run TestLang -v`
Expected: PASS（全部 5 个）

- [ ] **Step 10: 跑整个 `internal/cli` 包，确认 `lang` 新增没有把命令计数相关的东西带崩（先确认哪里会崩，下一步再修）**

Run: `go build -o /tmp/brickkit-i18n-check ./cmd/brickkit && go run ./scripts/gen-schemas 2>/dev/null; python3 scripts/check-cli-docs.py /tmp/brickkit-i18n-check`

Expected: 在"命令数目：文档与实现一致"这一步报错，形如：

```
❌ 文档里的命令数目对不上：N 处（真实是 17 个业务命令）
```

这是预期的：`scripts/check-cli-docs.py` 目前只把 `"version"` 排除在业务命令计数之外，`lang` 现在被当成了第 17 个业务命令，但所有文档仍然写着 16。下一步把 `lang` 也加进排除名单——这跟已批准的设计（`lang` 跟 `version` 一样不计入业务命令数）是同一件事，不是去改文档凑数字。

- [ ] **Step 11: 改 `scripts/check-cli-docs.py`，把 `lang` 也排除在业务命令计数之外**

把 `check_command_count` 函数里的：

```python
    real = len({name for name in surface if name and name not in COBRA_BUILTINS
                and name != "version"})
```

改成：

```python
    # version 与 lang 都是 CLI 自身命令（跟业务无关），不计入"业务命令"数——
    # 这是已批准设计的一部分（docs/superpowers/specs/2026-09-20-cli-i18n-design.md
    # §3.3），不是凑数字。
    NON_BUSINESS_COMMANDS = {"version", "lang"}
    real = len({name for name in surface if name and name not in COBRA_BUILTINS
                and name not in NON_BUSINESS_COMMANDS})
```

- [ ] **Step 12: 重新跑 check-cli-docs.py 确认恢复通过**

Run: `python3 scripts/check-cli-docs.py /tmp/brickkit-i18n-check`
Expected: 全部 ✅，包括"命令数目：文档与实现一致（16 个业务命令）"

- [ ] **Step 13: Commit**

```bash
git add internal/msgid internal/i18n internal/cli/lang.go internal/cli/lang_test.go internal/cli/root.go scripts/check-cli-docs.py
git commit -m "$(cat <<'EOF'
新增：brickkit lang / brickkit lang set 命令

跟 version 一样是 CLI 自身命令：不分组、不计入"业务命令"数——
scripts/check-cli-docs.py 的统计相应把 lang 也排除掉，否则命令数会
对不上现有文档（这不是凑数字，是已批准设计的一部分）。

Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>
EOF
)"
```

---

### Task 9: 全量验证——真实二进制走完 spec §3.5 的验收场景，跑一次完整 `make lint`

**Files:** 无代码改动，只验证。

- [ ] **Step 1: 干净重新构建，并固定一个空的全局配置目录**

后面除 Step 5、6 外的每一步都靠"默认是英语"来判断对不对；如果直接用跑这个验证的机器上真实的全局配置目录，而这台机器上又真的执行过 `brickkit lang set zh`（完全可能，这就是这个功能本身的用途），下面几步的"预期是英文"就会不成立，看起来像是改动有 bug，其实只是没隔离。用一个确定不存在的目录整个盖掉：

Run:
```bash
go build -o /tmp/brickkit-i18n-check ./cmd/brickkit
export BRICKKIT_USERCONFIG_DIR=/tmp/brickkit-i18n-userconfig-default
rm -rf "$BRICKKIT_USERCONFIG_DIR"
```
Expected: 构建无错误；`$BRICKKIT_USERCONFIG_DIR` 此后对本任务余下的 Step 2-4 都生效（同一个 shell 会话），且目录此刻不存在，等价于"从没设置过语言"

- [ ] **Step 2: 默认（英文）路径——`--help`**

Run: `/tmp/brickkit-i18n-check --help | head -3`
Expected: 第一行 `Usage:`

- [ ] **Step 3: `BRICKKIT_LANG=zh` 路径——`--help`**

Run: `BRICKKIT_LANG=zh /tmp/brickkit-i18n-check --help | head -3`
Expected: 第一行 `用法：`（环境变量优先级最高，盖过 Step 1 那个空的全局配置目录）

- [ ] **Step 4: 空目录下 `brickkit up`，两种语言**

Run:
```bash
mkdir -p /tmp/brickkit-i18n-empty && cd /tmp/brickkit-i18n-empty
/tmp/brickkit-i18n-check up --log-level off
BRICKKIT_LANG=zh /tmp/brickkit-i18n-check up --log-level off
cd -
```
Expected: 第一条输出 `❌ Error: project config file not found` 起始的英文块；第二条输出 `❌ 错误：项目配置文件不存在` 起始的中文块（跟改动前完全一样）

跑完这四步后 `unset BRICKKIT_USERCONFIG_DIR`，避免带进 Step 5（它要自己管理另一个专用目录）。

- [ ] **Step 5: `brickkit lang set` 持久化，用一个干净的全局配置目录**

Run:
```bash
export BRICKKIT_USERCONFIG_DIR=/tmp/brickkit-i18n-userconfig
rm -rf "$BRICKKIT_USERCONFIG_DIR"
/tmp/brickkit-i18n-check lang
/tmp/brickkit-i18n-check lang set zh
/tmp/brickkit-i18n-check lang
cat "$BRICKKIT_USERCONFIG_DIR/config.json"
unset BRICKKIT_USERCONFIG_DIR
```
Expected：
- 第一次 `lang`：`Current language: en (source: default)`
- `lang set zh`：`✅ 语言已设为 zh`（这条确认信息本身已经用新语言渲染）
- 第二次 `lang`：`当前语言：zh（来源：全局配置文件）`
- `config.json` 内容是 `{"lang": "zh"}` 加换行

- [ ] **Step 6: `brickkit lang set` 校验不支持的值**

用一个全新的隔离目录（不复用 Step 5 那个已经写了 `zh` 进去的目录），保证这一步的期望输出是确定的英文，不取决于跑这个验证的机器上真实的全局配置目录里已经有什么：

Run:
```bash
BRICKKIT_USERCONFIG_DIR=/tmp/brickkit-i18n-userconfig-clean /tmp/brickkit-i18n-check lang set fr; echo "exit=$?"
```
Expected: stderr 报 `❌ Unsupported language: fr (supported: en, zh)`，`exit=2`

- [ ] **Step 7: 跑完整 `make lint`**

Run: `cd /home/zhijie/Desktop/github/brickKit && make lint 2>&1 | tail -60`
Expected: 全部 ✅，跟本计划开始前的那次全绿输出一致（命令数目仍是"16 个业务命令"，覆盖率仍不低于门槛）

- [ ] **Step 8: 确认没有遗留的临时文件被误提交**

Run: `git status`
Expected: 只有本计划各任务里已经 commit 的改动，没有未跟踪的临时文件（`/tmp/` 下的构建产物与验证目录不在仓库里，不会出现）

- [ ] **Step 9: 最终确认所有任务的 commit 都在**

Run: `git log --oneline -10`
Expected: 能看到 Task 1-8 各自的 commit，共 8 个（Task 9 本身不产生代码改动，不需要 commit）

本任务不需要 commit——它只是验证，前 8 个任务已经各自提交过了。若 Step 7 的 `make lint` 发现任何问题，回到对应任务修，不在这里新开改动。
