# brickkit graph / brickkit lint / JSON Schema Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 新增两条只读命令——`brickkit graph`（把依赖拓扑输出成 Mermaid）与 `brickkit lint`（离线的结构校验，含独立组件仓库模式）——并从 Go 结构体反射生成 `component.yaml` / `brickkit.yaml` 的 JSON Schema，让编辑器有字段补全与红线提示。三者都不新增任何生成逻辑、校验规则或运行时状态，只是把平台早就有的知识换一个入口摆出来。

**Architecture:** ① 共用两处小重构：`detectScope`（项目 vs 独立组件仓库的判断，从 `skills` 里抽出）与 `resolveTopology`（"解析依赖图 + 算级联"，今天在 up / down·status / sync 三处各写一遍）。② `graph` 是纯读取：`resolveTopology` 的结果 + `brickkit.yaml` 里各条目的 `local`/`servedBy`，渲染成 Mermaid，stdout 里只有 Mermaid。③ `lint` 对 `brickkit.yaml`（`config.ParseConfigFile`）与本地安装源目录下每一份 `component.yaml`（`manifest.Parse` + `PropertyKeyWarnings` + 新导出的 `inject.ReservedKeyWarnings`）逐个跑一遍已有的校验；`source` 新增只读的 `LocalManifestFiles` 负责枚举，不走会写缓存的 `Client.Manifest`。④ `internal/schemagen` 用反射 + `jsonschema` struct tag + 一张极小的覆盖表生成 schema，落盘到 `schemas/`，配防漂移测试与"约束与真实校验器互相钉住"的语义测试。

**Tech Stack:** Go 1.22，`testify`，`gopkg.in/yaml.v3`，`encoding/json`，`spf13/cobra`，Python 3（`scripts/check-*.py`），Markdown 双语文档。

**Spec:** `docs/superpowers/specs/2026-09-19-graph-lint-schema-design.md`（先读它——它对着代码逐条核实过，且含"更正"说明）

## Global Constraints

- 代码注释、提交信息、文档正文用中文；标识符用英文（跟周边一致）。注释风格跟周边一致：写"为什么"，不写"做了什么"。
- **新增的 CLI 表面只有两条命令与两个参数**：`brickkit graph`（参数：`--ignore-served-by`）、`brickkit lint`（参数：`--strict`）。**没有** `--output`、HTML/SVG、过滤子图、`--format`。要存文件用 shell 重定向。
- **`graph` 的 stdout 里只有 Mermaid，一个多余字符都没有。** 提示写成 `%%` 注释行，警告走 stderr。
- **`lint` 纯只读、不联网**：不写 `.brickkit/` 下任何东西（尤其不能调 `Client.Manifest`，它会写缓存），不取 market/git 源，不读公钥文件（不用 `newSourceClient`，直接 `source.New`）。
- **不新增任何校验规则。** `lint` 只复用 `manifest.Parse`、`config.ParseConfigFile`、`manifest.PropertyKeyWarnings`，以及把 `inject` 里已有的保留变量判断提成导出函数。不检查 `servedBy` 目标是否存在、不解析依赖图、不校验 `configSchema` 里 `enum`/`minimum` 对应的**值**（AGENTS §9.12）。
- 新增错误码只有一个：`LINT_FAILED`（`clierr.CodeLintFailed`），必须同时写进 `docs/{en,zh}/06-architecture/10-error-codes.md`（`tests/docfields` 会拦住漏写），码一旦发布不改名、不挪作他用。
- JSON Schema 里的约束（enum / pattern / 范围）**必须与真实校验器互相钉住**：`jsonschema` tag 写的取值，要有测试拿真实的 `manifest.Parse` / `config.ParseConfig` 验证（Task 8）。`required` 规则是"yaml tag 没有 `omitempty` 且不是 `bool` 且不是指针"，`jsonschema:"optional"` 是唯一的例外开关。
- 每个任务结束前：完整 `make lint` 与 `go test ./... -count=1` 都要绿，再单独提交。**判断是否通过要看退出码**，
  这台机器是 zsh，`${PIPESTATUS[0]}` 不存在，`make lint | grep` 会把失败看成成功。写成：
  `make lint > "$SCRATCH/lint.log" 2>&1; echo "exit=$?"`，再 `tail` 日志。
- 提交命令从 `git` 开头，**不带 `cd` 前缀**；提交信息末尾加一行 `Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>`。提交信息里含中文全角引号时，先写进文件再 `git commit -F <文件>`（zsh 会把全角引号后的内容截断）。
- zsh 会把 `--include=*.go` 当通配符展开，一律写成 `--include='*.go'`；不要 `echo "======"`。
- 不碰仓库根目录未跟踪的 `改进计划.md`。若 `make lint` 的 `check-docs` / `check-cli-docs` 被它拦住，临时 `mv` 到 scratchpad、跑完再 `mv` 回来，别改它、别提交它；移回后 `git status --porcelain` 只应比任务开始前多出你自己的改动，不应少了它。
- 文档：`docs/en` 与 `docs/zh` 各写一份，各自用自己的语言写得自然，**事实与结论必须一致，CLI 输出块必须逐字取自真实输出，且两边一致**。结构允许平行（这是仓库现有文档的实际做法：段落几乎一一对应）。面向用户的文字不预设读者背景（先大白话讲是什么，再讲好处与代价，最后讲 BrickKit 怎么对待）；不引用、不链接 `docs/archive/`。`AGENTS.md` / `AGENTS.zh.md` 是写给 AI 的压缩版，不受"不预设背景"约束。
- 凡是关于"解析器 / 渲染器 / 校验器会怎样"的说法，**先跑再写**。测试里写死的期望输出（尤其 Mermaid 的节点顺序）是按设计书推演的：若真实输出**只有顺序不同**，连跑五次确认顺序稳定，再采用真实顺序；若有别的差异，停下来报告，不要为了让测试变绿去改断言。
- **命令总数的文字声明每个加命令的任务自己顺手改**：`scripts/check-cli-docs.py` 的 `check_command_count` 在真实命令数一变就红（它只认中文措辞 `N 个命令` / `N 条命令`），而 `make` 停在第一个失败的前置目标——不改的话，golangci-lint 与覆盖率两道门在后面的任务里根本不会被跑到。所以 Task 3（14→15）与 Task 6（15→16）各自 `git grep -nE "1[0-9] (个命令|条命令|commands)"`（排除 `docs/archive`、`docs/superpowers`、`改进计划.md`）把**所有**这类声明（中英文一起，避免两种语言的数字不一致）改成当时的真实数。中间状态里数字会比 AGENTS §8 / README 的命令清单多一两条——清单在 Task 9 补齐，最终状态自洽。
- 覆盖率门槛 92%（`./internal/...`，`make cover-check`）：新增代码要有测试，不靠门槛的余量。
- `market-server/` 是独立 module，本计划不碰它。

## File Structure

| 文件 | 职责 | 任务 |
| --- | --- | --- |
| `internal/cli/skills.go` | 抽出 `detectScope`；`skillsInstaller` 改调它 | 1 |
| `internal/cli/topology.go`（新建） | `resolveTopology`、`clearServedBy` | 2 |
| `internal/cli/up.go`、`lifecycle.go`、`sync.go` | 改调 `resolveTopology` / `clearServedBy` | 2 |
| `internal/cli/graph.go`（新建） | `brickkit graph`：命令 + Mermaid 渲染 | 3 |
| `internal/cli/root.go` | 注册 `graph`、`lint` | 3、6 |
| `internal/source/local.go`、`list.go` | 目录遍历从 `listComponents` 抽出为 `manifestFiles`；新增 `Client.LocalManifestFiles` | 4 |
| `internal/inject/reserved.go` | 导出 `ReservedKeyWarnings`；`matchReserved` 与它共用 `staticReserved` | 5 |
| `internal/cli/lint.go`（新建）、`internal/clierr/clierr.go` | `brickkit lint`；`CodeLintFailed` | 6 |
| `docs/{en,zh}/06-architecture/10-error-codes.md` | `LINT_FAILED` 一节 | 6 |
| `internal/yamlcheck/unknown.go` | 导出 `KnownFields` | 7 |
| `internal/schemagen/`（新建） | 反射生成器（通用部分） | 7 |
| `internal/manifest/types.go`、`internal/config/config.go` | `jsonschema` tag | 8 |
| `internal/schemagen/schemagen.go`、`schemas/*.json`、`cmd/gen-schemas/main.go`、`Makefile` | 真实的两份 schema、落盘工具、`generate-schemas` / `check-schemas` | 8 |
| `docs/…`、`AGENTS*.md`、`README*.md`、`llms*.txt`、`CHANGELOG.md`、`internal/skills/assets/…` | 文档与计数 | 9 |

---

## Task 1: 抽出 `detectScope`

**背景：** `skillsInstaller` 里内联了"这是 BrickKit 项目还是独立组件仓库"的判断：两个 `os.Stat`，两者都有时按项目算，都没有时报 `PROJECT_MISSING`。`lint` 要用同一条规则，抽成共用函数，行为一字不变。

**Files:**
- Modify: `internal/cli/skills.go`（`skillsInstaller` 上方新增 `detectScope`）
- Test: `internal/cli/skills_test.go`（末尾追加）

**Interfaces:**
- Produces（Task 6 依赖）：`func detectScope(opts *Options) (skills.Scope, config.Layout, error)`。返回的 `Layout` 无论成功与否都有效；两者都没有时 error 是 `clierr.CodeProjectMissing`，文案与现在完全一致。

- [ ] **Step 1: 写测试**

在 `internal/cli/skills_test.go` 末尾追加（若文件里已有 `os`/`filepath`/`clierr` 导入则复用，缺哪个补哪个）：

```go
func TestDetectScope(t *testing.T) {
	touch := func(t *testing.T, dir string, names ...string) {
		t.Helper()
		for _, name := range names {
			require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte("x: 1\n"), 0o644))
		}
	}
	optsFor := func(dir, configPath string) *Options {
		return &Options{WorkDir: dir, ConfigPath: configPath}
	}

	t.Run("只有 brickkit.yaml 是项目", func(t *testing.T) {
		dir := t.TempDir()
		touch(t, dir, "brickkit.yaml")
		scope, layout, err := detectScope(optsFor(dir, DefaultConfigFile))
		require.NoError(t, err)
		assert.Equal(t, skills.ScopeProject, scope)
		assert.Equal(t, dir, layout.Root)
	})

	t.Run("只有 component.yaml 是组件仓库", func(t *testing.T) {
		dir := t.TempDir()
		touch(t, dir, "component.yaml")
		scope, _, err := detectScope(optsFor(dir, DefaultConfigFile))
		require.NoError(t, err)
		assert.Equal(t, skills.ScopeComponent, scope)
	})

	t.Run("两者都有时按项目算", func(t *testing.T) {
		dir := t.TempDir()
		touch(t, dir, "brickkit.yaml", "component.yaml")
		scope, _, err := detectScope(optsFor(dir, DefaultConfigFile))
		require.NoError(t, err)
		assert.Equal(t, skills.ScopeProject, scope)
	})

	t.Run("--config 指向别的文件名", func(t *testing.T) {
		dir := t.TempDir()
		touch(t, dir, "brickkit.prod.yaml")
		scope, _, err := detectScope(optsFor(dir, "brickkit.prod.yaml"))
		require.NoError(t, err)
		assert.Equal(t, skills.ScopeProject, scope)
	})

	t.Run("两者都没有报 PROJECT_MISSING", func(t *testing.T) {
		dir := t.TempDir()
		_, layout, err := detectScope(optsFor(dir, DefaultConfigFile))
		require.Error(t, err)
		e := clierr.As(err)
		assert.Equal(t, clierr.CodeProjectMissing, e.Code)
		assert.Contains(t, e.Message, "既不是 BrickKit 项目，也不是组件仓库")
		assert.Equal(t, dir, layout.Root, "出错时 Layout 仍然有效")
	})
}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./internal/cli/ -run TestDetectScope -count=1`
Expected: 编译失败，`undefined: detectScope`

- [ ] **Step 3: 实现**

在 `internal/cli/skills.go` 里，把 `skillsInstaller` 整个替换成下面两个函数（保留原有的函数注释里那段"不确认的话……"的解释，挪到 `skillsInstaller` 上）：

```go
// detectScope 判断当前目录是 BrickKit 项目（有 brickkit.yaml）还是独立组件仓库
// （有 component.yaml、没有 brickkit.yaml）；两者都没有时返回错误。
//
// 两者都有时按项目算：那是 brickkit skills 一直以来的行为，lint 沿用同一条规则，
// 不新发明一条。返回的 Layout 无论成败都有效——调用方走哪一支都要用它定位文件。
func detectScope(opts *Options) (skills.Scope, config.Layout, error) {
	layout := config.NewLayout(opts.WorkDir, opts.ConfigPath)

	if _, err := os.Stat(layout.ConfigPath()); err == nil {
		return skills.ScopeProject, layout, nil
	}
	if _, err := os.Stat(filepath.Join(layout.Root, manifest.FileName)); err == nil {
		return skills.ScopeComponent, layout, nil
	}
	return skills.ScopeProject, layout, clierr.New(clierr.CodeProjectMissing,
		"错误：当前目录既不是 BrickKit 项目，也不是组件仓库").
		WithDetail("找不到", layout.ConfigName()+"，也没有 "+manifest.FileName).
		WithHint("项目：先执行 brickkit init <项目名称>，或用 --config 指定配置文件",
			"组件仓库：在含 "+manifest.FileName+" 的目录里执行")
}

// skillsInstaller 构造 Installer，并先确认这儿是它管得着的地方：
// BrickKit 项目（有 brickkit.yaml），或独立的组件仓库（有 component.yaml、没有 brickkit.yaml）。
//
// 不确认的话，在随便一个目录里敲 skills update 会默默建出 .claude/ 与
// AGENTS.md——在别人家里留下文件，比报个错糟糕得多。
func skillsInstaller(opts *Options) (skills.Installer, error) {
	scope, layout, err := detectScope(opts)
	if err != nil {
		return skills.Installer{}, err
	}
	return skills.Installer{
		Root:     layout.Root,
		LockPath: layout.SkillsLockPath(),
		Version:  version.Version,
		Scope:    scope,
	}, nil
}
```

若 `layout.Root` 在测试里不等于 `dir`（比如 `NewLayout` 会做 `filepath.Abs`/`Clean`），把断言改成与 `config.NewLayout(dir, "").Root` 比较，而不是改实现。

- [ ] **Step 4: 跑测试**

Run: `go test ./internal/cli/ -run 'TestDetectScope|TestSkills' -count=1`
Expected: PASS（`TestSkills*` 是既有测试，证明行为没变）

- [ ] **Step 5: 全量检查并提交**

```bash
go test ./... -count=1
# 完整 make lint（见 Global Constraints 里对 改进计划.md 的处理与退出码写法）
git add internal/cli/skills.go internal/cli/skills_test.go
git commit -F <写好信息的文件>   # 例如：重构：把"项目还是组件仓库"的判断抽成 detectScope
```

---

## Task 2: 抽出 `resolveTopology` 与 `clearServedBy`

**背景：** "解析依赖图 → 算级联"这两步在三处各写了一遍：`up.go` 的 `buildUpPlan`、`lifecycle.go` 的 `project.resolve`、`sync.go` 的 `syncFocus`。`graph` 是第四个。抽成一个函数，三处改调用，行为一字不变。`--ignore-served-by` 那段"清空所有 `servedBy`"同样抽出来给 `graph` 共用。

**Files:**
- Create: `internal/cli/topology.go`
- Modify: `internal/cli/up.go`（`buildUpPlan` 里那两步与 servedBy 清空循环）
- Modify: `internal/cli/lifecycle.go`（`project.resolve`）
- Modify: `internal/cli/sync.go`（`syncFocus`）
- Create: `internal/cli/topology_test.go`

**Interfaces:**
- Produces（Task 3 依赖）：
  - `func resolveTopology(ctx context.Context, client *source.Client, cfg *config.Config) (*resolver.Graph, *cascade.Result, error)`——client 由调用方创建、也由调用方关闭；出错时前两个返回值都是 nil。
  - `func clearServedBy(cfg *config.Config)`——只清空、不打印。

- [ ] **Step 1: 写测试**

新建 `internal/cli/topology_test.go`：

```go
package cli

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/config"
	"github.com/brickkit/brickkit/internal/resolver"
	"github.com/brickkit/brickkit/internal/source"
)

// newTopologyClient 为一个测试项目构造安装源客户端。
func newTopologyClient(t *testing.T, f *projectFixture, cfg *config.Config) *source.Client {
	t.Helper()
	client, err := source.New(f.Layout, cfg, source.Options{})
	require.NoError(t, err)
	t.Cleanup(func() { _ = client.Close() })
	return client
}

func TestResolveTopologyReturnsGraphAndCascade(t *testing.T) {
	dir := t.TempDir()
	sources := oneLocalSource(t, dir,
		comp{ID: "demo/caller", Version: "1.0.0", Requires: []string{"demo/hello@1.0.0"}},
		comp{ID: "demo/hello", Version: "1.0.0"},
	)
	f := newProjectFixtureAt(t, dir, sources...)
	require.Equal(t, clierr.ExitOK, runIn(t, f.Dir, "add", "--local").code)

	cfg := f.parsed(t)
	graph, states, err := resolveTopology(context.Background(), newTopologyClient(t, f, cfg), cfg)
	require.NoError(t, err)

	assert.Len(t, graph.Nodes, 2)
	assert.ElementsMatch(t,
		[]resolver.Ref{{ID: "demo/caller", Version: "1.0.0"}, {ID: "demo/hello", Version: "1.0.0"}},
		states.Running())
}

func TestResolveTopologyHonoursDisabledTopLevel(t *testing.T) {
	dir := t.TempDir()
	sources := oneLocalSource(t, dir,
		comp{ID: "demo/caller", Version: "1.0.0", Requires: []string{"demo/hello@1.0.0"}},
		comp{ID: "demo/hello", Version: "1.0.0"},
	)
	f := newProjectFixtureAt(t, dir, sources...)
	f.writeConfig(t, `components:
  - id: demo/caller
    version: 1.0.0
    enabled: false
resources: []
`)

	cfg := f.parsed(t)
	_, states, err := resolveTopology(context.Background(), newTopologyClient(t, f, cfg), cfg)
	require.NoError(t, err)
	assert.True(t, states.Empty(), "顶层被关掉，它带来的依赖也不跑")
}

func TestResolveTopologyFailsWhenComponentMissing(t *testing.T) {
	dir := t.TempDir()
	sources := oneLocalSource(t, dir, comp{ID: "demo/hello", Version: "1.0.0"})
	f := newProjectFixtureAt(t, dir, sources...)
	f.writeConfig(t, `components:
  - id: demo/ghost
    version: 1.0.0
