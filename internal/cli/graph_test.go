package cli

import (
	"bytes"
	"regexp"
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

// classLineOf 返回 `class <ids> <class>` 那一行（没有就返回空串）。
func classLineOf(stdout, class string) string {
	for _, line := range strings.Split(stdout, "\n") {
		if strings.HasPrefix(line, "    class ") && strings.HasSuffix(line, " "+class) {
			return line
		}
	}
	return ""
}

// mermaidStatement 列出 graph 会输出的每一种语句：顶层缩进 4 格，子图里的节点缩进 8 格。
// 输出里出现这之外的任何一行，就说明有别的东西写进了 stdout——`> graph.mmd` 存下来的
// 文件就不再是合法的 Mermaid。
var mermaidStatement = regexp.MustCompile(`^(?:` +
	` {4}(?:` +
	`%% \S.*|` + // 注释：提示只能写成这样
	`subgraph [a-z0-9_]+\["[^"]*"\]|end|` + // 子图
	`[a-z0-9_]+\["[^"]*"\]|` + // 节点
	`[a-z0-9_]+ (?:-->|-\.->) [a-z0-9_]+|` + // 边
	`classDef [a-z]+ \S.*;|` + // 样式定义
	`class [a-z0-9_,]+ [a-z]+` + // 样式套用
	`)| {8}[a-z0-9_]+\["[^"]*"\]` + // 子图里只有节点
	`)$`)

// requirePureMermaid 断言 stdout 是纯 Mermaid：第一行是 graph TD，其余每一行都是
// mermaidStatement 里的某一种，没有警告、没有状态符号、没有别的说明文字。
func requirePureMermaid(t *testing.T, stdout string) {
	t.Helper()
	require.True(t, strings.HasSuffix(stdout, "\n"), "输出以换行结尾：%q", stdout)
	lines := strings.Split(strings.TrimSuffix(stdout, "\n"), "\n")
	require.Equal(t, "graph TD", lines[0])
	for _, line := range lines[1:] {
		assert.Regexp(t, mermaidStatement, line, "不是 graph 会输出的 Mermaid 语句：%q", line)
	}
	for _, prose := range []string{"⚠️", "✅", "❌", "📋", "🚀"} {
		assert.NotContains(t, stdout, prose)
	}
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
    demo_hello_1_0_0["demo/hello@1.0.0"]
    demo_caller_1_0_0["demo/caller@1.0.0"]
    demo_caller_1_0_0 --> demo_hello_1_0_0
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
	requirePureMermaid(t, r.stdout)
	assert.Contains(t, r.stdout, "    demo_caller_1_0_0 -.-> demo_cache_1_0_0\n")
	assert.NotContains(t, r.stdout, "demo_caller_1_0_0 --> demo_cache_1_0_0")
}

// 被关掉的顶层带着它下面的一串一起置灰；不相干的组件不受影响。
func TestGraphDisabledComponentsAreGreyedOut(t *testing.T) {
	f := graphProject(t, `components:
  - id: demo/caller
    version: 1.0.0
    mode: disable
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
	requirePureMermaid(t, r.stdout)
	assert.Contains(t, r.stdout, "    classDef disabled fill:#eee,stroke:#999,color:#999;\n")

	classLine := classLineOf(r.stdout, "disabled")
	require.NotEmpty(t, classLine, r.stdout)
	assert.Contains(t, classLine, "demo_caller_1_0_0")
	assert.Contains(t, classLine, "demo_hello_1_0_0")
	assert.NotContains(t, classLine, "demo_solo_1_0_0")
}

func TestGraphMarksLocalDebugComponent(t *testing.T) {
	f := graphProject(t, `components:
  - id: demo/hello
    version: 1.0.0
    mode: debug
    localPort: 8081
resources: []
`, comp{ID: "demo/hello", Version: "1.0.0"})

	r := runIn(t, f.Dir, "graph")
	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)
	requirePureMermaid(t, r.stdout)
	assert.Contains(t, r.stdout, `demo_hello_1_0_0["demo/hello@1.0.0<br/>local debug :8081"]`)
	assert.Contains(t, r.stdout, "    classDef local fill:#e6f2ff,stroke:#3673a8;\n")
	assert.Contains(t, r.stdout, "    class demo_hello_1_0_0 local\n")
	assert.NotContains(t, r.stdout, "classDef disabled", "没有被关掉的组件就不输出 disabled 样式")
}

// localPort 不是必填：没写时由 up 在生成阶段分配，图上算不出来，也不能编一个端口出来。
func TestGraphLocalDebugWithoutLocalPortShowsNoPort(t *testing.T) {
	f := graphProject(t, `components:
  - id: demo/hello
    version: 1.0.0
    mode: debug
resources: []
`, comp{ID: "demo/hello", Version: "1.0.0"})

	r := runIn(t, f.Dir, "graph")
	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)
	assert.Contains(t, r.stdout, `demo_hello_1_0_0["demo/hello@1.0.0<br/>local debug"]`)
	assert.NotContains(t, r.stdout, "local debug :")
	assert.Contains(t, r.stdout, "    class demo_hello_1_0_0 local\n")
}

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
// 跟下面断言它不在 disabled 里的结果矛盾——这条测试的意义正在于验证
// "钉住"确实推翻了这条默认的级联规则。
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

// mode: debug 与 mode: enabled 一样是"钉住"：上层全被关掉，它照样在跑，所以永远
// 不会被置灰——local 样式因此只会落在真的在跑的组件上。被跳过的（web 被关、api
// 没人需要）套 disabled，"置灰 = 这次不会启动"是唯一的信号。
func TestGraphDebugComponentIsPinnedAndNeverGreyedOut(t *testing.T) {
	f := graphProject(t, `components:
  - id: demo/web
    version: 1.0.0
    mode: disable
  - id: demo/api
    version: 1.0.0
  - id: demo/db
    version: 1.0.0
    mode: debug
    localPort: 9001
  - id: demo/solo
    version: 1.0.0
    mode: debug
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

	requirePureMermaid(t, r.stdout)

	// 上层全被关掉，但 db 是 debug（钉住）：它照跑，不在 disabled 里
	assert.Contains(t, r.stdout, `demo_db_1_0_0["demo/db@1.0.0<br/>local debug :9001"]`)
	disabled := classLineOf(r.stdout, "disabled")
	require.NotEmpty(t, disabled, r.stdout)
	assert.Contains(t, disabled, "demo_web_1_0_0", "web 自己被关")
	assert.Contains(t, disabled, "demo_api_1_0_0", "api 上面没人需要它")
	assert.NotContains(t, disabled, "demo_db_1_0_0", "debug 被钉住，不会被级联跳过")
	assert.NotContains(t, disabled, "demo_solo_1_0_0")

	// 两个 debug 组件都在跑，都套 local
	assert.Equal(t, "    class demo_db_1_0_0,demo_solo_1_0_0 local", classLineOf(r.stdout, "local"), r.stdout)
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

	requirePureMermaid(t, r.stdout)
	assert.Contains(t, r.stdout, "    subgraph demo_shell_1_0_0_members[\"Shell: demo/shell@1.0.0\"]\n"+
		"        demo_a_1_0_0[\"demo/a@1.0.0\"]\n"+
		"        demo_b_1_0_0[\"demo/b@1.0.0\"]\n"+
		"    end\n")
	// 外壳自己与不相干的组件画在子图外面
	assert.Contains(t, r.stdout, "\n    demo_shell_1_0_0[\"demo/shell@1.0.0\"]\n")
	assert.Contains(t, r.stdout, "\n    demo_free_1_0_0[\"demo/free@1.0.0\"]\n")
}

func TestGraphIgnoreServedByDropsGroupingAndSaysSo(t *testing.T) {
	f := graphProject(t, graphServedByBody, servedByComps()...)

	r := runIn(t, f.Dir, "graph", "--ignore-served-by")
	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)
	requirePureMermaid(t, r.stdout)
	assert.NotContains(t, r.stdout, "subgraph")
	assert.Contains(t, r.stdout, "    %% All servedBy declarations are ignored")

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
	assert.Contains(t, r.stdout, `demo_ghost_1_0_0["demo/ghost@1.0.0<br/>not installed"]`)
	assert.Contains(t, r.stdout, "    demo_caller_1_0_0 -.-> demo_ghost_1_0_0\n")
	assert.Contains(t, r.stdout, "    class demo_ghost_1_0_0 missing\n")
	assert.Contains(t, r.stdout, "    classDef missing ")

	// stdout 里只有 Mermaid：解析警告在 stderr
	requirePureMermaid(t, r.stdout)
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
	requirePureMermaid(t, r.stdout)
}

func TestGraphEmptyProject(t *testing.T) {
	dir := t.TempDir()
	require.Equal(t, 0, runIn(t, dir, "init", "p", "--no-skills").code)

	r := runIn(t, dir, "graph")
	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)
	assert.Equal(t, "graph TD\n    %% The current project has no components\n", r.stdout)
}

