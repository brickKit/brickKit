package deployfile_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/brickkit/brickkit/internal/deployfile"
)

func ptr(s string) *string { return &s }

const freshTeam = `target: docker
vars:
  DB_PASSWORD: "${PROD_DB_PASSWORD}"
components:
  - id: erp/backend
    mode: enabled
  - id: erp/shell
    members:
      - id: erp/api
        expose: false
`

// 提案 §6.6 的例子：本地文件里每一处和团队文件不同（或团队文件没有）的值都列出来。
func TestDiffLocalReportsEntryFieldTopLevelAndVars(t *testing.T) {
	old := `target: k8s
k8s:
  namespace: mine
vars:
  DB_PASSWORD: local_dev_pwd
components:
  - id: erp/backend
    mode: debug
    localPort: 8081
  - id: erp/shell
    members:
      - id: erp/api
        expose: false
`
	changes, err := deployfile.DiffLocal([]byte(old), []byte(freshTeam))
	require.NoError(t, err)
	assert.Equal(t, []deployfile.LocalChange{
		{Scope: "deploy", Field: "target", Old: "k8s", New: ptr("docker")},
		{Scope: "deploy", Field: "k8s.namespace", Old: "mine"},
		{Scope: "vars", Field: "DB_PASSWORD", Old: "local_dev_pwd", New: ptr("${PROD_DB_PASSWORD}")},
		{Scope: "erp/backend", Field: "mode", Old: "debug", New: ptr("enabled")},
		{Scope: "erp/backend", Field: "localPort", Old: "8081"},
	}, changes)
}

// 外壳下面的成员条目按它自己的条目 id 报，不带外壳的路径。
func TestDiffLocalKeysMembersByEntryID(t *testing.T) {
	old := `target: docker
vars:
  DB_PASSWORD: "${PROD_DB_PASSWORD}"
components:
  - id: erp/backend
    mode: enabled
  - id: erp/shell
    members:
      - id: erp/api
        expose: true
        exposePort: 9000
`
	changes, err := deployfile.DiffLocal([]byte(old), []byte(freshTeam))
	require.NoError(t, err)
	assert.Equal(t, []deployfile.LocalChange{
		{Scope: "erp/api", Field: "expose", Old: "true", New: ptr("false")},
		{Scope: "erp/api", Field: "exposePort", Old: "9000"},
	}, changes)
}

func TestDiffLocalWholeEntryGone(t *testing.T) {
	old := freshTeam + "  - id: crm/old\n    mode: debug\n"
	changes, err := deployfile.DiffLocal([]byte(old), []byte(freshTeam))
	require.NoError(t, err)
	assert.Equal(t, []deployfile.LocalChange{{Scope: "crm/old"}}, changes)
}

// 比的是数据：注释、键序、引号写法都不算本地修改；新文件多出来的也不算（那是团队的标准）。
func TestDiffLocalIgnoresCommentsAndOrder(t *testing.T) {
	old := `# my local copy
components:
  - mode: 'enabled' # still the same
    id: erp/backend
  - id: erp/shell
    members:
      - expose: false
        id: erp/api
vars:
  DB_PASSWORD: ${PROD_DB_PASSWORD}
target: "docker"
`
	fresh := freshTeam + "  - id: crm/new\n"
	changes, err := deployfile.DiffLocal([]byte(old), []byte(fresh))
	require.NoError(t, err)
	assert.Empty(t, changes)
}

// 列表与映射按一行的流式写法比较与展示。
func TestDiffLocalRendersListsOnOneLine(t *testing.T) {
	old := "target: docker\ncomponents:\n  - id: a/b\n    skipWaitFor: [c/d, e/f]\n"
	fresh := "target: docker\ncomponents:\n  - id: a/b\n    skipWaitFor: [c/d]\n"
	changes, err := deployfile.DiffLocal([]byte(old), []byte(fresh))
	require.NoError(t, err)
	assert.Equal(t, []deployfile.LocalChange{
		{Scope: "a/b", Field: "skipWaitFor", Old: `["c/d","e/f"]`, New: ptr(`["c/d"]`)},
	}, changes)
}

// 形状不对的部分（非映射的文档、没有 id 的条目、不是列表的 components）不报改动、也不出错：
// 那些是 up 装载时该报的错，摘要只比能比的。
func TestDiffLocalToleratesOddShapes(t *testing.T) {
	changes, err := deployfile.DiffLocal([]byte("- just a list\n"), []byte(freshTeam))
	require.NoError(t, err)
	assert.Empty(t, changes)

	odd := "target: docker\ncomponents:\n  - mode: debug\n  - plain\n"
	changes, err = deployfile.DiffLocal([]byte(odd), []byte("target: docker\ncomponents: nope\n"))
	require.NoError(t, err)
	assert.Empty(t, changes)

	_, err = deployfile.DiffLocal([]byte("target: [\n"), []byte(freshTeam))
	require.Error(t, err)
	_, err = deployfile.DiffLocal([]byte(freshTeam), []byte("target: [\n"))
	require.Error(t, err)
}

// 本地把成员从外壳里挪到顶层单独跑（调试它时最常见的改动）：字段没变，位置变了，也是本地修改。
func TestDiffLocalReportsMemberMovedOutOfShell(t *testing.T) {
	old := `target: docker
vars:
  DB_PASSWORD: "${PROD_DB_PASSWORD}"
components:
  - id: erp/backend
    mode: enabled
  - id: erp/shell
  - id: erp/api
    expose: false
`
	changes, err := deployfile.DiffLocal([]byte(old), []byte(freshTeam))
	require.NoError(t, err)
	assert.Equal(t, []deployfile.LocalChange{
		{Scope: "erp/api", Field: deployfile.FieldPlacement, Old: "", New: ptr("erp/shell")},
	}, changes)
}

// 反过来：本地把一个顶层组件收进了外壳。
func TestDiffLocalReportsEntryMovedIntoShell(t *testing.T) {
	fresh := "target: docker\ncomponents:\n  - id: erp/shell\n  - id: erp/api\n"
	old := "target: docker\ncomponents:\n  - id: erp/shell\n    members:\n      - id: erp/api\n"
	changes, err := deployfile.DiffLocal([]byte(old), []byte(fresh))
	require.NoError(t, err)
	assert.Equal(t, []deployfile.LocalChange{
		{Scope: "erp/api", Field: deployfile.FieldPlacement, Old: "erp/shell", New: ptr("")},
	}, changes)
}