resources: []
`)

	cfg := f.parsed(t)
	graph, states, err := resolveTopology(context.Background(), newTopologyClient(t, f, cfg), cfg)
	require.Error(t, err)
	assert.Nil(t, graph)
	assert.Nil(t, states)
	assert.NotEmpty(t, clierr.As(err).Code, "是结构化错误，带稳定的错误码")
}

func TestClearServedBy(t *testing.T) {
	cfg := &config.Config{Components: []config.Component{
		{ID: "demo/a", Version: "1.0.0", ServedBy: "demo/shell@1.0.0"},
		{ID: "demo/shell", Version: "1.0.0"},
		{ID: "demo/b", Version: "1.0.0", ServedBy: "demo/shell@1.0.0"},
	}}
	clearServedBy(cfg)
	for _, c := range cfg.Components {
		assert.Empty(t, c.ServedBy, c.ID)
	}
	assert.Len(t, cfg.Components, 3, "只清 servedBy，条目本身不动")
}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./internal/cli/ -run 'TestResolveTopology|TestClearServedBy' -count=1`
Expected: 编译失败，`undefined: resolveTopology`

- [ ] **Step 3: 实现**

新建 `internal/cli/topology.go`：

```go
package cli

// 本文件是"这个项目的拓扑是什么样"的共用部分：解析依赖图、算级联。
//
// up、down/status、sync、graph 四处问的都是同一个问题的不同侧面。前三处从前各自
// 写了一遍这两步——以后其中任何一步的调用方式变了，要记得同步三处，而
// "记得"正是会出事的地方。共用的只该是它们都需要的这一部分。

import (
	"context"

	"github.com/brickkit/brickkit/internal/cascade"
	"github.com/brickkit/brickkit/internal/config"
	"github.com/brickkit/brickkit/internal/resolver"
	"github.com/brickkit/brickkit/internal/source"
)

// resolveTopology 解析依赖图并算出级联状态（哪些组件这次会启动）。
//
// client 由调用方创建、也由调用方关闭：up 在这之后还要用同一个客户端补升级摘要，
// 不能在这里替它关。出错时两个结果都是 nil。
func resolveTopology(
	ctx context.Context, client *source.Client, cfg *config.Config,
) (*resolver.Graph, *cascade.Result, error) {
	graph, err := resolver.New(resolver.FromSource(client)).ResolveConfig(ctx, cfg)
	if err != nil {
		return nil, nil, err
	}
	states, err := cascade.Compute(cfg, graph)
	if err != nil {
		return nil, nil, err
	}
	return graph, states, nil
}

// clearServedBy 在内存里清空全部 servedBy 声明（--ignore-served-by 的实现）。
//
// 格式校验在 ParseConfigFile 里已经跑完，不会被这一步绕过。下游 resolver /
// shell.Resolve / compose / k8s 全部只读 ServedBy 这一个字段，没有任何一处维护
// 自己的派生状态，清空一次就够，不需要逐处打补丁。只改内存里的 cfg，从不写回
// 磁盘上的 brickkit.yaml。
//
// 只清空、不打印：up 用一行 ⚠️ 告诉使用者，graph 的 stdout 必须是纯 Mermaid，
// 只能写成注释——各自决定怎么说。
func clearServedBy(cfg *config.Config) {
	for i := range cfg.Components {
		cfg.Components[i].ServedBy = ""
	}
}
```

三处调用点：

`internal/cli/up.go`，把
```go
	if flags.ignoreServedBy {
		// 格式校验已经跑完……
		for i := range cfg.Components {
			cfg.Components[i].ServedBy = ""
		}
		opts.Printf("⚠️  已忽略全部 servedBy 声明（仅用于验证组件独立启动能力，不写回 brickkit.yaml）\n")
	}
```
改成
```go
	if flags.ignoreServedBy {
		clearServedBy(cfg)
		opts.Printf("⚠️  已忽略全部 servedBy 声明（仅用于验证组件独立启动能力，不写回 brickkit.yaml）\n")
	}
```
并把
```go
	plan.graph, err = resolver.New(resolver.FromSource(client)).ResolveConfig(ctx, cfg)
	if err != nil {
		return nil, err
	}
	plan.states, err = cascade.Compute(cfg, plan.graph)
	if err != nil {
		return nil, err
	}
```
改成
```go
	plan.graph, plan.states, err = resolveTopology(ctx, client, cfg)
	if err != nil {
		return nil, err
	}
```

`internal/cli/lifecycle.go` 的 `resolve`，把两个 `if` 合成：
```go
	if p.graph, p.states, err = resolveTopology(ctx, client, p.cfg); err != nil {
		return err
	}
```
（`loadProject` 在出错时本来就把 `graph/states/order` 全部置 nil，所以"级联失败时 graph 已经赋值"这一点差异不影响任何调用方。）

`internal/cli/sync.go` 的 `syncFocus`：
```go
	_, states, err := resolveTopology(ctx, client, cfg)
	if err != nil {
		return nil, err
	}
	return focusFrom(cfg, states), nil
```

按编译器提示清掉三个文件里不再用到的 `resolver` / `cascade` 导入（`up.go` 与 `lifecycle.go` 仍在用它们的类型，多半不用删）。

- [ ] **Step 4: 跑测试**

Run: `go test ./internal/cli/ -count=1`
Expected: 全部 PASS（既有的 up / status / down / sync 测试证明三处行为没变）

- [ ] **Step 5: 全量检查并提交**

```bash
go test ./... -count=1
# 完整 make lint
git add internal/cli/topology.go internal/cli/topology_test.go internal/cli/up.go internal/cli/lifecycle.go internal/cli/sync.go
git commit -F <写好信息的文件>   # 重构：抽出 resolveTopology / clearServedBy，up、down·status、sync 共用
```

---

## Task 3: `brickkit graph`

**Files:**
- Create: `internal/cli/graph.go`
- Create: `internal/cli/graph_test.go`
- Modify: `internal/cli/root.go`（注册命令）
- Modify: `internal/cli/cli_test.go`（`allCommands` 加 `"graph"`；`TestSubcommandFlags` 的 `want` 加 `"graph": {"ignore-served-by"}`）

**Interfaces:**
- Consumes（Task 2）：`resolveTopology`、`clearServedBy`。另外用到既有的 `shell.ParseRef`（`internal/shell`）、`manifest.ServiceName`、`resolver.Node.{Requires,Optional,MissingOptional}`、`cascade.Result.IsRunning`、`newSourceClient`。
- Produces：`newGraphCommand(opts *Options) *cobra.Command`；`renderMermaid(cfg *config.Config, graph *resolver.Graph, states *cascade.Result, ignoredServedBy bool) string`（Task 9 的文档输出取自它）。

**输出规则（设计书 §2.3–§2.4，逐条实现）：**

- 首行 `graph TD`；缩进 4 个空格，`subgraph` 里的节点缩进 8 个。
- 节点 `ID["标签"]`，ID = `manifest.ServiceName(id, version)`，标签 = `id@version`；`local: true` 的追加 `<br/>本地调试 :<localPort>`。
- 声明顺序：① 每个 `servedBy` 外壳一个 `subgraph <外壳服务名>-members["外壳：<外壳 id@version>"] … end`，里面是它收编的成员节点；② 其余节点，按 `graph.Nodes` 的顺序；③ "取不到的弱依赖"占位节点 `ID["id@version<br/>未安装"]`（同一个 ref 只声明一次）；④ 边；⑤ `classDef` + `class`。
- 边：强依赖 `A --> B`；弱依赖 `A -.-> B`；取不到的弱依赖 `A -.-> 占位节点`。箭头两侧留空格。
- 样式类只输出用到的：`disabled`（`!states.IsRunning`）、`local`（`local: true`）、`missing`（占位节点），`classDef` 文本见下面代码；`class` 行把该类的节点 ID 用逗号连起来。
- 空项目：`graph TD` 加一行 `    %% 当前项目没有组件`，退出码 0；此时不构造安装源客户端。
- `--ignore-served-by`：首行之后加一行 `    %% 已忽略全部 servedBy 声明（--ignore-served-by：仅用于验证组件独立启动能力）`。
- 依赖解析的警告（`graph.Warnings`）用 `w.Format()` 写到 `opts.Stderr`；解析失败按 `up` 的方式原样返回错误。

- [ ] **Step 1: 写测试**

新建 `internal/cli/graph_test.go`：

```go
package cli

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/brickkit/brickkit/internal/clierr"
)

// graphProject 建一个带本地安装源的项目，用 body 作为 brickkit.yaml 的 components/resources 部分。
func graphProject(t *testing.T, body string, comps ...comp) *projectFixture {
	t.Helper()
	dir := t.TempDir()
	f := newProjectFixtureAt(t, dir, oneLocalSource(t, dir, comps...)...)
	f.writeConfig(t, body)
	return f
}

func TestGraphBasicEdges(t *testing.T) {
	f := graphProject(t, `components:
  - id: demo/hello
    version: 1.0.0
  - id: demo/caller
    version: 1.0.0
resources: []
`,
		comp{ID: "demo/hello", Version: "1.0.0"},
		comp{ID: "demo/caller", Version: "1.0.0", Requires: []string{"demo/hello@1.0.0"}},
	)

	r := runIn(t, f.Dir, "graph")
	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)
	assert.Equal(t, `graph TD
    demo-hello-1-0-0["demo/hello@1.0.0"]
    demo-caller-1-0-0["demo/caller@1.0.0"]
    demo-caller-1-0-0 --> demo-hello-1-0-0
`, r.stdout)
}

func TestGraphWeakDependencyIsDashed(t *testing.T) {
	f := graphProject(t, `components:
  - id: demo/cache
    version: 1.0.0
  - id: demo/caller
    version: 1.0.0
resources: []
`,
		comp{ID: "demo/cache", Version: "1.0.0"},
		comp{ID: "demo/caller", Version: "1.0.0", Optional: []string{"demo/cache@1.0.0"}},
	)

	r := runIn(t, f.Dir, "graph")
	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)
	assert.Contains(t, r.stdout, "    demo-caller-1-0-0 -.-> demo-cache-1-0-0\n")
	assert.NotContains(t, r.stdout, "demo-caller-1-0-0 --> demo-cache-1-0-0")
}

// 被关掉的顶层带着它下面的一串一起置灰；不相干的组件不受影响。
func TestGraphDisabledComponentsAreGreyedOut(t *testing.T) {
	f := graphProject(t, `components:
  - id: demo/caller
    version: 1.0.0
    enabled: false
  - id: demo/hello
    version: 1.0.0
  - id: demo/solo
    version: 1.0.0
resources: []
`,
		comp{ID: "demo/hello", Version: "1.0.0"},
		comp{ID: "demo/caller", Version: "1.0.0", Requires: []string{"demo/hello@1.0.0"}},
		comp{ID: "demo/solo", Version: "1.0.0"},
	)

	r := runIn(t, f.Dir, "graph")
	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)
	assert.Contains(t, r.stdout, "    classDef disabled fill:#eee,stroke:#999,color:#999;\n")

	var classLine string
	for _, line := range strings.Split(r.stdout, "\n") {
		if strings.HasPrefix(line, "    class ") && strings.HasSuffix(line, " disabled") {
			classLine = line
		}
	}
	require.NotEmpty(t, classLine, r.stdout)
	assert.Contains(t, classLine, "demo-caller-1-0-0")
	assert.Contains(t, classLine, "demo-hello-1-0-0")
	assert.NotContains(t, classLine, "demo-solo-1-0-0")
}

func TestGraphMarksLocalDebugComponent(t *testing.T) {
	f := graphProject(t, `components:
  - id: demo/hello
    version: 1.0.0
    local: true
    localPort: 8081
resources: []
`, comp{ID: "demo/hello", Version: "1.0.0"})

	r := runIn(t, f.Dir, "graph")
	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)
	assert.Contains(t, r.stdout, `demo-hello-1-0-0["demo/hello@1.0.0<br/>本地调试 :8081"]`)
	assert.Contains(t, r.stdout, "    classDef local fill:#e6f2ff,stroke:#3673a8;\n")
	assert.Contains(t, r.stdout, "    class demo-hello-1-0-0 local\n")
	assert.NotContains(t, r.stdout, "classDef disabled", "没有被关掉的组件就不输出 disabled 样式")
}

const graphServedByBody = `components:
  - id: demo/shell
    version: 1.0.0
  - id: demo/a
    version: 1.0.0
    servedBy: demo/shell@1.0.0
  - id: demo/b
    version: 1.0.0
    servedBy: demo/shell@1.0.0
  - id: demo/free
    version: 1.0.0
resources: []
`

func servedByComps() []comp {
	return []comp{
		{ID: "demo/shell", Version: "1.0.0", Port: 8080},
		{ID: "demo/a", Version: "1.0.0", Port: 8081},
		{ID: "demo/b", Version: "1.0.0", Port: 8082},
		{ID: "demo/free", Version: "1.0.0", Port: 8083},
	}
}

func TestGraphGroupsServedByMembersUnderTheirShell(t *testing.T) {
	f := graphProject(t, graphServedByBody, servedByComps()...)

	r := runIn(t, f.Dir, "graph")
	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)

	assert.Contains(t, r.stdout, "    subgraph demo-shell-1-0-0-members[\"外壳：demo/shell@1.0.0\"]\n"+
		"        demo-a-1-0-0[\"demo/a@1.0.0\"]\n"+
		"        demo-b-1-0-0[\"demo/b@1.0.0\"]\n"+
		"    end\n")
	// 外壳自己与不相干的组件画在子图外面
	assert.Contains(t, r.stdout, "\n    demo-shell-1-0-0[\"demo/shell@1.0.0\"]\n")
	assert.Contains(t, r.stdout, "\n    demo-free-1-0-0[\"demo/free@1.0.0\"]\n")
}

func TestGraphIgnoreServedByDropsGroupingAndSaysSo(t *testing.T) {
	f := graphProject(t, graphServedByBody, servedByComps()...)

	r := runIn(t, f.Dir, "graph", "--ignore-served-by")
	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)
	assert.NotContains(t, r.stdout, "subgraph")
	assert.Contains(t, r.stdout, "    %% 已忽略全部 servedBy 声明")

	// 只在内存里清：brickkit.yaml 一个字节没动
	assert.Contains(t, f.config(t), "servedBy: demo/shell@1.0.0")
}

// 取不到的弱依赖也要画：up 的"依赖图"一节把它们写成"（弱，未安装）"。
func TestGraphDrawsMissingOptionalDependency(t *testing.T) {
	f := graphProject(t, `components:
  - id: demo/caller
    version: 1.0.0
resources: []
`, comp{ID: "demo/caller", Version: "1.0.0", Optional: []string{"demo/ghost@1.0.0"}})

	r := runIn(t, f.Dir, "graph")
	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)
	assert.Contains(t, r.stdout, `demo-ghost-1-0-0["demo/ghost@1.0.0<br/>未安装"]`)
	assert.Contains(t, r.stdout, "    demo-caller-1-0-0 -.-> demo-ghost-1-0-0\n")
	assert.Contains(t, r.stdout, "    class demo-ghost-1-0-0 missing\n")
	assert.Contains(t, r.stdout, "    classDef missing ")

	// stdout 里只有 Mermaid：解析警告在 stderr
	assert.NotContains(t, r.stdout, "⚠️")
	assert.Contains(t, r.stderr, "⚠️")
}

func TestGraphStdoutIsPureMermaid(t *testing.T) {
	f := graphProject(t, `components:
  - id: demo/caller
    version: 1.0.0
resources: []
`, comp{ID: "demo/caller", Version: "1.0.0", Optional: []string{"demo/ghost@1.0.0"}})

	r := runIn(t, f.Dir, "graph")
	require.Equal(t, clierr.ExitOK, r.code)
	lines := strings.Split(strings.TrimSuffix(r.stdout, "\n"), "\n")
	require.Equal(t, "graph TD", lines[0])
	for _, line := range lines[1:] {
		assert.True(t, strings.HasPrefix(line, "    "), "每一行都是 Mermaid 的缩进语句：%q", line)
	}
}

func TestGraphEmptyProject(t *testing.T) {
	dir := t.TempDir()
	require.Equal(t, 0, runIn(t, dir, "init", "p", "--no-skills").code)

	r := runIn(t, dir, "graph")
	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)
	assert.Equal(t, "graph TD\n    %% 当前项目没有组件\n", r.stdout)
}

func TestGraphFailsLikeUpWhenRequiredDependencyMissing(t *testing.T) {
	f := graphProject(t, `components:
  - id: demo/caller
    version: 1.0.0
resources: []
`, comp{ID: "demo/caller", Version: "1.0.0", Requires: []string{"demo/ghost@1.0.0"}})

	r := runIn(t, f.Dir, "graph")
	assert.Equal(t, clierr.ExitError, r.code)
	assert.Empty(t, r.stdout, "解析不出来就什么都不画")
	assert.Contains(t, r.stderr, "DEPENDENCY_MISSING")
}

func TestGraphOutputIsStable(t *testing.T) {
	f := graphProject(t, graphServedByBody, servedByComps()...)
	first := runIn(t, f.Dir, "graph")
	for i := 0; i < 5; i++ {
		assert.Equal(t, first.stdout, runIn(t, f.Dir, "graph").stdout)
	}
}

func TestGraphRejectsPositionalArguments(t *testing.T) {
	f := graphProject(t, "components: []\nresources: []\n")
	assert.Equal(t, clierr.ExitUsage, runIn(t, f.Dir, "graph", "extra").code)
}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./internal/cli/ -run TestGraph -count=1`
Expected: FAIL（`unknown command "graph"`，退出码 2）

- [ ] **Step 3: 实现**

新建 `internal/cli/graph.go`：

```go
package cli

// 本文件实现 brickkit graph：把项目的依赖拓扑输出成 Mermaid。
//
// 它是纯读取：数据来源与 up --dry-run 相同（依赖图 + 级联结果），再加 brickkit.yaml
// 里各组件条目自己写的 local / servedBy。跳过 up 才需要的一切——镜像权限检查、迁移展示、
// 引擎解析、环境变量注入、生成部署文件。
//
// 只输出 Mermaid、只写 stdout：Mermaid 在 GitHub、VS Code 里本来就能渲染，
// 自己再造一个 HTML/SVG 渲染器是重新发明已经免费拿到的东西。要存文件用 shell 重定向。

import (
	"context"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/brickkit/brickkit/internal/cascade"
	"github.com/brickkit/brickkit/internal/config"
	"github.com/brickkit/brickkit/internal/manifest"
	"github.com/brickkit/brickkit/internal/resolver"
	"github.com/brickkit/brickkit/internal/shell"
	"github.com/brickkit/brickkit/internal/source"
)

// newGraphCommand 实现 brickkit graph。
func newGraphCommand(opts *Options) *cobra.Command {
	var ignoreServedBy bool

	cmd := &cobra.Command{
		Use:     "graph",
		Short:   "把项目的依赖拓扑输出成 Mermaid 图",
		GroupID: groupProject,
		Long: `把当前项目的依赖拓扑画成 Mermaid 图，打印到 stdout。

