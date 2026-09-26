package deployfile_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/deployfile"
	"github.com/brickkit/brickkit/internal/i18n"
	"github.com/brickkit/brickkit/internal/msgid"
)

const team = `target: docker
vars:
  DB_PASSWORD: "${PROD_DB_PASSWORD}"
components:
  - id: erp/shell
    expose: true
    exposePort: 8080
    members:
      - id: erp/backend
        mode: enabled
  - id: people/basic@1.0.0
    mode: local
    localPort: 9001
  - id: people/basic
`

func fields(err error) []string {
	e := clierr.As(err)
	if e == nil {
		return nil
	}
	var out []string
	for _, d := range e.Details {
		out = append(out, d.Key)
	}
	return out
}

func parse(t *testing.T, yaml string, role deployfile.Role) (*deployfile.File, []*clierr.Error, error) {
	t.Helper()
	return deployfile.Parse([]byte(yaml), "deploy.yaml", role)
}

func TestParseTeamFile(t *testing.T) {
	t.Setenv("PROD_DB_PASSWORD", "leak")
	f, warnings, err := parse(t, team, deployfile.RoleTeam)
	require.NoError(t, err)
	assert.Empty(t, warnings)
	// vars 的值留给渲染器求值，解析时绝不展开
	assert.Equal(t, "${PROD_DB_PASSWORD}", f.Vars["DB_PASSWORD"].Value)

	e, ok := f.Entry("people/basic", "1.0.0", false)
	require.True(t, ok)
	assert.Equal(t, 9001, e.LocalPort)
	e, ok = f.Entry("people/basic", "2.0.0", true)
	require.True(t, ok, "裸 ID 条目覆盖默认版本")
	assert.Equal(t, "people/basic", e.ID)
	_, ok = f.Entry("people/basic", "3.0.0", false)
	assert.False(t, ok, "裸 ID 条目不覆盖非默认版本")
	id, version := f.Components[1].Key()
	assert.Equal(t, "people/basic", id)
	assert.Equal(t, "1.0.0", version)
	e, ok = f.Entry("erp/backend", "2.0.0", true)
	require.True(t, ok, "外壳下面的成员条目同样查得到")
	assert.True(t, e.IsPinned())
	assert.Equal(t, 1, e.ReplicaCount())
}

func TestDebugOnlyInLocalRole(t *testing.T) {
	doc := "target: docker\ncomponents:\n  - id: a/b\n    mode: debug\n    localPort: 9000\n"
	_, _, err := parse(t, doc, deployfile.RoleTeam)
	require.Error(t, err)
	assert.Contains(t, fields(err), "components[0].mode")

	_, _, err = parse(t, doc, deployfile.RoleLocal)
	require.NoError(t, err)
}

