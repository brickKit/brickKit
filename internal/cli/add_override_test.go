package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/override"
)

func TestIsOverrideDefaultOnlyTreatsNilAsDefaultOnly(t *testing.T) {
	assert.True(t, isOverrideDefaultOnly(nil))
}

func TestIsOverrideDefaultOnlyAcceptsBareIDsOnly(t *testing.T) {
	ov := &override.Override{Components: []override.ComponentOverride{
		{ID: "demo/hello"},
		{ID: "erp/backend", Members: []override.MemberOverride{{ID: "people/basic"}}},
	}}
	assert.True(t, isOverrideDefaultOnly(ov))
}

func TestIsOverrideDefaultOnlyRejectsTargetOverride(t *testing.T) {
	ov := &override.Override{Target: "podman"}
	assert.False(t, isOverrideDefaultOnly(ov))
}

func TestIsOverrideDefaultOnlyRejectsComponentMode(t *testing.T) {
	ov := &override.Override{Components: []override.ComponentOverride{{ID: "demo/hello", Mode: "debug"}}}
	assert.False(t, isOverrideDefaultOnly(ov))
}

func TestIsOverrideDefaultOnlyRejectsNestedMemberLocalPort(t *testing.T) {
	ov := &override.Override{Components: []override.ComponentOverride{
		{ID: "erp/backend", Members: []override.MemberOverride{{ID: "people/basic", LocalPort: 9001}}},
	}}
	assert.False(t, isOverrideDefaultOnly(ov))
}

// 新组件要出现在 override.yaml 里——它是穷举式的（AGENTS.md §7.1），新组件没有一行，
// 使用者会以为它没有覆盖开关，而不是"从来没人给它补线"。这份 override.yaml 只有裸
// id 默认行，没有真实覆盖，所以 add 该直接静默重新生成。
func TestAddRegeneratesDefaultOnlyOverrideForNewComponent(t *testing.T) {
	comps := []comp{
		{ID: "demo/hello", Version: "1.0.0"},
		{ID: "demo/caller", Version: "1.0.0", Requires: []string{"demo/hello@1.0.0"}},
	}
	f := addedProject(t, comps, "demo/hello@1.0.0")
	f.writeConfig(t, `components:
  - id: demo/hello
    version: 1.0.0
`)
	f.writeOverride(t, `components:
  - id: demo/hello
`)

	r := runIn(t, f.Dir, "add", "demo/caller@1.0.0", "--yes")

	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)
	data, err := os.ReadFile(filepath.Join(f.Dir, "override.yaml"))
	require.NoError(t, err)
	assert.Contains(t, string(data), "- id: demo/caller")
}

// override.yaml 里有真实覆盖时，add 一个字节都不碰——静默重新生成有真的丢失自定义值的
// 风险（设计书 §9 自己的原话）。只打印提醒，把决定权留给使用者。
func TestAddDoesNotTouchOverrideWithRealCustomizations(t *testing.T) {
	comps := []comp{
		{ID: "demo/hello", Version: "1.0.0"},
		{ID: "demo/caller", Version: "1.0.0", Requires: []string{"demo/hello@1.0.0"}},
	}
	f := addedProject(t, comps, "demo/hello@1.0.0")
	f.writeConfig(t, `components:
  - id: demo/hello
    version: 1.0.0
`)
	f.writeOverride(t, `components:
  - id: demo/hello
    mode: debug
    localPort: 9001
`)

	r := runIn(t, f.Dir, "add", "demo/caller@1.0.0", "--yes")

	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)
	assert.Contains(t, r.stdout, "override.yaml")
	data, err := os.ReadFile(filepath.Join(f.Dir, "override.yaml"))
	require.NoError(t, err)
	text := string(data)
	assert.NotContains(t, text, "demo/caller", "有真实覆盖时不该自动改写")
	assert.Contains(t, text, "mode: debug", "已有的覆盖必须原样保留")
}

// add --local 走的是 add_local.go 里完全不同的一份写配置代码，必须独立确认它也接上了
// 同一个同步逻辑——这不是重新测 syncOverrideAfterAdd 本身（上面两条测试已经测过），
// 是测 runAddLocal 真的调用了它。
func TestAddLocalRegeneratesDefaultOnlyOverride(t *testing.T) {
	comps := []comp{{ID: "demo/hello", Version: "1.0.0"}}
	f := addedProject(t, comps)
	f.writeConfig(t, `components: []
`)
	f.writeOverride(t, `components: []
`)

	r := runIn(t, f.Dir, "add", "--local", "--yes")

	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)
	data, err := os.ReadFile(filepath.Join(f.Dir, "override.yaml"))
	require.NoError(t, err)
	assert.Contains(t, string(data), "- id: demo/hello")
}

// override.yaml 完全不存在时，add 什么都不该做——它是可选机制，"没有覆盖"本身就是
// 完全合法、最常见的状态，不该因为 add 就凭空生出一份文件。
func TestAddDoesNotCreateOverrideWhenNoneExists(t *testing.T) {
	comps := []comp{
		{ID: "demo/hello", Version: "1.0.0"},
	}
	f := addedProject(t, comps)

	r := runIn(t, f.Dir, "add", "demo/hello@1.0.0", "--yes")

	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)
	assert.NoFileExists(t, filepath.Join(f.Dir, "override.yaml"))
}