图上有什么：
  节点     组件 id@版本；local: true 的标上本地调试端口
  实线     强依赖；虚线是弱依赖（取不到的弱依赖画成"未安装"节点）
  置灰     这次不会启动的组件（被 enabled: false 关掉的，以及跟着上层一起不跑的）
  外壳分组 servedBy 收编的成员画在它们的外壳（subgraph）里

边总是画出来，不管对方这次有没有启动——图展示的是声明的结构，"启动与否"用节点样式表达。

stdout 里只有 Mermaid，可以直接存文件（brickkit graph > graph.mmd），粘进
GitHub / Notion / Markdown 里就会被渲染。依赖解析产生的警告写到 stderr。
和 up 一样，还没缓存的市场 / Git 组件会联网取 Manifest；解析不出依赖图时报同样的错误。`,
		Example: `  brickkit graph
  brickkit graph > graph.mmd
  brickkit graph --ignore-served-by   不看 servedBy，看每个组件独立启动时的样子`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runGraph(cmd.Context(), opts, ignoreServedBy)
		},
	}

	cmd.Flags().BoolVar(&ignoreServedBy, "ignore-served-by", false,
		"内存里清空全部 servedBy 声明再画（与 up --ignore-served-by 同义）；不写回 brickkit.yaml")
	return cmd
}

// runGraph 执行 brickkit graph。
func runGraph(ctx context.Context, opts *Options, ignoreServedBy bool) error {
	if ctx == nil {
		ctx = context.Background()
	}

	layout := config.NewLayout(opts.WorkDir, opts.ConfigPath)
	cfg, err := config.ParseConfigFile(layout.ConfigPath())
	if err != nil {
		return err
	}
	if ignoreServedBy {
		clearServedBy(cfg)
	}

	// 没有组件就不必去碰安装源：那一步会读公钥文件，而这里根本用不上
	if len(cfg.Components) == 0 {
		opts.Printf("%s", renderMermaid(cfg, &resolver.Graph{}, &cascade.Result{}, ignoreServedBy))
		return nil
	}

	client, err := newSourceClient(opts, layout, cfg, source.Options{})
	if err != nil {
		return err
	}
	defer func() { _ = client.Close() }()

	graph, states, err := resolveTopology(ctx, client, cfg)
	if err != nil {
		return err
	}

	// stdout 只留给 Mermaid：解析警告（弱依赖取不到之类）走 stderr
	for _, w := range graph.Warnings {
		_, _ = fmt.Fprint(opts.Stderr, w.Format())
	}
	opts.Printf("%s", renderMermaid(cfg, graph, states, ignoreServedBy))
	return nil
}

// 节点样式类。
const (
	classDisabled = "disabled"
	classLocal    = "local"
	classMissing  = "missing"
)

// mermaidClassDefs 按输出顺序列出样式类。用 classDef + class 而不是逐节点 style：
// 一处改样式，全图跟着变。
var mermaidClassDefs = []struct{ name, style string }{
	{classDisabled, "fill:#eee,stroke:#999,color:#999"},
	{classLocal, "fill:#e6f2ff,stroke:#3673a8"},
	{classMissing, "fill:#fff4e5,stroke:#c77700,stroke-dasharray:4 3"},
}

// mermaidID 是组件在图里的节点 ID：版本化服务名（小写、/ 与 . 都换成 -），
// 已经是合法的 Mermaid 标识符，不必再发明一套命名规则。
func mermaidID(ref resolver.Ref) string { return manifest.ServiceName(ref.ID, ref.Version) }

// renderMermaid 把拓扑渲染成 Mermaid 文本。
//
// 输出全由输入决定、顺序全按 graph.Nodes 的顺序（依赖先于依赖方）：同一份配置
// 每次画出的图逐字节相同，才能放进版本控制里看 diff。
func renderMermaid(
	cfg *config.Config, graph *resolver.Graph, states *cascade.Result, ignoredServedBy bool,
) string {
	entries := make(map[resolver.Ref]config.Component, len(cfg.Components))
	for _, c := range cfg.Components {
		entries[resolver.Ref{ID: c.ID, Version: c.Version}] = c
	}

	var b strings.Builder
	b.WriteString("graph TD\n")
	if ignoredServedBy {
		b.WriteString("    %% 已忽略全部 servedBy 声明（--ignore-served-by：仅用于验证组件独立启动能力）\n")
	}
	if len(graph.Nodes) == 0 {
		b.WriteString("    %% 当前项目没有组件\n")
		return b.String()
	}

	// 样式类 → 用到它的节点 ID，按节点声明的顺序累积
	classes := map[string][]string{}
	tag := func(class string, ref resolver.Ref) {
		classes[class] = append(classes[class], mermaidID(ref))
	}
	declare := func(indent string, ref resolver.Ref) {
		label := ref.String()
		if entry := entries[ref]; entry.Local {
			label += fmt.Sprintf("<br/>本地调试 :%d", entry.LocalPort)
			tag(classLocal, ref)
		}
		if !states.IsRunning(ref) {
			tag(classDisabled, ref)
		}
		fmt.Fprintf(&b, "%s%s[\"%s\"]\n", indent, mermaidID(ref), label)
	}

	// servedBy 分组：只看各条目自己写的 servedBy，不跑环境变量注入。
	// 外壳不在图里（目标不存在）时照样成组——目标在不在是 up 在生成阶段报的事。
	members := map[resolver.Ref][]resolver.Ref{}
	var shells []resolver.Ref
	inShell := map[resolver.Ref]bool{}
	for _, node := range graph.Nodes {
		entry, ok := entries[node.Ref]
		if !ok || entry.ServedBy == "" {
			continue
		}
		target, ok := shell.ParseRef(entry.ServedBy)
		if !ok {
			continue
		}
		if _, seen := members[target]; !seen {
			shells = append(shells, target)
		}
		members[target] = append(members[target], node.Ref)
		inShell[node.Ref] = true
	}

	// 子图 ID 带 -members 后缀：外壳自己也是一个以它的服务名为 ID 的普通节点，
	// 两者不能同名。版本化服务名总以 -数字-数字-数字 结尾，所以不会撞上任何组件节点。
	for _, target := range shells {
		fmt.Fprintf(&b, "    subgraph %s-members[\"外壳：%s\"]\n", mermaidID(target), target)
		for _, ref := range members[target] {
			declare("        ", ref)
		}
		b.WriteString("    end\n")
	}
	for _, node := range graph.Nodes {
		if !inShell[node.Ref] {
			declare("    ", node.Ref)
		}
	}

	// 取不到的弱依赖：画成占位节点。up 的"依赖图"一节把它们写成"（弱，未安装）"，
	// 图里不画等于悄悄丢掉一条声明过的依赖
	placeholder := map[resolver.Ref]bool{}
	for _, node := range graph.Nodes {
		for _, ref := range node.MissingOptional {
			if placeholder[ref] {
				continue
			}
			placeholder[ref] = true
			fmt.Fprintf(&b, "    %s[\"%s<br/>未安装\"]\n", mermaidID(ref), ref)
			tag(classMissing, ref)
		}
	}

	// 边总是画出来，不管对方这次有没有启动：图展示的是声明的结构
	for _, node := range graph.Nodes {
		from := mermaidID(node.Ref)
		for _, dep := range node.Requires {
			fmt.Fprintf(&b, "    %s --> %s\n", from, mermaidID(dep))
		}
		for _, dep := range node.Optional {
			fmt.Fprintf(&b, "    %s -.-> %s\n", from, mermaidID(dep))
		}
		for _, dep := range node.MissingOptional {
			fmt.Fprintf(&b, "    %s -.-> %s\n", from, mermaidID(dep))
		}
	}

	for _, def := range mermaidClassDefs {
		ids := classes[def.name]
		if len(ids) == 0 {
			continue
		}
		fmt.Fprintf(&b, "    classDef %s %s;\n", def.name, def.style)
		fmt.Fprintf(&b, "    class %s %s\n", strings.Join(ids, ","), def.name)
	}
	return b.String()
}
```

`internal/cli/root.go`：在 `root.AddCommand(...)` 里 `newSkillsCommand(opts),` 之后加 `newGraphCommand(opts),`。`internal/cli/cli_test.go`：`allCommands` 加 `"graph"`；`TestSubcommandFlags` 的 `want` 加 `"graph": {"ignore-served-by"}`。

- [ ] **Step 4: 跑测试**

Run: `go test ./internal/cli/ -run 'TestGraph|TestRootHelp|TestEachSubcommandHelp|TestSubcommandFlags|TestGlobalConfigFlag' -count=1`
Expected: PASS。若 Task 3 的期望输出只有顺序不同，按 Global Constraints 里"先跑再写"的规矩处理。

- [ ] **Step 5: 用真实 Mermaid 渲染器把关（一次性人工核对，不进仓库）**

把 `TestGraphGroupsServedByMembersUnderTheirShell` 那种带子图的输出存成文件，确认 Mermaid 能解析：
`npx --yes @mermaid-js/mermaid-cli -i graph.mmd -o graph.svg`（需要联网与 Chromium；装不上就跳过，在报告里写明"未做渲染器核对"，不要假装做了）。子图 ID 带 `-members`、标签里的 `<br/>`、`class a,b name` 这三处是最可能踩到语法坑的地方。

- [ ] **Step 6: 把命令总数的文字声明改成 15**（见 Global Constraints）

`git grep -nE "1[0-9] (个命令|条命令|commands)" -- ':!docs/archive' ':!docs/superpowers' ':!改进计划.md'`，把所有"14 个命令""14 commands"之类改成 15（`AGENTS.md`/`AGENTS.zh.md` §8 标题、`README.md`/`README.zh.md`、`llms.txt`/`llms.zh.txt`），只改数字，不动命令清单（清单在 Task 9 补齐）。改完 `bash .superpowers/sdd/2026-09-19-graph-lint-schema/lint.sh` 必须 exit=0。

- [ ] **Step 7: 全量检查并提交**

```bash
go test ./... -count=1
# 完整 make lint
git add AGENTS.md AGENTS.zh.md README.md README.zh.md llms.txt llms.zh.txt internal/cli/graph.go internal/cli/graph_test.go internal/cli/root.go internal/cli/cli_test.go
git commit -F <写好信息的文件>   # 新增：brickkit graph，把依赖拓扑输出成 Mermaid
```

> **实施修订（Task 3 实现者发现、控制器裁决，代码已按此实现；上面的代码块与测试是初稿）：**
> ① `local: true` 没写 `localPort` 时**不画端口**（初稿会画出 `本地调试 :0`——`localPort` 是可选的，没写时由 compose 分配）；`本地调试` 恒有，`:<port>` 只在写了 `localPort` 时带。
> ② `local` 样式类只套给**在跑**的组件：`cascade` 从不读 `local`，`local: true` 的组件也可能被跳过；不在跑的节点只套 `disabled`，标签里的"本地调试"保留。（设计书 §2.4 已同步更正，Task 9 的文档不要写"local 与 disabled 不会同时出现"。）
> ③ `TestGraphFailsLikeUpWhenRequiredDependencyMissing` 用 `runWith(... LogLevel: logging.LevelInfo ...)` 并断言 `"error_code":"DEPENDENCY_MISSING"`：`runIn` 把日志关了，错误码只在 JSON 日志行里。Task 6 的测试同理，已改用 `runWithLogs`。
> ④ 命令总数的文字声明由每个加命令的任务自己改（见 Global Constraints）。

---

## Task 4: `source.Client.LocalManifestFiles`

**背景：** `lint` 要离线、只读地找到本地安装源目录下**每一份** `component.yaml`。现有的两条路都不合用：`Client.Manifest` 会写 `.brickkit/manifests/` 缓存；`LocalComponents` 先过一遍"表头"筛选，表头不合格的文件被当成"问题"只给一句话原因，而这些恰是 `lint` 要完整报告的。所以新增一个只枚举、不解析的方法。它与 `listComponents` 共用目录遍历，从 `listComponents` 里抽出来，不复制第二份。

**Files:**
- Modify: `internal/source/local.go`（抽出 `manifestFiles`；`listComponents` 改用它）
- Modify: `internal/source/list.go`（`listableFetcher` 加 `manifestFiles`；新增 `LocalManifestFile` 与 `Client.LocalManifestFiles`）
- Modify: `internal/source/list_test.go`（追加测试；先读它，沿用里面建本地源的辅助函数）

**Interfaces:**
- Produces（Task 6 依赖）：
  ```go
  type LocalManifestFile struct {
      ID       string // 按目录名（<scope>/<name>）拼出来的组件 ID
      SourceID string // 提供它的安装源 id
      Path     string // component.yaml 的完整路径
  }
  func (c *Client) LocalManifestFiles() ([]LocalManifestFile, error)
  ```
  只列启用的 local 源；跳过点开头的目录（`.archived/`、`.git/`）、非法组件 ID 的目录、没有 `component.yaml` 的目录；**不**过表头筛选（内容坏的文件照样列出）；不写任何文件；不碰 git / market 源。某个本地源的根目录不存在时返回该源的 `CONFIG_INVALID` 错误（与 `LocalComponents` 一致）。

- [ ] **Step 1: 写测试**

这个文件已经有辅助函数，直接用：`newProject(t)`（建一个项目目录，返回 `config.Layout`）、`writeComponent(t, root, componentSpec{ID, Version})`（往本地源目录写一个合法组件）、`writeFile(t, path, text)`、`mkdirs(t, root, dirs...)`、`newClient(t, layout, cfg, Options{})`、`cfgWithSources(...)`。在文件末尾追加：

```go
const localDev = "local-dev"

func localDevConfig() *config.Config {
	return cfgWithSources(config.Source{ID: localDev, Type: config.SourceTypeLocal, Path: "./components"})
}

// 内容坏掉的文件也要列出来：那正是 lint 要报告的，不能在枚举这一步就丢掉。
func TestLocalManifestFilesIncludesBrokenFiles(t *testing.T) {
	layout := newProject(t)
	root := filepath.Join(layout.Root, "components")
	writeComponent(t, root, componentSpec{ID: "demo/ok", Version: "1.0.0"})
	writeFile(t, filepath.Join(root, "demo", "broken", "component.yaml"), "这不是合法的 YAML: [\n")
	writeFile(t, filepath.Join(root, "demo", "noheader", "component.yaml"), "apiVersion: brickkit/v1\n")

	c := newClient(t, layout, localDevConfig(), Options{})
	got, err := c.LocalManifestFiles()
	require.NoError(t, err)

	require.Len(t, got, 3)
	assert.Equal(t, []string{"demo/broken", "demo/noheader", "demo/ok"},
		[]string{got[0].ID, got[1].ID, got[2].ID}, "ID 取目录名，按目录顺序，输出稳定")
	assert.Equal(t, localDev, got[0].SourceID)
	assert.Equal(t, filepath.Join(root, "demo", "broken", "component.yaml"), got[0].Path)
}

// 与 LocalComponents 同一套目录规则：点开头的目录、没有 component.yaml 的目录、非法组件 ID 都不算。
func TestLocalManifestFilesSkipsArchivedAndNonComponents(t *testing.T) {
	layout := newProject(t)
	root := filepath.Join(layout.Root, "components")
	writeComponent(t, root, componentSpec{ID: "demo/ok", Version: "1.0.0"})
	writeComponent(t, filepath.Join(root, ".archived"), componentSpec{ID: "demo/old", Version: "1.0.0"})
	writeComponent(t, filepath.Join(root, ".git"), componentSpec{ID: "demo/git", Version: "1.0.0"})
	mkdirs(t, root, "demo/nofile") // 有目录、没有 component.yaml
	writeFile(t, filepath.Join(root, "Demo", "Upper", "component.yaml"), "x: 1\n") // 大写：非法组件 ID

	c := newClient(t, layout, localDevConfig(), Options{})
	got, err := c.LocalManifestFiles()
	require.NoError(t, err)

	require.Len(t, got, 1)
	assert.Equal(t, "demo/ok", got[0].ID)
}

func TestLocalManifestFilesSkipsDisabledAndNonLocalSources(t *testing.T) {
	layout := newProject(t)
	shared := filepath.Join(layout.Root, "components")
	off := filepath.Join(layout.Root, "off")
	writeComponent(t, shared, componentSpec{ID: "demo/ok", Version: "1.0.0"})
	writeComponent(t, off, componentSpec{ID: "demo/hidden", Version: "1.0.0"})

	disabled := false
	cfg := cfgWithSources(
		config.Source{ID: localDev, Type: config.SourceTypeLocal, Path: "./components"},
		config.Source{ID: "off", Type: config.SourceTypeLocal, Path: "./off", Enabled: &disabled},
		// 这两个地址连不上：如果这个方法碰了它们，调用会失败或卡住
		config.Source{ID: "git", Type: config.SourceTypeGit, URL: "http://127.0.0.1:1/x.git"},
		config.Source{ID: "market", Type: config.SourceTypeMarket, URL: "http://127.0.0.1:1/api/v1"},
	)
	c := newClient(t, layout, cfg, Options{})

	got, err := c.LocalManifestFiles()
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, "demo/ok", got[0].ID)
}

func TestLocalManifestFilesListsEverySourceInOrder(t *testing.T) {
	layout := newProject(t)
	writeComponent(t, filepath.Join(layout.Root, "a"), componentSpec{ID: "demo/one", Version: "1.0.0"})
	writeComponent(t, filepath.Join(layout.Root, "b"), componentSpec{ID: "demo/two", Version: "1.0.0"})
	cfg := cfgWithSources(
		config.Source{ID: "first", Type: config.SourceTypeLocal, Path: "./a"},
		config.Source{ID: "second", Type: config.SourceTypeLocal, Path: "./b"},
	)

	got, err := newClient(t, layout, cfg, Options{}).LocalManifestFiles()
	require.NoError(t, err)
	require.Len(t, got, 2)
	assert.Equal(t, "first", got[0].SourceID)
	assert.Equal(t, "demo/one", got[0].ID)
	assert.Equal(t, "second", got[1].SourceID)
	assert.Equal(t, "demo/two", got[1].ID)
}

func TestLocalManifestFilesReportsMissingRoot(t *testing.T) {
	layout := newProject(t)
	cfg := cfgWithSources(config.Source{ID: "gone", Type: config.SourceTypeLocal, Path: "./nowhere"})

	_, err := newClient(t, layout, cfg, Options{}).LocalManifestFiles()
	require.Error(t, err)
	assert.Equal(t, clierr.CodeConfigInvalid, clierr.As(err).Code)
	assert.Contains(t, clierr.As(err).Message, "本地安装源路径不存在")
}

// lint 靠它保证"纯只读"：调用前后，项目目录下没有多出任何文件（尤其是 .brickkit/manifests）。
func TestLocalManifestFilesWritesNothing(t *testing.T) {
	layout := newProject(t)
	writeComponent(t, filepath.Join(layout.Root, "components"), componentSpec{ID: "demo/ok", Version: "1.0.0"})
	c := newClient(t, layout, localDevConfig(), Options{})

	tree := func() []string {
		var out []string
		require.NoError(t, filepath.WalkDir(layout.Root, func(p string, _ os.DirEntry, err error) error {
			require.NoError(t, err)
			out = append(out, p)
			return nil
		}))
		return out
	}
	before := tree()
	_, err := c.LocalManifestFiles()
	require.NoError(t, err)
	assert.Equal(t, before, tree())
}
```

