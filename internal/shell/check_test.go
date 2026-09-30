package shell_test

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/brickkit/brickkit/internal/cascade"
	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/deployfile"
	"github.com/brickkit/brickkit/internal/i18n"
	"github.com/brickkit/brickkit/internal/manifest"
	"github.com/brickkit/brickkit/internal/msgid"
	"github.com/brickkit/brickkit/internal/resolver"
	"github.com/brickkit/brickkit/internal/shell"
)

// shellManifest 造一个声明了 shell.members 的外壳；members 每项写 id@version。
func shellManifest(id, version string, port int, members ...string) *manifest.Manifest {
	m := simple(id, version, port)
	m.Shell = &manifest.Shell{Members: members}
	return m
}

// brickkit.yaml 标了 kind: shell，component.yaml 却没有 shell 块：两处说法对不上。
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
		"erp/shell@1.0.0": shellManifest("erp/shell", "1.0.0", 8080, "erp/a@1.0.0"),
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
		"erp/shell@1.0.0": shellManifest("erp/shell", "1.0.0", 8080, "erp/a@1.0.0"),
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
		"erp/shell@1.0.0": shellManifest("erp/shell", "1.0.0", 8080, "erp/a@1.0.0"),
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
		"erp/shell@1.0.0": shellManifest("erp/shell", "1.0.0", 8080, "erp/a@1.0.0"),
		"erp/a@1.0.0":     simple("erp/a", "1.0.0", 8081),
	})
	require.NoError(t, err)
	require.Len(t, groups, 1)
}

// skipWaitFor 只能写这个组件真实的强依赖：写错名字、写了弱依赖，都是"以为生效了其实没有"，
// 生成时大声失败，并指向那个条目。
func TestSkipWaitForMustNameARequiredDependency(t *testing.T) {
	a := dependsOn(simple("erp/a", "1.0.0", 8081), "erp/db", "1.0.0")
	a.Dependencies.Components = append(a.Dependencies.Components, manifest.ComponentDep{ID: "erp/weak", Version: "1.0.0", Optional: true})
	manifests := map[string]*manifest.Manifest{
		"erp/a@1.0.0":    a,
		"erp/db@1.0.0":   simple("erp/db", "1.0.0", 5432),
		"erp/weak@1.0.0": simple("erp/weak", "1.0.0", 5433),
	}
	build := func(skip ...string) error {
		a := comp("erp/a", "1.0.0", "")
		a.SkipWaitFor = skip
		_, err := resolveRaw(t, &testCfg{Components: []testComp{
			a, comp("erp/db", "1.0.0", ""), comp("erp/weak", "1.0.0", ""),
		}}, manifests)
		return err
	}
	require.NoError(t, build("erp/db"))
	for _, wrong := range []string{"erp/dbb", "erp/weak"} {
		err := build(wrong)
		require.Error(t, err, wrong)
		assert.Contains(t, err.Error(), wrong)
		assert.Contains(t, err.Error(), "components[0].skipWaitFor[0]")
		assert.Contains(t, err.Error(), "erp/db", "列出它真正的强依赖")
	}
}

// 外壳条目上的 skipWaitFor 只管外壳自己的依赖：写了一个成员的依赖，报错要提示写到成员的条目上去。
func TestShellSkipWaitForNamingAMemberDependency(t *testing.T) {
	sh := comp("erp/shell", "1.0.0", "")
	sh.SkipWaitFor = []string{"erp/x"}
	_, err := resolveFixture(t, &testCfg{Components: []testComp{
		sh, comp("erp/a", "1.0.0", "erp/shell@1.0.0"), comp("erp/x", "1.0.0", ""),
	}}, map[string]*manifest.Manifest{
		"erp/shell@1.0.0": simple("erp/shell", "1.0.0", 8080),
		"erp/a@1.0.0":     dependsOn(simple("erp/a", "1.0.0", 8081), "erp/x", "1.0.0"),
		"erp/x@1.0.0":     simple("erp/x", "1.0.0", 8090),
	})
	require.Error(t, err)
	assert.Contains(t, clierr.As(err).Hints, i18n.T(msgid.ShellHintSkipWaitForOnMember))
}

