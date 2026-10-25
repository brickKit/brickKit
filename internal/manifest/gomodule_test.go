package manifest

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/brickkit/brickkit/internal/clierr"
)

// 与 Go 的 modfile.ModulePath 同样的读法：注释、引号、module 后面没有路径、moduleX 都照它处理。
func TestGoModulePath(t *testing.T) {
	for in, want := range map[string]string{
		"module example.com/api\n\ngo 1.22\n":          "example.com/api",
		"// header\nmodule example.com/api/v3 // vN\n": "example.com/api/v3",
		"module \"example.com/quoted\"\n":              "example.com/quoted",
		"module `example.com/raw`\n":                   "example.com/raw",
		"\tmodule   example.com/spaced  \r\n":          "example.com/spaced",
		"go 1.22\n":                                    "",
		"module\n":                                     "",
		"modulex example.com/api\n":                    "",
		"module \"broken\n":                            "",
	} {
		assert.Equal(t, want, goModulePath([]byte(in)), in)
	}
}

func TestGoMajorSuffix(t *testing.T) {
	for _, c := range []struct {
		module    string
		major     int
		got, want string
	}{
		{"example.com/api", 0, "", ""},
		{"example.com/api", 1, "", ""},
		{"example.com/api", 3, "", "/v3"},
		{"example.com/api/v3", 3, "/v3", "/v3"},
		{"example.com/api/v2", 3, "/v2", "/v3"},
		{"example.com/api/v2", 1, "/v2", ""},
		{"example.com/api/v1", 1, "/v1", ""}, // Go 不认 /v1：照样报出来
		{"example.com/apiv2", 2, "", "/v2"},  // 不是 /v2，只是名字以 v2 结尾
		{"example.com/api2", 2, "", "/v2"},
		{"gopkg.in/yaml.v3", 3, ".v3", ".v3"},
		{"gopkg.in/yaml.v1", 0, ".v1", ".v0"},
	} {
		got, want := goMajorSuffix(c.module, c.major)
		assert.Equal(t, c.got, got, "%s @ %d", c.module, c.major)
		assert.Equal(t, c.want, want, "%s @ %d", c.module, c.major)
	}
}

func goModuleManifest(t *testing.T, version string, goModule bool) *Manifest {
	t.Helper()
	m := &Manifest{Metadata: Metadata{ID: "erp/api", Version: version}}
	if goModule {
		m.Release = &Release{GoModule: true}
	}
	return m
}

func TestGoModuleProblem(t *testing.T) {
	dir := t.TempDir()
	assert.Nil(t, GoModuleProblem(goModuleManifest(t, "3.0.0", false), dir), "没写 goModule：什么都不查")

	e := GoModuleProblem(goModuleManifest(t, "1.0.0", true), dir)
	require.NotNil(t, e)
	assert.Equal(t, clierr.CodeManifestInvalid, e.Code)
	assert.Contains(t, e.Format(), "there is no go.mod")

	write := func(content string) {
		require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"), []byte(content), 0o644))
	}
	write("go 1.22\n")
	assert.Contains(t, GoModuleProblem(goModuleManifest(t, "1.0.0", true), dir).Format(), "has no module line")

	write("module example.com/erp/api\n")
	assert.Nil(t, GoModuleProblem(goModuleManifest(t, "1.4.2", true), dir))
	e = GoModuleProblem(goModuleManifest(t, "2.0.0", true), dir)
	require.NotNil(t, e)
	assert.Contains(t, e.Format(), "has to end in the major version, here /v2")
	assert.Contains(t, e.Format(), "example.com/erp/api")

	write("module example.com/erp/api/v3\n")
	assert.Nil(t, GoModuleProblem(goModuleManifest(t, "3.0.7", true), dir))
	assert.Contains(t, GoModuleProblem(goModuleManifest(t, "4.0.0", true), dir).Format(), "here /v4")
	assert.Contains(t, GoModuleProblem(goModuleManifest(t, "1.0.0", true), dir).Format(), "and it ends in /v3")
}

// lint 拿同一条当警告：内容不变，原来那条还是错误。
func TestGoModuleProblemAsWarning(t *testing.T) {
	e := GoModuleProblem(goModuleManifest(t, "1.0.0", true), t.TempDir())
	require.NotNil(t, e)
	w := e.AsWarning().WithDetail("extra", "x")
	assert.True(t, w.Warning)
	assert.Equal(t, clierr.ExitOK, w.ExitCode())
	assert.False(t, e.Warning)
	assert.NotContains(t, e.Format(), "extra")
}