func TestValidateRejects(t *testing.T) {
	cases := map[string]struct {
		yaml  string
		field string
	}{
		"missing target":         {"components: []\n", "target"},
		"bad target":             {"target: nomad\n", "target"},
		"old top-level context":  {"target: k8s\ncontext: prod\n", "context"},
		"local on k8s":           {"target: k8s\ncomponents:\n  - {id: a/b, mode: local}\n", "components[0].mode"},
		"k8s expose no hostname": {"target: k8s\ncomponents:\n  - {id: a/b, expose: true}\n", "components[0].hostname"},
		"localPort without mode": {"target: docker\ncomponents:\n  - {id: a/b, localPort: 9000}\n", "components[0].localPort"},
		"localPort conflict":     {"target: docker\ncomponents:\n  - {id: a/b, mode: local, localPort: 9000}\n  - {id: a/c, mode: local, localPort: 9000}\n", "components[1].localPort"},
		"duplicate entry":        {"target: docker\ncomponents:\n  - {id: a/b}\n  - {id: a/b}\n", "components[1].id"},
		"bad versioned id":       {"target: docker\ncomponents:\n  - {id: a/b@latest}\n", "components[0].id"},
		"member with range":      {"target: docker\ncomponents:\n  - {id: a/s, members: [{id: a/b@^1.0.0}]}\n", "components[0].members[0].id"},
		"member same id twice":   {"target: docker\ncomponents:\n  - {id: a/s, members: [{id: a/b}, {id: a/b@1.0.0}]}\n", "components[0].members[1].id"},
		"member self":            {"target: docker\ncomponents:\n  - {id: a/s, members: [{id: a/s}]}\n", "components[0].members[0].id"},
		"member nested twice":    {"target: docker\ncomponents:\n  - {id: a/s, members: [{id: a/b, members: [{id: a/c}]}]}\n", "components[0].members[0].members"},
		"member also top-level":  {"target: docker\ncomponents:\n  - {id: a/s, members: [{id: a/b}]}\n  - {id: a/b}\n", "components[1].id"},
		"member port conflict":   {"target: docker\ncomponents:\n  - {id: a/s, members: [{id: a/b, mode: local, localPort: 9000}]}\n  - {id: a/c, mode: local, localPort: 9000}\n", "components[1].localPort"},
		"member written as text": {"target: docker\ncomponents:\n  - {id: a/s, members: [a/b]}\n", "components[0].members[0]"},
		"bad var name":           {"target: docker\nvars:\n  1BAD: x\n", "vars.1BAD"},
		"replicas zero":          {"target: k8s\ncomponents:\n  - {id: a/b, replicas: 0}\n", "components[0].replicas"},
		"exposePort w/o expose":  {"target: docker\ncomponents:\n  - {id: a/b, exposePort: 8080}\n", "components[0].exposePort"},
		"egress both":            {"target: k8s\nk8s:\n  networkPolicy:\n    enabled: true\n    egress:\n      enabled: true\n      allowTo:\n        - {name: db, namespace: x, cidr: 10.0.0.0/8}\n", "k8s.networkPolicy.egress.allowTo[0]"},
		"entry not mapping":      {"target: docker\ncomponents:\n  - a/b\n", "components[0]"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			_, _, err := parse(t, tc.yaml, deployfile.RoleLocal)
			require.Error(t, err)
			assert.Contains(t, fields(err), tc.field)
		})
	}
}

func TestWarningsForTarget(t *testing.T) {
	_, warnings, err := parse(t, "target: docker\nk8s:\n  namespace: shop\ncomponents:\n  - {id: a/b, replicas: 2}\n", deployfile.RoleTeam)
	require.NoError(t, err)
	assert.Len(t, warnings, 2)

	_, warnings, err = parse(t, "target: k8s\ncomponents:\n  - {id: a/b, expose: true, hostname: a.example.com, exposePort: 8080}\n", deployfile.RoleTeam)
	require.NoError(t, err)
	assert.Len(t, warnings, 1)
}

// 同一个字段被好几个组件写了：一行说清，并点名是哪些组件（用 id，不用下标——
// 人要拿它去文件里找）。k8s: 块里写了什么就点名什么，而不是笼统一句"k8s 被忽略"。
func TestWarningsForTargetGroupedByFieldAndNamed(t *testing.T) {
	_, warnings, err := parse(t, `target: docker
k8s:
  context: prod
  namespace: shop
components:
  - {id: demo/a, replicas: 2}
  - {id: demo/b@1.0.0, replicas: 3, hostname: b.example.com}
  - {id: demo/c}
`, deployfile.RoleTeam)
	require.NoError(t, err)

	var text []string
	for _, w := range warnings {
		text = append(text, w.Format())
	}
	all := strings.Join(text, "")
	require.Len(t, warnings, 3, all)
	assert.Contains(t, all, "k8s.context, k8s.namespace")
	assert.Contains(t, all, "replicas has no effect with target: docker")
	assert.Contains(t, all, "demo/a, demo/b@1.0.0")
	assert.Contains(t, all, "hostname has no effect")
	assert.NotContains(t, all, "components[", "点名组件用 id，不用下标")
}

func TestSettingsDefaults(t *testing.T) {
	f, _, err := parse(t, "target: k8s\n", deployfile.RoleTeam)
	require.NoError(t, err)
	assert.True(t, f.ShouldCreateNamespace())
	assert.False(t, f.NetworkPolicyEnabled())
	assert.False(t, f.ServiceAccountEnabled())
}