func TestGraphFailsLikeUpWhenRequiredDependencyMissing(t *testing.T) {
	f := graphProject(t, `components:
  - id: demo/caller
    version: 1.0.0
resources: []
`, comp{ID: "demo/caller", Version: "1.0.0", Requires: []string{"demo/ghost@1.0.0"}})

	r := runWithLogs(t, f.Dir, "graph")
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
	assert.Equal(t, 1, strings.Count(r.stdout, `demo_ghost_1_0_0["`), r.stdout)
	assert.Contains(t, r.stdout, "    demo_a_1_0_0 -.-> demo_ghost_1_0_0\n")
	assert.Contains(t, r.stdout, "    demo_b_1_0_0 -.-> demo_ghost_1_0_0\n")
	assert.Contains(t, r.stdout, "    class demo_ghost_1_0_0 missing\n")
}

// brickkit.yaml 里没列出来的传递依赖照样画：resolver 会把它拉进图（up 也是），
// 它没有自己的条目，也就没有 mode / servedBy 可读。
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
    demo_hello_1_0_0["demo/hello@1.0.0"]
    demo_caller_1_0_0["demo/caller@1.0.0"]
    demo_caller_1_0_0 --> demo_hello_1_0_0
`, r.stdout)
}

func TestGraphFailsWithoutProjectConfig(t *testing.T) {
	r := runWithLogs(t, t.TempDir(), "graph")
	assert.Equal(t, clierr.ExitError, r.code)
	assert.Empty(t, r.stdout)
	assert.Contains(t, r.stderr, `"error_code":"PROJECT_MISSING"`)
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
	assert.Equal(t, "graph TD\n    %% The current project has no components\n", r.stdout)
}

// 有组件时同一份配置照样报错：graph 不绕过验签配置，报的就是 up 那一条。
// 上一条证明了这份配置本身能通过解析，所以这里的失败只可能来自读公钥；断言稳定的错误码，
// 而不是别的包里那句报错文案——文案改了，这条不该跟着碎。
func TestGraphReportsUnreadableTrustedKey(t *testing.T) {
	f := graphProject(t, graphUnreadableKeyConfig+`components:
  - id: demo/hello
    version: 1.0.0
