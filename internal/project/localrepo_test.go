package project_test

import (
	"os/exec"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/brickkit/brickkit/internal/project"
	"github.com/brickkit/brickkit/internal/project/projecttest"
)

func componentYAML(id, version string) string {
	return "apiVersion: brickkit/v1\nkind: Component\nmetadata: {id: " + id + ", name: x, version: " + version +
		", description: x}\ndeployment: {type: container, image: x, port: 8080}\nhealthCheck: {type: none}\n"
}

func TestLocalRepoLookupOrder(t *testing.T) {
	p := projecttest.Load(t, projecttest.Files{
		"brickkit.yaml": `project: p
sources:
  - {name: shells, type: local, path: ./shell}
components:
  - {id: erp/shell, version: 1.0.0}
  - {id: erp/own, version: 1.0.0, source: {type: local, path: ./elsewhere/own}}
  - {id: erp/mono, version: 1.0.0, source: {type: git, repo: "https://x/platform", path: packages/mono}}
  - {id: erp/cloned, version: 1.0.0}
  - {id: erp/none, version: 1.0.0}
`,
		"deploy.yaml":                                      "target: docker\ncomponents: [{id: erp/shell}, {id: erp/own}, {id: erp/mono}, {id: erp/cloned}, {id: erp/none}]\n",
		"shell/erp/shell/component.yaml":                   componentYAML("erp/shell", "1.0.0"),
		"elsewhere/own/component.yaml":                     componentYAML("erp/own", "1.0.0"),
		"components/erp/mono/packages/mono/component.yaml": componentYAML("erp/mono", "1.0.0"),
		"components/erp/cloned/component.yaml":             componentYAML("erp/cloned", "1.2.0"),
	})
	for id, want := range map[string]string{
		"erp/shell":  "shell/erp/shell",
		"erp/own":    "elsewhere/own",
		"erp/mono":   "components/erp/mono/packages/mono",
		"erp/cloned": "components/erp/cloned",
	} {
		dir, ok := p.LocalRepo(id)
		assert.True(t, ok, id)
		assert.Equal(t, p.Layout.Resolve(want), dir, id)
	}
	_, ok := p.LocalRepo("erp/none")
	assert.False(t, ok)

	dir, _ := p.LocalRepo("erp/cloned")
	v, err := project.LocalRepoVersion(dir)
	assert.NoError(t, err)
	assert.Equal(t, "1.2.0", v)
}

// 仓库源（sources[].repo）的组件，--repo 克隆的是整个 monorepo：组件在克隆里按 metadata.id 找，
// 版本 tag 带命名空间。没提交的、在 . 开头目录里的、克隆之外的都不算。
func TestLocalRepoFindsTheComponentInAMonorepoClone(t *testing.T) {
	p := projecttest.Load(t, projecttest.Files{
		"brickkit.yaml": `project: p
sources:
  - {name: mono, type: git, repo: "https://x/acceptance.git"}
components:
  - {id: test/probe-go, version: 0.1.0}
  - {id: test/probe-py, version: 0.1.0}
  - {id: test/untracked, version: 0.1.0}
`,
		"deploy.yaml": "target: docker\ncomponents: [{id: test/probe-go}, {id: test/probe-py}, {id: test/untracked}]\n",
		"components/test/probe-go/probes/go/component.yaml":                                         componentYAML("test/probe-go", "0.1.0"),
		"components/test/probe-go/probes/py/component.yaml":                                         componentYAML("test/probe-py", "0.1.0"),
		"components/test/probe-go/probes/go/.brickkit/manifests/test/probe-go/0.0.1/component.yaml": componentYAML("test/probe-go", "0.0.1"),
		"components/test/probe-go/new/component.yaml":                                               componentYAML("test/untracked", "0.1.0"),
	})
	clone := p.Layout.Resolve("components/test/probe-go")
	gitIn(t, clone, "init", "--quiet")
	gitIn(t, clone, "add", "probes")
	gitIn(t, clone, "add", "--force", "probes/go/.brickkit")

	dir, ok := p.LocalRepo("test/probe-go")
	require.True(t, ok)
	assert.Equal(t, p.Layout.Resolve("components/test/probe-go/probes/go"), dir)
	assert.Equal(t, "probes/go", p.LocalRepoSubpath("test/probe-go"))

	_, ok = p.LocalRepo("test/untracked")
	assert.False(t, ok, "only files git tracks count")
	_, ok = p.LocalRepo("test/probe-py")
	assert.False(t, ok, "test/probe-py was not cloned: the clone at components/test/probe-go is another component's")
}

func gitIn(t *testing.T, dir string, args ...string) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not on PATH")
	}
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, string(out))
}
