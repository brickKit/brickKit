// 本文件是 Step 9 的代码层单测：参数解析、确认提示、清理与 clone 的失败路径。
package cli

import (
	"bytes"
	"context"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/manifest"
	"github.com/brickkit/brickkit/internal/project"
	"github.com/brickkit/brickkit/internal/resolver"
	"github.com/brickkit/brickkit/internal/source"
)

func skipIfRoot(t *testing.T) {
	t.Helper()
	if os.Geteuid() == 0 {
		t.Skip("以 root 运行时权限位不生效")
	}
}

// ============================================================
// parseComponentRef
// ============================================================

func TestParseComponentRef(t *testing.T) {
	id, version, err := parseComponentRef("people/basic@1.0.0")
	require.NoError(t, err)
	assert.Equal(t, "people/basic", id)
	assert.Equal(t, "1.0.0", version)

	// 省略版本合法：add 由此触发"取最新版本"，remove/fetch 由调用方推断
	id, version, err = parseComponentRef("people/basic")
	require.NoError(t, err)
	assert.Equal(t, "people/basic", id)
	assert.Empty(t, version)

	// 前后空白无所谓
	id, _, err = parseComponentRef("  people/basic@1.0.0 ")
	require.NoError(t, err)
	assert.Equal(t, "people/basic", id)
}

func TestParseComponentRefErrors(t *testing.T) {
	cases := []struct {
		name     string
		arg      string
		contains string
	}{
		{"组件 ID 非法", "PeopleBasic@1.0.0", "<scope>/<name>"},
		{"组件 ID 含大写", "People/Basic@1.0.0", "invalid component ID"},
		{"版本非法", "people/basic@abc", "invalid version"},
		{"版本非精确", "people/basic@^1.0.0", "exact version"},
		{"remove 的 ID 也要合法", "Nope", "invalid component ID"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, _, err := parseComponentRef(c.arg)
			require.Error(t, err)
			e := clierr.As(err)
			assert.Equal(t, clierr.ExitUsage, e.ExitCode(), "参数写错属于用法错误")
			assert.Contains(t, e.Format(), c.contains)
		})
	}
}

// ============================================================
// confirm
// ============================================================

func TestConfirm(t *testing.T) {
	cases := map[string]bool{
		"y\n": true, "Y\n": true, "yes\n": true, "YES\n": true,
		"n\n": false, "\n": false, "": false, "随便\n": false,
	}
	for input, want := range cases {
		var out bytes.Buffer
		opts := &Options{Stdin: strings.NewReader(input), Stdout: &out}
		assert.Equal(t, want, confirm(opts, "继续？[y/N]: "), "输入 %q", input)
		assert.Contains(t, out.String(), "继续？[y/N]: ")
	}
}

// 没有标准输入时视为拒绝，且不能卡住。
func TestConfirmWithoutStdin(t *testing.T) {
	var out bytes.Buffer
	opts := &Options{Stdout: &out}

	assert.False(t, confirm(opts, "继续？[y/N]: "))
	assert.Contains(t, out.String(), "继续？")
}

func TestItoa(t *testing.T) {
	assert.Equal(t, "0", itoa(0))
	assert.Equal(t, "7", itoa(7))
	assert.Equal(t, "42", itoa(42))
	assert.Equal(t, "1024", itoa(1024))
}

// ============================================================
// 依赖分类
// ============================================================

// 同时被强依赖和弱依赖引用的组件按"强"算：它无论如何都要装。
func TestDependencyKinds(t *testing.T) {
	shared := resolver.Ref{ID: "x/shared", Version: "1.0.0"}
	weak := resolver.Ref{ID: "x/weak", Version: "1.0.0"}
	graph := &resolver.Graph{Nodes: []*resolver.Node{
		{Ref: resolver.Ref{ID: "erp/a", Version: "1.0.0"}, Requires: []resolver.Ref{shared}},
		{Ref: resolver.Ref{ID: "erp/b", Version: "1.0.0"}, Optional: []resolver.Ref{shared, weak}},
	}}

	kinds := dependencyKinds(graph)

	assert.False(t, kinds[shared], "有人强依赖它，就不算仅弱依赖可达")
	assert.True(t, kinds[weak])
}

// ============================================================
// 弱依赖装完就会跑（003 §4.3）
// ============================================================

// ============================================================
// 失败路径
// ============================================================

// 某个组件的产物下载整体失败时记为警告，不中断其他组件。
func TestDownloadArtifactsWarnsInsteadOfFailing(t *testing.T) {
	layout := project.NewLayout(t.TempDir())
	client, err := source.New(layout, nil, source.Options{})
	require.NoError(t, err)
	t.Cleanup(func() { _ = client.Close() })

	// 版本号非法的 Manifest：DownloadArtifacts 直接返回错误
	broken := &manifest.Manifest{
		Metadata: manifest.Metadata{ID: "people/basic", Version: "latest"},
	}
	graph := &resolver.Graph{Nodes: []*resolver.Node{
		{Ref: resolver.Ref{ID: "people/basic", Version: "latest"}, Manifest: broken},
	}}

	sum := downloadArtifacts(context.Background(), client, graph)
	assert.Zero(t, sum.downloaded)
	require.Len(t, sum.warnings, 1)
	assert.Contains(t, sum.warnings[0].Format(), "invalid version")
}