若 `newProject` 已经建了 `.brickkit/` 之类的目录，`WritesNothing` 照样成立（比较的是前后差异）。若 `newClient` 的第三个参数类型与上面不符（比如它要 `*config.Config` 之外的东西），照文件里现有用例的调用方式改，别改辅助函数。再确认既有的 `LocalComponents` 相关测试原样通过——这次重构不许改变它们。

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./internal/source/ -run TestLocalManifestFiles -count=1`
Expected: 编译失败，`c.LocalManifestFiles undefined`

- [ ] **Step 3: 实现**

`internal/source/local.go`：把 `listComponents` 里"扫 `<scope>/<name>`"的遍历抽成 `manifestFiles`，`listComponents` 改成用它。注意保持 `listComponents` 的可观察行为不变：

```go
// localManifestFile 是本地安装源目录下的一份 component.yaml。
type localManifestFile struct {
	// id 是按目录名（<scope>/<name>）拼出来的组件 ID。
	id string
	// path 是 component.yaml 的完整路径。
	path string
}

// manifestFiles 枚举 <root>/<scope>/<name>/component.yaml（003 §6.4）。
//
// 两道过滤缺一不可：
//   - 点开头的目录一律不当作 scope。默认约定里 local 源就指向 ./components，
//     而 components/.archived/（brickkit sync 的归档目录）和 .git/ 都在那底下。
//   - 目录名拼出来必须是合法组件 ID。非法 ID 进不了 brickkit.yaml，
//     扫出来只会在后面炸；这里挡住，报错才有意义。
//
// 只看文件在不在，读不读得动、内容对不对是调用方的事：listComponents 要在此之上
// 做"表头"筛选，lint 要完整报告每一份文件——枚举这一步不能替它们丢东西。
func (s *localSource) manifestFiles() ([]localManifestFile, error) {
	if err := s.checkRoot(); err != nil {
		return nil, err
	}

	scopes, err := os.ReadDir(s.root)
	if err != nil {
		return nil, s.listError(s.root, err)
	}

	var out []localManifestFile
	for _, scope := range scopes {
		if !scope.IsDir() || strings.HasPrefix(scope.Name(), ".") {
			continue
		}
		names, err := os.ReadDir(filepath.Join(s.root, scope.Name()))
		if err != nil {
			return nil, s.listError(filepath.Join(s.root, scope.Name()), err)
		}
		for _, name := range names {
			if !name.IsDir() || strings.HasPrefix(name.Name(), ".") {
				continue
			}
			id := scope.Name() + "/" + name.Name()
			if manifest.ComponentIDProblem(id) != "" {
				continue
			}
			dir := filepath.Join(s.root, scope.Name(), name.Name())
			if !hasManifest(dir) {
				// 没有 component.yaml 的只是个普通目录，不是组件
				continue
			}
			out = append(out, localManifestFile{id: id, path: filepath.Join(dir, manifest.FileName)})
		}
	}
	return out, nil
}
```

`listComponents` 保留原来的函数注释里"下面几种是'像组件、但用不了'"那段解释，主体改成：

```go
func (s *localSource) listComponents() ([]string, []listProblem, error) {
	files, err := s.manifestFiles()
	if err != nil {
		return nil, nil, err
	}

	var ids []string
	var problems []listProblem
	for _, f := range files {
		data, err := os.ReadFile(f.path)
		if err != nil {
			continue
		}
		// （原有的 componentHeader 解析与三种 problems 判断，把 id 换成 f.id，其余一字不动）
	}

	sort.Strings(ids)
	return ids, problems, nil
}
```

`internal/source/list.go`：`listableFetcher` 接口加一个方法，并新增导出的类型与方法：

```go
type listableFetcher interface {
	// listComponents 返回该源里的组件 ID（已排序），以及"像组件但用不了"的那些。
	listComponents() ([]string, []listProblem, error)
	// manifestFiles 返回该源里每一份 component.yaml 的位置，不解析内容。
	manifestFiles() ([]localManifestFile, error)
}

// LocalManifestFile 是本地安装源目录下的一份 component.yaml。
type LocalManifestFile struct {
	// ID 是按目录名（<scope>/<name>）拼出来的组件 ID——不是文件里写的那个，
	// 两者对不上正是 lint 要报的一种问题。
	ID string
	// SourceID 是提供它的安装源 id。
	SourceID string
	// Path 是这份文件的完整路径。
	Path string
}

// LocalManifestFiles 列出所有启用的本地安装源里、活跃目录下的 component.yaml。
//
// 只做枚举，不解析内容、不写任何缓存、不碰 git / market 源——brickkit lint 靠它
// 离线、只读地找到"使用者自己能编辑的那些文件"。
//
// 与 LocalComponents 用同一套目录规则（manifestFiles），但不像它那样先过一遍"表头"筛选：
// 表头不合格的文件正是 lint 要完整报告的对象。归档目录（.archived/）因此同样不扫：
// 那是 sync 挪开的、暂时不用的那份，不是使用者此刻在编辑的文件。
func (c *Client) LocalManifestFiles() ([]LocalManifestFile, error) {
	var out []LocalManifestFile
	for _, f := range c.fetchers {
		lister, ok := f.(listableFetcher)
		if !ok {
			continue
		}
		files, err := lister.manifestFiles()
		if err != nil {
			return nil, err
		}
		for _, m := range files {
			out = append(out, LocalManifestFile{ID: m.id, SourceID: f.id(), Path: m.path})
		}
	}
	return out, nil
}
```

- [ ] **Step 4: 跑测试**

Run: `go test ./internal/source/ -count=1`
Expected: 全部 PASS（含既有的 `LocalComponents` 用例）

- [ ] **Step 5: 全量检查并提交**

```bash
go test ./... -count=1
# 完整 make lint
git add internal/source/local.go internal/source/list.go internal/source/list_test.go
git commit -F <写好信息的文件>   # 新增：source.LocalManifestFiles，只读地枚举本地源里的 component.yaml
```

---

## Task 5: `inject.ReservedKeyWarnings`

**背景：** 配置项名字（转成环境变量名后）撞上平台保留变量，今天只有两处会发现：市场发布时拒绝、`up` 注入时警告并跳过。组件作者在自己的仓库里、发布之前，没有任何办法提前知道。规则本身早就存在（`inject.envBuilder.matchReserved`），这里把"不依赖 `envPrefix` 的那部分"提成包级函数，`up` 注入与 `lint` 共用同一份。`envPrefix` 是使用者在 `brickkit.yaml` 里定的，组件仓库里看不到，所以不在检查范围内——与市场发布时看不到它是同一个道理。

**Files:**
- Modify: `internal/inject/reserved.go`
- Test: `internal/inject/reserved_test.go`（已有则追加，没有就新建；先 `ls internal/inject/` 确认）

**Interfaces:**
- Produces（Task 6 依赖）：`func ReservedKeyWarnings(m *manifest.Manifest) []*clierr.Error`——按配置项名字排序，每个撞上的配置项一条 `reservedConflictWarning`（警告，`Warning: true`）；`m` 为 nil 或没有 `configSchema` 时返回 nil。

- [ ] **Step 1: 写测试**

```go
package inject

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/manifest"
)

func manifestWithConfigKeys(keys ...string) *manifest.Manifest {
	props := map[string]manifest.ConfigProperty{}
	for _, k := range keys {
		props[k] = manifest.ConfigProperty{Type: "string"}
	}
	return &manifest.Manifest{
		Metadata:     manifest.Metadata{ID: "demo/hello"},
		ConfigSchema: &manifest.ConfigSchema{Properties: props},
	}
}

func TestReservedKeyWarningsFlagsEveryReservedShape(t *testing.T) {
	m := manifestWithConfigKeys(
		"databaseHost",     // DATABASE_HOST：资源前缀
		"notifierEndpoint", // NOTIFIER_ENDPOINT：*_ENDPOINT 后缀
		"componentId",      // COMPONENT_ID：精确匹配
		"pageSize",         // 不冲突
	)
	warnings := ReservedKeyWarnings(m)
	require.Len(t, warnings, 3)
	for _, w := range warnings {
		assert.True(t, w.Warning, "是警告不是错误：写错一个配置项名不该让整个项目起不来")
		assert.Equal(t, clierr.CodeConfigConflict, w.Code)
	}

	var keys []string
	for _, w := range warnings {
		for _, d := range w.Details {
			if d.Key == "配置项" {
				keys = append(keys, d.Value)
			}
		}
	}
	assert.Equal(t, []string{"componentId", "databaseHost", "notifierEndpoint"}, keys, "按配置项名字排序，输出稳定")
}

func TestReservedKeyWarningsNamesTheComponent(t *testing.T) {
	warnings := ReservedKeyWarnings(manifestWithConfigKeys("databaseHost"))
	require.Len(t, warnings, 1)
	assert.Contains(t, warnings[0].Format(), "demo/hello")
	assert.Contains(t, warnings[0].Format(), "DATABASE_HOST")
}

func TestReservedKeyWarningsNothingToCheck(t *testing.T) {
	assert.Nil(t, ReservedKeyWarnings(nil))
	assert.Nil(t, ReservedKeyWarnings(&manifest.Manifest{}))
	assert.Nil(t, ReservedKeyWarnings(manifestWithConfigKeys("pageSize", "timeoutSeconds")))
}

// up 注入与 lint 必须是同一份规则：没有 envPrefix 时，matchReserved 与 staticReserved 处处一致。
func TestStaticReservedMatchesEnvBuilderWithoutPrefixes(t *testing.T) {
	b := &envBuilder{}
	for _, name := range []string{
		"DATABASE_HOST", "REDIS_URL", "MQ_VHOST", "STORAGE_BUCKET", "SEARCH_INDEX", "SMTP_HOST",
		"COMPONENT_ID", "COMPONENT_VERSION", "BRICKKIT_SERVED_MEMBERS", "BRICKKIT_SERVED_MEMBERS_CONFIG",
		"X_ENDPOINT", "PAGE_SIZE", "DATABASE", "ENDPOINT", "",
	} {
		wantPattern, wantHit := staticReserved(name)
		gotPattern, gotHit := b.matchReserved(name)
		assert.Equal(t, wantHit, gotHit, name)
		assert.Equal(t, wantPattern, gotPattern, name)
	}
}

func TestEnvBuilderStillRespectsUserDefinedPrefixes(t *testing.T) {
	b := &envBuilder{reservedPrefixes: []string{"PRIMARY_"}}
	pattern, hit := b.matchReserved("PRIMARY_HOST")
	assert.True(t, hit)
	assert.Equal(t, "PRIMARY_*", pattern)
	_, hit = staticReserved("PRIMARY_HOST")
	assert.False(t, hit, "envPrefix 是项目里才知道的，静态规则不含它")
}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./internal/inject/ -run 'TestReservedKeyWarnings|TestStaticReserved|TestEnvBuilderStill' -count=1`
Expected: 编译失败，`undefined: staticReserved` / `undefined: ReservedKeyWarnings`

- [ ] **Step 3: 实现**

`internal/inject/reserved.go`：把 `matchReserved` 里前三个循环提成 `staticReserved`，`matchReserved` 先调它、再看使用者定义的前缀；新增 `ReservedKeyWarnings`。导入里补 `"github.com/brickkit/brickkit/internal/manifest"`。

```go
// staticReserved 判断环境变量名是否命中保留模式中**不依赖项目配置**的那部分
// （精确匹配、*_ENDPOINT 后缀、资源类型前缀），返回命中的模式。
//
// 与市场发布时校验的是同一套规则（007 §18.1）。它单独成函数，是为了让不在
// 注入现场的调用方——brickkit lint 在组件仓库里检查 Manifest——也能用同一份判断，
// 而不是再抄一遍。
func staticReserved(name string) (string, bool) {
	for _, exact := range reservedExact {
		if name == exact {
			return exact, true
		}
	}
	for _, suffix := range reservedSuffix {
		if strings.HasSuffix(name, suffix) {
			return "*" + suffix, true
		}
	}
	for _, prefix := range reservedPrefix {
		if strings.HasPrefix(name, prefix) {
			return prefix + "*", true
		}
	}
	return "", false
}

// matchReserved 判断环境变量名是否命中保留模式，返回命中的模式。
func (b *envBuilder) matchReserved(name string) (string, bool) {
	if pattern, hit := staticReserved(name); hit {
		return pattern, true
	}
	for _, prefix := range b.reservedPrefixes {
		if strings.HasPrefix(name, prefix) {
			return prefix + "*", true
		}
	}
	return "", false
}

// ReservedKeyWarnings 检查一份 Manifest 的 configSchema 里有没有配置项名字撞上平台保留变量。
//
// 这是 up 注入时那条警告的离线版：规则同一份（staticReserved），措辞同一份
// （reservedConflictWarning）。不含使用者在 brickkit.yaml 里定的 envPrefix——组件仓库里
// 看不到它，市场发布时同样看不到，所以那一半只能留给注入现场。
//
// 是警告不是错误，理由同 reservedConflictWarning：一个配置项名字写错，不该让整个项目起不来。
func ReservedKeyWarnings(m *manifest.Manifest) []*clierr.Error {
	if m == nil || m.ConfigSchema == nil {
		return nil
	}
	var warnings []*clierr.Error
	for _, key := range sortedConfigKeys(m.ConfigSchema.Properties) {
		name := EnvVarName(key)
		if pattern, hit := staticReserved(name); hit {
			warnings = append(warnings, reservedConflictWarning(m.Metadata.ID, key, name, pattern))
		}
	}
	return warnings
}
```

`sortedConfigKeys` 已存在于 `inject.go`（`addConfig` 在用）；先看它的签名，若它不接受 `map[string]manifest.ConfigProperty` 就照它的签名传。

- [ ] **Step 4: 跑测试**

Run: `go test ./internal/inject/ -count=1`
Expected: 全部 PASS（既有的注入用例证明 `up` 侧行为没变）

- [ ] **Step 5: 全量检查并提交**

```bash
go test ./... -count=1
# 完整 make lint
git add internal/inject/reserved.go internal/inject/reserved_test.go
git commit -F <写好信息的文件>   # 新增：inject.ReservedKeyWarnings，保留变量冲突的离线检查
```

---

## Task 6: `brickkit lint` 与 `LINT_FAILED`

**Files:**
- Create: `internal/cli/lint.go`
- Create: `internal/cli/lint_test.go`
- Modify: `internal/clierr/clierr.go`（新增 `CodeLintFailed`）
- Modify: `internal/cli/root.go`（注册）、`internal/cli/cli_test.go`（`allCommands` 加 `"lint"`；`TestSubcommandFlags` 加 `"lint": {"strict"}`）
- Modify: `docs/en/06-architecture/10-error-codes.md`、`docs/zh/06-architecture/10-error-codes.md`（`LINT_FAILED` 一节 + 索引行）

**Interfaces:**
- Consumes：`detectScope`（Task 1）、`source.New` + `Client.LocalManifestFiles`（Task 4）、`inject.ReservedKeyWarnings`（Task 5）、既有的 `manifest.Parse`、`manifest.PropertyKeyWarnings`、`config.ParseConfigFile`、`displayPath`（`up.go`）。
- Produces：`clierr.CodeLintFailed Code = "LINT_FAILED"`；`newLintCommand`。

**行为（设计书 §3）：**

- `detectScope`：项目模式 → 检查 `brickkit.yaml`（`config.ParseConfigFile`），通过后再对 `source.New(layout, cfg, source.Options{})` 的 `LocalManifestFiles()` 逐个文件检查；组件仓库模式 → 只检查当前目录的 `component.yaml`。两者都没有 → `detectScope` 的 `PROJECT_MISSING`。
- 每份 `component.yaml`：`os.ReadFile` → `manifest.Parse`（失败 → 错误，用 `clierr.As(err)` 原样带上）；`Parse` 成功后：项目模式下核对 `m.Metadata.ID` 与目录名拼出的 ID 一致（不一致 → `MANIFEST_INVALID` 错误）；无论 `Parse` 成败都跑 `manifest.PropertyKeyWarnings(raw, 显示路径)`（它自己对 YAML 语法错误返回 nil）；`Parse` 成功时再跑 `inject.ReservedKeyWarnings(m)`，并给每条警告补一行 `来源：<显示路径>`（它的块里本来只有组件 ID）。
- `brickkit.yaml` 没通过时：跳过本地源扫描，汇总前打一行 `ℹ️` 说明跳过了什么、为什么。`LocalManifestFiles` 自己出错（本地源根目录不存在）时，错误算在 `brickkit.yaml` 头上（那是它的 `sources[].path` 配错了）。
- 报告写 **stdout**：干净的文件一行 `✅ <相对路径>`；有问题的直接打印 `Format()` 块（不再另起标题行）；末行汇总 `📋 检查了 N 个文件：M 个有错误，K 条警告`。
- 退出：没有错误、且（没开 `--strict` 或没有警告）→ 0；否则返回 `clierr.New(clierr.CodeLintFailed, "错误：结构检查未通过")`（带"已检查 / 有错误 / 警告"明细与一条建议），退出码 1。

- [ ] **Step 1: 写测试**

新建 `internal/cli/lint_test.go`。先 `Read` `internal/cli/testsupport_component_test.go` 里的 `comp.files()`、`breakLocalManifest`、`newComponentRepo`、`writeTree`，沿用它们：

```go
package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/logging"
)

// runWithLogs 把日志级别打开再执行：错误码只出现在 stderr 的 JSON 日志行里
// （❌ 块本身不带码，AGENTS §10），而 runIn 默认把日志关了。
func runWithLogs(t *testing.T, dir string, args ...string) result {
	t.Helper()
	return runWith(t, func(o *Options) { o.LogLevel = logging.LevelInfo }, dir, args...)
}

// newLintFixture 建一个带本地源的项目，并把 comps 都 add 进 brickkit.yaml。
// （不叫 lintProject：那是 lint.go 里生产代码的函数名，同一个包里不能重名。）
func newLintFixture(t *testing.T, comps ...comp) *projectFixture {
	t.Helper()
	dir := t.TempDir()
	f := newProjectFixtureAt(t, dir, oneLocalSource(t, dir, comps...)...)
	require.Equal(t, clierr.ExitOK, runIn(t, f.Dir, "add", "--local").code)
	return f
}

// manifestPath 是 oneLocalSource 把组件写到的位置。
func manifestPath(f *projectFixture, id string) string {
	return filepath.Join(f.Dir, "shared", filepath.FromSlash(id), "component.yaml")
}

func appendTo(t *testing.T, path, text string) {
	t.Helper()
	old, err := os.ReadFile(path)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, append(old, []byte(text)...), 0o644))
}

