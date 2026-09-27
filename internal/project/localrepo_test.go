package project_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

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
