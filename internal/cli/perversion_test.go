package cli

// 本文件钉住命令输出里"按组件版本、不按组件 ID"的判断，以及外壳成员在输出里的位置。

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/deployfile"
	"github.com/brickkit/brickkit/internal/i18n"
	"github.com/brickkit/brickkit/internal/msgid"
	"github.com/brickkit/brickkit/internal/project"
	"github.com/brickkit/brickkit/internal/project/projecttest"
	"github.com/brickkit/brickkit/internal/resolver"
)

// 团队文件与本地文件一模一样：2.0.0 关着，1.0.0 没写 mode。2.0.0 那一行不该被标成
// "来自 deploy.local.yaml"——从前按组件 ID 记团队的 mode，拿 1.0.0 的本地 mode 去比 2.0.0 的团队 mode。
func TestStatusLabelsEachVersionOnItsOwn(t *testing.T) {
	f := addedProject(t, []comp{
		{ID: "people/basic", Version: "1.0.0"},
		{ID: "people/basic", Version: "2.0.0"},
		{ID: "legacy/caller", Version: "1.0.0", Requires: []string{"people/basic@2.0.0"}},
	}, "people/basic@1.0.0")
	f.writeConfig(t, `components:
  - id: people/basic
    version: 1.0.0
  - id: people/basic
    version: 2.0.0
    mode: disable
  - id: legacy/caller
    version: 1.0.0
    mode: disable
`)
	team := readFile(t, f.Layout.DeployPath())
	require.NoError(t, os.WriteFile(f.Layout.DeployLocalPath(), []byte(team), 0o644))
	require.NoError(t, project.SetLocalMode(f.Layout, true))

	r := statusOf(t, newFakeEngine(), f.Dir)
	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)
	assert.NotContains(t, r.stdout, "deploy.local.yaml", "两份文件的 mode 完全一样，不该标出处：\n%s", r.stdout)
}

// mode: local 却没有源码目录：报错指向部署文件里的那个条目（mode 写在那里），并按组件版本判断
// 这次跑不跑——同 ID 的另一个版本在跑，不等于这个 mode: local 的版本在跑。
func TestLocalSourceMissingPointsAtDeployEntry(t *testing.T) {
	p := projecttest.Build(t, projecttest.Spec{Entries: []projecttest.Entry{
		{ID: "demo/other", Version: "1.0.0"},
		{ID: "demo/hello", Version: "2.0.0"},
		{ID: "demo/hello", Version: "1.0.0", Mode: deployfile.ModeLocal},
	}})

	require.NoError(t, checkLocalSources(p, []resolver.Ref{{ID: "demo/hello", Version: "2.0.0"}}),
		"1.0.0 这次不跑，不查它的源码目录")

	err := checkLocalSources(p, []resolver.Ref{{ID: "demo/hello", Version: "1.0.0"}})
	require.Error(t, err)
	e := clierr.As(err)
	var keys, values []string
	for _, d := range e.Details {
		keys, values = append(keys, d.Key), append(values, d.Value)
	}
	assert.Contains(t, keys, "components[2]", "指向部署文件里的条目")
	assert.Contains(t, values, p.DeployPath)
	assert.Contains(t, keys, i18n.T(msgid.LabelFile))
}

// 启动顺序列的是这次真正要起的工作负载：外壳承载的成员不单独列，写在外壳那一行。
func TestStartOrderSkipsHostedMembers(t *testing.T) {
	dir := copyFixture(t, "three-layer-shell")
	r := runWithEngine(t, newFakeEngine(), dir, "up", "--dry-run")
	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)

	step := regexp.MustCompile(`(?m)^\s+\d+\. (\S+)\s+(.*)$`)
	var shellLine string
	for _, m := range step.FindAllStringSubmatch(r.stdout, -1) {
		assert.NotEqual(t, "erp-api-1-0-0", m[1], "外壳里的成员不单独列")
		assert.NotEqual(t, "erp-worker-1-0-0", m[1])
		if m[1] == "erp-shell-1-0-0" {
			shellLine = m[0]
		}
	}
	require.NotEmpty(t, shellLine, r.stdout)
	assert.Contains(t, shellLine, "erp/api")
	assert.Contains(t, shellLine, "erp/worker")
}

// graph 与 up 一样核对外壳的三处声明：成员不在外壳的能力列表里时大声失败，不画一张错图。
func TestGraphRejectsInconsistentShell(t *testing.T) {
	dir := copyFixture(t, "three-layer-shell")
	deploy := strings.Replace(readFile(t, filepath.Join(dir, "deploy.yaml")),
		"  - id: erp/portal\n", "", 1)
	deploy = strings.Replace(deploy, "      - id: erp/worker\n", "      - id: erp/worker\n      - id: erp/portal\n", 1)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "deploy.yaml"), []byte(deploy), 0o644))

	r := runIn(t, dir, "graph")
	assert.NotEqual(t, clierr.ExitOK, r.code, r.stdout)
	assert.Contains(t, r.stderr, "erp/portal")
}
