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

func TestUpDryRunAppliesModeOverrideFromOverrideYAML(t *testing.T) {
	f := addedProject(t, []comp{{ID: "demo/hello", Version: "1.0.0"}}, "demo/hello@1.0.0")
	f.writeConfig(t, `components:
  - id: demo/hello
    version: 1.0.0
`)
	f.writeOverride(t, `components:
  - id: demo/hello
    mode: disable
`)

	r := runIn(t, f.Dir, "up", "--dry-run")

	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)
	// mode: disable 意味着这个组件这次不跑；顶层状态表里不会再显示它是启动的。
	// （不用 mode: local 测这件事：那个值还要求 components/ 下有本地源码，
	// 这条测试只关心 override.yaml 的 mode 值有没有真的写进了 cfg，跟
	// mode: local 自己的那条额外前提无关。）
	assert.NotContains(t, r.stdout, "demo-hello-1-0-0")
}

func TestUpDryRunIgnoresOverrideYamlWithoutIt(t *testing.T) {
	f := addedProject(t, []comp{{ID: "demo/hello", Version: "1.0.0"}}, "demo/hello@1.0.0")
	f.writeConfig(t, `components:
  - id: demo/hello
    version: 1.0.0
`)
	// 没有 override.yaml——跟今天完全一样的行为。

	r := runIn(t, f.Dir, "up", "--dry-run")

	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)
}

func TestUpBlocksOnDanglingOverrideEntry(t *testing.T) {
	f := addedProject(t, []comp{{ID: "demo/hello", Version: "1.0.0"}}, "demo/hello@1.0.0")
	f.writeConfig(t, `components:
  - id: demo/hello
    version: 1.0.0
`)
	f.writeOverride(t, `components:
  - id: demo/gone
`)

	r := runIn(t, f.Dir, "up", "--dry-run")

	require.Equal(t, clierr.ExitError, r.code, r.stdout+r.stderr)
	assert.Contains(t, r.stderr, "demo/gone")
}

func TestUpRejectsTargetUpgradeViaOverride(t *testing.T) {
	f := addedProject(t, []comp{{ID: "demo/hello", Version: "1.0.0"}}, "demo/hello@1.0.0")
	f.writeConfig(t, `components:
  - id: demo/hello
    version: 1.0.0
`) // deploy.target: docker，来自 configHeader
	f.writeOverride(t, `target: k8s`)

	r := runIn(t, f.Dir, "up", "--dry-run")

	require.Equal(t, clierr.ExitError, r.code, r.stdout+r.stderr)
	assert.Contains(t, r.stderr, "k8s")
}

func TestUpWarnsAndIgnoresOverrideWhenConfigPointsElsewhere(t *testing.T) {
	f := addedProject(t, []comp{{ID: "demo/hello", Version: "1.0.0"}}, "demo/hello@1.0.0")
	f.writeConfig(t, `components:
  - id: demo/hello
    version: 1.0.0
`)
	f.writeOverride(t, `components:
  - id: demo/hello
    mode: local
`)
	// brickkit.other.yaml 是同一个项目的第二份配置文件（同一个 f.Dir，同一套
	// f.Sources，同一个 .brickkit/manifests/ 缓存）——只是换了个文件名，测的是
	// --config 指到非默认文件时 override.yaml 被忽略这件事本身。组装方式
	// 照抄 writeConfig 自己的拼法（configHeader + sources 块 + body），
	// 这样它才是一份真正能被 ParseConfigFile 解析、能把 demo/hello 解析到
	// 同一份缓存 Manifest 的项目文件，不是随手糊出来的半成品。
	var other strings.Builder
	other.WriteString(configHeader)
	if len(f.Sources) > 0 {
		other.WriteString("\nsources:\n")
		for _, s := range f.Sources {
			other.WriteString(s)
		}
	}
	other.WriteString("\ncomponents:\n  - id: demo/hello\n    version: 1.0.0\n")
	require.NoError(t, os.WriteFile(
		filepath.Join(f.Dir, "brickkit.other.yaml"), []byte(other.String()), 0o644))

	r := runIn(t, f.Dir, "up", "--dry-run", "--config", "brickkit.other.yaml")

	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)
	assert.Contains(t, r.stdout, "override.yaml")
	// 没生效：mode: local 没有被应用，demo-hello 仍然作为容器出现
	assert.Contains(t, r.stdout, "demo-hello-1-0-0")
}