func TestLintCleanProject(t *testing.T) {
	f := newLintFixture(t, comp{ID: "demo/hello", Version: "1.0.0"}, comp{ID: "demo/caller", Version: "1.0.0"})

	r := runIn(t, f.Dir, "lint")
	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)
	assert.Contains(t, r.stdout, "✅ brickkit.yaml\n")
	assert.Contains(t, r.stdout, "✅ "+filepath.Join("shared", "demo", "hello", "component.yaml")+"\n")
	assert.Contains(t, r.stdout, "检查了 3 个文件：0 个有错误，0 条警告")
}

// 这条是 lint 存在的理由：已经 add 过的本地组件，编辑之后引入拼写错误，
// 今天要跑到 up（要引擎、要走完整级联）才会发现。
func TestLintCatchesTypoInAlreadyAddedLocalComponent(t *testing.T) {
	f := newLintFixture(t, comp{ID: "demo/hello", Version: "1.0.0"})
	appendTo(t, manifestPath(f, "demo/hello"), "dependancies:\n  components: []\n")

	r := runWithLogs(t, f.Dir, "lint")
	assert.Equal(t, clierr.ExitError, r.code)
	assert.Contains(t, r.stdout, "dependancies")
	assert.Contains(t, r.stdout, "未知字段")
	assert.Contains(t, r.stdout, "1 个有错误")
	assert.Contains(t, r.stderr, "LINT_FAILED")
}

func TestLintReportsEveryBrokenFileNotJustTheFirst(t *testing.T) {
	f := newLintFixture(t, comp{ID: "demo/hello", Version: "1.0.0"}, comp{ID: "demo/caller", Version: "1.0.0"})
	appendTo(t, manifestPath(f, "demo/hello"), "dependancies: []\n")
	appendTo(t, manifestPath(f, "demo/caller"), "migrations: {}\n")

	r := runIn(t, f.Dir, "lint")
	assert.Equal(t, clierr.ExitError, r.code)
	assert.Contains(t, r.stdout, "dependancies")
	assert.Contains(t, r.stdout, "migrations")
	assert.Contains(t, r.stdout, "2 个有错误")
}

func TestLintNotYetAddedComponentIsAlsoChecked(t *testing.T) {
	dir := t.TempDir()
	f := newProjectFixtureAt(t, dir, oneLocalSource(t, dir, comp{ID: "demo/hello", Version: "1.0.0"})...)
	appendTo(t, manifestPath(f, "demo/hello"), "dependancies: []\n")

	r := runIn(t, f.Dir, "lint")
	assert.Equal(t, clierr.ExitError, r.code, "没 add 过也照样检查：编辑这份文件的人就是使用者自己")
}

func TestLintInvalidBrickkitYamlSkipsLocalSources(t *testing.T) {
	f := newLintFixture(t, comp{ID: "demo/hello", Version: "1.0.0"})
	body := f.config(t)
	require.Contains(t, body, "target: docker")
	require.NoError(t, os.WriteFile(f.Layout.ConfigPath(),
		[]byte(strings.Replace(body, "target: docker", "target: swarm", 1)), 0o644))

	r := runIn(t, f.Dir, "lint")
	assert.Equal(t, clierr.ExitError, r.code)
	assert.Contains(t, r.stdout, "deploy.target")
	assert.NotContains(t, r.stdout, filepath.Join("shared", "demo"), "brickkit.yaml 没通过就不去扫本地源，报告里不出现任何本地组件的路径")
	assert.Contains(t, r.stdout, "ℹ️")
	assert.Contains(t, r.stdout, "1 个有错误")
}

func TestLintMissingLocalSourceDirectoryIsReported(t *testing.T) {
	dir := t.TempDir()
	f := newProjectFixtureAt(t, dir, "  - id: gone\n    type: local\n    path: ./nowhere\n")

	r := runIn(t, f.Dir, "lint")
	assert.Equal(t, clierr.ExitError, r.code)
	assert.Contains(t, r.stdout, "本地安装源路径不存在")
}

func TestLintDirectoryNameMustMatchMetadataID(t *testing.T) {
	dir := t.TempDir()
	f := newProjectFixtureAt(t, dir, oneLocalSource(t, dir, comp{ID: "demo/hello", Version: "1.0.0"})...)
	other := comp{ID: "demo/other", Version: "1.0.0"}
	require.NoError(t, os.WriteFile(manifestPath(f, "demo/hello"), []byte(other.yamlText()), 0o644))

	r := runIn(t, f.Dir, "lint")
	assert.Equal(t, clierr.ExitError, r.code)
	assert.Contains(t, r.stdout, "demo/hello")
	assert.Contains(t, r.stdout, "demo/other")
	assert.Contains(t, r.stdout, "对不上")
}

func TestLintIgnoresArchivedComponents(t *testing.T) {
	dir := t.TempDir()
	f := newProjectFixtureAt(t, dir, oneLocalSource(t, dir, comp{ID: "demo/hello", Version: "1.0.0"})...)
	writeTree(t, filepath.Join(dir, "shared", ".archived", "demo", "old"),
		map[string]string{"component.yaml": "这不是合法的 YAML: [\n"})

	r := runIn(t, f.Dir, "lint")
	assert.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)
}

const misspelledPropertyKey = `configSchema:
  type: object
  properties:
    pageSize:
      type: integer
      defualt: 20
`

func TestLintWarningsDoNotFailWithoutStrict(t *testing.T) {
	f := newLintFixture(t, comp{ID: "demo/hello", Version: "1.0.0"})
	// 追加到 healthCheck 之后即可：configSchema 是顶层键，位置无所谓
	appendTo(t, manifestPath(f, "demo/hello"), misspelledPropertyKey)

	r := runIn(t, f.Dir, "lint")
	assert.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)
	assert.Contains(t, r.stdout, "defualt")
	assert.Contains(t, r.stdout, "1 条警告")
}

func TestLintStrictTurnsWarningsIntoFailure(t *testing.T) {
	f := newLintFixture(t, comp{ID: "demo/hello", Version: "1.0.0"})
	appendTo(t, manifestPath(f, "demo/hello"), misspelledPropertyKey)

	r := runWithLogs(t, f.Dir, "lint", "--strict")
	assert.Equal(t, clierr.ExitError, r.code)
	assert.Contains(t, r.stdout, "defualt")
	assert.Contains(t, r.stderr, "LINT_FAILED")
	assert.Contains(t, r.stderr, "--strict")
}

func TestLintWarnsWhenConfigKeyCollidesWithReservedVariable(t *testing.T) {
	f := newLintFixture(t, comp{ID: "demo/hello", Version: "1.0.0"})
	appendTo(t, manifestPath(f, "demo/hello"), `configSchema:
  type: object
  properties:
    databaseHost:
      type: string
`)

	r := runIn(t, f.Dir, "lint")
	assert.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)
	assert.Contains(t, r.stdout, "DATABASE_HOST")
	assert.Contains(t, r.stdout, "来源："+filepath.Join("shared", "demo", "hello", "component.yaml"),
		"块里要带上文件路径，否则多个组件时不知道是哪一份")
}

func TestLintStandaloneComponentRepository(t *testing.T) {
	dir := t.TempDir()
	c := comp{ID: "demo/hello", Version: "1.0.0"}
	require.NoError(t, os.WriteFile(filepath.Join(dir, "component.yaml"), []byte(c.yamlText()), 0o644))

	r := runIn(t, dir, "lint")
	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)
	assert.Contains(t, r.stdout, "组件仓库")
	assert.Contains(t, r.stdout, "✅ component.yaml")
	assert.Contains(t, r.stdout, "检查了 1 个文件")

	appendTo(t, filepath.Join(dir, "component.yaml"), "dependancies: []\n")
	bad := runIn(t, dir, "lint")
	assert.Equal(t, clierr.ExitError, bad.code)
	assert.Contains(t, bad.stdout, "dependancies")
}

func TestLintOutsideAnyProjectFails(t *testing.T) {
	r := runWithLogs(t, t.TempDir(), "lint")
	assert.Equal(t, clierr.ExitError, r.code)
	assert.Contains(t, r.stderr, "PROJECT_MISSING")
}

// 承诺是"纯只读、不联网"，所以要证明：目录树前后一致，且指向不可达地址的 market / git 源不影响结果。
func TestLintIsReadOnlyAndOffline(t *testing.T) {
	dir := t.TempDir()
	sources := append(
		oneLocalSource(t, dir, comp{ID: "demo/hello", Version: "1.0.0"}),
		"  - id: unreachable-market\n    type: market\n    url: http://127.0.0.1:1/api/v1\n",
		"  - id: unreachable-git\n    type: git\n    url: http://127.0.0.1:1/x.git\n",
	)
	f := newProjectFixtureAt(t, dir, sources...)

	snapshot := func() map[string]string {
		out := map[string]string{}
		require.NoError(t, filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
			require.NoError(t, err)
			if d.IsDir() {
				out[p] = "<dir>"
				return nil
			}
			data, err := os.ReadFile(p)
			require.NoError(t, err)
			out[p] = string(data)
			return nil
		}))
		return out
	}
	before := snapshot()

	r := runIn(t, f.Dir, "lint")
	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)
	assert.Equal(t, before, snapshot(), "lint 一个字节都不该写")
}

func TestLintRejectsPositionalArguments(t *testing.T) {
	f := newLintFixture(t, comp{ID: "demo/hello", Version: "1.0.0"})
	assert.Equal(t, clierr.ExitUsage, runIn(t, f.Dir, "lint", "extra").code)
}
```

`TestLintIsReadOnlyAndOffline` 里 `unreachable-market` 用的是"如果被碰会怎样"的地址：连不上要么超时要么立刻失败，任何一种都会让退出码或耗时暴露问题。若 `sources` 里同时出现 market 源会让 `config.Validate` 要求别的字段（如 `installer`），按报错补齐，别删掉这两个源。

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./internal/cli/ -run TestLint -count=1`
Expected: FAIL（`unknown command "lint"`）

- [ ] **Step 3: 实现**

`internal/clierr/clierr.go`，在 `// 源码工作区。` 那一组之后加：

```go
	// 结构检查（brickkit lint）。
	//
	// CodeLintFailed 是"lint 查出了问题"——逐条问题已经打印在 stdout，这个码只标记整条命令的结局。
	// 不复用 CONFIG_INVALID / MANIFEST_INVALID：一次 lint 可以两者兼有，汇总只能带一个码；
	// 而 CI 脚本要区分的恰恰是"lint 查出了问题"和"配置读不出来"。
	CodeLintFailed Code = "LINT_FAILED"
```

新建 `internal/cli/lint.go`：

```go
package cli

// 本文件实现 brickkit lint：离线的结构校验。
//
// 它没有新增任何规则——只是把散在 up / add / publish 里、早就存在的结构检查，
// 收拢到一个不联网、不需要引擎、不写任何文件的入口。今天没有任何一个命令能对着
// 这两种东西单独跑一遍校验：
//   - 独立的组件仓库（只有 component.yaml、没有 brickkit.yaml）；
//   - 已经 add --local 过的本地组件——add --local 对已在配置里的同版本组件是静默跳过，
//     编辑之后引入的拼写错误，要跑到 up（要引擎、要走完整级联）才会发现。
//
// 不做的事（都有明确的理由，见设计书 §3.4）：不解析依赖图、不检查 servedBy 指向的组件
// 是否存在（那要联网，留给 up / add）、不校验 configSchema 里 enum / minimum 对应的值
// （AGENTS.md §9.12：configSchema 是说明书，不是安全闸）。

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/config"
	"github.com/brickkit/brickkit/internal/inject"
	"github.com/brickkit/brickkit/internal/manifest"
	"github.com/brickkit/brickkit/internal/skills"
	"github.com/brickkit/brickkit/internal/source"
)

// newLintCommand 实现 brickkit lint。
func newLintCommand(opts *Options) *cobra.Command {
	var strict bool

	cmd := &cobra.Command{
		Use:     "lint",
		Short:   "离线检查 brickkit.yaml 与 component.yaml 的结构",
		GroupID: groupProject,
		Long: `不联网、不需要 Docker / K8s，只读地把这个目录里的 YAML 结构检查一遍。

两种场景，按当前目录自动判断（与 brickkit skills 同一条规则，两者都有时按项目算）：

  项目（有 brickkit.yaml）
      检查 brickkit.yaml 本身，再检查本地安装源（type: local）目录下的每一份
      component.yaml——不管有没有 add 过。归档目录（.archived/）不检查。
      brickkit.yaml 自己没通过时，本地安装源在哪都不可信，会跳过后一步并说明。
  组件仓库（有 component.yaml、没有 brickkit.yaml）
      只检查这一份 component.yaml。

查的是已有的结构规则：必填字段、类型、未知字段（拼写笔误）、版本号格式、端口范围；
警告有两类——configSchema 里拼错的键（比如 defualt）不会生效，以及配置项名字撞上
平台保留变量。

不查：依赖能不能解析、servedBy 指向的组件在不在（这些要联网，留给 up / add）；
configSchema 里 enum、minimum 之类对应的值（平台不校验值，见 AGENTS.md §9.12）。

有错误时退出码为 1；只有警告时为 0，加 --strict 则警告也算失败，给 CI 门禁用。`,
		Example: `  brickkit lint
  brickkit lint --strict   警告也算失败（CI 门禁）
  brickkit lint --config brickkit.prod.yaml`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runLint(opts, strict)
		},
	}

	cmd.Flags().BoolVar(&strict, "strict", false, "警告也算失败（退出码 1），给 CI 门禁用")
	return cmd
}

// lintFile 是对一个文件的检查结论。
type lintFile struct {
	// path 是给人看的路径（相对工作目录）。
	path     string
	errors   []*clierr.Error
	warnings []*clierr.Error
}

func runLint(opts *Options, strict bool) error {
	scope, layout, err := detectScope(opts)
	if err != nil {
		return err
	}

	var files []lintFile
	var notes []string
	if scope == skills.ScopeComponent {
		opts.Printf("📦 组件仓库（有 %s、没有 %s）：只检查 %s\n",
			manifest.FileName, layout.ConfigName(), manifest.FileName)
		files = append(files, lintManifest(opts, filepath.Join(layout.Root, manifest.FileName), ""))
	} else {
		files, notes = lintProject(opts, layout)
	}

	return reportLint(opts, files, notes, strict)
}

// lintProject 检查 brickkit.yaml，再检查本地安装源里的每一份 component.yaml。
func lintProject(opts *Options, layout config.Layout) ([]lintFile, []string) {
	head := lintFile{path: displayPath(opts.WorkDir, layout.ConfigPath())}

	cfg, err := config.ParseConfigFile(layout.ConfigPath())
	if err != nil {
		head.errors = append(head.errors, clierr.As(err))
		return []lintFile{head}, []string{
			"brickkit.yaml 没通过检查，本地安装源在哪都不可信，已跳过本地组件的 " + manifest.FileName,
		}
	}

	// 直接 source.New，不走 newSourceClient：后者会先去读 installer.publicKeys 指向的公钥文件，
	// 而公钥缺失是 up / add 该报的事，不该让一条"离线校验 YAML"的命令因此失败。
	// source.New 本身不联网——三种安装源都是惰性的，只有真去取 Manifest 才会碰网络，lint 从不取。
	client, err := source.New(layout, cfg, source.Options{})
	if err != nil {
		head.errors = append(head.errors, clierr.As(err))
		return []lintFile{head}, nil
	}
	defer func() { _ = client.Close() }()

	found, err := client.LocalManifestFiles()
	if err != nil {
		// 本地源的根目录不存在之类：那是 brickkit.yaml 里 sources[].path 配错了
		head.errors = append(head.errors, clierr.As(err))
		return []lintFile{head}, nil
	}

	files := []lintFile{head}
	for _, f := range found {
		files = append(files, lintManifest(opts, f.Path, f.ID))
	}
	return files, nil
}

// lintManifest 检查一份 component.yaml。dirID 非空时（项目模式）还要核对目录名与 metadata.id 一致。
func lintManifest(opts *Options, path, dirID string) lintFile {
	f := lintFile{path: displayPath(opts.WorkDir, path)}

	raw, err := os.ReadFile(path)
	if err != nil {
		f.errors = append(f.errors, clierr.New(clierr.CodeManifestInvalid,
			"错误：读取 "+manifest.FileName+" 失败").
			WithDetail("路径", f.path).
			WithDetail("原因", err.Error()).
			WithHint("检查文件权限"))
		return f
	}

	m, err := manifest.Parse(raw, f.path)
	if err != nil {
		f.errors = append(f.errors, clierr.As(err))
	} else {
		if dirID != "" && m.Metadata.ID != dirID {
			f.errors = append(f.errors, clierr.New(clierr.CodeManifestInvalid,
				"错误："+manifest.FileName+" 里的组件 ID 与目录名对不上").
				WithDetail("文件", f.path).
				WithDetail("目录名", dirID).
				WithDetail("metadata.id", m.Metadata.ID).
				WithHint("本地安装源按 <scope>/<name>/"+manifest.FileName+" 找组件，两者必须一致"))
		}
		for _, w := range inject.ReservedKeyWarnings(m) {
			// 它的块里本来只有组件 ID；检查一堆文件时得告诉人是哪一份
			f.warnings = append(f.warnings, w.WithDetail("来源", f.path))
		}
	}
	// 与 Parse 成败无关：它只依赖 YAML 本身，语法错误时自己返回 nil
	f.warnings = append(f.warnings, manifest.PropertyKeyWarnings(raw, f.path)...)
	return f
}

// reportLint 把结论打印出来，并决定这条命令是成功还是失败。
//
// 报告写 stdout：它是这条命令的产出，不是"命令失败了"的错误。失败时另外返回一个
// 汇总错误，走 stderr 与 JSON 日志行的老路径。
func reportLint(opts *Options, files []lintFile, notes []string, strict bool) error {
	failed, warned := 0, 0
	for _, f := range files {
		if len(f.errors) == 0 && len(f.warnings) == 0 {
			opts.Printf("✅ %s\n", f.path)
			continue
		}
		for _, e := range f.errors {
			opts.Printf("%s", e.Format())
		}
		for _, w := range f.warnings {
			opts.Printf("%s", w.Format())
		}
		if len(f.errors) > 0 {
			failed++
		}
		warned += len(f.warnings)
	}

	for _, n := range notes {
		opts.Printf("ℹ️ %s\n", n)
	}
	opts.Printf("\n📋 检查了 %d 个文件：%d 个有错误，%d 条警告\n", len(files), failed, warned)

	if failed == 0 && (!strict || warned == 0) {
		return nil
	}
	e := clierr.New(clierr.CodeLintFailed, "错误：结构检查未通过").
		WithDetail("已检查", fmt.Sprintf("%d 个文件", len(files)))
	if failed > 0 {
		e = e.WithDetail("有错误", fmt.Sprintf("%d 个文件", failed))
	}
	if strict && warned > 0 {
		e = e.WithDetail("警告", fmt.Sprintf("%d 条（--strict：警告也算失败）", warned))
	}
	return e.WithHint("按上面逐条列出的位置修改，再执行 brickkit lint")
}
```

