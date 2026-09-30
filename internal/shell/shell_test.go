// 本文件测试 internal/shell 的校验与合并逻辑——这是 servedBy 唯一的一份
// 目标无关逻辑，Docker（internal/compose）与 K8s（internal/k8s）都只消费
// 它的结果，不重新实现任何一条规则。
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
	SkipWaitFor                 []string
	// RequiredBy 写进 brickkit.yaml；同一个 ID 没写它的那一行是默认版本。
	RequiredBy []string
	// Bare 让部署文件里的条目不写版本（= brickkit.yaml 的默认版本）。
	Bare bool
}

type testCfg struct {
	Components []testComp
	// Env 是 ${VAR} 的取值来源；Files 是额外写进项目根的文件（file:// 指向的）。
	Env   map[string]string
	Files projecttest.Files
}

// projectFrom 把 testCfg 写成三层文件并装载。
func projectFrom(t *testing.T, cfg *testCfg) *project.Project {
	t.Helper()
	shells := map[string][]testComp{}
	for _, c := range cfg.Components {
		if c.ServedBy != "" {
			id := strings.SplitN(c.ServedBy, "@", 2)[0]
			shells[id] = append(shells[id], c)
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
		requiredBy := ""
		if len(c.RequiredBy) > 0 {
			requiredBy = ", requiredBy: [" + strings.Join(c.RequiredBy, ", ") + "]"
		}
		fmt.Fprintf(&decl, "  - {id: %s, version: %s%s%s}\n", c.ID, c.Version, kind, requiredBy)
		if len(c.Config) > 0 {
			data, err := yaml.Marshal(c.Config)
			require.NoError(t, err)
			files["config/"+configdir.FileName(c.ID, c.Version)] = string(data)
		}
		if c.ServedBy != "" {
			continue // 成员条目嵌在外壳条目下面
		}
		fmt.Fprintf(&deploy, "  - id: %s\n", entryID(c))
		if c.Mode != "" {
			fmt.Fprintf(&deploy, "    mode: %s\n", c.Mode)
		}
		if len(c.SkipWaitFor) > 0 {
			fmt.Fprintf(&deploy, "    skipWaitFor: [%s]\n", strings.Join(c.SkipWaitFor, ", "))
		}
		if members := shells[c.ID]; len(members) > 0 {
			deploy.WriteString("    members:\n")
			for _, m := range members {
				fmt.Fprintf(&deploy, "      - id: %s\n", entryID(m))
				if m.Mode != "" {
					fmt.Fprintf(&deploy, "        mode: %s\n", m.Mode)
				}
				if len(m.SkipWaitFor) > 0 {
					fmt.Fprintf(&deploy, "        skipWaitFor: [%s]\n", strings.Join(m.SkipWaitFor, ", "))
				}
			}
		}
	}
	for name, content := range cfg.Files {
		files[name] = content
	}
	files["brickkit.yaml"] = decl.String()
	files["deploy.yaml"] = deploy.String()
	root := t.TempDir()
	projecttest.Write(t, root, files)
	p, err := project.Load(root, project.LoadOptions{})
	require.NoError(t, err)
	return p
}

// entryID 是部署文件条目的 id：Bare 时不写版本。
func entryID(c testComp) string {
	if c.Bare {
		return c.ID
	}
	return c.ID + "@" + c.Version
}

// resolveFixture 跑完整条链路（解析依赖图 → 级联 → 注入 → shell.Resolve），
// 供每个用例只关心自己要断言的那一小段。
//
// 外壳的 Manifest 按 cfg 里声明的成员补上 shell.members（成员及其版本）——三处声明
// 一致是 shell.Check 的前提，大多数用例关心的不是它；专门测不一致的用例用 resolveRaw。
func resolveFixture(t *testing.T, cfg *testCfg, manifests map[string]*manifest.Manifest) ([]shell.Group, error) {
	t.Helper()
	for _, c := range cfg.Components {
		if c.ServedBy == "" {
			continue
		}
		if m := manifests[c.ServedBy]; m != nil && m.Shell == nil {
			m.Shell = &manifest.Shell{}
		}
		if m := manifests[c.ServedBy]; m != nil {
			if _, ok := m.HostedVersion(c.ID); !ok {
				m.Shell.Members = append(m.Shell.Members, c.ID+"@"+c.Version)
			}
		}
	}
	return resolveRaw(t, cfg, manifests)
}

// resolveRaw 与 resolveFixture 相同，但不补外壳的能力声明。
func resolveRaw(t *testing.T, cfg *testCfg, manifests map[string]*manifest.Manifest) ([]shell.Group, error) {
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

	return shell.Resolve(p, graph, states, env, func(name string) (string, bool) {
		value, ok := cfg.Env[name]
		return value, ok
	})
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

// ---- 环境变量合并：成员自己的 config 值改走带组件 ID 前缀的独立变量
// （brickKit 反馈：servedBy 的密钥类 config 值会被 docker-compose 撑坏
// JSON——密钥类 config 值必须继续走 "${VAR} 占位符 + docker compose
// 自己展开" 这条已证明安全的老路，不能被塞进 BRICKKIT_SERVED_MEMBERS_CONFIG
// 的 JSON 字符串内部，那样会被 docker compose 的全文本替换撑坏结构）----

// ---- 环境变量合并：同名同值放过，同名不同值报错 ----

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

// ---- BRICKKIT_SERVED_MEMBERS_CONFIG ----

func TestServedMembersConfigEmptyWhenNoMembers(t *testing.T) {
	assert.Equal(t, "[]", shell.Group{}.ServedMembersConfig(),
		"零个成员时是空数组，不是 null——外壳读到它必须知道'确实是零个'，跟 ServedMembers 空字符串同一个精神")
}

// ---- 同一个外壳收编同一个组件的两个不同版本（版本迁移期间的真实用法：
// 一部分调用方还依赖旧版本，一部分已经切到新版本，两个版本同时活在
// 同一个外壳里）----

// ---- ParseRef ----

// 外壳成员可以设 mode: local / debug——这一次它以裸进程在宿主机上跑，
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

// ---- BRICKKIT_SERVED_MEMBERS_CONFIG：CLI 提前求值的 JSON ----

const memberPEM = "-----BEGIN KEY-----\nab$c\"d\\e\n-----END KEY-----\n"

// 每个成员在 JSON 里拿到的，就是它独立运行时会拿到的那份环境（配置 + 依赖地址），
// 值已经求好：file:// 读了文件、${VAR} 展开了、依赖地址是真实地址。
// 成员的配置不再以带前缀的变量摊进外壳的环境。
func TestServedMembersConfigCarriesEvaluatedValues(t *testing.T) {
	cfg := &testCfg{
		Components: []testComp{
			comp("erp/shell", "1.0.0", ""),
			{ID: "erp/a", Version: "1.0.0", ServedBy: "erp/shell@1.0.0", Config: map[string]any{
				"CERT": "file://secrets/a.pem", "TOKEN": "${A_TOKEN}", "MODE": "strict",
			}},
			comp("erp/db", "1.0.0", ""),
		},
		Env:   map[string]string{"A_TOKEN": "t$k"},
		Files: projecttest.Files{"secrets/a.pem": memberPEM},
	}
	a := dependsOn(simple("erp/a", "1.0.0", 8081), "erp/db", "1.0.0")
	a.Deployment.ExtraPorts = []manifest.ExtraPort{{Name: "grpc", Port: 9091}}
	a.ConfigSchema = &manifest.ConfigSchema{Properties: map[string]manifest.ConfigProperty{
		"CERT": {Type: "string", Secret: true}, "TOKEN": {Type: "string"}, "MODE": {Type: "string"},
		"PAGE": {Type: "integer", Default: 20},
	}}
	groups, err := resolveFixture(t, cfg, map[string]*manifest.Manifest{
		"erp/shell@1.0.0": simple("erp/shell", "1.0.0", 8080),
		"erp/a@1.0.0":     a,
		"erp/db@1.0.0":    simple("erp/db", "1.0.0", 5432),
	})
	require.NoError(t, err)
	require.Len(t, groups, 1)

	var entries []map[string]any
	require.NoError(t, json.Unmarshal([]byte(groups[0].ServedMembersConfig()), &entries))
	require.Len(t, entries, 1)
	assert.Equal(t, "erp/a", entries[0]["componentId"])
	assert.Equal(t, "1.0.0", entries[0]["version"])
	assert.Equal(t, float64(8081), entries[0]["httpPort"])
	assert.Equal(t, []any{map[string]any{"name": "grpc", "port": float64(9091)}}, entries[0]["extraPorts"])
	assert.Equal(t, map[string]any{
		"CERT": memberPEM, "TOKEN": "t$k", "MODE": "strict", "PAGE": "20",
		"ERP_DB_ENDPOINT": "http://erp-db-1-0-0:5432",
	}, entries[0]["config"])

	out := shell.Apply(nil, groups[0])
	byName := map[string]inject.Var{}
	for _, v := range out {
		byName[v.Name] = v
	}
	json := byName[shell.EnvVarServedMembersConfig]
	assert.True(t, json.Secret, "JSON 里装着成员的密钥：按密钥放置（Docker 进 0600 env 文件，K8s 进 Secret）")
	assert.Equal(t, shell.EnvVarServedMembersConfig, json.Key)
	assert.Equal(t, "erp-a-1-0-0", byName[shell.EnvVarServedMembers].Value.Text)
	for name := range byName {
		assert.NotContains(t, name, "ERP_A_", "成员配置不再以带前缀的变量摊进外壳环境")
	}
}

// ${VAR} 取不到：大声失败，点名成员、配置项与变量——绝不能把字面的 ${VAR} 塞进 JSON。
func TestResolveMemberTemplateUnresolved(t *testing.T) {
	a := simple("erp/a", "1.0.0", 8081)
	a.ConfigSchema = &manifest.ConfigSchema{Properties: map[string]manifest.ConfigProperty{"TOKEN": {Type: "string"}}}
	_, err := resolveFixture(t, &testCfg{Components: []testComp{
		comp("erp/shell", "1.0.0", ""),
		{ID: "erp/a", Version: "1.0.0", ServedBy: "erp/shell@1.0.0", Config: map[string]any{"TOKEN": "${MISSING_TOKEN}"}},
	}}, map[string]*manifest.Manifest{"erp/shell@1.0.0": simple("erp/shell", "1.0.0", 8080), "erp/a@1.0.0": a})
	require.Error(t, err)
	for _, want := range []string{"erp/a", "TOKEN", "MISSING_TOKEN"} {
		assert.Contains(t, err.Error(), want)
	}
}

// existingSecret 引用的是集群里别人建的 Secret，CLI 读不到值，而外壳的 JSON 需要值。
func TestResolveMemberExistingSecretRejected(t *testing.T) {
	a := simple("erp/a", "1.0.0", 8081)
	a.ConfigSchema = &manifest.ConfigSchema{Properties: map[string]manifest.ConfigProperty{"TOKEN": {Type: "string", Secret: true}}}
	_, err := resolveFixture(t, &testCfg{Components: []testComp{
		comp("erp/shell", "1.0.0", ""),
		{ID: "erp/a", Version: "1.0.0", ServedBy: "erp/shell@1.0.0",
			Config: map[string]any{"TOKEN": map[string]any{"existingSecret": "vault", "key": "token"}}},
	}}, map[string]*manifest.Manifest{"erp/shell@1.0.0": simple("erp/shell", "1.0.0", 8080), "erp/a@1.0.0": a})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "existingSecret")
	assert.Contains(t, err.Error(), "erp/a")
}

// 不是合法 UTF-8 的值编不进 JSON 而不改字节：大声失败，而不是悄悄换成替换字符。
func TestResolveMemberInvalidUTF8Rejected(t *testing.T) {
	a := simple("erp/a", "1.0.0", 8081)
	a.ConfigSchema = &manifest.ConfigSchema{Properties: map[string]manifest.ConfigProperty{"BLOB": {Type: "string"}}}
	_, err := resolveFixture(t, &testCfg{
		Components: []testComp{
			comp("erp/shell", "1.0.0", ""),
			{ID: "erp/a", Version: "1.0.0", ServedBy: "erp/shell@1.0.0", Config: map[string]any{"BLOB": "file://blob.bin"}},
		},
		Files: projecttest.Files{"blob.bin": "ok\xff\xfe"},
	}, map[string]*manifest.Manifest{"erp/shell@1.0.0": simple("erp/shell", "1.0.0", 8080), "erp/a@1.0.0": a})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "BLOB")
	assert.Contains(t, err.Error(), "UTF-8")
}
