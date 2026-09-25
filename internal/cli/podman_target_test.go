package cli

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/config"
	"github.com/brickkit/brickkit/internal/engine"
)

// --dry-run 只需要生成一份 compose 文件——engine-agnostic，podman 消费的是
// 同一份 compose.yaml。target: podman 不该拦住它。
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

// 真跑（非 --dry-run）时，target: podman 现在必须真的把工作交给 Podman
// 引擎——005 §7 挪掉的那个 engine.Engine 实现已经回来了。这里注入假引擎，
// 验证的是"分发对了"，不实际调用真 podman 二进制。
func TestUpRealRunSucceedsWithPodmanTarget(t *testing.T) {
	f := addedProject(t, []comp{{ID: "demo/hello", Version: "1.0.0"}}, "demo/hello@1.0.0")
	f.writeConfig(t, `components:
  - id: demo/hello
    version: 1.0.0
`)
	f.writeOverride(t, `target: podman`)
	eng := newFakeEngine()
	eng.name = engine.Podman

	r := runWithEngine(t, eng, f.Dir, "up")

	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)
	assert.NotEmpty(t, eng.ups, "target: podman 时 up 必须真的调用引擎，不能再报'还没实现'")
}

// down 与 status 走的是同一个 resolveEngineFor，同源但独立的调用路径。
func TestDownSucceedsWithPodmanTarget(t *testing.T) {
	f := addedProject(t, []comp{{ID: "demo/hello", Version: "1.0.0"}}, "demo/hello@1.0.0")
	f.writeConfig(t, `components:
  - id: demo/hello
    version: 1.0.0
`)
	f.writeOverride(t, `target: podman`)
	eng := newFakeEngine()
	eng.name = engine.Podman

	r := runWithEngine(t, eng, f.Dir, "down")

	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)
	assert.NotEmpty(t, eng.downs)
}

func TestStatusSucceedsWithPodmanTarget(t *testing.T) {
	f := addedProject(t, []comp{{ID: "demo/hello", Version: "1.0.0"}}, "demo/hello@1.0.0")
	f.writeConfig(t, `components:
  - id: demo/hello
    version: 1.0.0
`)
	f.writeOverride(t, `target: podman`)
	eng := newFakeEngine()
	eng.name = engine.Podman

	r := runWithEngine(t, eng, f.Dir, "status")

	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)
}

// 没有注入假引擎时，target: podman 必须真的分发到 Podman 引擎（Name() 是
// "podman"），而不是当年那个"还没实现"的错误——这是唯一断言"真调用路径
// 选对了引擎"的用例，只检查 resolveEngineFor 的返回值，不调用它的任何方法，
// 所以永远不会真的去 exec 一个 podman 二进制。
func TestResolveEngineForPodmanTargetDispatchesRealEngine(t *testing.T) {
	cfg := &config.Config{Deploy: config.Deploy{Target: config.TargetPodman}}

	eng, err := resolveEngineFor(&Options{}, cfg)

	require.NoError(t, err)
	assert.Equal(t, engine.Podman, eng.Name())
}
