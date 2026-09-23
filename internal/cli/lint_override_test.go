package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/brickkit/brickkit/internal/clierr"
)

// 悬空条目——override.yaml 指着一个 brickkit.yaml 里已经不存在的组件——是错误，
// 不需要 --strict 也该失败：它跟 up/sync/status/down/brickkit override 走的是
// 同一条 CheckAgainst 校验，lint 不该是唯一放行它的地方。
func TestLintCatchesDanglingOverrideEntry(t *testing.T) {
	f := addedProject(t, []comp{{ID: "demo/hello", Version: "1.0.0"}}, "demo/hello@1.0.0")
	f.writeConfig(t, `components:
  - id: demo/hello
    version: 1.0.0
`)
	f.writeOverride(t, `components:
  - id: demo/gone
`)

	r := runIn(t, f.Dir, "lint")

	require.Equal(t, clierr.ExitError, r.code, r.stdout+r.stderr)
	assert.Contains(t, r.stdout, "override.yaml")
	assert.Contains(t, r.stdout, "demo/gone")
}

// baseline 漂移是警告，不阻断——设计书 §7 明说"a warning, not an error"，跟
// lintManifest 里拼错的 configSchema 键同一个待遇。
func TestLintWarnsOnStaleBaselineWithoutFailing(t *testing.T) {
	f := addedProject(t, []comp{{ID: "demo/hello", Version: "1.0.0"}}, "demo/hello@1.0.0")
	f.writeConfig(t, `components:
  - id: demo/hello
    version: 1.0.0
    mode: local
`)
	f.writeOverride(t, `components:
  - id: demo/hello
    mode: debug
    baseline: enabled
`)

	r := runIn(t, f.Dir, "lint")

	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)
	assert.Contains(t, r.stdout, "Drift: demo/hello")
}

func TestLintStrictFailsOnStaleBaseline(t *testing.T) {
	f := addedProject(t, []comp{{ID: "demo/hello", Version: "1.0.0"}}, "demo/hello@1.0.0")
	f.writeConfig(t, `components:
  - id: demo/hello
    version: 1.0.0
    mode: local
`)
	f.writeOverride(t, `components:
  - id: demo/hello
    mode: debug
    baseline: enabled
`)

	r := runIn(t, f.Dir, "lint", "--strict")

	require.Equal(t, clierr.ExitError, r.code, r.stdout+r.stderr)
}

func TestLintSkipsOverrideCheckWhenFileAbsent(t *testing.T) {
	f := addedProject(t, []comp{{ID: "demo/hello", Version: "1.0.0"}}, "demo/hello@1.0.0")
	f.writeConfig(t, `components:
  - id: demo/hello
    version: 1.0.0
`)

	r := runIn(t, f.Dir, "lint")

	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)
	assert.NotContains(t, r.stdout, "override.yaml")
}

func TestLintPassesOnCleanOverride(t *testing.T) {
	f := addedProject(t, []comp{{ID: "demo/hello", Version: "1.0.0"}}, "demo/hello@1.0.0")
	f.writeConfig(t, `components:
  - id: demo/hello
    version: 1.0.0
`)
	f.writeOverride(t, `components:
  - id: demo/hello
`)

	r := runIn(t, f.Dir, "lint")

	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)
	assert.Contains(t, r.stdout, "override.yaml")
}

// --config 指到非默认文件时，lint 不该检查默认的 override.yaml——评审指出：
// 这里的 override.yaml 对默认 brickkit.yaml 是干净的，但如果拿它去跟
// brickkit.other.yaml（demo/hello 根本没声明）核对，会被误判成悬空引用。
// override.yaml 只对默认文件的运行生效（设计书 §10），lint 不该是例外。
func TestLintIgnoresOverrideWhenConfigPointsElsewhere(t *testing.T) {
	f := addedProject(t, []comp{
		{ID: "demo/hello", Version: "1.0.0"},
		{ID: "demo/caller", Version: "1.0.0"},
	}, "demo/hello@1.0.0", "demo/caller@1.0.0")
	f.writeConfig(t, `components:
  - id: demo/hello
    version: 1.0.0
`)
	f.writeOverride(t, `components:
  - id: demo/hello
`)
	var other strings.Builder
	other.WriteString(configHeader)
	if len(f.Sources) > 0 {
		other.WriteString("\nsources:\n")
		for _, s := range f.Sources {
			other.WriteString(s)
		}
	}
	other.WriteString("\ncomponents:\n  - id: demo/caller\n    version: 1.0.0\n")
	require.NoError(t, os.WriteFile(
		filepath.Join(f.Dir, "brickkit.other.yaml"), []byte(other.String()), 0o644))

	r := runIn(t, f.Dir, "lint", "--config", "brickkit.other.yaml")

	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)
	// 打印"override.yaml 被忽略"这句说明本身合法、也提到了这个词——真正要断的是
	// 它没有被当成 brickkit.other.yaml 的一份文件去检查，不会报"demo/hello 是
	// 悬空引用"这类只有拿错配置文件核对才会出现的错误。
	assert.NotContains(t, r.stdout, "is not one of")
	assert.NotContains(t, r.stdout, "declared components")
}
