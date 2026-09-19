package cli

import (
	"bytes"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/brickkit/brickkit/internal/cascade"
	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/config"
	"github.com/brickkit/brickkit/internal/logging"
	"github.com/brickkit/brickkit/internal/resolver"
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

// localPort 不是必填：没写时由 up 在生成阶段分配，图上算不出来，也不能编一个端口出来。
func TestGraphLocalDebugWithoutLocalPortShowsNoPort(t *testing.T) {
	f := graphProject(t, `components:
  - id: demo/hello
    version: 1.0.0
    local: true
resources: []
`, comp{ID: "demo/hello", Version: "1.0.0"})

	r := runIn(t, f.Dir, "graph")
	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)
	assert.Contains(t, r.stdout, `demo-hello-1-0-0["demo/hello@1.0.0<br/>本地调试"]`)
	assert.NotContains(t, r.stdout, "本地调试 :")
	assert.Contains(t, r.stdout, "    class demo-hello-1-0-0 local\n")
}

// classLineOf 返回 `class <ids> <class>` 那一行（没有就返回空串）。
func classLineOf(stdout, class string) string {
	for _, line := range strings.Split(stdout, "\n") {
		if strings.HasPrefix(line, "    class ") && strings.HasSuffix(line, " "+class) {
			return line
		}
	}
	return ""
}

// cascade 从不读 local：local: true 的组件与别的组件一样跟着上层走，也可能被跳过。
// 被跳过的只套 disabled——"置灰 = 这次不会启动"是唯一的信号；标签里的"本地调试"
// 仍在，因为那是声明的结构。在跑的 local 组件照旧套 local。
func TestGraphLocalClassOnlyAppliesToRunningComponents(t *testing.T) {
	f := graphProject(t, `components:
  - id: demo/web
    version: 1.0.0
    enabled: false
  - id: demo/api
    version: 1.0.0
  - id: demo/db
    version: 1.0.0
    local: true
    localPort: 9001
  - id: demo/solo
    version: 1.0.0
    local: true
    localPort: 9002
resources: []
`,
		comp{ID: "demo/db", Version: "1.0.0"},
		comp{ID: "demo/api", Version: "1.0.0", Requires: []string{"demo/db@1.0.0"}},
		comp{ID: "demo/web", Version: "1.0.0", Requires: []string{"demo/api@1.0.0"}},
		comp{ID: "demo/solo", Version: "1.0.0"},
	)

	r := runIn(t, f.Dir, "graph")
	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)

	// 被跳过的 local 组件：标签保留，样式只有 disabled
	assert.Contains(t, r.stdout, `demo-db-1-0-0["demo/db@1.0.0<br/>本地调试 :9001"]`)
	disabled := classLineOf(r.stdout, "disabled")
	require.NotEmpty(t, disabled, r.stdout)
	assert.Contains(t, disabled, "demo-db-1-0-0")
	assert.NotContains(t, disabled, "demo-solo-1-0-0")

	// 在跑的 local 组件：只有它套 local
	assert.Equal(t, "    class demo-solo-1-0-0 local", classLineOf(r.stdout, "local"), r.stdout)
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

	// 错误码只在 stderr 的 JSON 日志行里（runIn 把日志整个关掉了），所以这里打开日志级别
	r := runWith(t, func(o *Options) { o.LogLevel = logging.LevelInfo }, f.Dir, "graph")
	assert.Equal(t, clierr.ExitError, r.code)
	assert.Empty(t, r.stdout, "解析不出来就什么都不画")
	assert.Contains(t, r.stderr, `"error_code":"DEPENDENCY_MISSING"`)
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

// 两个组件弱依赖同一个取不到的组件：占位节点只声明一次，class 行里也只列一次。
func TestGraphDeclaresMissingOptionalPlaceholderOnce(t *testing.T) {
	f := graphProject(t, `components:
  - id: demo/a
    version: 1.0.0
  - id: demo/b
    version: 1.0.0
resources: []
`,
		comp{ID: "demo/a", Version: "1.0.0", Optional: []string{"demo/ghost@1.0.0"}},
		comp{ID: "demo/b", Version: "1.0.0", Optional: []string{"demo/ghost@1.0.0"}},
	)

	r := runIn(t, f.Dir, "graph")
	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)
	assert.Equal(t, 1, strings.Count(r.stdout, `demo-ghost-1-0-0["`), r.stdout)
	assert.Contains(t, r.stdout, "    demo-a-1-0-0 -.-> demo-ghost-1-0-0\n")
	assert.Contains(t, r.stdout, "    demo-b-1-0-0 -.-> demo-ghost-1-0-0\n")
	assert.Contains(t, r.stdout, "    class demo-ghost-1-0-0 missing\n")
}

