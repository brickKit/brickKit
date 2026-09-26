// 本文件测试 internal/shell 的校验与合并逻辑——这是 servedBy 唯一的一份
// 目标无关逻辑，Docker（internal/compose）与 K8s（internal/k8s）都只消费
// 它的结果，不重新实现任何一条规则（servedBy 设计书 §3）。
package shell_test

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"

	"github.com/brickkit/brickkit/internal/cascade"
	"github.com/brickkit/brickkit/internal/configdir"
	"github.com/brickkit/brickkit/internal/deployfile"
	"github.com/brickkit/brickkit/internal/inject"
	"github.com/brickkit/brickkit/internal/manifest"
	"github.com/brickkit/brickkit/internal/project"
	"github.com/brickkit/brickkit/internal/project/projecttest"
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
		Metadata:    manifest.Metadata{ID: id, Name: id, Version: version},
		Deployment:  manifest.Deployment{Type: manifest.DeploymentTypeContainer, Image: "x:" + version, Port: port},
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

// testComp / testCfg 是测试里描述项目的简写：ServedBy 写成员视角的 "外壳@版本"，
// projectFrom 把它翻译成真实的三层文件（外壳标 kind: shell，外壳条目写 members）。
type testComp struct {
	ID, Version, ServedBy, Mode string
	Config                      map[string]any
}

type testCfg struct{ Components []testComp }

// projectFrom 把 testCfg 写成三层文件并装载。
func projectFrom(t *testing.T, cfg *testCfg) *project.Project {
	t.Helper()
	shells := map[string][]string{}
	for _, c := range cfg.Components {
		if c.ServedBy != "" {
			id := strings.SplitN(c.ServedBy, "@", 2)[0]
			shells[id] = append(shells[id], c.ID)
		}
	}
	var decl, deploy strings.Builder
	decl.WriteString("project: p\ncomponents:\n")
	deploy.WriteString("target: docker\ncomponents:\n")
	files := projecttest.Files{}
	for _, c := range cfg.Components {
		kind := ""
		if _, isShell := shells[c.ID]; isShell {
			kind = ", kind: shell"
		}
		fmt.Fprintf(&decl, "  - {id: %s, version: %s%s}\n", c.ID, c.Version, kind)
		fmt.Fprintf(&deploy, "  - id: %s@%s\n", c.ID, c.Version)
		if c.Mode != "" {
			fmt.Fprintf(&deploy, "    mode: %s\n", c.Mode)
		}
		if members := shells[c.ID]; len(members) > 0 {
			fmt.Fprintf(&deploy, "    members: [%s]\n", strings.Join(members, ", "))
		}
		if len(c.Config) > 0 {
			data, err := yaml.Marshal(c.Config)
			require.NoError(t, err)
			files["config/"+configdir.FileName(c.ID, c.Version)] = string(data)
		}
	}
	files["brickkit.yaml"] = decl.String()
	files["deploy.yaml"] = deploy.String()
	root := t.TempDir()
	projecttest.Write(t, root, files)
	p, err := project.Load(root, project.LoadOptions{})
	require.NoError(t, err)
	return p
}

// resolveFixture 跑完整条链路（解析依赖图 → 级联 → 注入 → shell.Resolve），
// 供每个用例只关心自己要断言的那一小段。
func resolveFixture(t *testing.T, cfg *testCfg, manifests map[string]*manifest.Manifest) ([]shell.Group, error) {
	t.Helper()

	p := projectFrom(t, cfg)
	provider := stubProvider(manifests)
	var roots []resolver.Ref
	for _, c := range cfg.Components {
		roots = append(roots, resolver.Ref{ID: c.ID, Version: c.Version})
	}

	graph, err := resolver.New(provider).Resolve(context.Background(), roots...)
	require.NoError(t, err)

	states, err := cascade.Compute(p, graph)
	require.NoError(t, err)

	env, err := inject.Build(p, graph, states)
	require.NoError(t, err)

	return shell.Resolve(p, graph, states, env)
}

