package cli

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/brickkit/brickkit/internal/clierr"
)

// 在组件目录里不带参数 deps：打印这个组件的依赖树，不是整个项目。
func TestDepsWithoutArgumentsInAComponentDirectory(t *testing.T) {
	dir := focusFixture(t)
	r := runIn(t, in(dir, "components", "erp", "portal"), "deps")
	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)
	assert.Contains(t, r.stdout, "erp/portal")
	assert.Contains(t, r.stdout, "erp/api")
	assert.NotContains(t, r.stdout, "erp/worker")
}

// 在组件目录里不带参数 build：只构建这个组件。
func TestBuildWithoutArgumentsInAComponentDirectory(t *testing.T) {
	dir := focusFixture(t)
	for _, rel := range [][]string{{"components", "erp", "portal"}, {"components", "erp", "worker"}} {
		editFile(t, in(dir, append(rel, "component.yaml")...), "deployment:\n",
			"deployment:\n  build: { context: ., dockerfile: Dockerfile }\n")
		writeTree(t, in(dir, rel...), map[string]string{"Dockerfile": "FROM scratch\n"})
	}
	imgs := newFakeImages()
	r := runWith(t, func(o *Options) { o.Images = imgs }, in(dir, "components", "erp", "portal"), "build")
	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)
	require.Len(t, imgs.built(), 1)
	assert.Contains(t, imgs.built()[0], "erp-portal")
}

// 在项目根不带参数：与今天相同（deps 整个项目）。
func TestDepsAtTheRootIsUnchanged(t *testing.T) {
	dir := focusFixture(t)
	r := runIn(t, dir, "deps")
	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)
	assert.Contains(t, r.stdout, "erp/worker")
}
