package cascade_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/brickkit/brickkit/internal/cascade"
	"github.com/brickkit/brickkit/internal/deployfile"
	"github.com/brickkit/brickkit/internal/project"
	"github.com/brickkit/brickkit/internal/projfile"
	"github.com/brickkit/brickkit/internal/resolver"
)

// shellProject：erp/shell 承载 erp/a 与 erp/b；modes 给出各条目的 mode。
func shellProject(t *testing.T, modes map[string]string) *project.Project {
	t.Helper()
	decl := &projfile.File{Project: "p", Components: []projfile.Component{
		{ID: "erp/shell", Version: "1.0.0", Kind: projfile.KindShell},
		{ID: "erp/a", Version: "1.0.0"},
		{ID: "erp/b", Version: "1.0.0"},
		{ID: "erp/solo", Version: "1.0.0"},
	}}
	deploy := &deployfile.File{Target: deployfile.TargetDocker, Components: []deployfile.Component{
		{ID: "erp/shell", Mode: modes["erp/shell"], Members: []string{"erp/a", "erp/b"}},
		{ID: "erp/a", Mode: modes["erp/a"]},
		{ID: "erp/b", Mode: modes["erp/b"]},
		{ID: "erp/solo", Mode: modes["erp/solo"]},
	}}
	p, err := project.Assemble(project.NewLayout(t.TempDir()), decl, deploy)
	require.NoError(t, err)
	return p
}

// HostOf 是"这次谁在谁的进程里跑"的唯一判据：inject（地址重写）、shell（JSON）、
// compose / k8s（生不生成容器）、graph（怎么画）都问它，不各自再判一遍。
func TestHostOf(t *testing.T) {
	graph := newGraph(t, spec{id: "erp/shell"}, spec{id: "erp/a"}, spec{id: "erp/b"}, spec{id: "erp/solo"})
	shell := ref("erp/shell")

	p := shellProject(t, map[string]string{"erp/b": deployfile.ModeLocal})
	states, err := cascade.Compute(p, graph)
	require.NoError(t, err)

	got, ok := states.HostOf(p, ref("erp/a"))
	assert.True(t, ok)
	assert.Equal(t, shell, got)
	_, ok = states.HostOf(p, ref("erp/b"))
	assert.False(t, ok, "以裸进程运行的成员这次不进外壳（附录 A18）")
	_, ok = states.HostOf(p, ref("erp/solo"))
	assert.False(t, ok)

	declared, ok := cascade.ShellOf(p, ref("erp/b"))
	assert.True(t, ok, "声明上的成员关系与这次跑不跑无关")
	assert.Equal(t, shell, declared)

	// 外壳没跑：成员回落成独立组件
	stopped := shellProject(t, map[string]string{"erp/shell": deployfile.ModeDisable, "erp/a": deployfile.ModeEnabled})
	states, err = cascade.Compute(stopped, graph)
	require.NoError(t, err)
	_, ok = states.HostOf(stopped, ref("erp/a"))
	assert.False(t, ok)
}

// 成员有两个版本、外壳承载其中一个：只有那个版本被承载，另一个版本独立部署。
func TestHostOfIsVersionAware(t *testing.T) {
	decl := &projfile.File{Project: "p", Components: []projfile.Component{
		{ID: "erp/shell", Version: "1.0.0", Kind: projfile.KindShell},
		{ID: "erp/a", Version: "1.0.0"},
		{ID: "erp/a", Version: "2.0.0"},
	}}
	deploy := &deployfile.File{Target: deployfile.TargetDocker, Components: []deployfile.Component{
		{ID: "erp/shell", Members: []string{"erp/a@2.0.0"}},
		{ID: "erp/a"},
	}}
	p, err := project.Assemble(project.NewLayout(t.TempDir()), decl, deploy)
	require.NoError(t, err)
	graph := newGraph(t, spec{id: "erp/shell"}, spec{id: "erp/a"})
	states, err := cascade.Compute(p, graph)
	require.NoError(t, err)

	_, ok := cascade.ShellOf(p, resolver.Ref{ID: "erp/a", Version: "1.0.0"})
	assert.False(t, ok, "1.0.0 不在外壳里")
	_, ok = cascade.ShellOf(p, resolver.Ref{ID: "erp/a", Version: "2.0.0"})
	assert.True(t, ok)
	_ = states
}