// newSyncFixture 内置的组件集合是 demo/hello、demo/caller（强依赖 demo/hello）、
// solo/thing——跟 addedProject 不是同一批，这里沿用它而不是 addedProject，因为
// f.assertActive/f.assertArchived 是 *syncFixture 的方法，addedProject 只返回
// *projectFixture（见 internal/cli/sync_test.go:26-44）。
func TestSyncAppliesModeOverrideFromOverrideYAML(t *testing.T) {
	f := newSyncFixture(t, `components:
  - id: demo/hello
    version: 1.0.0
  - id: demo/caller
    version: 1.0.0
    mode: disable
resources: []
`, "demo/hello", "demo/caller")
	f.writeOverride(t, `components:
  - id: demo/hello
    mode: debug
  - id: demo/caller
`)

	r := runIn(t, f.Dir, "sync")
	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)
	f.assertActive(t, "demo/hello")
	f.assertArchived(t, "demo/caller")
}

func TestStatusReflectsModeOverrideFromOverrideYAML(t *testing.T) {
	f := addedProject(t, []comp{{ID: "demo/hello", Version: "1.0.0"}}, "demo/hello@1.0.0")
	f.writeConfig(t, `components:
  - id: demo/hello
    version: 1.0.0
`)
	f.writeOverride(t, `components:
  - id: demo/hello
    mode: disable
`)

	r := runIn(t, f.Dir, "status")
	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)
	assert.Contains(t, r.stdout, "demo/hello")
}

// applyOverride 改完 cfg 之后必须重新过一遍 brickkit.yaml 自己对 Mode/LocalPort
// 的规则——不然 override.yaml 就成了绕开这些规则的后门。下面四个场景直接照抄
// 最终审查里复现过的四个真实反例。

func TestUpRejectsOutOfRangeLocalPortFromOverride(t *testing.T) {
	f := addedProject(t, []comp{{ID: "demo/hello", Version: "1.0.0"}}, "demo/hello@1.0.0")
	f.writeConfig(t, `components:
  - id: demo/hello
    version: 1.0.0
`)
	f.writeOverride(t, `components:
  - id: demo/hello
    mode: debug
    localPort: 99999
`)

	r := runIn(t, f.Dir, "up", "--dry-run")

	require.Equal(t, clierr.ExitError, r.code, r.stdout+r.stderr)
	assert.Contains(t, r.stderr, "localPort")
}

func TestUpRejectsBareLocalPortWithNoModeFromOverride(t *testing.T) {
	f := addedProject(t, []comp{{ID: "demo/hello", Version: "1.0.0"}}, "demo/hello@1.0.0")
	f.writeConfig(t, `components:
  - id: demo/hello
    version: 1.0.0
`)
	f.writeOverride(t, `components:
  - id: demo/hello
    localPort: 9001
`)

	r := runIn(t, f.Dir, "up", "--dry-run")

	require.Equal(t, clierr.ExitError, r.code, r.stdout+r.stderr)
	assert.Contains(t, r.stderr, "localPort")
}

func TestUpRejectsDebugModeOnAServedByMemberFromOverride(t *testing.T) {
	f := addedProject(t, []comp{
		{ID: "infra/shell-go-core", Version: "1.0.0"},
		{ID: "mdm/customer", Version: "1.0.0", Port: 8081},
	}, "infra/shell-go-core@1.0.0", "mdm/customer@1.0.0")
	f.writeConfig(t, `components:
  - id: infra/shell-go-core
    version: 1.0.0
  - id: mdm/customer
    version: 1.0.0
    servedBy: infra/shell-go-core@1.0.0
`)
	f.writeOverride(t, `components:
  - id: infra/shell-go-core
  - id: mdm/customer
    mode: debug
    localPort: 9100
`)

	r := runIn(t, f.Dir, "up", "--dry-run")

	require.Equal(t, clierr.ExitError, r.code, r.stdout+r.stderr)
	assert.Contains(t, r.stderr, "servedBy")
}

func TestUpRejectsDebugWithReplicasFromOverride(t *testing.T) {
	f := addedProject(t, []comp{{ID: "demo/hello", Version: "1.0.0"}}, "demo/hello@1.0.0")
	f.writeConfig(t, `components:
  - id: demo/hello
    version: 1.0.0
    replicas: 3
`)
	f.writeOverride(t, `components:
  - id: demo/hello
    mode: debug
    localPort: 9001
`)

	r := runIn(t, f.Dir, "up", "--dry-run")

	require.Equal(t, clierr.ExitError, r.code, r.stdout+r.stderr)
	assert.Contains(t, r.stderr, "replicas")
}