func comp(id, version, servedBy string) testComp {
	return testComp{ID: id, Version: version, ServedBy: servedBy}
}

// ---- 基本分组 ----

func TestResolveGroupsMemberUnderItsShell(t *testing.T) {
	cfg := &testCfg{Components: []testComp{
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

func TestResolveSkipsMemberWhenShellIsDisabled(t *testing.T) {
	cfg := &testCfg{Components: []testComp{
		{ID: "infra/shell-go-core", Version: "1.0.0", Mode: deployfile.ModeDisable},
		comp("mdm/customer", "1.0.7", "infra/shell-go-core@1.0.0"),
	}}
	groups, err := resolveFixture(t, cfg, map[string]*manifest.Manifest{
		"infra/shell-go-core@1.0.0": simple("infra/shell-go-core", "1.0.0", 9000),
		"mdm/customer@1.0.7":        simple("mdm/customer", "1.0.7", 8080),
	})
	require.NoError(t, err)
	assert.Empty(t, groups, "the shell isn't running, so no group should form for it at all")
}

// ---- 端口冲突 ----

func TestResolveErrorsOnPortConflictWithinGroup(t *testing.T) {
	cfg := &testCfg{Components: []testComp{
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
	cfg := &testCfg{Components: []testComp{
		comp("infra/shell-go-core", "1.0.0", ""),
		comp("mdm/customer", "1.0.7", "infra/shell-go-core@1.0.0"),
		comp("infra/database", "1.0.0", ""),
	}}
	member := dependsOn(simple("mdm/customer", "1.0.7", 8080), "infra/database", "1.0.0")
	member.ConfigSchema = &manifest.ConfigSchema{Properties: map[string]manifest.ConfigProperty{
		"PG_SCHEMA": {Type: "string", Default: "mdm_customer"},
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

// ---- 环境变量合并：成员自己的 config 值改走带组件 ID 前缀的独立变量
// （brickKit 反馈：servedBy 的密钥类 config 值会被 docker-compose 撑坏
// JSON——密钥类 config 值必须继续走 "${VAR} 占位符 + docker compose
// 自己展开" 这条已证明安全的老路，不能被塞进 BRICKKIT_SERVED_MEMBERS_CONFIG
// 的 JSON 字符串内部，那样会被 docker compose 的全文本替换撑坏结构）----

func TestResolveMergesMemberConfigAsNamespacedVars(t *testing.T) {
	cfg := &testCfg{Components: []testComp{
		comp("infra/shell-go-core", "1.0.0", ""),
		{ID: "erp/sales", Version: "1.0.0", ServedBy: "infra/shell-go-core@1.0.0",
			Config: map[string]any{"PG_SCHEMA": "sales"}},
	}}
	member := simple("erp/sales", "1.0.0", 8080)
	member.ConfigSchema = &manifest.ConfigSchema{Properties: map[string]manifest.ConfigProperty{
		"PG_SCHEMA": {Type: "string", Default: "public"},
	}}

	groups, err := resolveFixture(t, cfg, map[string]*manifest.Manifest{
		"infra/shell-go-core@1.0.0": simple("infra/shell-go-core", "1.0.0", 9000),
		"erp/sales@1.0.0":           member,
	})
	require.NoError(t, err)
	require.Len(t, groups, 1)

	byName := map[string]string{}
	for _, v := range groups[0].Env {
		byName[v.Name] = v.Value.String()
	}
	assert.Equal(t, "sales", byName["ERP_SALES_PG_SCHEMA"],
		"成员自己的 config 值要各自生成一条带组件 ID 前缀的独立变量，进外壳共享环境——"+
			"跟 *_ENDPOINT 用同一个前缀算法（manifest.EnvPrefix）")
}

// ---- 环境变量合并：同名同值放过，同名不同值报错 ----

func TestResolveEndpointCollisionSameValueIsFine(t *testing.T) {
	cfg := &testCfg{Components: []testComp{
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
	cfg := &testCfg{Components: []testComp{
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

// ---- labels：成员自己的不参与合并，只有外壳自己的算数 ----
//
// 从前这里测的是"同名同值放过，同名不同值报错"——跟 env 变量同一套规则。
// 但 prometheus.io/port 这类值就该因组件而异的标签，在真实多组件收编场景
// 里几乎必然"同名不同值"，那套规则套在这种键上是必然假阳性
// （brickKit 反馈：servedBy 的 labels 合并漏了排除规则）。现在的规则是
// "成员的 labels 一律不参与合并"，下面两个测试改成验证这一点：
// 值相同不再意味着会被合并进 Group，值不同也不再报错。

func TestResolveMemberLabelsAreNotMerged(t *testing.T) {
	cfg := &testCfg{Components: []testComp{
		comp("infra/shell-go-core", "1.0.0", ""),
		comp("mdm/customer", "1.0.7", "infra/shell-go-core@1.0.0"),
		comp("erp/sales", "1.0.0", "infra/shell-go-core@1.0.0"),
	}}
	a := simple("mdm/customer", "1.0.7", 8080)
	a.Deployment.Labels = map[string]string{"team.owner": "erp"}
	b := simple("erp/sales", "1.0.0", 8081)
	b.Deployment.Labels = map[string]string{"team.owner": "erp"} // 即使两边给的值相同

	groups, err := resolveFixture(t, cfg, map[string]*manifest.Manifest{
		"infra/shell-go-core@1.0.0": simple("infra/shell-go-core", "1.0.0", 9000),
		"mdm/customer@1.0.7":        a,
		"erp/sales@1.0.0":           b,
	})
	require.NoError(t, err)
	require.Len(t, groups, 1)
	// Group 已经没有 Labels 字段——这不是一句能在运行时断言的话，而是编译期
	// 就成立的事实：成员的 labels 无论值是否相同，从 shell.Resolve 的返回
	// 结构上就已经无法再被外壳读到。这里只确认合并本身仍然成功、不受影响。
}

func TestResolveMemberLabelsDifferingDoesNotError(t *testing.T) {
	cfg := &testCfg{Components: []testComp{
		comp("infra/shell-go-core", "1.0.0", ""),
		comp("mdm/customer", "1.0.7", "infra/shell-go-core@1.0.0"),
		comp("erp/sales", "1.0.0", "infra/shell-go-core@1.0.0"),
	}}
	a := simple("mdm/customer", "1.0.7", 8080)
	a.Deployment.Labels = map[string]string{"prometheus.io/port": "8080"}
	b := simple("erp/sales", "1.0.0", 8081)
	b.Deployment.Labels = map[string]string{"prometheus.io/port": "8081"} // 语义上就该因组件而异

	_, err := resolveFixture(t, cfg, map[string]*manifest.Manifest{
		"infra/shell-go-core@1.0.0": simple("infra/shell-go-core", "1.0.0", 9000),
		"mdm/customer@1.0.7":        a,
		"erp/sales@1.0.0":           b,
	})
	require.NoError(t, err, "成员的 labels 不参与合并，不同值不该报冲突")
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
		{Name: "COMPONENT_ID", Value: inject.Literal("infra/shell-go-core"), Source: inject.SourcePlatform},
	}
	g := shell.Group{
		Members: []shell.Member{{Ref: resolver.Ref{ID: "mdm/customer", Version: "1.0.7"}}},
		Env:     []inject.Var{{Name: "INFRA_DATABASE_ENDPOINT", Value: inject.Literal("http://infra-database-1-0-0:5432"), Source: inject.SourceEndpoint}},
	}

	out := shell.Apply(shellEnv, g)

	byName := map[string]string{}
	for _, v := range out {
		byName[v.Name] = v.Value.String()
	}
	assert.Equal(t, "infra/shell-go-core", byName["COMPONENT_ID"], "外壳自己的变量不受影响")
	assert.Equal(t, "http://infra-database-1-0-0:5432", byName["INFRA_DATABASE_ENDPOINT"])
	assert.Equal(t, "mdm-customer-1-0-7", byName[shell.EnvVarServedMembers])
}

// ---- BRICKKIT_SERVED_MEMBERS_CONFIG（brickKit 反馈：两个降低 servedBy
// 运维摩擦的架构提案，提案一）----

// Resolve 要把每个成员的 extraPorts 与合并后的自身 config（原始 key）
// 一并存进 Member，供 ServedMembersConfig 使用——这份数据在 inject.Build
// 阶段已经算好，Resolve 只是把它顺路捎带上，不重新计算。
func TestResolvePopulatesMemberExtraPortsAndConfig(t *testing.T) {
	cfg := &testCfg{Components: []testComp{
		comp("infra/shell-go-core", "1.0.0", ""),
		{ID: "erp/sales", Version: "1.0.0", ServedBy: "infra/shell-go-core@1.0.0",
			Config: map[string]any{"PG_SCHEMA": "sales"}},
	}}
	member := simple("erp/sales", "1.0.0", 8080)
	member.Deployment.ExtraPorts = []manifest.ExtraPort{{Name: "grpc", Port: 9090}}
	member.ConfigSchema = &manifest.ConfigSchema{Properties: map[string]manifest.ConfigProperty{
		"PG_SCHEMA":       {Type: "string", Default: "public"},
		"defaultPageSize": {Type: "integer", Default: 20},
	}}

	groups, err := resolveFixture(t, cfg, map[string]*manifest.Manifest{
		"infra/shell-go-core@1.0.0": simple("infra/shell-go-core", "1.0.0", 9000),
		"erp/sales@1.0.0":           member,
	})
	require.NoError(t, err)
	require.Len(t, groups, 1)
	require.Len(t, groups[0].Members, 1)

	m := groups[0].Members[0]
	assert.Equal(t, []manifest.ExtraPort{{Name: "grpc", Port: 9090}}, m.ExtraPorts)
	assert.Equal(t, map[string]string{"PG_SCHEMA": "sales", "defaultPageSize": "20"}, m.Config,
		"覆盖值（pgSchema）与默认值（defaultPageSize）都要进来，原始 key 不是转换后的环境变量名")
}

func TestServedMembersConfigFormatting(t *testing.T) {
	g := shell.Group{Members: []shell.Member{
		{
			Ref: resolver.Ref{ID: "erp/sales", Version: "1.0.0"}, Port: 8080,
			ExtraPorts: []manifest.ExtraPort{{Name: "grpc", Port: 9090}},
			Config:     map[string]string{"PG_SCHEMA": "sales"},
		},
		{
			Ref: resolver.Ref{ID: "mdm/customer", Version: "1.0.7"}, Port: 8081,
			Config: map[string]string{"PG_SCHEMA": "customer"},
		},
	}}

	var entries []map[string]any
	require.NoError(t, json.Unmarshal([]byte(g.ServedMembersConfig()), &entries))
	require.Len(t, entries, 2, "按 componentId 字典序排列")

	assert.Equal(t, "erp/sales", entries[0]["componentId"])
	assert.Equal(t, "1.0.0", entries[0]["version"])
	assert.Equal(t, float64(8080), entries[0]["httpPort"])
	assert.Equal(t, []any{map[string]any{"name": "grpc", "port": float64(9090)}}, entries[0]["extraPorts"])
	assert.Equal(t, map[string]any{"PG_SCHEMA": "ERP_SALES_PG_SCHEMA"}, entries[0]["configEnvVars"],
		"携带的是算出来的变量名，不是原始值——外壳去读那条独立变量，不从 JSON 里抠值")

	assert.Equal(t, "mdm/customer", entries[1]["componentId"])
	assert.Equal(t, map[string]any{"PG_SCHEMA": "MDM_CUSTOMER_PG_SCHEMA"}, entries[1]["configEnvVars"])
}

// 直接的回归测试：就算某个成员的 config 值本身还是未展开的 ${VAR} 占位符
// （brickkit up 那个进程查不到这个环境变量、只在 .env 里有时，就会是这个
// 样子——见 config.ExpandEnv），BRICKKIT_SERVED_MEMBERS_CONFIG 的 JSON 里
// 也不该出现这段文本——这正是撑坏 JSON 那个 bug 的根源。
func TestServedMembersConfigNeverEmbedsRawPlaceholderText(t *testing.T) {
	g := shell.Group{Members: []shell.Member{
		{
			Ref: resolver.Ref{ID: "infra/iam-casdoor", Version: "1.0.0"}, Port: 8080,
			Config: map[string]string{"APP_TOKEN_SIGNING_KEY_PEM": "${APP_TOKEN_SIGNING_KEY_PEM}"},
		},
	}}

	out := g.ServedMembersConfig()

	assert.NotContains(t, out, "${",
		"值本身是不是 ${VAR} 占位符不该影响 JSON 是否合法——JSON 里现在只装变量名")

	var entries []map[string]any
	require.NoError(t, json.Unmarshal([]byte(out), &entries))
	configEnvVars, ok := entries[0]["configEnvVars"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "INFRA_IAM_CASDOOR_APP_TOKEN_SIGNING_KEY_PEM", configEnvVars["APP_TOKEN_SIGNING_KEY_PEM"])
}

func TestServedMembersConfigEmptyWhenNoMembers(t *testing.T) {
	assert.Equal(t, "[]", shell.Group{}.ServedMembersConfig(),
		"零个成员时是空数组，不是 null——外壳读到它必须知道'确实是零个'，跟 ServedMembers 空字符串同一个精神")
}

func TestApplyUpsertsServedMembersConfig(t *testing.T) {
	g := shell.Group{Members: []shell.Member{
		{Ref: resolver.Ref{ID: "mdm/customer", Version: "1.0.7"}, Port: 8080, Config: map[string]string{"PG_SCHEMA": "customer"}},
	}}

	out := shell.Apply(nil, g)

	byName := map[string]string{}
	for _, v := range out {
		byName[v.Name] = v.Value.String()
	}
	var entries []map[string]any
	require.NoError(t, json.Unmarshal([]byte(byName[shell.EnvVarServedMembersConfig]), &entries))
	require.Len(t, entries, 1)
	assert.Equal(t, "mdm/customer", entries[0]["componentId"])
}

// ---- 同一个外壳收编同一个组件的两个不同版本（版本迁移期间的真实用法：
// 一部分调用方还依赖旧版本，一部分已经切到新版本，两个版本同时活在
// 同一个外壳里）----

// ---- ParseRef ----


// 附录 A18：外壳成员可以设 mode: local / debug——这一次它以裸进程在宿主机上跑，
// 不并进外壳（完整语义 P3 设计；这里钉住"至少能设置、且不被当成成员"）。
func TestResolveBareProcessMemberStaysOutOfShell(t *testing.T) {
	member := testComp{ID: "erp/sales", Version: "1.0.0", ServedBy: "infra/shell-go-core@1.0.0", Mode: deployfile.ModeLocal}
	groups, err := resolveFixture(t, &testCfg{Components: []testComp{
		comp("infra/shell-go-core", "1.0.0", ""), member,
	}}, map[string]*manifest.Manifest{
		"infra/shell-go-core@1.0.0": simple("infra/shell-go-core", "1.0.0", 9000),
		"erp/sales@1.0.0":           simple("erp/sales", "1.0.0", 8080),
	})
	require.NoError(t, err)
	for _, g := range groups {
		assert.Empty(t, g.Members, "裸进程成员这次不在外壳里")
	}
}
