package cli

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/brickkit/brickkit/internal/clierr"
)

// --dry-run 只需要生成一份 compose 文件——engine-agnostic，podman 消费的是
// 同一份 docker-compose.yaml。target: podman 不该拦住它。
func TestUpDryRunSucceedsWithPodmanTarget(t *testing.T) {
	f := addedProject(t, []comp{{ID: "demo/hello", Version: "1.0.0"}}, "demo/hello@1.0.0")
	f.writeConfig(t, `components:
  - id: demo/hello
    version: 1.0.0
`)
	f.writeOverride(t, `target: podman`)

	r := runIn(t, f.Dir, "up", "--dry-run")

	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)
}

// 真跑（非 --dry-run）时，target: podman 必须清楚地报错，而不是悄悄退回 docker、
// 也不是对着 nil 的 engine.Engine panic——005 §7 挪掉的那份 Podman engine.Engine
// 实现还没有回来（override.yaml 设计书 §11 明确排除在这份计划之外）。
func TestUpRealRunFailsClearlyWithPodmanTarget(t *testing.T) {
	f := addedProject(t, []comp{{ID: "demo/hello", Version: "1.0.0"}}, "demo/hello@1.0.0")
	f.writeConfig(t, `components:
  - id: demo/hello
    version: 1.0.0
`)
	f.writeOverride(t, `target: podman`)

	r := runIn(t, f.Dir, "up")

	require.Equal(t, clierr.ExitError, r.code, r.stdout+r.stderr)
	assert.Contains(t, r.stderr, "podman")
}
