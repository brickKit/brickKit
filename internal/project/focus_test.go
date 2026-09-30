package project_test

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/project"
	"github.com/brickkit/brickkit/internal/project/projecttest"
)

// loadFocused 写一个只有 erp/api 的项目，个人文件里写 focus 与 erp/api 条目的额外行，按个人文件装载。
func loadFocused(t *testing.T, focus, apiExtra string) (*project.Project, error) {
	t.Helper()
	root := t.TempDir()
	projecttest.Write(t, root, projecttest.Files{
		"brickkit.yaml":     "project: shop\ncomponents:\n  - id: erp/api\n    version: 1.0.0\n",
		"deploy.yaml":       "target: docker\ncomponents:\n  - id: erp/api\n",
		"deploy.local.yaml": "target: docker\nfocus: " + focus + "\ncomponents:\n  - id: erp/api\n" + apiExtra,
	})
	return project.LoadTopology(root, project.LoadOptions{DeployFile: filepath.Join(root, "deploy.local.yaml")})
}

// 焦点指向 brickkit.yaml 里没有的组件：COMPONENT_NOT_FOUND。
func TestFocusOnAnUnknownComponent(t *testing.T) {
	_, err := loadFocused(t, "erp/apj", "")
	require.Error(t, err)
	assert.Equal(t, clierr.CodeComponentNotFound, clierr.As(err).Code)
}

// 焦点组件的条目写着 mode: disable：COMPONENT_DISABLED。
func TestFocusOnADisabledEntry(t *testing.T) {
	_, err := loadFocused(t, "erp/api", "    mode: disable\n")
	require.Error(t, err)
	assert.Equal(t, clierr.CodeComponentDisabled, clierr.As(err).Code)
}

// 焦点组件的默认版本读出来是 mode: local；IgnoreFocus 之后照旧。
func TestFocusAndIgnoreFocus(t *testing.T) {
	p, err := loadFocused(t, "erp/api", "")
	require.NoError(t, err)
	id, version, ok := p.FocusRef()
	require.True(t, ok)
	assert.Equal(t, "erp/api@1.0.0", id+"@"+version)
	assert.Equal(t, "local", p.DeployEntry("erp/api", "1.0.0").Mode)

	p.IgnoreFocus()
	_, _, ok = p.FocusRef()
	assert.False(t, ok)
	assert.Equal(t, "", p.DeployEntry("erp/api", "1.0.0").Mode)
}
