package shell_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/brickkit/brickkit/internal/deployfile"
	"github.com/brickkit/brickkit/internal/manifest"
)

// shellManifest 造一个声明了 shell.members 的外壳。
func shellManifest(id, version string, port int, members ...string) *manifest.Manifest {
	m := simple(id, version, port)
	m.Shell = &manifest.Shell{Members: members}
	return m
}

// brickkit.yaml 标了 kind: shell，component.yaml 却没有 shell 块：两处说法对不上（附录 A11）。
func TestCheckKindWithoutShellBlock(t *testing.T) {
	_, err := resolveRaw(t, &testCfg{Components: []testComp{
		comp("erp/shell", "1.0.0", ""),
		comp("erp/a", "1.0.0", "erp/shell@1.0.0"),
	}}, map[string]*manifest.Manifest{
		"erp/shell@1.0.0": simple("erp/shell", "1.0.0", 8080),
		"erp/a@1.0.0":     simple("erp/a", "1.0.0", 8081),
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "erp/shell")
	assert.Contains(t, err.Error(), "kind: shell")
}

// 反过来：component.yaml 声明自己是外壳，brickkit.yaml 却没标 kind: shell。
func TestCheckShellBlockWithoutKind(t *testing.T) {
	_, err := resolveRaw(t, &testCfg{Components: []testComp{
		comp("erp/shell", "1.0.0", ""),
	}}, map[string]*manifest.Manifest{
		"erp/shell@1.0.0": shellManifest("erp/shell", "1.0.0", 8080, "erp/a"),
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "erp/shell")
	assert.Contains(t, err.Error(), "kind: shell")
}

// 部署文件把外壳没声明过能承载的组件放进了 members。
func TestCheckMemberNotHostable(t *testing.T) {
	_, err := resolveRaw(t, &testCfg{Components: []testComp{
		comp("erp/shell", "1.0.0", ""),
		comp("erp/x", "1.0.0", "erp/shell@1.0.0"),
	}}, map[string]*manifest.Manifest{
		"erp/shell@1.0.0": shellManifest("erp/shell", "1.0.0", 8080, "erp/a"),
		"erp/x@1.0.0":     simple("erp/x", "1.0.0", 8081),
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "erp/x")
	assert.Contains(t, err.Error(), "erp/shell")
	assert.Contains(t, err.Error(), "erp/a", "要列出外壳能承载哪些组件")
}

// 外壳自己以裸进程运行、承载着成员：允许——本地开发一开始就是这样（成员跟着外壳在宿主机上跑）。
func TestCheckBareShellWithMembersAllowed(t *testing.T) {
	groups, err := resolveRaw(t, &testCfg{Components: []testComp{
		{ID: "erp/shell", Version: "1.0.0", Mode: deployfile.ModeLocal},
		comp("erp/a", "1.0.0", "erp/shell@1.0.0"),
	}}, map[string]*manifest.Manifest{
		"erp/shell@1.0.0": shellManifest("erp/shell", "1.0.0", 8080, "erp/a"),
		"erp/a@1.0.0":     simple("erp/a", "1.0.0", 8081),
	})
	require.NoError(t, err)
	require.Len(t, groups, 1)
	assert.Len(t, groups[0].Members, 1)
}

func TestCheckConsistentProject(t *testing.T) {
	groups, err := resolveRaw(t, &testCfg{Components: []testComp{
		comp("erp/shell", "1.0.0", ""),
		comp("erp/a", "1.0.0", "erp/shell@1.0.0"),
	}}, map[string]*manifest.Manifest{
		"erp/shell@1.0.0": shellManifest("erp/shell", "1.0.0", 8080, "erp/a"),
		"erp/a@1.0.0":     simple("erp/a", "1.0.0", 8081),
	})
	require.NoError(t, err)
	require.Len(t, groups, 1)
}