注册与测试表：`internal/cli/root.go` 的 `AddCommand` 里在 `newGraphCommand(opts),` 后加 `newLintCommand(opts),`；`internal/cli/cli_test.go` 的 `allCommands` 加 `"lint"`，`TestSubcommandFlags` 的 `want` 加 `"lint": {"strict"}`。

**错误码文档**（`tests/docfields/errorcodes_test.go` 会核对"每个码都写了"与"文档里的标题真的存在于源码"）：先 `Read` 两份 `10-error-codes.md`，照现有的写法：
- 顶部索引里，在 `Source workspace` / `源码工作区` 那一组之后新增一个只含 `LINT_FAILED` 的小分类（英文 `**Structure check**`，中文 `**结构检查**`），一行说明：`brickkit lint` found problems — the details were printed above the summary。
- 正文里在 `## Source workspace` 一节之后、`## Warnings` 之前新增 `## Structure check` 与 `### LINT_FAILED`：一张"situation by title"表，行是 `错误：结构检查未通过`（这就是 CLI 打印的标题，原样引用），写清原因（`brickkit lint` 查出了错误，或 `--strict` 下有警告）、怎么办（看命令 stdout 里逐条列出的位置，改完重跑）、退出码 1、以及"它不可重试：同样的输入会同样失败"。
- 同一节里补一句：lint 打印的逐条问题本身仍带着它们各自的码（`MANIFEST_INVALID` / `CONFIG_INVALID` 的标题），但那些块写在 stdout，不带 JSON 日志行；脚本要分支就认 `LINT_FAILED`。
- 中英两份事实一致、结构平行。

- [ ] **Step 4: 跑测试**

Run: `go test ./internal/cli/ -run 'TestLint|TestRootHelp|TestEachSubcommandHelp|TestSubcommandFlags' -count=1 && go test ./tests/docfields/ -count=1`
Expected: PASS

- [ ] **Step 5: 把命令总数的文字声明改成 16**（见 Global Constraints；此时应看到上一任务留下的 15）

`git grep -nE "1[0-9] (个命令|条命令|commands)|命令共 [0-9]+ 条" -- ':!docs/archive' ':!docs/superpowers' ':!改进计划.md'`，15 → 16，只改数字（第二种写法 `README.zh.md` 里的"命令共 15 条"是 Task 3 实现者发现的——数字在"条"前面，`check-cli-docs` 不认这种措辞，不改就悄悄过期）。

- [ ] **Step 6: 全量检查并提交**

```bash
go test ./... -count=1
# 完整 make lint
git add AGENTS.md AGENTS.zh.md README.md README.zh.md llms.txt llms.zh.txt internal/cli/lint.go internal/cli/lint_test.go internal/cli/root.go internal/cli/cli_test.go internal/clierr/clierr.go docs/en/06-architecture/10-error-codes.md docs/zh/06-architecture/10-error-codes.md
git commit -F <写好信息的文件>   # 新增：brickkit lint，离线检查 brickkit.yaml 与 component.yaml 的结构
```

---

## Task 7: `internal/schemagen`——反射生成器（通用部分）

**背景：** 从 Go 结构体反射生成 JSON Schema draft-07。这一步只做**通用的生成器**，用合成的测试类型验证；不碰真实的 `Manifest`/`Config`（Task 8）。生成器读三样东西：反射（字段名、类型、必填与否）、`jsonschema` struct tag（封闭取值的约束）、一张极小的"类型 → 手写 schema"覆盖表（有自定义 `UnmarshalYAML` 的类型）。字段名的判断直接复用 CLI 拒绝未知字段用的那份（`yamlcheck`），两边不可能给出不同的答案。

**Files:**
- Modify: `internal/yamlcheck/unknown.go`（导出 `KnownFields`）
- Create: `internal/schemagen/generator.go`
- Create: `internal/schemagen/generator_test.go`

**Interfaces:**
- Produces（Task 8 依赖）：
  - `yamlcheck.KnownFields(typ reflect.Type) map[string]reflect.StructField`——就是现有 `knownFieldsOf` 的导出版。
  - `schemagen` 包内：`type schema = map[string]any`；`newGenerator(overrides map[reflect.Type]func() schema) *generator`；`(*generator).typeSchema(t reflect.Type) (schema, error)`；`(*generator).document(t reflect.Type, title string) ([]byte, error)`（加 `$schema`/`title`、缩进 2 空格、末尾换行、**不做 HTML 转义**）；常量 `tagName = "jsonschema"`。

**生成规则（设计书 §4.2、§4.3）：**

| Go 类型 | schema |
| --- | --- |
| `string` | `{"type":"string"}` |
| `bool` | `{"type":"boolean"}` |
| 各种 `int`/`uint` | `{"type":"integer"}` |
| `float32/64` | `{"type":"number"}` |
| `interface{}` | `{}`（不加约束） |
| 指针 | 同它指向的类型 |
| slice / array | `{"type":"array","items":<元素>}` |
| `map[string]T` | `{"type":"object","additionalProperties":<T>}`；键不是 string 报错 |
| struct | `{"type":"object","properties":{…},"additionalProperties":false,"required":[…]}`（`required` 为空时省略这个键） |

- struct 的字段集合取 `yamlcheck.KnownFields(t)`（跳过 `yaml:"-"`、未导出字段；没写 yaml 名时用小写字段名）。
- `required` = 没有 `omitempty`、字段类型不是 `bool`、不是指针、且没有 `jsonschema:"optional"` 的字段；按字段名字典序排列。
- `jsonschema` tag：关键字之间 `,` 分隔；`enum=a|b|c`（取值 `|` 分隔，仅用于 string 类型的字段）、`pattern=<正则>`（仅 string）、`minimum=<数>`、`maximum=<数>`（仅 integer/number）、`optional`（不带 `=`）。不认识的关键字、缺 `=`、关键字与字段类型不匹配、数字解析失败，一律返回错误，错误文案里带类型名与字段名。
- 覆盖表：命中的类型直接用它的手写 schema（每次调用返回新的 map，避免被 tag 修改污染）。**有 `UnmarshalYAML` 方法（`*T` 实现了 `yaml.Unmarshaler`）却不在覆盖表里的类型，返回错误**——而不是生成一份悄悄错误的 schema。
- 递归类型返回错误（当前两份 schema 里没有）。

- [ ] **Step 1: 写测试**

新建 `internal/schemagen/generator_test.go`：

```go
package schemagen

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

type inner struct {
	Name string `yaml:"name"`
	Size int    `yaml:"size,omitempty"`
}

type sample struct {
	ID       string            `yaml:"id"`
	Enabled  bool              `yaml:"enabled"`
	Count    *int              `yaml:"count,omitempty"`
	Ratio    float64           `yaml:"ratio,omitempty"`
	Tags     []string          `yaml:"tags,omitempty"`
	Labels   map[string]string `yaml:"labels,omitempty"`
	Anything any               `yaml:"anything,omitempty"`
	Nested   inner             `yaml:"nested"`
	Items    []inner           `yaml:"items,omitempty"`
	ByName   map[string]inner  `yaml:"byName,omitempty"`
	NoTag    string
	Skipped  string `yaml:"-"`
	private  string //nolint:unused
}

func generate(t *testing.T, v any) schema {
	t.Helper()
	s, err := newGenerator(nil).typeSchema(reflect.TypeOf(v))
	require.NoError(t, err)
	return s
}

func props(s schema) map[string]any { return s["properties"].(map[string]any) }

func TestScalarAndContainerMapping(t *testing.T) {
	s := generate(t, sample{})
	p := props(s)

	assert.Equal(t, schema{"type": "string"}, p["id"])
	assert.Equal(t, schema{"type": "boolean"}, p["enabled"])
	assert.Equal(t, schema{"type": "integer"}, p["count"], "指针与它指向的类型同形")
	assert.Equal(t, schema{"type": "number"}, p["ratio"])
	assert.Equal(t, schema{"type": "array", "items": schema{"type": "string"}}, p["tags"])
	assert.Equal(t, schema{"type": "object", "additionalProperties": schema{"type": "string"}}, p["labels"])
	assert.Equal(t, schema{}, p["anything"], "any 不加约束")
	assert.Equal(t, "array", p["items"].(schema)["type"])
	assert.Equal(t, "object", p["byName"].(schema)["type"])
	assert.Equal(t, schema{"type": "string"}, p["notag"], "没写 yaml 名时用小写字段名，与 yamlcheck 一致")
}

func TestStructsRejectUnknownKeysAndSkipHiddenFields(t *testing.T) {
	s := generate(t, sample{})
	assert.Equal(t, false, s["additionalProperties"])
	assert.NotContains(t, props(s), "-")
	assert.NotContains(t, props(s), "Skipped")
	assert.NotContains(t, props(s), "private")

	nested := props(s)["nested"].(schema)
	assert.Equal(t, false, nested["additionalProperties"])
}

// required：没有 omitempty、不是 bool、不是指针。
func TestRequiredRule(t *testing.T) {
	s := generate(t, sample{})
	assert.Equal(t, []string{"id", "nested", "notag"}, s["required"],
		"bool（enabled）与指针虽然没写 omitempty 也不算必填；按字段名排序")
	assert.Equal(t, []string{"name"}, props(s)["nested"].(schema)["required"])
}

func TestRequiredKeyIsOmittedWhenEmpty(t *testing.T) {
	type allOptional struct {
		A string `yaml:"a,omitempty"`
	}
	assert.NotContains(t, generate(t, allOptional{}), "required")
}

func TestJSONSchemaTagKeywords(t *testing.T) {
	type tagged struct {
		Mode string   `yaml:"mode" jsonschema:"enum=fast|slow"`
		Ver  string   `yaml:"ver" jsonschema:"pattern=^[0-9]+[.][0-9]+$"`
		Port int      `yaml:"port" jsonschema:"minimum=1,maximum=65535"`
		Opt  string   `yaml:"opt" jsonschema:"optional"`
		Both string   `yaml:"both" jsonschema:"enum=a|b,optional"`
		Skip []string `yaml:"skip,omitempty"`
	}
	s := generate(t, tagged{})
	p := props(s)

	assert.Equal(t, []any{"fast", "slow"}, p["mode"].(schema)["enum"])
	assert.Equal(t, "^[0-9]+[.][0-9]+$", p["ver"].(schema)["pattern"])
	assert.EqualValues(t, 1, p["port"].(schema)["minimum"])
	assert.EqualValues(t, 65535, p["port"].(schema)["maximum"])
	assert.Equal(t, []string{"mode", "port", "ver"}, s["required"], "optional 把字段挪出 required")
}

func TestJSONSchemaTagErrorsAreLoud(t *testing.T) {
	cases := map[string]any{
		"不认识的关键字":  struct{ A string `yaml:"a" jsonschema:"enumm=x"` }{},
		"缺 =":       struct{ A string `yaml:"a" jsonschema:"pattern"` }{},
		"enum 用在数字": struct{ A int `yaml:"a" jsonschema:"enum=1|2"` }{},
		"范围用在字符串":  struct{ A string `yaml:"a" jsonschema:"minimum=1"` }{},
		"数字解析失败":   struct{ A int `yaml:"a" jsonschema:"minimum=abc"` }{},
	}
	for name, v := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := newGenerator(nil).typeSchema(reflect.TypeOf(v))
			require.Error(t, err)
		})
	}
}

type withUnmarshaler struct{ Raw string }

func (w *withUnmarshaler) UnmarshalYAML(value *yaml.Node) error { return value.Decode(&w.Raw) }

func TestTypesWithCustomUnmarshalerNeedAnOverride(t *testing.T) {
	type holder struct {
		V withUnmarshaler `yaml:"v"`
	}

	_, err := newGenerator(nil).typeSchema(reflect.TypeOf(holder{}))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "UnmarshalYAML")
	assert.Contains(t, err.Error(), "withUnmarshaler")

	overrides := map[reflect.Type]func() schema{
		reflect.TypeOf(withUnmarshaler{}): func() schema { return schema{"type": "string"} },
	}
	s, err := newGenerator(overrides).typeSchema(reflect.TypeOf(holder{}))
	require.NoError(t, err)
	assert.Equal(t, schema{"type": "string"}, props(s)["v"])
}

func TestOverrideReturnsAFreshMapEveryTime(t *testing.T) {
	overrides := map[reflect.Type]func() schema{
		reflect.TypeOf(inner{}): func() schema { return schema{"type": "string"} },
	}
	g := newGenerator(overrides)
	a, err := g.typeSchema(reflect.TypeOf(inner{}))
	require.NoError(t, err)
	a["mutated"] = true
	b, err := g.typeSchema(reflect.TypeOf(inner{}))
	require.NoError(t, err)
	assert.NotContains(t, b, "mutated")
}

type recursive struct {
	Next *recursive `yaml:"next,omitempty"`
}

func TestRecursiveTypesAreRejected(t *testing.T) {
	_, err := newGenerator(nil).typeSchema(reflect.TypeOf(recursive{}))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "递归")
}

func TestUnsupportedTypesAreRejected(t *testing.T) {
	type badKey struct {
		M map[int]string `yaml:"m"`
	}
	_, err := newGenerator(nil).typeSchema(reflect.TypeOf(badKey{}))
	require.Error(t, err)

	type badKind struct {
		C chan int `yaml:"c"`
	}
	_, err = newGenerator(nil).typeSchema(reflect.TypeOf(badKind{}))
	require.Error(t, err)
}

func TestDocumentEnvelopeAndFormatting(t *testing.T) {
	type doc struct {
		Expr string `yaml:"expr" jsonschema:"pattern=^<[a-z]+>&$"`
	}
	out, err := newGenerator(nil).document(reflect.TypeOf(doc{}), "示例")
	require.NoError(t, err)

	text := string(out)
	assert.True(t, strings.HasSuffix(text, "}\n"), "以换行结尾")
	assert.Contains(t, text, "\n  \"$schema\": \"http://json-schema.org/draft-07/schema#\"")
	assert.Contains(t, text, "\"title\": \"示例\"")
	assert.Contains(t, text, "^<[a-z]+>&$", "不做 HTML 转义：<、>、& 原样输出")

	var back map[string]any
	require.NoError(t, json.Unmarshal(out, &back), "输出是合法 JSON")
}

func TestDocumentIsDeterministic(t *testing.T) {
	first, err := newGenerator(nil).document(reflect.TypeOf(sample{}), "t")
	require.NoError(t, err)
	for i := 0; i < 5; i++ {
		again, err := newGenerator(nil).document(reflect.TypeOf(sample{}), "t")
		require.NoError(t, err)
		assert.Equal(t, string(first), string(again))
	}
}
```

（`sample.private` 那一行的 `//nolint:unused` 若 golangci 不需要就去掉；关键是要有一个未导出字段来证明它被跳过。）

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./internal/schemagen/ -count=1`
Expected: 编译失败（包不存在 / `undefined: newGenerator`）

- [ ] **Step 3: 实现**

`internal/yamlcheck/unknown.go`：在 `knownFieldsOf` 之前加导出版，并让 `walkStruct`/`closestField` 继续用内部的（不动它们）：

```go
// KnownFields 列出结构体在 YAML 里认识的键（键 → 字段）。
//
// 导出是因为"这份 YAML 认识哪些键"有两个读者：Walk 用它拒绝拼错的键，
// internal/schemagen 用它生成 JSON Schema 里的 properties。两边共用同一个判断，
// 编辑器里的红线与 CLI 报的"未知字段"就不可能各说各话。
func KnownFields(typ reflect.Type) map[string]reflect.StructField { return knownFieldsOf(typ) }
```

新建 `internal/schemagen/generator.go`：

```go
// Package schemagen 从 Manifest（component.yaml）与 Config（brickkit.yaml）的 Go 结构体
// 反射生成 JSON Schema（draft-07），给编辑器做字段补全、类型提示与未知字段红线。
//
// # 为什么生成而不是手写
//
// 手写的 schema 是仓库里从没有过的一种"容易悄悄过期"的东西——改了结构体，schema 不会
// 报错，只会让编辑器给出过时的提示。这与 tests/docfields 已经在防的是同一类问题，
// 所以同一个办法：生成，再用测试把生成物与签入的文件钉死（见 schemas_test.go）。
//
// # 约束从哪来
//
// 三样东西，各管各的：
//
//   - 反射：字段名（与 CLI 拒绝未知字段用的是同一份，见 yamlcheck.KnownFields）、类型、
//     必填与否。必填 = yaml tag 没有 omitempty，且不是 bool、不是指针，且没写
//     jsonschema:"optional"。
//   - `jsonschema` struct tag：封闭取值的约束——enum、pattern、minimum、maximum。
//     关键字之间用 `,` 分隔，enum 的取值之间用 `|` 分隔；取值与 pattern 里不能出现
//     `,` 与 `|`（也就不必在 struct tag 里转义反斜杠，正则写成 [.] 而不是 \.）。
//     不认识的关键字直接报错，写错了不会悄悄不生效。
//   - 覆盖表：有自定义 UnmarshalYAML 的类型（目前只有 manifest.ComponentDep）。
//     反射看不出它既能写成字符串也能写成映射，只能手写。遇到有 UnmarshalYAML 却不在表里
//     的类型，生成器报错，而不是生成一份悄悄错误的 schema。
//
// # 需要人记得的两处
//
// tag 里抄了一份"取值范围"，覆盖表里手写了一份"ComponentDep 的形状"——它们都是校验代码之外的
// 另一份真相。前者由 schemas_test.go 里的约束测试拿真实校验器逐项核对；后者只有"有
// UnmarshalYAML 的类型都得在表里"这一层自动保证，内容对不对靠改 ComponentDep 解析写法的人
// 想起来这里。
package schemagen

import (
	"bytes"
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/brickkit/brickkit/internal/yamlcheck"
)

// tagName 是承载约束的 struct tag 名。
const tagName = "jsonschema"

// schema 是一个 JSON Schema 节点。用 map 而不是结构体：encoding/json 按键排序输出 map，
// 同一份输入每次生成的字节都相同，签入仓库才有稳定的 diff。
type schema = map[string]any

var yamlUnmarshalerType = reflect.TypeOf((*yaml.Unmarshaler)(nil)).Elem()

type generator struct {
	overrides map[reflect.Type]func() schema
	visiting  map[reflect.Type]bool
}

func newGenerator(overrides map[reflect.Type]func() schema) *generator {
	return &generator{overrides: overrides, visiting: map[reflect.Type]bool{}}
}

