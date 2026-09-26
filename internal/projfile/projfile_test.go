package projfile_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/projfile"
)

const valid = `project: my-shop
sources:
  - name: default
    type: git
    baseUrl: https://github.com/myorg/
  - name: dev
    type: local
    path: ./components
  - name: market
    type: market
    url: https://market.example.com/api/v1
    authToken: ${MARKET_TOKEN}
components:
  - id: erp/shell
    version: 1.0.0
    kind: shell
  - id: erp/backend
    version: 2.0.0
  - id: erp/backend
    version: 1.0.0
  - id: third-party/payment
    version: 1.0.0
    source:
      type: git
      repo: https://github.com/other-org/payment-gateway
  - id: infra/logger
    version: 0.3.0
    source:
      type: local
      path: ./components/infra-logger
installer:
  requireSignature: false
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

func TestParseValid(t *testing.T) {
	t.Setenv("MARKET_TOKEN", "tok")
	f, err := projfile.Parse([]byte(valid), "brickkit.yaml")
	require.NoError(t, err)
	assert.Equal(t, "my-shop", f.Project)
	assert.Equal(t, "tok", f.Sources[2].AuthToken)
	assert.True(t, f.Components[0].IsShell())
	assert.True(t, f.IsShellID("erp/shell"))
	assert.Equal(t, []string{"2.0.0", "1.0.0"}, f.Versions("erp/backend"))
	assert.Equal(t, []string{"erp/shell", "erp/backend", "third-party/payment", "infra/logger"}, f.IDs())
	assert.False(t, f.RequireSignature())
	assert.Equal(t, "erp/backend@2.0.0", f.Components[1].Ref())
}

func TestParseRejectsOldFields(t *testing.T) {
	_, err := projfile.Parse([]byte("project: p\ndeploy:\n  target: docker\nresources: []\n"), "brickkit.yaml")
	require.Error(t, err)
	assert.Contains(t, fields(err), "deploy")
	assert.Contains(t, fields(err), "resources")
}

func TestValidateComponents(t *testing.T) {
	cases := map[string]struct {
		yaml  string
		field string
	}{
		"range version":        {"project: p\ncomponents:\n  - id: a/b\n    version: ^1.0.0\n", "components[0].version"},
		"literal local":        {"project: p\ncomponents:\n  - id: a/b\n    version: local\n", "components[0].version"},
		"duplicate":            {"project: p\ncomponents:\n  - {id: a/b, version: 1.0.0}\n  - {id: a/b, version: 1.0.0}\n", "components[1]"},
		"bad kind":             {"project: p\ncomponents:\n  - {id: a/b, version: 1.0.0, kind: connector}\n", "components[0].kind"},
		"two shell versions":   {"project: p\ncomponents:\n  - {id: a/s, version: 1.0.0, kind: shell}\n  - {id: a/s, version: 2.0.0, kind: shell}\n", "components[1]"},
		"kind inconsistent":    {"project: p\ncomponents:\n  - {id: a/s, version: 1.0.0, kind: shell}\n  - {id: a/s, version: 2.0.0}\n", "components[1].kind"},
		"git source no repo":   {"project: p\ncomponents:\n  - id: a/b\n    version: 1.0.0\n    source: {type: git}\n", "components[0].source.repo"},
		"local source no path": {"project: p\ncomponents:\n  - id: a/b\n    version: 1.0.0\n    source: {type: local}\n", "components[0].source.path"},
		"bad source type":      {"project: p\ncomponents:\n  - id: a/b\n    version: 1.0.0\n    source: {type: market}\n", "components[0].source.type"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := projfile.Parse([]byte(tc.yaml), "brickkit.yaml")
			require.Error(t, err)
			assert.Contains(t, fields(err), tc.field)
		})
	}
}

func TestValidateSources(t *testing.T) {
	_, err := projfile.Parse([]byte(`project: p
sources:
  - {name: a, type: git}
  - {name: a, type: local}
  - {name: m, type: market}
  - {name: x, type: svn}
`), "brickkit.yaml")
	require.Error(t, err)
	got := fields(err)
	assert.Contains(t, got, "sources[0].baseUrl")
	assert.Contains(t, got, "sources[1].name")
	assert.Contains(t, got, "sources[1].path")
	assert.Contains(t, got, "sources[2].url")
	assert.Contains(t, got, "sources[3].type")
}

func TestParseFileMissing(t *testing.T) {
	_, err := projfile.ParseFile(filepath.Join(t.TempDir(), "brickkit.yaml"))
	require.Error(t, err)
	assert.Equal(t, clierr.CodeProjectMissing, clierr.As(err).Code)
}

func TestParseFileRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "brickkit.yaml")
	require.NoError(t, os.WriteFile(path, []byte("project: p\n"), 0o644))
	f, err := projfile.ParseFile(path)
	require.NoError(t, err)
	assert.Equal(t, path, f.Source)
	assert.True(t, f.RequireSignature())
}