resources: []
`, comp{ID: "demo/hello", Version: "1.0.0"})

	r := runWithLogs(t, f.Dir, "graph")
	assert.Equal(t, clierr.ExitError, r.code)
	assert.Empty(t, r.stdout)
	assert.Contains(t, r.stderr, `"error_code":"CONFIG_INVALID"`)
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
	assert.Contains(t, out.String(), `demo_hello_1_0_0["demo/hello@1.0.0"]`)
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
	assert.Contains(t, out, `    demo_a_1_0_0["demo/a@1.0.0"]`+"\n")
}

// 版本化服务名不总是合法的 Mermaid 标识符：组件 ID 的规则允许连续的 `--`（my--scope/a）
// 和任何单词作 scope（graph/store、end/x）。含 `--` 的 ID 会被 Mermaid 当成边，以
// end / graph 之类开头的会被当成关键字——`up` 对这些 ID 完全正常，而 graph 若直接
// 用服务名，会退出码 0 地打印出任何渲染器都不认的文本（用真实的 mermaid-cli 验过）。
// 所以节点 ID 里一个 `-` 都不留。
func TestGraphNodeIDsAreLegalMermaidIdentifiers(t *testing.T) {
	f := graphProject(t, `components:
  - id: my--scope/a
    version: 1.0.0
  - id: graph/store
    version: 1.0.0
  - id: end/x
    version: 1.0.0