// document 生成一份完整的 schema 文档：节点 + $schema + title，缩进 2 空格，末尾换行。
func (g *generator) document(t reflect.Type, title string) ([]byte, error) {
	root, err := g.typeSchema(t)
	if err != nil {
		return nil, err
	}
	root["$schema"] = "http://json-schema.org/draft-07/schema#"
	root["title"] = title

	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetIndent("", "  ")
	// 不做 HTML 转义：pattern 里的 < > & 要原样出现在文件里，编辑器读到的才是原来的正则
	enc.SetEscapeHTML(false)
	if err := enc.Encode(root); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func (g *generator) typeSchema(t reflect.Type) (schema, error) {
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if build, ok := g.overrides[t]; ok {
		return build(), nil
	}
	if reflect.PointerTo(t).Implements(yamlUnmarshalerType) {
		return nil, fmt.Errorf("%s 有自定义的 UnmarshalYAML，反射看不出它接受哪些写法，"+
			"需要在覆盖表里手写它的 schema", t)
	}

	switch t.Kind() {
	case reflect.String:
		return schema{"type": "string"}, nil
	case reflect.Bool:
		return schema{"type": "boolean"}, nil
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return schema{"type": "integer"}, nil
	case reflect.Float32, reflect.Float64:
		return schema{"type": "number"}, nil
	case reflect.Interface:
		return schema{}, nil
	case reflect.Slice, reflect.Array:
		items, err := g.typeSchema(t.Elem())
		if err != nil {
			return nil, err
		}
		return schema{"type": "array", "items": items}, nil
	case reflect.Map:
		if t.Key().Kind() != reflect.String {
			return nil, fmt.Errorf("%s：map 的键必须是 string", t)
		}
		values, err := g.typeSchema(t.Elem())
		if err != nil {
			return nil, err
		}
		return schema{"type": "object", "additionalProperties": values}, nil
	case reflect.Struct:
		return g.structSchema(t)
	}
	return nil, fmt.Errorf("不支持的类型 %s", t)
}

func (g *generator) structSchema(t reflect.Type) (schema, error) {
	if g.visiting[t] {
		return nil, fmt.Errorf("递归类型 %s 不支持", t)
	}
	g.visiting[t] = true
	defer delete(g.visiting, t)

	known := yamlcheck.KnownFields(t)
	names := make([]string, 0, len(known))
	for name := range known {
		names = append(names, name)
	}
	sort.Strings(names)

	properties := map[string]any{}
	var required []string
	for _, name := range names {
		field := known[name]
		node, err := g.typeSchema(field.Type)
		if err != nil {
			return nil, fmt.Errorf("%s.%s：%w", t.Name(), field.Name, err)
		}
		optional, err := applyTag(node, field.Tag.Get(tagName))
		if err != nil {
			return nil, fmt.Errorf("%s.%s：%w", t.Name(), field.Name, err)
		}
		properties[name] = node

		if !omitsEmpty(field) && !optional &&
			field.Type.Kind() != reflect.Bool && field.Type.Kind() != reflect.Pointer {
			required = append(required, name)
		}
	}

	out := schema{"type": "object", "properties": properties, "additionalProperties": false}
	if len(required) > 0 {
		out["required"] = required
	}
	return out, nil
}

// omitsEmpty 判断字段的 yaml tag 是否带 omitempty。
func omitsEmpty(field reflect.StructField) bool {
	options := strings.Split(field.Tag.Get("yaml"), ",")
	for _, opt := range options[1:] {
		if opt == "omitempty" {
			return true
		}
	}
	return false
}

// applyTag 把 `jsonschema:"..."` 里的约束写进字段的 schema 节点，返回该字段是否被标成 optional。
func applyTag(node schema, tag string) (optional bool, err error) {
	if tag == "" {
		return false, nil
	}
	kind, _ := node["type"].(string)

	for _, item := range strings.Split(tag, ",") {
		if item == "optional" {
			optional = true
			continue
		}
		key, value, found := strings.Cut(item, "=")
		if !found {
			return false, fmt.Errorf("%s 约束 %q 缺少 =", tagName, item)
		}
		switch key {
		case "enum":
			if kind != "string" {
				return false, fmt.Errorf("enum 只能用在 string 字段上（这个字段是 %q）", kind)
			}
			values := strings.Split(value, "|")
			list := make([]any, len(values))
			for i, v := range values {
				list[i] = v
			}
			node["enum"] = list
		case "pattern":
			if kind != "string" {
				return false, fmt.Errorf("pattern 只能用在 string 字段上（这个字段是 %q）", kind)
			}
			node["pattern"] = value
		case "minimum", "maximum":
			if kind != "integer" && kind != "number" {
				return false, fmt.Errorf("%s 只能用在 integer / number 字段上（这个字段是 %q）", key, kind)
			}
			n, err := strconv.ParseFloat(value, 64)
			if err != nil {
				return false, fmt.Errorf("%s=%q 不是数字", key, value)
			}
			node[key] = n
		default:
			return false, fmt.Errorf("不认识的 %s 约束 %q", tagName, key)
		}
	}
	return optional, nil
}
```

- [ ] **Step 4: 跑测试**

Run: `go test ./internal/schemagen/ ./internal/yamlcheck/ -count=1`
Expected: PASS

- [ ] **Step 5: 全量检查并提交**

```bash
go test ./... -count=1
# 完整 make lint
git add internal/yamlcheck/unknown.go internal/schemagen/
git commit -F <写好信息的文件>   # 新增：internal/schemagen，从 Go 结构体反射生成 JSON Schema 的通用生成器
```

---

## Task 8: 真实的两份 schema、`jsonschema` tag、落盘与防漂移

**Files:**
- Modify: `internal/manifest/types.go`、`internal/config/config.go`（加 `jsonschema` tag）
- Create: `internal/schemagen/schemagen.go`（`Component()`、`Project()`、`Files()`、覆盖表）
- Create: `internal/schemagen/schemas_test.go`（防漂移、必填集合、约束）
- Create: `cmd/gen-schemas/main.go`
- Create: `schemas/component.schema.json`、`schemas/brickkit.schema.json`（由工具生成，不手写）
- Modify: `Makefile`（`generate-schemas`、`check-schemas`，并把 `check-schemas` 加进 `lint`）

**Interfaces:**
- Consumes（Task 7）：`newGenerator`、`(*generator).document`、`tagName`、`schema`。
- Produces：`schemagen.Component() ([]byte, error)`、`schemagen.Project() ([]byte, error)`、`schemagen.Files() (map[string][]byte, error)`（键是 `schemas/` 下的文件名 `component.schema.json` / `brickkit.schema.json`）。

**`jsonschema` tag 清单（设计书 §4.3；这是全部，别多加）：**

| 字段 | tag |
| --- | --- |
| `config.Deploy.Target` | `jsonschema:"enum=docker\|k8s"`（写在 struct tag 里就是 `enum=docker|k8s`） |
| `config.Source.Type` | `enum=market|git|local` |
| `manifest.Manifest.APIVersion` | `enum=brickkit/v1` |
| `manifest.Manifest.Kind` | `enum=Component` |
| `manifest.Metadata.Version` | `pattern=^[0-9]+[.][0-9]+[.][0-9]+$` |
| `manifest.Deployment.Type` | `enum=container` |
| `manifest.Deployment.Port`、`manifest.ExtraPort.Port` | `minimum=1,maximum=65535` |
| `manifest.HealthCheck.Type` | `enum=http|tcp|none` |
| `manifest.ResourceDep.Kind`、`config.Resource.Kind` | `enum=database|cache|mq|storage|search|smtp` |
| `manifest.ConfigProperty.Type` | `enum=string|integer|number|boolean|array|object` |
| `manifest.ItemDef.Type` | `optional`（校验器从不检查 `items.type`——AGENTS §6：`items` 只是说明书；它没写 `omitempty` 却并不必填） |

在每个被标注的类型旁边补一行注释指向 `internal/schemagen`（"这里的 jsonschema tag 与 Validate 里的规则是同一份取值，改一处要改另一处，`schemas_test.go` 会核对"），别把这条约定只放在 schemagen 的包注释里。

**覆盖表（`manifest.ComponentDep`）：** 字符串写法 `<id>@<精确版本>`，或 `{id, optional}` 映射。

```go
func componentDepSchema() schema {
	const ref = "^[^@ ]+@[0-9]+[.][0-9]+[.][0-9]+$" // 精确版本：不接受 ^ ~ 范围（AGENTS §9.2）
	return schema{"oneOf": []any{
		schema{"type": "string", "pattern": ref},
		schema{
			"type": "object",
			"properties": map[string]any{
				"id":       schema{"type": "string", "pattern": ref},
				"optional": schema{"type": "boolean"},
			},
			"required":             []string{"id"},
			"additionalProperties": false,
		},
	}}
}
```

- [ ] **Step 1: 写测试**

新建 `internal/schemagen/schemas_test.go`。测试分三块，都用真实的 `Manifest`/`Config`，共用下面的两份**合法**基准 YAML 与一组改写辅助函数：

```go
package schemagen

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"

	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/config"
	"github.com/brickkit/brickkit/internal/manifest"
)

// 两份合法基准：字段尽量铺满，好让"删掉某个必填字段 / 改成某个值"都有落脚点。
// 先跑 TestBaselinesAreValid——基准自己不合法，后面所有断言都没有意义。
const baselineComponent = `apiVersion: brickkit/v1
kind: Component
metadata:
  id: demo/hello
  name: Hello
  version: 1.0.0
  description: 基准
artifacts:
  - type: api-contract
    files: [openapi.yaml]
dependencies:
  components:
    - demo/other@1.0.0
    - id: demo/weak@1.0.0
      optional: true
  resources:
    - kind: database
      engine: postgresql
configSchema:
  type: object
  properties:
    pageSize:
      type: integer
      default: 20
    tags:
      type: array
      items: {}
deployment:
  type: container
  image: registry.example.com/demo/hello:1.0.0
  port: 8080
  extraPorts:
    - name: grpc
      port: 9090
migration:
  command: ["./migrate"]
healthCheck:
  type: http
  path: /healthz
`

const baselineProject = `project: demo
deploy:
  target: k8s
  networkPolicy:
    enabled: true
    ingressController:
      namespace: ingress-nginx
    allowFrom:
      - name: prometheus
        namespace: monitoring
    egress:
      enabled: true
      allowTo:
        - name: db
          resource: main-db
sources:
  - id: local-dev
    type: local
    path: ./components
components:
  - id: demo/hello
    version: 1.0.0
resources:
  - kind: database
    engine: postgresql
    id: main-db
    host: db.example.com
    port: 5432
    bindings:
      - componentId: demo/hello
        database: hello
`
```

**块一：防漂移。**

```go
func TestCheckedInSchemasAreUpToDate(t *testing.T) {
	files, err := Files()
	require.NoError(t, err)
	require.NotEmpty(t, files)
	for name, want := range files {
		got, err := os.ReadFile(filepath.Join("..", "..", "schemas", name))
		require.NoError(t, err, "缺 schemas/%s：先跑 make generate-schemas", name)
		assert.Equal(t, string(want), string(got),
			"schemas/%s 与结构体生成的不一致：改了 manifest / config 的结构体或 jsonschema tag 之后，要跑 make generate-schemas 并提交结果", name)
	}
}
```

**块二：必填集合（钉规则 + 校验器核对）。** 先写一张手写的"schema 路径 → 必填字段"的黄金表，再对每一项做两件事：与生成结果逐项相等（两个方向都查——生成结果里出现表里没有的必填集合，也要失败，逼着新类型的作者回来核对），并且**从合法基准里删掉那个字段，真实的解析必须失败**。黄金表的内容（已逐条对照 `manifest.Validate` / `config.Validate` 核实过）：

| 文档 | schema 路径 | 必填 | 基准里的数据路径 |
| --- | --- | --- | --- |
| component | 根 | `apiVersion` `deployment` `healthCheck` `kind` `metadata` | 根 |
| component | `metadata` | `description` `id` `name` `version` | `metadata` |
| component | `artifacts[]` | `files` `type` | `artifacts[0]` |
| component | `dependencies/resources[]` | `engine` `kind` | `dependencies.resources[0]` |
| component | `dependencies/components[]#oneOf[1]` | `id` | `dependencies.components[1]`（映射写法） |
| component | `configSchema/properties{}` | `type` | `configSchema.properties.pageSize` |
| component | `deployment` | `image` `port` `type` | `deployment` |
| component | `deployment/extraPorts[]` | `name` `port` | `deployment.extraPorts[0]` |
| component | `healthCheck` | `type` | `healthCheck` |
| component | `migration` | `command` | `migration` |
| project | 根 | `deploy` `project` | 根 |
| project | `deploy` | `target` | `deploy` |
| project | `deploy/networkPolicy/ingressController` | `namespace` | `deploy.networkPolicy.ingressController` |
| project | `deploy/networkPolicy/allowFrom[]` | `name` `namespace` | `deploy.networkPolicy.allowFrom[0]` |
| project | `deploy/networkPolicy/egress/allowTo[]` | `name` | `deploy.networkPolicy.egress.allowTo[0]` |
| project | `sources[]` | `id` `type` | `sources[0]` |
| project | `components[]` | `id` `version` | `components[0]` |
| project | `resources[]` | `engine` `host` `id` `kind` `port` | `resources[0]` |
| project | `resources[]/bindings[]` | `componentId` | `resources[0].bindings[0]` |

另外断言：`deploy/networkPolicy`、`deploy/networkPolicy/egress`、`deploy/serviceAccount` 的必填集合里**没有** `enabled`（bool 例外）；`configSchema/properties{}/items`（`ItemDef`）没有必填（`optional`）。

**块三：约束测试**（设计书 §4.3 的"tag 与校验代码用测试互相钉住"）。一张表，每行一个被标注的字段：

```go
type constraintCase struct {
	name string
	doc  string // "component" | "project"
	// schemaPath 指向 schema 里该字段的节点（用来读出 tag 生成的 enum / pattern / 范围）。
	schemaPath string
	// dataPath 是该字段在基准 YAML 里的位置；errField 是校验器报错时用的字段名。
	dataPath []any
	errField string
	// valid / invalid：拿去改基准里那个字段的值。enum 行的 valid 从 schema 里读，不在这里写。
	valid, invalid []any
}
```

每一行做三件事：
1. **schema 自己**：enum 行——读出 schema 的 enum 列表，断言 `invalid` 与它不相交；pattern 行——用 `regexp` 编译 schema 里的 pattern，断言匹配所有 `valid`、不匹配任何 `invalid`；范围行——断言 `valid` 都在 `[minimum, maximum]` 里、`invalid` 都在外面。
2. **合法值不被校验器拒绝**：对每个合法值（enum 行取 schema 的 enum 全集）改写基准、走真实的解析（`manifest.Parse` / `config.ParseConfig`），断言返回的错误（若有）**没有**以 `errField` 为键的明细——别的字段因为这次改动而报错（比如把 `sources[0].type` 改成 `market` 后缺 `url`）不算。
3. **非法值被校验器拒绝**：同上，断言错误里**有**以 `errField` 为键的明细。

行的内容：

| 文档 | 字段 | `errField` | `invalid` |
| --- | --- | --- | --- |
| project | `deploy.target` | `deploy.target` | `swarm`、`Docker`、`` |
| project | `sources[0].type` | `sources[0].type` | `svn`、`LOCAL` |
| component | `apiVersion` | `apiVersion` | `brickkit/v2`、`v1` |
| component | `kind` | `kind` | `component`、`Service` |
| component | `metadata.version` | `metadata.version` | `1.0`、`^1.0.0`、`1.0.0-beta`、`v1.0.0`、`1.0.0.0`；valid：`1.0.0`、`10.20.30` |
| component | `deployment.type` | `deployment.type` | `docker`、`static` |
| component | `deployment.port` | `deployment.port` | `0`、`-1`、`65536`；valid：`1`、`8080`、`65535` |
| component | `deployment.extraPorts[0].port` | `deployment.extraPorts[0].port` | 同上 |
| component | `healthCheck.type` | `healthCheck.type` | `ftp`、`HTTP` |
| component | `dependencies.resources[0].kind` | `dependencies.resources[0].kind` | `redis`、`Database` |
| project | `resources[0].kind` | `resources[0].kind` | 同上 |
| component | `configSchema.properties.pageSize.type` | `configSchema.properties.pageSize.type` | `int`、`str`、`float` |
| component | `dependencies.components[0]`（字符串写法，覆盖表的 pattern） | `dependencies.components[0]` | `demo/other@^1.0.0`、`demo/other@~1.0.0`、`demo/other@latest`；valid：`demo/other@1.0.0`、`demo/other@10.2.3` |

`errField` 的确切写法以校验器实际为准（用 `problemFields(err)` 打印一次，对着改）；若某一行的校验器根本不会因为该值而报错（即 tag 比校验器**更严**），那是 tag 写错了，改 tag，不要放宽测试。`dependencies.components[0]` 这一行若校验器把"无版本"当成别的字段的错，就只保留能对上的那几个值，并在测试注释里说明。

辅助函数（放在测试文件里）：

```go
// problemFields 取出一个校验错误里所有明细的键（字段路径）。
func problemFields(err error) []string {
	if err == nil {
		return nil
	}
	var out []string
	for _, d := range clierr.As(err).Details {
		out = append(out, d.Key)
	}
	return out
}

// mutate 解析 base，把 path 指向的位置改成 value（remove 时删掉那个键），再编码回 YAML。
// path 的元素：string 表示映射的键，int 表示数组下标。
func mutate(t *testing.T, base string, path []any, value any, remove bool) []byte {
	t.Helper()
	var root any
	require.NoError(t, yaml.Unmarshal([]byte(base), &root))
	set(t, &root, path, value, remove)
	out, err := yaml.Marshal(root)
	require.NoError(t, err)
	return out
}

func set(t *testing.T, node *any, path []any, value any, remove bool) {
	t.Helper()
	if len(path) == 0 {
		*node = value
		return
	}
	switch key := path[0].(type) {
	case string:
		m := (*node).(map[string]any)
		if len(path) == 1 && remove {
			delete(m, key)
			return
		}
		child := m[key]
		set(t, &child, path[1:], value, remove)
		m[key] = child
	case int:
		s := (*node).([]any)
		child := s[key]
		set(t, &child, path[1:], value, remove)
		s[key] = child
	}
}
```

（`yaml.v3` 把嵌套映射解到 `any` 时得到 `map[string]interface{}`，已经用小程序核实过；若这里的类型断言 panic，先打印 `%T` 再改，别猜。）

外加三个小测试：`TestBaselinesAreValid`（两份基准分别通过 `manifest.Parse` / `config.ParseConfig`）、`TestEveryUnmarshalerTypeIsCoveredByAnOverride`（用反射遍历 `manifest.Manifest` 与 `config.Config` 可达的全部类型，凡是 `*T` 实现了 `yaml.Unmarshaler` 的都必须在覆盖表里——这与生成器自己的报错是同一个保证，但放在这里能让报错指向"覆盖表"而不是生成失败的堆栈）、`TestPropertyNamesMatchWhatTheCLIAccepts`（对两个根类型递归，每个 struct 节点的 `properties` 键集合必须等于 `yamlcheck.KnownFields` 给出的键集合——覆盖表里的类型除外）。

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./internal/schemagen/ -count=1`
Expected: 编译失败，`undefined: Files` / `Component` / `Project`

- [ ] **Step 3: 实现**

`internal/schemagen/schemagen.go`：

```go
package schemagen

