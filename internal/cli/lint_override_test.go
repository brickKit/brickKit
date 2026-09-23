package cli

import (
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