resources: []
`,
		comp{ID: "end/x", Version: "1.0.0"},
		comp{ID: "graph/store", Version: "1.0.0", Requires: []string{"end/x@1.0.0"}},
		comp{ID: "my--scope/a", Version: "1.0.0", Requires: []string{"graph/store@1.0.0"}},
	)

	r := runIn(t, f.Dir, "graph")
	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)
	requirePureMermaid(t, r.stdout)

	nodeID := regexp.MustCompile(`(?m)^\s+([^\s\[]+)\["`)
	ids := nodeID.FindAllStringSubmatch(r.stdout, -1)
	require.Len(t, ids, 3, r.stdout)
	for _, m := range ids {
		assert.NotContains(t, m[1], "-", "节点 ID 不能含连字符：%s", m[1])
	}

	// 直接用服务名的话会是这几个——它们一个都不能出现
	for _, naive := range []string{"my--scope-a-1-0-0", "graph-store-1-0-0", "end-x-1-0-0"} {
		assert.NotContains(t, r.stdout, naive)
	}

	assert.Equal(t, `graph TD
    end_x_1_0_0["end/x@1.0.0"]
    graph_store_1_0_0["graph/store@1.0.0"]
    my__scope_a_1_0_0["my--scope/a@1.0.0"]
    graph_store_1_0_0 --> end_x_1_0_0
    my__scope_a_1_0_0 --> graph_store_1_0_0
`, r.stdout)
}

// 服务名只含 [a-z0-9-]，所以把 - 换成 _ 是单射：不会让两个不同的组件撞 ID
// （my-scope/a 与 my--scope/a 是两个组件）。
func TestMermaidIDReplacesEveryHyphen(t *testing.T) {
	cases := []struct{ id, version, want string }{
		{"demo/hello", "1.0.0", "demo_hello_1_0_0"},
		{"my--scope/a", "1.0.0", "my__scope_a_1_0_0"},
		{"my-scope/a", "1.0.0", "my_scope_a_1_0_0"},
		{"graph/store", "1.0.0", "graph_store_1_0_0"},
		{"end/x", "2.10.3", "end_x_2_10_3"},
	}
	seen := map[string]string{}
	for _, c := range cases {
		got := mermaidID(resolver.Ref{ID: c.id, Version: c.version})
		assert.Equal(t, c.want, got)
		assert.NotContains(t, got, "-")
		if prev, dup := seen[got]; dup {
			t.Errorf("%s 与 %s 撞了同一个节点 ID %s", prev, c.id, got)
		}
		seen[got] = c.id
	}
}

// servedBy 指向的外壳不在项目里：配置校验不管这件事，up 才在生成阶段报。graph 画的是
// 声明的结构，照样把成员归在那个外壳名下，只是外壳自己没有节点。
func TestGraphGroupsMembersEvenWhenTheShellIsNotInTheProject(t *testing.T) {
	f := graphProject(t, `components:
  - id: demo/a
    version: 1.0.0
    servedBy: demo/absentshell@1.0.0
  - id: demo/b
    version: 1.0.0
    servedBy: demo/absentshell@1.0.0
resources: []
`,
		comp{ID: "demo/a", Version: "1.0.0", Port: 8081},
		comp{ID: "demo/b", Version: "1.0.0", Port: 8082},
	)

	r := runIn(t, f.Dir, "graph")
	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)
	requirePureMermaid(t, r.stdout)
	assert.Equal(t, `graph TD
    subgraph demo_absentshell_1_0_0_members["Shell: demo/absentshell@1.0.0"]
        demo_a_1_0_0["demo/a@1.0.0"]
        demo_b_1_0_0["demo/b@1.0.0"]
    end
`, r.stdout)
}
