package cli

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/brickkit/brickkit/internal/clierr"
)

// brickkit.yaml 的 mode 从 override.yaml 上次确认时记的 baseline 变了——drift 提示
// 该出现，但不阻断 up（设计书 §7：这是警告级的、非阻断的提示）。
func TestUpPrintsDriftNoteWhenComponentBaselineIsStale(t *testing.T) {
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

	r := runIn(t, f.Dir, "up", "--dry-run")

	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)
	assert.Contains(t, r.stdout, "Drift: demo/hello")
}

func TestUpPrintsDriftNoteWhenTargetBaselineIsStale(t *testing.T) {
	f := addedProject(t, []comp{{ID: "demo/hello", Version: "1.0.0"}}, "demo/hello@1.0.0")
	f.writeConfig(t, `components:
  - id: demo/hello
    version: 1.0.0
`)
	f.writeOverride(t, `target: docker
targetBaseline: k8s
`)

	r := runIn(t, f.Dir, "up", "--dry-run")

	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)
	assert.Contains(t, r.stdout, "Drift: target")
}

func TestUpPrintsNoDriftNoteWhenBaselineMatches(t *testing.T) {
	f := addedProject(t, []comp{{ID: "demo/hello", Version: "1.0.0"}}, "demo/hello@1.0.0")
	f.writeConfig(t, `components:
  - id: demo/hello
    version: 1.0.0
`)
	f.writeOverride(t, `components:
  - id: demo/hello
`)

	r := runIn(t, f.Dir, "up", "--dry-run")

	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)
	assert.NotContains(t, r.stdout, "Drift:")
}