// brickkit.yaml 里没列出来的传递依赖照样画：resolver 会把它拉进图（up 也是），
// 它没有自己的条目，也就没有 local / servedBy 可读。
func TestGraphDrawsTransitiveDependencyNotListedInConfig(t *testing.T) {
	f := graphProject(t, `components:
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

func TestGraphFailsWithoutProjectConfig(t *testing.T) {
	r := runIn(t, t.TempDir(), "graph")
	assert.Equal(t, clierr.ExitError, r.code)
	assert.Empty(t, r.stdout)
	assert.Contains(t, r.stderr, "项目配置文件不存在")
}

const graphUnreadableKeyConfig = `installer:
  publicKeys:
    keys/nope.pub: keys/nope.pub
`

// 没有组件就不构造安装源客户端：那一步会读 installer.publicKeys 指向的公钥文件，
// 而一张空项目的图根本用不上它。
func TestGraphEmptyProjectDoesNotNeedTrustedKeys(t *testing.T) {
	f := graphProject(t, graphUnreadableKeyConfig+"components: []\nresources: []\n")

	r := runIn(t, f.Dir, "graph")
	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)
	assert.Equal(t, "graph TD\n    %% 当前项目没有组件\n", r.stdout)
}

// 有组件时同一份配置照样报错：graph 不绕过验签配置，报的就是 up 那一条。
func TestGraphReportsUnreadableTrustedKey(t *testing.T) {
	f := graphProject(t, graphUnreadableKeyConfig+`components:
  - id: demo/hello
    version: 1.0.0
resources: []
`, comp{ID: "demo/hello", Version: "1.0.0"})

	r := runIn(t, f.Dir, "graph")
	assert.Equal(t, clierr.ExitError, r.code)
	assert.Empty(t, r.stdout)
	assert.Contains(t, r.stderr, "读取可信公钥失败")
}

// runGraph 允许 ctx 为 nil（与 runAdd / runRemove 一样：cobra 在某些路径下不注入 context）。
// 项目里放一个组件，ctx 才会真的流进依赖解析。
func TestRunGraphWithNilContext(t *testing.T) {
	f := graphProject(t, `components:
  - id: demo/hello
    version: 1.0.0
resources: []
`, comp{ID: "demo/hello", Version: "1.0.0"})

	var out, errBuf bytes.Buffer
	opts := &Options{
		WorkDir: f.Dir, ConfigPath: DefaultConfigFile, LogLevel: logging.LevelOff,
		Stdout: &out, Stderr: &errBuf,
	}
	//nolint:staticcheck // 显式传 nil 是本用例的目的
	require.NoError(t, runGraph(nil, opts, false))
	assert.Contains(t, out.String(), `demo-hello-1-0-0["demo/hello@1.0.0"]`)
}

// config 校验保证 servedBy 是合法的 id@version，但渲染这一层不依赖这个保证
// （shell.ParseRef 本身也不做这个假设）：格式不对的当作没写，节点照常画。
func TestRenderMermaidIgnoresMalformedServedBy(t *testing.T) {
	ref := resolver.Ref{ID: "demo/a", Version: "1.0.0"}
	cfg := &config.Config{Components: []config.Component{
		{ID: ref.ID, Version: ref.Version, ServedBy: "not-a-ref"},
	}}
	graph := &resolver.Graph{Nodes: []*resolver.Node{{Ref: ref}}}

	out := renderMermaid(cfg, graph, &cascade.Result{}, false)
	assert.NotContains(t, out, "subgraph")
	assert.Contains(t, out, `    demo-a-1-0-0["demo/a@1.0.0"]`+"\n")
}