// 外壳镜像里编进的是 erp/a@1.2.0，这次按 brickkit.yaml 的默认版本承载的却是 1.3.0——
// 外壳进程里跑的代码与平台注入的版本对不上。生成前大声失败，给出三条具体出路。
func TestCheckMemberVersionMismatch(t *testing.T) {
	_, err := resolveRaw(t, &testCfg{Components: []testComp{
		comp("erp/shell", "1.0.0", ""),
		{ID: "erp/a", Version: "1.3.0", ServedBy: "erp/shell@1.0.0", Bare: true},
	}}, map[string]*manifest.Manifest{
		"erp/shell@1.0.0": shellManifest("erp/shell", "1.0.0", 8080, "erp/a@1.2.0"),
		"erp/a@1.3.0":     simple("erp/a", "1.3.0", 8081),
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "erp/shell@1.0.0")
	assert.Contains(t, err.Error(), "erp/a@1.2.0")
	assert.Contains(t, err.Error(), "erp/a@1.3.0")
	assert.Contains(t, err.Error(), "components[0].members[0]", "指向部署文件里那个成员条目")

	ce := clierr.As(err)
	require.NotNil(t, ce)
	hints := strings.Join(ce.Hints, "\n")
	require.Len(t, ce.Hints, 3)
	assert.Contains(t, ce.Hints[0], "erp/a@1.3.0", "出路一：升级到编进了 1.3.0 的外壳")
	assert.Contains(t, ce.Hints[1], "erp/a", "出路二：把成员挪到顶层独立跑")
	assert.Contains(t, hints, "{id: erp/a, version: 1.2.0, requiredBy: [erp/shell]}", "出路三：brickkit.yaml 要加的那一行")
	assert.Contains(t, hints, "- id: erp/a@1.2.0", "出路三：外壳下面要写的那一行")
}

// 外壳编进 1.2.0：brickkit.yaml 保留 1.2.0（requiredBy 外壳），外壳下写 erp/a@1.2.0；默认版本 1.3.0 独立跑。
func TestCheckMemberPinnedToDeclaredVersion(t *testing.T) {
	groups, err := resolveRaw(t, &testCfg{Components: []testComp{
		comp("erp/shell", "1.0.0", ""),
		comp("erp/a", "1.3.0", ""),
		{ID: "erp/a", Version: "1.2.0", ServedBy: "erp/shell@1.0.0", RequiredBy: []string{"erp/shell"}},
	}}, map[string]*manifest.Manifest{
		"erp/shell@1.0.0": shellManifest("erp/shell", "1.0.0", 8080, "erp/a@1.2.0"),
		"erp/a@1.3.0":     simple("erp/a", "1.3.0", 8081),
		"erp/a@1.2.0":     simple("erp/a", "1.2.0", 8081),
	})
	require.NoError(t, err)
	require.Len(t, groups, 1)
	require.Len(t, groups[0].Members, 1)
	assert.Equal(t, resolver.Ref{ID: "erp/a", Version: "1.2.0"}, groups[0].Members[0].Ref)
}

// 这次没被外壳承载的成员不核对版本：外壳这次不加载它，镜像里编进的是哪个版本与它无关。
func TestCheckSkipsMembersNotHostedThisRun(t *testing.T) {
	manifests := func() map[string]*manifest.Manifest {
		return map[string]*manifest.Manifest{
			"erp/shell@1.0.0": shellManifest("erp/shell", "1.0.0", 8080, "erp/a@1.2.0"),
			"erp/a@1.3.0":     simple("erp/a", "1.3.0", 8081),
		}
	}
	cases := map[string][]testComp{
		// debug 与 local 走同一个判据（IsBareProcess）；debug 只能写在本地文件里，这里用 local
		"成员 mode: local（在宿主机上自己跑）": {
			comp("erp/shell", "1.0.0", ""),
			{ID: "erp/a", Version: "1.3.0", ServedBy: "erp/shell@1.0.0", Bare: true, Mode: deployfile.ModeLocal},
		},
		"外壳 mode: disable（成员回落成独立组件）": {
			{ID: "erp/shell", Version: "1.0.0", Mode: deployfile.ModeDisable},
			{ID: "erp/a", Version: "1.3.0", ServedBy: "erp/shell@1.0.0", Bare: true, Mode: deployfile.ModeEnabled},
		},
	}
	for name, components := range cases {
		_, err := resolveRaw(t, &testCfg{Components: components}, manifests())
		assert.NoError(t, err, name)
	}

	// --ignore-shells：每个组件都独立部署
	cfg := &testCfg{Components: []testComp{
		comp("erp/shell", "1.0.0", ""),
		{ID: "erp/a", Version: "1.3.0", ServedBy: "erp/shell@1.0.0", Bare: true},
	}}
	p := projectFrom(t, cfg)
	p.IgnoreShells()
	graph, err := resolver.New(stubProvider(manifests())).Resolve(context.Background(),
		resolver.Ref{ID: "erp/shell", Version: "1.0.0"}, resolver.Ref{ID: "erp/a", Version: "1.3.0"})
	require.NoError(t, err)
	states, err := cascade.Compute(p, graph)
	require.NoError(t, err)
	assert.NoError(t, shell.Check(p, graph, states), "--ignore-shells")
}

// 裸进程外壳（mode: local）照样核对：成员的代码就在外壳进程里跑。
func TestCheckBareShellMemberVersionMismatch(t *testing.T) {
	_, err := resolveRaw(t, &testCfg{Components: []testComp{
		{ID: "erp/shell", Version: "1.0.0", Mode: deployfile.ModeLocal},
		{ID: "erp/a", Version: "1.3.0", ServedBy: "erp/shell@1.0.0", Bare: true},
	}}, map[string]*manifest.Manifest{
		"erp/shell@1.0.0": shellManifest("erp/shell", "1.0.0", 8080, "erp/a@1.2.0"),
		"erp/a@1.3.0":     simple("erp/a", "1.3.0", 8081),
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "erp/a@1.2.0")
}

// brickkit.yaml 已经保留着外壳编进的那个版本，只是部署文件的成员条目没写版本：第三条出路
// 只需要改成员条目，不再叫人往 brickkit.yaml 里加一行已经有的东西。
func TestCheckMismatchHintWhenDeclaredVersionAlreadyKept(t *testing.T) {
	_, err := resolveRaw(t, &testCfg{Components: []testComp{
		comp("erp/shell", "1.0.0", ""),
		{ID: "erp/a", Version: "1.3.0", ServedBy: "erp/shell@1.0.0", Bare: true},
		{ID: "erp/a", Version: "1.2.0", RequiredBy: []string{"erp/shell"}},
	}}, map[string]*manifest.Manifest{
		"erp/shell@1.0.0": shellManifest("erp/shell", "1.0.0", 8080, "erp/a@1.2.0"),
		"erp/a@1.3.0":     simple("erp/a", "1.3.0", 8081),
		"erp/a@1.2.0":     simple("erp/a", "1.2.0", 8081),
	})
	require.Error(t, err)
	ce := clierr.As(err)
	require.NotNil(t, ce)
	require.Len(t, ce.Hints, 3)
	assert.NotContains(t, ce.Hints[2], "requiredBy", "brickkit.yaml 里已经有 erp/a 1.2.0")
	assert.Contains(t, ce.Hints[2], "`- id: erp/a@1.2.0`")
}
