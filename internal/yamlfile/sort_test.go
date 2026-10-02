package yamlfile_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/brickkit/brickkit/internal/yamlfile"
)

func byID(a, b yamlfile.EntryKey) bool { return a.ID < b.ID }

func sorted(t *testing.T, name, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
	e, err := yamlfile.OpenEdit(path)
	require.NoError(t, err)
	e.SortEntries("components", byID)
	return saved(t, e, path)
}

// 条目整个搬：字段、行尾注释、写在它上方的注释都跟着走；别的顶层键一个字不动。
func TestSortEntriesMovesWholeEntries(t *testing.T) {
	assert.Equal(t, `# what this project is made of
project: demo

sources:
  - name: z
    type: local
    path: ./components
  - name: a
    type: git

components:
  # the directory
  - id: erp/api
    version: 2.0.0
  - id: infra/authz # checked on every request
    version: 1.0.0
    kind: shell
  - id: mdm/customer
    version: 1.0.0
`, sorted(t, "brickkit.yaml", `# what this project is made of
project: demo

sources:
  - name: z
    type: local
    path: ./components
  - name: a
    type: git

components:
  - id: mdm/customer
    version: 1.0.0
  - id: infra/authz # checked on every request
    version: 1.0.0
    kind: shell
  # the directory
  - id: erp/api
    version: 2.0.0
`))
}

// 外壳下面的成员各自排好，成员不会跑到顶层去。
func TestSortEntriesSortsMembersInsideTheirShell(t *testing.T) {
	assert.Equal(t, `target: docker
components:
  - id: crm/lead
  - id: erp/shell
    members:
      - id: erp/api
        mode: disable
      - id: erp/stock
  - id: mdm/customer
`, sorted(t, "deploy.yaml", `target: docker
components:
  - id: mdm/customer
  - id: erp/shell
    members:
      - id: erp/stock
      - id: erp/api
        mode: disable
  - id: crm/lead
`))
}

// 列表末尾下方的注释不属于恰好排在最后的那一条：排完它仍在列表末尾
// （yaml.v3 写回时把它放在 components 键的缩进上，排不排序都一样）。
func TestSortEntriesKeepsTrailingCommentAtTheEnd(t *testing.T) {
	assert.Equal(t, `target: docker
components:
  - id: erp/api
    expose: true
  - id: mdm/customer
    resources:
      limits:
        memory: 256Mi
# more components: brickkit add <id>

vars:
  REGION: eu
`, sorted(t, "deploy.yaml", `target: docker
components:
  - id: mdm/customer
    resources:
      limits:
        memory: 256Mi
  - id: erp/api
    expose: true
  # more components: brickkit add <id>

vars:
  REGION: eu
`))
}

// 末尾有两段注释时，yaml.v3 把缩进的那段挂在最后一条的最后一个键上（哪怕它嵌得很深）：
// 排完它跟着挪到新的最后一条下面，不随原来那一条跑到列表中间。
func TestSortEntriesMovesCommentHeldByTheLastEntry(t *testing.T) {
	assert.Equal(t, `project: demo
components:
  - id: erp/api
    version: 2.0.0
  - id: mdm/customer
    version: 1.0.0
    requiredBy: [erp/api]
    # more components: brickkit add <id>
# end of file
`, sorted(t, "brickkit.yaml", `project: demo
components:
  - id: mdm/customer
    version: 1.0.0
    requiredBy: [erp/api]
  - id: erp/api
    version: 2.0.0
  # more components: brickkit add <id>
# end of file
`))
}

// 已经排好的文件，排一次等于没排。
func TestSortEntriesLeavesSortedFileAlone(t *testing.T) {
	const content = `target: docker
components:
  - id: erp/api
  - id: mdm/customer # the customers

vars:
  REGION: eu
`
	assert.Equal(t, content, sorted(t, "deploy.yaml", content))
}

// 列表里混进了不是条目的东西：不排，原样留给校验去报。
func TestSortEntriesSkipsMalformedList(t *testing.T) {
	const content = `target: docker
components:
  - id: mdm/customer
  - erp/api
`
	assert.Equal(t, content, sorted(t, "deploy.yaml", content))
}

func TestSortEntriesPassesVersionAndRequiredBy(t *testing.T) {
	path := filepath.Join(t.TempDir(), "brickkit.yaml")
	require.NoError(t, os.WriteFile(path, []byte(`project: demo
components:
  - id: erp/api
    version: 1.0.0
    requiredBy: [erp/web]
  - id: erp/api
    version: 2.0.0
  - id: erp/web
    version: 1.0.0
    requiredBy: []
`), 0o644))
	e, err := yamlfile.OpenEdit(path)
	require.NoError(t, err)

	seen := map[yamlfile.EntryKey]bool{}
	e.SortEntries("components", func(a, b yamlfile.EntryKey) bool {
		seen[a], seen[b] = true, true
		return false
	})
	assert.Equal(t, map[yamlfile.EntryKey]bool{
		{ID: "erp/api", Version: "1.0.0", RequiredBy: true}: true,
		{ID: "erp/api", Version: "2.0.0"}:                   true,
		{ID: "erp/web", Version: "1.0.0"}:                   true,
	}, seen)
}
