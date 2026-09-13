// 本文件测试 internal/shell 的校验与合并逻辑——这是 servedBy 唯一的一份
// 目标无关逻辑，Docker（internal/compose）与 K8s（internal/k8s）都只消费
// 它的结果，不重新实现任何一条规则（servedBy 设计书 §3）。
package shell_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/brickkit/brickkit/internal/cascade"
	"github.com/brickkit/brickkit/internal/config"
	"github.com/brickkit/brickkit/internal/inject"
	"github.com/brickkit/brickkit/internal/manifest"
	"github.com/brickkit/brickkit/internal/resolver"
	"github.com/brickkit/brickkit/internal/shell"
)

type stubProvider map[string]*manifest.Manifest

func (p stubProvider) Manifest(_ context.Context, id, version string) (*manifest.Manifest, error) {
	m, ok := p[id+"@"+version]
	if !ok {
		return nil, errNotFound{id + "@" + version}
	}
	return m, nil
}

type errNotFound struct{ ref string }

func (e errNotFound) Error() string { return "夹具里没有 " + e.ref }

func simple(id, version string, port int) *manifest.Manifest {
	return &manifest.Manifest{
		APIVersion: manifest.APIVersion, Kind: manifest.Kind,
		Metadata:   manifest.Metadata{ID: id, Name: id, Version: version},
		Deployment: manifest.Deployment{Type: manifest.DeploymentTypeContainer, Image: "x:" + version, Port: port},
		HealthCheck: manifest.HealthCheck{Type: manifest.HealthCheckHTTP, Path: "/healthz"},
	}
}

func dependsOn(m *manifest.Manifest, id, version string) *manifest.Manifest {
	if m.Dependencies == nil {
		m.Dependencies = &manifest.Dependencies{}
	}
	m.Dependencies.Components = append(m.Dependencies.Components, manifest.ComponentDep{ID: id, Version: version})
	return m
}

// resolveFixture 跑完整条链路（解析依赖图 → 级联 → 注入 → shell.Resolve），
// 供每个用例只关心自己要断言的那一小段。
func resolveFixture(t *testing.T, cfg *config.Config, manifests map[string]*manifest.Manifest) ([]shell.Group, error) {
	t.Helper()

	provider := stubProvider(manifests)
	var roots []resolver.Ref
	for _, c := range cfg.Components {
		roots = append(roots, resolver.Ref{ID: c.ID, Version: c.Version})
	}

	graph, err := resolver.New(provider).Resolve(context.Background(), roots...)
	require.NoError(t, err)

	states, err := cascade.Compute(cfg, graph)
	require.NoError(t, err)

	env, err := inject.Build(cfg, graph, states)
	require.NoError(t, err)

	return shell.Resolve(cfg, graph, states, env)
}

func comp(id, version, servedBy string) config.Component {
	return config.Component{ID: id, Version: version, ServedBy: servedBy}
}

// ---- 基本分组 ----

func TestResolveGroupsMemberUnderItsShell(t *testing.T) {
	cfg := &config.Config{Project: "p", Deploy: config.Deploy{Target: config.TargetDocker}, Components: []config.Component{
		comp("infra/shell-go-core", "1.0.0", ""),
		comp("mdm/customer", "1.0.7", "infra/shell-go-core@1.0.0"),
	}}
	groups, err := resolveFixture(t, cfg, map[string]*manifest.Manifest{
		"infra/shell-go-core@1.0.0": simple("infra/shell-go-core", "1.0.0", 9000),
		"mdm/customer@1.0.7":        simple("mdm/customer", "1.0.7", 8080),
	})
	require.NoError(t, err)
	require.Len(t, groups, 1)
	assert.Equal(t, resolver.Ref{ID: "infra/shell-go-core", Version: "1.0.0"}, groups[0].Shell)
	require.Len(t, groups[0].Members, 1)
	assert.Equal(t, resolver.Ref{ID: "mdm/customer", Version: "1.0.7"}, groups[0].Members[0].Ref)
	assert.Equal(t, 8080, groups[0].Members[0].Port)
}

// ---- 存在性 / 运行态 ----