import (
	"reflect"

	"github.com/brickkit/brickkit/internal/config"
	"github.com/brickkit/brickkit/internal/manifest"
)

// 落盘的文件名（在仓库根目录的 schemas/ 下）。
const (
	ComponentFile = "component.schema.json"
	ProjectFile   = "brickkit.schema.json"
)

// overrides 是"类型 → 手写 schema"的覆盖表：有自定义 UnmarshalYAML 的类型。
func overrides() map[reflect.Type]func() schema {
	return map[reflect.Type]func() schema{
		reflect.TypeOf(manifest.ComponentDep{}): componentDepSchema,
	}
}

// componentDepSchema：依赖项有两种写法（002 §3.2）——字符串 department/tree@1.0.0，
// 或映射 {id, optional}。版本必须精确：不接受 ^ ~ 范围（AGENTS §9.2），与 Validate 一致。
func componentDepSchema() schema { /* 见上面 Task 8 的代码块，原样放进来 */ }

// Component 返回 component.yaml 的 JSON Schema。
func Component() ([]byte, error) {
	return newGenerator(overrides()).document(reflect.TypeOf(manifest.Manifest{}), "BrickKit component.yaml")
}

// Project 返回 brickkit.yaml 的 JSON Schema。
func Project() ([]byte, error) {
	return newGenerator(overrides()).document(reflect.TypeOf(config.Config{}), "BrickKit brickkit.yaml")
}

// Files 返回要落盘的全部 schema：文件名 → 内容。落盘工具与防漂移测试共用它，
// 以后多一份 schema 只改这一处。
func Files() (map[string][]byte, error) {
	component, err := Component()
	if err != nil {
		return nil, err
	}
	project, err := Project()
	if err != nil {
		return nil, err
	}
	return map[string][]byte{ComponentFile: component, ProjectFile: project}, nil
}
```

给 `internal/manifest/types.go` 与 `internal/config/config.go` 的字段加上上面清单里的 tag（写法如 `` `yaml:"target" jsonschema:"enum=docker|k8s"` ``），并补指向 `internal/schemagen` 的注释。`ComponentDep` 的注释里也补一句：它有自定义 `UnmarshalYAML`，改写法要同步 `schemagen.componentDepSchema`。

`cmd/gen-schemas/main.go`：

```go
// gen-schemas 把 component.yaml 与 brickkit.yaml 的 JSON Schema 写进 schemas/。
//
// 它不进 brickkit 二进制：schema 是仓库里的一份生成物，不是使用者机器上要有的东西。
// 用法：make generate-schemas（等价于 go run ./cmd/gen-schemas [目标目录]）。
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/brickkit/brickkit/internal/schemagen"
)

func main() {
	dir := "schemas"
	if len(os.Args) > 1 {
		dir = os.Args[1]
	}
	if err := run(dir); err != nil {
		fmt.Fprintln(os.Stderr, "❌ 生成 schema 失败：", err)
		os.Exit(1)
	}
}

func run(dir string) error {
	files, err := schemagen.Files()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, files[name], 0o644); err != nil {
			return err
		}
		fmt.Println("✅", path)
	}
	return nil
}
```

`Makefile`：新增两个目标，并把 `check-schemas` 加进 `lint` 的先决条件列表（在 `check-doc-fields` 之后），`lint` 的说明文字里补上"JSON Schema 与结构体一致"：

```make
.PHONY: generate-schemas
generate-schemas: ## 从 Go 结构体重新生成 schemas/*.json（改了 manifest / config 的字段或 jsonschema tag 之后跑）
	@$(GO) run ./cmd/gen-schemas

.PHONY: check-schemas
check-schemas: ## 检查签入的 schemas/*.json 与结构体生成的一致，且约束与真实校验器一致
	@$(GO) test ./internal/schemagen/ -count=1
```

然后 `make generate-schemas`，把 `schemas/*.json` **读一遍**（别盲提交）：确认 `metadata.version` 的 pattern、`deploy.target` 的 enum、`dependencies.components` 的 `oneOf`、`healthCheck.required` 都在，没有 `<` 之类的转义。

- [ ] **Step 4: 跑测试**

Run: `go test ./internal/schemagen/ -count=1 -v 2>&1 | tail -40`
Expected: PASS。约束测试与必填测试若失败，**先弄清是 tag / 规则写错还是基准写错**，按 Task 说明里"tag 比校验器更严就改 tag，不放宽测试"的原则处理，并在报告里逐条说明改了什么、为什么。

再做一次反向验证（变异测试，证明这些测试真的会红）：临时把 `config.Deploy.Target` 的 tag 改成 `enum=docker`、跑 `go test ./internal/schemagen/`，确认**防漂移测试与约束测试都失败**；再临时把 `Metadata.Version` 的 pattern 放宽成 `^.+$`，确认约束测试失败；两处都还原后重跑通过。这两次变异要在报告里写明结果。

- [ ] **Step 5: 用真实的 YAML 语言服务器把关（一次性人工核对，不进仓库）**

若能装 `npx yaml-language-server`/VS Code 就用一份带拼写错误的 `component.yaml` 与 `brickkit.yaml` 各验证一次"未知字段红线 + `deploy.target` 补全"；装不上就在报告里写"未做编辑器核对"，不要假装做了。至少用 Python 的 `jsonschema` 库（`pip install jsonschema` 到临时 venv，别装进系统）对两份基准 YAML 与几份故意写坏的 YAML 各校验一次，确认基准通过、坏的被拒——这一步不依赖编辑器，必须做。

- [ ] **Step 6: 全量检查并提交**

```bash
go test ./... -count=1
# 完整 make lint（现在包含 check-schemas）
git add internal/manifest/types.go internal/config/config.go internal/schemagen/ cmd/gen-schemas/ schemas/ Makefile
git commit -F <写好信息的文件>   # 新增：component.yaml / brickkit.yaml 的 JSON Schema（从 Go 结构体生成，配防漂移与约束核对测试）
```

---

## Task 9: 文档与计数

**背景：** 两条新命令、`LINT_FAILED`（Task 6 已写）、`schemas/` 都要让读文档的人与读文档的 AI 找得到。命令总数从 14 变成 16。**先跑真实命令拿输出，再写文档**——凡是"输出长什么样"的地方都用真实输出块。

**Files（先 `git grep` 确认，别只信这张表）：**
- `AGENTS.md`、`AGENTS.zh.md`
- `README.md`、`README.zh.md`
- `llms.txt`、`llms.zh.txt`
- `CHANGELOG.md`
- `docs/en/06-architecture/09-cli-reference.md`、`docs/zh/06-architecture/09-cli-reference.md`
- `docs/en/00-quick-start.md`、`docs/zh/00-quick-start.md`
- `docs/en/06-architecture/07-component-yaml-reference.md`、`08-brickkit-yaml-reference.md` 及 `docs/zh` 对应两份
- `docs/{en,zh}/06-architecture/00-overview.md`（拒绝清单的编号条目）
- `docs/{en,zh}/08-troubleshooting.md`（仅当真跑出值得记的误用）
- `internal/skills/assets/claude/skills/brickkit-component/SKILL.md`（及 `brickkit-troubleshoot`、`brickkit-assemble` 里合适的一句）

- [ ] **Step 1: 确认命令总数的声明都已经是 16，并拿真实输出**

Task 3 与 Task 6 已经各自把"命令总数"的文字声明改过（14→15→16），这里只确认没有遗漏——中英文一起：
```bash
git grep -nE "[0-9]+ (个命令|条命令|commands)|命令共 [0-9]+ 条" -- ':!docs/archive' ':!docs/superpowers' ':!改进计划.md'   # 期望：数字全是 16
make build-cli   # 得到 bin/brickkit
```
在一个真实项目里跑两条新命令拿输出：用 `tests/components/` 里的两个真实组件（`demo-hello`、`demo-caller`，与 cli-reference 现有的示例同一套）建项目，跑 `brickkit graph`（一次带 `--ignore-served-by` 不必，除非文档要讲它）与 `brickkit lint`（一次干净、一次故意在某个 `component.yaml` 里拼错一个键——例如把 `dependencies` 写成 `dependancies`）。输出块**逐字**取自真实输出。

- [ ] **Step 2: AGENTS.md / AGENTS.zh.md**

- §8 标题里的数字（Task 3/6 已改成 16）；命令表里加 `brickkit graph`、`brickkit lint` 两行（一句话核心行为）；"Common flags" 里各加一条例子（`brickkit graph > graph.mmd`、`brickkit lint --strict`）。
- §11.1 代码结构图：`internal/` 下加 `schemagen/`（一行说明：从 Go 结构体反射生成 JSON Schema），仓库根加 `schemas/`、`cmd/gen-schemas/`（注意 §11.1 现在写的是 `cmd/brickkit/   CLI entry point`，照那个格式）。
- §11.2 "where to dig deeper" 表：加一行"编辑器补全 / JSON Schema"指向 `schemas/` 与 quick-start 的对应一节。
- §4.1 拒绝清单：加两行——① `brickkit graph` 的 HTML/SVG 输出 / 自建渲染器 → 替代：Mermaid 文本，GitHub 与 VS Code 原生渲染；② `brickkit lint` 做依赖解析 / 跨文件引用检查（比如 `servedBy` 目标是否存在）→ 替代：`brickkit up --dry-run`，它本来就要联网解析依赖图。每一行的措辞要和现有行一致，且英文版**不能**出现字面的 `brickkit <不存在的命令>`（`check-cli-docs` 的规矩：只有中文里的墓碑标记豁免；`graph` 与 `lint` 现在是真命令，没这个问题，但别写 `brickkit graph --output`、`brickkit lint --fix` 这类不存在的参数）。参照第一轮加拒绝清单的那次提交（`git log --oneline --grep 拒绝清单` 找到，`git show` 看它改了哪几处、编号怎么排）把 `docs/{en,zh}/06-architecture/00-overview.md` 里对应的编号条目同步加上（接在现有最后一条之后）。
- §9 是"为什么"的论证；这两条拒绝的理由若 §9 里还没有覆盖，在 §9 加一条简短的 `9.25`（AGENTS 现在的最后一条是 `9.23`/`9.24`，先看清编号再加，并同步 `tests/docfields/principles_test.go` 若它盯着这里的编号）。

- [ ] **Step 3: README、llms、CHANGELOG**

- `README.md` 里 "**16 commands in total:** …" 那一行（数字 Task 3/6 已改）的命令清单里补 `graph` `lint`（放在 `new` 附近，与 AGENTS §8 的顺序一致）；`README.zh.md` 同步。
- `llms.txt` 里 "Every one of the 14 commands plus `version`" 改成 16；`llms.zh.txt` 里 "14 个命令加 version" 改成 16。两份 llms 文件在 CLI 参考那一条之后各加一条 JSON Schema 的条目（指向 `schemas/component.schema.json` 与 `schemas/brickkit.schema.json` 的 raw 链接，一句话说明用途）。**注意 `check-docs-bilingual.py` 对 llms 链接的检查**——先读它，确认新加的条目符合它的规则（能过就行，别为它改脚本）。
- `CHANGELOG.md`：`## [Unreleased]` 下新增 `### Added`，三条（英文，"loosely Keep a Changelog"，一条一个用户可感知的东西）：`brickkit graph`、`brickkit lint`（含 `--strict`，含独立组件仓库模式）、`schemas/component.schema.json` 与 `schemas/brickkit.schema.json`；再加一条 `### Changed`：`brickkit up` / `status` / `sync` 内部共用同一份依赖解析（**行为不变**，用户看不到，就别写进去——只写用户感知得到的）。所以 Changed 不加。

- [ ] **Step 4: cli-reference（en / zh）**

`docs/{en,zh}/06-architecture/09-cli-reference.md`：
- 顶部"All the commands at a glance"表：在 `brickkit skills` 之后加 `brickkit graph`、`brickkit lint` 两行（分组列写在 `skills` 那一行的下面，参照现有"空分组格"的写法）。
- 在 `## brickkit skills` 一节之后（`## brickkit new` 之前）新增 `## brickkit graph` 与 `## brickkit lint` 两节，格式照 `## brickkit restore`：`**Syntax:**`、说明段落（先大白话讲它解决什么问题，再讲图上有什么/查什么，再讲**不做什么**以及为什么）、`**Flags**` 表、真实输出的 `**Example**`。
  - `graph`：一段真实的 Mermaid 输出；讲 stdout 纯 Mermaid、`> graph.mmd`、`--ignore-served-by`、GitHub / VS Code 怎么渲染（"把输出粘进一个 ```` ```mermaid ```` 围栏里"）；弱依赖取不到画成"未安装"；没有 `--output` 与 HTML/SVG（一句话说明理由：Mermaid 已经免费渲染）。
  - `lint`：两种模式、干净与出错各一段真实输出、`--strict`、退出码（0/1）、`LINT_FAILED` 链到错误码文档、**不查什么**（依赖解析、`servedBy` 目标、`configSchema` 里 enum/minimum 的值）与为什么。
- 两份的事实一致、输出块一致。

- [ ] **Step 5: quick-start 与 reference 文档里的 schema 说明**

- `docs/{en,zh}/00-quick-start.md`：新增一小节"给编辑器接上自动补全 / Wire up your editor"（放在合适的位置，先读一遍全文再定，别硬塞到不相干处）：先讲它是什么、能得到什么（字段补全、类型提示、拼错的键立刻红线，与 `brickkit lint` 报的是同一批问题）；然后给两种接法——文件顶部一行注释 `# yaml-language-server: $schema=https://raw.githubusercontent.com/brickKit/brickKit/main/schemas/component.schema.json`（brickkit.yaml 对应 `brickkit.schema.json`），以及 VS Code 的 `yaml.schemas` 设置（把两个文件名模式映射到两份 schema；用 `component.yaml` 与 `brickkit*.yaml` 这两个模式）；最后一句实话：schema 只覆盖结构（字段名、类型、必填、封闭取值），不覆盖跨文件的规则，那些看 `brickkit lint`（结构）与 `brickkit up --dry-run`（依赖与生成）。
- `docs/{en,zh}/06-architecture/07-component-yaml-reference.md` 与 `08-brickkit-yaml-reference.md` 顶部各加一句：本页字段有对应的 JSON Schema，链接到仓库里的 `schemas/…`（相对链接，`check-docs.py` 会检查它不悬空）。**`tests/docfields/reference_test.go` 盯着这两份文档的字段行，加的是一段说明文字，不是字段行，改完要跑它。**

- [ ] **Step 6: AI 助手技能**

`internal/skills/assets/claude/skills/brickkit-component/SKILL.md`：在写完/改完 `component.yaml` 的那一步加一句"先跑 `brickkit lint`（离线、秒回）再谈别的"；`brickkit-troubleshoot`：在 `brickkit up --dry-run` 那句旁边加一句"想看依赖关系图用 `brickkit graph`"；`brickkit-assemble` 里 `up --dry-run` 那句同理。先读 `internal/skills/skillmd_test.go` 看它对 SKILL.md 有什么约束（长度、命令引用必须真实存在……），改完跑 `go test ./internal/skills/`。技能文件的内容变了，`brickkit skills status` 会把已装的显示为"过期"——这是预期行为（CLI 升级后本来就要刷新一次），不需要处理。

- [ ] **Step 7: troubleshooting（可选，先跑再写）**

真去试几种误用：在一个既没有 `brickkit.yaml` 也没有 `component.yaml` 的目录里 `lint`；`brickkit.yaml` 通过了、`component.yaml` 里 `configSchema` 拼错键；用 `graph` 画出来的图在 GitHub 上不渲染（多半是没放进 mermaid 围栏）。只把**真跑出来、确有帮助**的写进 `docs/{en,zh}/08-troubleshooting.md`（症状 → 原因 → 修法，格式照现有条目）；一条没有就不加，报告里说明。

- [ ] **Step 8: 验证**

```bash
make check-docs check-docs-bilingual check-doc-fields
go test ./tests/docfields/ ./internal/skills/ -count=1
make build-cli && python3 scripts/check-cli-docs.py bin/brickkit
git grep -nE "[0-9]+ (个命令|条命令|commands)" -- ':!docs/archive' ':!docs/superpowers' ':!改进计划.md'   # 期望：出现的数字全是 16
```
`check-cli-docs.py` 的"详尽性"方向只打印不计入退出码，但这次要看它的输出：`brickkit graph`/`lint` 及它们的参数都应该被文档覆盖，不该出现在"未写进任何文档"的清单里。

- [ ] **Step 9: 全量检查并提交**

```bash
go test ./... -count=1
# 完整 make lint
git add AGENTS.md AGENTS.zh.md README.md README.zh.md llms.txt llms.zh.txt CHANGELOG.md docs/ internal/skills/assets/
git commit -F <写好信息的文件>   # 文档：graph / lint / JSON Schema，命令总数 14 → 16
```

---

## Task 10: 最终核对

**Files:** 无新增；只核对与修 Task 1–9 遗留的问题。

- [ ] **Step 1: 全量测试与静态检查**

```bash
go build ./... && go vet ./...
go test ./... -race -count=1
# 完整 make lint（含 check-schemas、cover-check）
```
把三条命令的**退出码**与覆盖率数字记进报告。

- [ ] **Step 2: 端到端冒烟（真二进制，不靠测试夹具）**

```bash
make build-cli
```
在临时目录里：`brickkit init demo --no-skills` → 用 `tests/components/` 里的真实组件走一遍 `add --local` → `brickkit graph`（输出粘进 mermaid 围栏或用 `npx @mermaid-js/mermaid-cli` 渲染核对，做不了就明说没做）→ `brickkit graph --ignore-served-by` → 故意把某个 `component.yaml` 拼错一个键 → `brickkit lint`（期望退出码 1、stdout 有逐字段报告、stderr 有 `LINT_FAILED`）→ 改回来 → `brickkit lint --strict`（期望 0）→ 再到一个只有 `component.yaml` 的目录里 `brickkit lint`（组件仓库模式）。每一步记下真实输出与退出码。

- [ ] **Step 3: 对照设计书逐条核**

打开 `docs/superpowers/specs/2026-09-19-graph-lint-schema-design.md`，逐节确认：§2（graph 的每条规则）、§3（lint 的两种模式、只读、离线、`LINT_FAILED`）、§4（schema 的必填规则、tag 清单、覆盖表、防漂移、`make generate-schemas`）、§5（计数与文档清单）、§6（范围之外的事一件都没做：没有 HTML/SVG、没有 `--output`、没有 `servedBy` 目标检查）。在报告里列出"设计书条目 → 对应的代码/测试/文档位置"，找不到对应的就是缺口。

- [ ] **Step 4: 收尾**

`git status --porcelain` 只应有未跟踪的 `改进计划.md`；`git log --oneline` 看本计划的提交序列是否清晰。不推送——推送要用户点头。
