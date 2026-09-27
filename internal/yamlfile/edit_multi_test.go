package yamlfile_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/brickkit/brickkit/internal/yamlfile"
)

func openText(t *testing.T, name, text string) (*yamlfile.Edit, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	require.NoError(t, os.WriteFile(path, []byte(text), 0o644))
	e, err := yamlfile.OpenEdit(path)
	require.NoError(t, err)
	return e, path
}

const declSample = `project: shop

components:
  # the API, two versions
  - id: erp/api
    version: 1.1.0
  - id: erp/api
    version: 1.0.0
    requiredBy: [erp/portal]
  - id: erp/portal
    version: 1.0.0
`

// brickkit.yaml 里同一个组件 ID 可以有多行：按 ID + 版本选中，只动选中的那一行。
func TestSelectorTellsVersionsApart(t *testing.T) {
	e, path := openText(t, "brickkit.yaml", declSample)

	require.True(t, e.SetList("components", yamlfile.Selector{ID: "erp/api", Version: "1.0.0"}, "requiredBy", []string{"erp/portal", "erp/shell"}))
	require.True(t, e.SetList("components", yamlfile.Selector{ID: "erp/api", Version: "1.1.0"}, "requiredBy", nil) == false,
		"没有这个字段时去掉它等于没改")
	assert.Equal(t, `project: shop

components:
  # the API, two versions
  - id: erp/api
    version: 1.1.0
  - id: erp/api
    version: 1.0.0
    requiredBy: [erp/portal, erp/shell]
  - id: erp/portal
    version: 1.0.0
`, saved(t, e, path))

	e, path = openText(t, "brickkit.yaml", declSample)
	require.True(t, e.SetList("components", yamlfile.Selector{ID: "erp/api", Version: "1.0.0"}, "requiredBy", nil))
	require.True(t, e.RemoveWhere("components", yamlfile.Selector{ID: "erp/api", Version: "1.1.0"}))
	assert.False(t, e.RemoveWhere("components", yamlfile.Selector{ID: "erp/api", Version: "9.9.9"}))
	assert.Equal(t, `project: shop

components:
  # the API, two versions
  - id: erp/api
    version: 1.0.0
  - id: erp/portal
    version: 1.0.0
`, saved(t, e, path))
}

func TestAppendMappingWritesFlowList(t *testing.T) {
	e, path := openText(t, "brickkit.yaml", declSample)
	require.True(t, e.AppendMapping("components", []yamlfile.Field{
		{Key: "id", Value: "erp/worker"}, {Key: "version", Value: "1.0.0"},
		{Key: "requiredBy", Value: []string{"erp/shell"}},
	}))
	require.True(t, e.AppendMapping("components", []yamlfile.Field{
		{Key: "id", Value: "erp/shell"}, {Key: "version", Value: "1.0.0"}, {Key: "kind", Value: "shell"},
	}))
	assert.Equal(t, declSample+`  - id: erp/worker
    version: 1.0.0
    requiredBy: [erp/shell]
  - id: erp/shell
    version: 1.0.0
    kind: shell
`, saved(t, e, path))
}

const deploySample = `target: docker
components:
  - id: erp/shell
  # the API runs on its own for now
  - id: erp/api
    expose: true
    exposePort: 18081
  - id: erp/portal
`

// 顶层条目挪到外壳下面：字段、上方的注释跟着走。
func TestNestMovesEntryWithFields(t *testing.T) {
	e, path := openText(t, "deploy.yaml", deploySample)
	require.True(t, e.Nest("components", "erp/shell", "erp/api"))
	require.True(t, e.Nest("components", "erp/shell", "erp/worker@1.0.0"), "不存在的条目以裸条目加进去")
	assert.False(t, e.Nest("components", "erp/nope", "erp/portal"), "外壳条目不存在")
	assert.Equal(t, `target: docker
components:
  - id: erp/shell
    members:
      # the API runs on its own for now
      - id: erp/api
        expose: true
        exposePort: 18081
      - id: erp/worker@1.0.0
  - id: erp/portal
`, saved(t, e, path))
}

func TestUnnestPromotesMembersAfterShell(t *testing.T) {
	e, path := openText(t, "deploy.yaml", `target: docker
components:
  - id: erp/shell
    expose: true
    members:
      - id: erp/api
        exposePort: 18081
      - id: erp/worker@1.0.0
  - id: erp/portal
`)
	assert.Equal(t, []string{"erp/api", "erp/worker@1.0.0"}, e.Unnest("components", "erp/shell"))
	assert.Empty(t, e.Unnest("components", "erp/portal"))
	assert.Equal(t, `target: docker
components:
  - id: erp/shell
    expose: true
  - id: erp/api
    exposePort: 18081
  - id: erp/worker@1.0.0
  - id: erp/portal
`, saved(t, e, path))
}

func TestRenameIDNestedAndTopLevel(t *testing.T) {
	e, path := openText(t, "deploy.yaml", `target: docker
components:
  - id: erp/shell
    members:
      - id: erp/api@1.0.0 # pinned
  - id: erp/portal@2.0.0
`)
	require.True(t, e.RenameID("components", "erp/api@1.0.0", "erp/api"))
	require.True(t, e.RenameID("components", "erp/portal@2.0.0", "erp/portal"))
	assert.False(t, e.RenameID("components", "erp/nope", "erp/x"))
	assert.Equal(t, `target: docker
components:
  - id: erp/shell
    members:
      - id: erp/api # pinned
  - id: erp/portal
`, saved(t, e, path))
}