// 外壳的成员是完整的部署条目，嵌在外壳条目下面：外壳不跑时，成员就按这些字段自己跑。
// All() 把嵌套的条目摊平，带上字段路径与所属外壳——所有逐条目的检查都走它。
func TestNestedMemberEntries(t *testing.T) {
	f, _, err := parse(t, "target: docker\ncomponents:\n"+
		"  - id: a/s\n    members:\n      - {id: a/b, expose: true, exposePort: 8081}\n      - {id: a/c@2.0.0, mode: disable}\n"+
		"  - id: a/c\n", deployfile.RoleTeam)
	require.NoError(t, err)
	all := f.All()
	require.Len(t, all, 4)
	assert.Equal(t, "components[0]", all[0].Field)
	assert.Equal(t, "", all[0].Shell)
	assert.Equal(t, "components[0].members[0]", all[1].Field)
	assert.Equal(t, "a/s", all[1].Shell)
	assert.Equal(t, 8081, all[1].ExposePort)
	assert.Equal(t, "a/c@2.0.0", all[2].ID)
	assert.Equal(t, "components[1]", all[3].Field)
	at, ok := f.EntryAt("a/c", "2.0.0", false)
	require.True(t, ok)
	assert.Equal(t, "components[0].members[1]", at.Field, "查找结果带着字段路径，报错据此指向条目")
	at, ok = f.EntryAt("a/c", "1.0.0", true)
	require.True(t, ok)
	assert.Equal(t, "components[1]", at.Field)

	_, _, err = parse(t, "target: docker\ncomponents:\n  - id: a/s\n    members:\n      - {id: a/b, mode: debug, localPort: 9000}\n",
		deployfile.RoleTeam)
	require.Error(t, err)
	assert.Contains(t, fields(err), "components[0].members[0].mode", "debug 只能写在本地文件里，成员也一样")
}

// 成员下面又写了 members：明确告诉使用者"只嵌一层"，而不是一句泛泛的"未知字段"；只报一次。
func TestThirdLevelMembersSaysOneLevel(t *testing.T) {
	_, _, err := parse(t, "target: docker\ncomponents:\n  - id: a/s\n    members:\n      - id: a/b\n        members:\n          - id: a/c\n",
		deployfile.RoleTeam)
	require.Error(t, err)
	var hits []string
	for _, d := range clierr.As(err).Details {
		if d.Key == "components[0].members[0].members" {
			hits = append(hits, d.Value)
		}
	}
	require.Len(t, hits, 1, "同一个字段只报一次")
	assert.Equal(t, i18n.T(msgid.DeployfileMemberNested), hits[0])
}

// skipWaitFor 列的是组件 ID：格式要对、不能带版本、不能重复、不能写自己。
func TestSkipWaitForValidation(t *testing.T) {
	cases := map[string]struct{ yaml, field string }{
		"bad id":    {"target: docker\ncomponents:\n  - {id: a/b, skipWaitFor: [Not_An_ID]}\n", "components[0].skipWaitFor[0]"},
		"versioned": {"target: docker\ncomponents:\n  - {id: a/b, skipWaitFor: [c/d@1.0.0]}\n", "components[0].skipWaitFor[0]"},
		"duplicate": {"target: docker\ncomponents:\n  - {id: a/b, skipWaitFor: [c/d, c/d]}\n", "components[0].skipWaitFor[1]"},
		"self":      {"target: docker\ncomponents:\n  - {id: a/b@1.0.0, skipWaitFor: [a/b]}\n", "components[0].skipWaitFor[0]"},
		"member":    {"target: docker\ncomponents:\n  - {id: a/s, members: [{id: a/b, skipWaitFor: [a/b]}]}\n", "components[0].members[0].skipWaitFor[0]"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			_, _, err := parse(t, tc.yaml, deployfile.RoleTeam)
			require.Error(t, err)
			assert.Contains(t, fields(err), tc.field)
		})
	}
	_, _, err := parse(t, "target: docker\ncomponents:\n  - {id: a/s, members: [{id: a/b, skipWaitFor: [c/d]}]}\n", deployfile.RoleTeam)
	require.NoError(t, err)
}

// K8s 的 Pod 之间没有启动顺序：skipWaitFor 写了也不起作用，警告但不拦。
func TestSkipWaitForIgnoredOnK8sWarns(t *testing.T) {
	_, warnings, err := parse(t, "target: k8s\ncomponents:\n  - {id: a/b, skipWaitFor: [c/d]}\n", deployfile.RoleTeam)
	require.NoError(t, err)
	require.Len(t, warnings, 1)
	assert.Contains(t, warnings[0].Format(), "skipWaitFor")
}
