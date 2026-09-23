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

// podmanClearErrorAssertions 断的不只是"提到 podman"这个弱断言：真正要紧的是
// 这条报错确实来自 podmanTargetNotImplemented（精确文案，不是随便一句提到
// podman 的报错），以及提示确实指回了 --dry-run 这条真能走通的路。
//
// 不测 error_code JSON 那一行——runIn 用的是 logging.LevelOff（测试默认，图安静），
// AGENTS.md §10 自己也说了 --log-level off 连失败时的 error_code 行都会一起关掉，
// 这是这批测试全文件的既定写法，不是这条测试该单独打破的东西；文案本身已经
// 唯一到能证明走的是这条路径。
func podmanClearErrorAssertions(t *testing.T, stderr string) {
	t.Helper()
	assert.Contains(t, stderr, "there is no Podman engine implementation to actually run it yet",
		"要断在 podmanTargetNotImplemented 的精确文案上，不能只对上 \"podman\" 这几个字母")
	assert.Contains(t, stderr, "--dry-run", "提示得指回一条真能走通的路")
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
	podmanClearErrorAssertions(t, r.stderr)
}

// down 与 status 走的是同一个 resolveEngineFor，同样的"选了 podman，但还没有真实
// engine.Engine"缺口对它们也成立——只测 up 会漏掉这两条同源但独立的调用路径。
func TestDownFailsClearlyWithPodmanTarget(t *testing.T) {
	f := addedProject(t, []comp{{ID: "demo/hello", Version: "1.0.0"}}, "demo/hello@1.0.0")
	f.writeConfig(t, `components:
  - id: demo/hello
    version: 1.0.0
`)
	f.writeOverride(t, `target: podman`)

	r := runIn(t, f.Dir, "down")

	require.Equal(t, clierr.ExitError, r.code, r.stdout+r.stderr)
	podmanClearErrorAssertions(t, r.stderr)
}

func TestStatusFailsClearlyWithPodmanTarget(t *testing.T) {
	f := addedProject(t, []comp{{ID: "demo/hello", Version: "1.0.0"}}, "demo/hello@1.0.0")
	f.writeConfig(t, `components:
  - id: demo/hello
    version: 1.0.0
`)
	f.writeOverride(t, `target: podman`)

	r := runIn(t, f.Dir, "status")

	require.Equal(t, clierr.ExitError, r.code, r.stdout+r.stderr)
	podmanClearErrorAssertions(t, r.stderr)
}