func TestResolveErrorsWhenShellDoesNotExist(t *testing.T) {
	cfg := &config.Config{Project: "p", Deploy: config.Deploy{Target: config.TargetDocker}, Components: []config.Component{
		comp("mdm/customer", "1.0.7", "infra/shell-go-core@1.0.0"),
	}}
	_, err := resolveFixture(t, cfg, map[string]*manifest.Manifest{
		"mdm/customer@1.0.7": simple("mdm/customer", "1.0.7", 8080),
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "不存在")
}

func TestResolveErrorsWhenShellIsDisabled(t *testing.T) {
	disabled := false
	cfg := &config.Config{Project: "p", Deploy: config.Deploy{Target: config.TargetDocker}, Components: []config.Component{
		{ID: "infra/shell-go-core", Version: "1.0.0", Enabled: &disabled},
		comp("mdm/customer", "1.0.7", "infra/shell-go-core@1.0.0"),
	}}
	_, err := resolveFixture(t, cfg, map[string]*manifest.Manifest{
		"infra/shell-go-core@1.0.0": simple("infra/shell-go-core", "1.0.0", 9000),
		"mdm/customer@1.0.7":        simple("mdm/customer", "1.0.7", 8080),
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "没有在运行")
}

// ---- 端口冲突 ----

func TestResolveErrorsOnPortConflictWithinGroup(t *testing.T) {
	cfg := &config.Config{Project: "p", Deploy: config.Deploy{Target: config.TargetDocker}, Components: []config.Component{
		comp("infra/shell-go-core", "1.0.0", ""),
		comp("mdm/customer", "1.0.7", "infra/shell-go-core@1.0.0"),
		comp("erp/sales", "1.0.0", "infra/shell-go-core@1.0.0"),
	}}
	_, err := resolveFixture(t, cfg, map[string]*manifest.Manifest{
		"infra/shell-go-core@1.0.0": simple("infra/shell-go-core", "1.0.0", 9000),
		"mdm/customer@1.0.7":        simple("mdm/customer", "1.0.7", 8080),
		"erp/sales@1.0.0":           simple("erp/sales", "1.0.0", 8080), // 撞了 mdm/customer
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "8080")
}

// ---- 环境变量合并：范围只收 *_ENDPOINT ----

func TestResolveMergesOnlyEndpointVars(t *testing.T) {
	cfg := &config.Config{Project: "p", Deploy: config.Deploy{Target: config.TargetDocker}, Components: []config.Component{
		comp("infra/shell-go-core", "1.0.0", ""),
		comp("mdm/customer", "1.0.7", "infra/shell-go-core@1.0.0"),
		comp("infra/database", "1.0.0", ""),
	}}
	member := dependsOn(simple("mdm/customer", "1.0.7", 8080), "infra/database", "1.0.0")
	member.ConfigSchema = &manifest.ConfigSchema{Properties: map[string]manifest.ConfigProperty{
		"pgSchema": {Type: "string", Default: "mdm_customer"},
	}}

	groups, err := resolveFixture(t, cfg, map[string]*manifest.Manifest{
		"infra/shell-go-core@1.0.0": simple("infra/shell-go-core", "1.0.0", 9000),
		"mdm/customer@1.0.7":        member,
		"infra/database@1.0.0":      simple("infra/database", "1.0.0", 5432),
	})
	require.NoError(t, err)
	require.Len(t, groups, 1)

	env := groups[0].Env
	names := make([]string, 0, len(env))
	for _, v := range env {
		names = append(names, v.Name)
	}
	assert.Contains(t, names, "INFRA_DATABASE_ENDPOINT", "依赖端点必须被合并")
	assert.NotContains(t, names, "PG_SCHEMA", "组件自己的配置绝不能进合并范围")
	assert.NotContains(t, names, "COMPONENT_ID", "身份变量绝不能进合并范围")
	assert.NotContains(t, names, "COMPONENT_VERSION")
}

// ---- 环境变量合并：同名同值放过，同名不同值报错 ----

func TestResolveEndpointCollisionSameValueIsFine(t *testing.T) {
	cfg := &config.Config{Project: "p", Deploy: config.Deploy{Target: config.TargetDocker}, Components: []config.Component{
		comp("infra/shell-go-core", "1.0.0", ""),
		comp("mdm/customer", "1.0.7", "infra/shell-go-core@1.0.0"),
		comp("erp/sales", "1.0.0", "infra/shell-go-core@1.0.0"),
		comp("infra/database", "1.0.0", ""),
	}}
	a := dependsOn(simple("mdm/customer", "1.0.7", 8080), "infra/database", "1.0.0")
	b := dependsOn(simple("erp/sales", "1.0.0", 8081), "infra/database", "1.0.0")

	groups, err := resolveFixture(t, cfg, map[string]*manifest.Manifest{
		"infra/shell-go-core@1.0.0": simple("infra/shell-go-core", "1.0.0", 9000),
		"mdm/customer@1.0.7":        a,
		"erp/sales@1.0.0":           b,
		"infra/database@1.0.0":      simple("infra/database", "1.0.0", 5432),
	})
	require.NoError(t, err, "两个成员依赖同一个组件的同一个版本，端点值相同，不该报冲突")
	require.Len(t, groups, 1)
}

func TestResolveEndpointCollisionDifferentVersionErrors(t *testing.T) {
	cfg := &config.Config{Project: "p", Deploy: config.Deploy{Target: config.TargetDocker}, Components: []config.Component{
		comp("infra/shell-go-core", "1.0.0", ""),
		comp("mdm/customer", "1.0.7", "infra/shell-go-core@1.0.0"),
		comp("erp/sales", "1.0.0", "infra/shell-go-core@1.0.0"),
		comp("infra/database", "1.0.0", ""),
		comp("infra/database", "2.0.0", ""),
	}}
	a := dependsOn(simple("mdm/customer", "1.0.7", 8080), "infra/database", "1.0.0")
	b := dependsOn(simple("erp/sales", "1.0.0", 8081), "infra/database", "2.0.0")

	_, err := resolveFixture(t, cfg, map[string]*manifest.Manifest{
		"infra/shell-go-core@1.0.0": simple("infra/shell-go-core", "1.0.0", 9000),
		"mdm/customer@1.0.7":        a,
		"erp/sales@1.0.0":           b,
		"infra/database@1.0.0":      simple("infra/database", "1.0.0", 5432),
		"infra/database@2.0.0":      simple("infra/database", "2.0.0", 5432),
	})
	require.Error(t, err, "同一个变量名不可能同时指向两个不同版本的地址")
	assert.Contains(t, err.Error(), "INFRA_DATABASE_ENDPOINT")
}

// ---- labels 合并：同名同值放过，同名不同值报错 ----

func TestResolveLabelCollisionSameValueIsFine(t *testing.T) {
	cfg := &config.Config{Project: "p", Deploy: config.Deploy{Target: config.TargetDocker}, Components: []config.Component{
		comp("infra/shell-go-core", "1.0.0", ""),
		comp("mdm/customer", "1.0.7", "infra/shell-go-core@1.0.0"),
		comp("erp/sales", "1.0.0", "infra/shell-go-core@1.0.0"),
	}}
	a := simple("mdm/customer", "1.0.7", 8080)
	a.Deployment.Labels = map[string]string{"team.owner": "erp"}
	b := simple("erp/sales", "1.0.0", 8081)
	b.Deployment.Labels = map[string]string{"team.owner": "erp"}

	groups, err := resolveFixture(t, cfg, map[string]*manifest.Manifest{
		"infra/shell-go-core@1.0.0": simple("infra/shell-go-core", "1.0.0", 9000),
		"mdm/customer@1.0.7":        a,
		"erp/sales@1.0.0":           b,
	})
	require.NoError(t, err, "两个成员对同一个标签键给出相同的值，不该报冲突")
	require.Len(t, groups, 1)
	assert.Equal(t, "erp", groups[0].Labels["team.owner"])
}

func TestResolveLabelCollisionDifferentValueErrors(t *testing.T) {
	cfg := &config.Config{Project: "p", Deploy: config.Deploy{Target: config.TargetDocker}, Components: []config.Component{
		comp("infra/shell-go-core", "1.0.0", ""),
		comp("mdm/customer", "1.0.7", "infra/shell-go-core@1.0.0"),
		comp("erp/sales", "1.0.0", "infra/shell-go-core@1.0.0"),
	}}
	a := simple("mdm/customer", "1.0.7", 8080)
	a.Deployment.Labels = map[string]string{"team.owner": "erp"}
	b := simple("erp/sales", "1.0.0", 8081)
	b.Deployment.Labels = map[string]string{"team.owner": "sales"} // 撞了 mdm/customer 的值

	_, err := resolveFixture(t, cfg, map[string]*manifest.Manifest{
		"infra/shell-go-core@1.0.0": simple("infra/shell-go-core", "1.0.0", 9000),
		"mdm/customer@1.0.7":        a,
		"erp/sales@1.0.0":           b,
	})
	require.Error(t, err, "同一个标签键不可能同时代表两个不同的值")
	assert.Contains(t, err.Error(), "team.owner")
}

// ---- BRICKKIT_SERVED_MEMBERS ----

func TestServedMembersFormatting(t *testing.T) {
	g := shell.Group{Members: []shell.Member{
		{Ref: resolver.Ref{ID: "erp/sales", Version: "1.0.0"}},
		{Ref: resolver.Ref{ID: "mdm/customer", Version: "1.0.7"}},
	}}
	assert.Equal(t, "erp-sales-1-0-0,mdm-customer-1-0-7", g.ServedMembers(),
		"按字典序排列，逗号分隔")
}

func TestServedMembersEmptyWhenNoMembers(t *testing.T) {
	assert.Equal(t, "", shell.Group{}.ServedMembers(),
		"零个成员时是空字符串——这个空字符串本身就是信号，不是变量缺失")
}

func TestApplyUpsertsEndpointsAndServedMembers(t *testing.T) {
	shellEnv := []inject.Var{
		{Name: "COMPONENT_ID", Value: "infra/shell-go-core", Source: inject.SourcePlatform},
	}
	g := shell.Group{
		Members: []shell.Member{{Ref: resolver.Ref{ID: "mdm/customer", Version: "1.0.7"}}},
		Env:     []inject.Var{{Name: "INFRA_DATABASE_ENDPOINT", Value: "http://infra-database-1-0-0:5432", Source: inject.SourceEndpoint}},
	}

	out := shell.Apply(shellEnv, g)

	byName := map[string]string{}
	for _, v := range out {
		byName[v.Name] = v.Value
	}
	assert.Equal(t, "infra/shell-go-core", byName["COMPONENT_ID"], "外壳自己的变量不受影响")
	assert.Equal(t, "http://infra-database-1-0-0:5432", byName["INFRA_DATABASE_ENDPOINT"])
	assert.Equal(t, "mdm-customer-1-0-7", byName[shell.EnvVarServedMembers])
}

// ---- 同一个外壳收编同一个组件的两个不同版本（版本迁移期间的真实用法：
// 一部分调用方还依赖旧版本，一部分已经切到新版本，两个版本同时活在
// 同一个外壳里）----

func TestResolveGroupsTwoVersionsOfSameComponentUnderOneShell(t *testing.T) {
	cfg := &config.Config{Project: "p", Deploy: config.Deploy{Target: config.TargetDocker}, Components: []config.Component{
		comp("infra/shell-go-core", "1.0.0", ""),
		comp("mdm/customer", "1.0.7", "infra/shell-go-core@1.0.0"),
		comp("mdm/customer", "2.0.0", "infra/shell-go-core@1.0.0"),
	}}
	groups, err := resolveFixture(t, cfg, map[string]*manifest.Manifest{
		"infra/shell-go-core@1.0.0": simple("infra/shell-go-core", "1.0.0", 9000),
		"mdm/customer@1.0.7":        simple("mdm/customer", "1.0.7", 8080),
		"mdm/customer@2.0.0":        simple("mdm/customer", "2.0.0", 8081),
	})
	require.NoError(t, err, "同一个逻辑组件的两个版本，只要端口不同，同一个外壳完全装得下")
	require.Len(t, groups, 1)
	require.Len(t, groups[0].Members, 2)

	byVersion := map[string]shell.Member{}
	for _, m := range groups[0].Members {
		byVersion[m.Ref.Version] = m
	}
	assert.Equal(t, 8080, byVersion["1.0.7"].Port)
	assert.Equal(t, 8081, byVersion["2.0.0"].Port)
}

func TestResolveErrorsWhenTwoVersionsOfSameComponentSharePort(t *testing.T) {
	cfg := &config.Config{Project: "p", Deploy: config.Deploy{Target: config.TargetDocker}, Components: []config.Component{
		comp("infra/shell-go-core", "1.0.0", ""),
		comp("mdm/customer", "1.0.7", "infra/shell-go-core@1.0.0"),
		comp("mdm/customer", "2.0.0", "infra/shell-go-core@1.0.0"),
	}}
	_, err := resolveFixture(t, cfg, map[string]*manifest.Manifest{
		"infra/shell-go-core@1.0.0": simple("infra/shell-go-core", "1.0.0", 9000),
		"mdm/customer@1.0.7":        simple("mdm/customer", "1.0.7", 8080),
		"mdm/customer@2.0.0":        simple("mdm/customer", "2.0.0", 8080), // 忘了改端口
	})
	require.Error(t, err, "两份代码要在同一个进程里各自监听，端口不能一样，哪怕是同一个逻辑组件的两个版本")
	assert.Contains(t, err.Error(), "8080")
}

// ---- ParseRef ----

func TestParseRef(t *testing.T) {
	ref, ok := shell.ParseRef("infra/shell-go-core@1.0.0")
	require.True(t, ok)
	assert.Equal(t, resolver.Ref{ID: "infra/shell-go-core", Version: "1.0.0"}, ref)

	_, ok = shell.ParseRef("infra/shell-go-core")
	assert.False(t, ok, "没有 @ 就解析不出版本")
}
