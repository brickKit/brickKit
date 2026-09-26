package deployfile_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/deployfile"
)

const team = `target: docker
vars:
  DB_PASSWORD: "${PROD_DB_PASSWORD}"
components:
  - id: erp/shell
    expose: true
    exposePort: 8080
    members: [erp/backend]
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
	assert.Equal(t, "${PROD_DB_PASSWORD}", f.Vars["DB_PASSWORD"])

	e, ok := f.Entry("people/basic", "1.0.0")
	require.True(t, ok)
	assert.Equal(t, 9001, e.LocalPort)
	e, ok = f.Entry("people/basic", "2.0.0")
	require.True(t, ok)
	assert.Equal(t, "people/basic", e.ID)
	id, version := f.Components[2].Key()
	assert.Equal(t, "people/basic", id)
	assert.Equal(t, "1.0.0", version)
	assert.Equal(t, []string{"erp/backend"}, f.Components[0].Members)
	assert.True(t, f.Components[1].IsPinned())
	assert.Equal(t, 1, f.Components[1].ReplicaCount())
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
		"member with version":    {"target: docker\ncomponents:\n  - {id: a/s, members: [a/b@1.0.0]}\n", "components[0].members[0]"},
		"member self":            {"target: docker\ncomponents:\n  - {id: a/s, members: [a/s]}\n", "components[0].members[0]"},
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

func TestSettingsDefaults(t *testing.T) {
	f, _, err := parse(t, "target: k8s\n", deployfile.RoleTeam)
	require.NoError(t, err)
	assert.True(t, f.ShouldCreateNamespace())
	assert.False(t, f.NetworkPolicyEnabled())
	assert.False(t, f.ServiceAccountEnabled())
}
