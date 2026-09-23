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

func TestRemoveComponentFromOverrideDeletesTopLevelEntry(t *testing.T) {
	entries := []override.ComponentOverride{{ID: "demo/hello", Mode: "debug"}, {ID: "demo/caller"}}

	changed, out := removeComponentFromOverride(entries, "demo/hello")

	assert.True(t, changed)
	require.Len(t, out, 1)
	assert.Equal(t, "demo/caller", out[0].ID)
}

func TestRemoveComponentFromOverrideDeletesNestedMember(t *testing.T) {
	entries := []override.ComponentOverride{
		{ID: "erp/backend", Members: []override.MemberOverride{
			{ID: "people/basic", Mode: "disable"},
			{ID: "auth/rbac"},
		}},
	}

	changed, out := removeComponentFromOverride(entries, "people/basic")

	assert.True(t, changed)
	require.Len(t, out, 1)
	require.Len(t, out[0].Members, 1)
	assert.Equal(t, "auth/rbac", out[0].Members[0].ID)
}

func TestRemoveComponentFromOverridePromotesRemainingMembers(t *testing.T) {
	entries := []override.ComponentOverride{
		{ID: "erp/backend", Members: []override.MemberOverride{
			{ID: "people/basic", Mode: "disable", LocalPort: 9001, Baseline: "local"},
		}},
	}

	changed, out := removeComponentFromOverride(entries, "erp/backend")

	assert.True(t, changed)
	require.Len(t, out, 1)
	promoted := out[0]
	assert.Equal(t, "people/basic", promoted.ID)
	assert.Equal(t, "disable", promoted.Mode, "被提升的成员要带着自己原来的覆盖值")
	assert.Equal(t, 9001, promoted.LocalPort)
	assert.Equal(t, "local", promoted.Baseline)
	assert.Empty(t, promoted.Members)
}

func TestRemoveComponentFromOverrideNoOpWhenIDNotPresent(t *testing.T) {
	entries := []override.ComponentOverride{{ID: "demo/hello"}}

	changed, out := removeComponentFromOverride(entries, "demo/gone")

	assert.False(t, changed)
	assert.Equal(t, entries, out)
}

func TestRemoveDeletesLineFromOverrideYaml(t *testing.T) {
	f := addedProject(t, []comp{{ID: "demo/hello", Version: "1.0.0"}}, "demo/hello@1.0.0")
	f.writeConfig(t, `components:
  - id: demo/hello
    version: 1.0.0
`)
	f.writeOverride(t, `components:
  - id: demo/hello
    mode: debug
    localPort: 9001
`)

	r := runIn(t, f.Dir, "remove", "demo/hello")

	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)
	assert.Contains(t, r.stdout, "override.yaml")
	data, err := os.ReadFile(filepath.Join(f.Dir, "override.yaml"))
	require.NoError(t, err)
	assert.NotContains(t, string(data), "demo/hello")
}

// 多版本共存：只删一个版本，这个 ID 在 brickkit.yaml 里还在，override.yaml 的条目
// 不该动——它按 ID 索引，跟版本无关（AGENTS.md §7.1）。
func TestRemoveKeepsOverrideLineWhenOtherVersionRemains(t *testing.T) {
	f := addedProject(t, []comp{
		{ID: "demo/hello", Version: "1.0.0"},
		{ID: "demo/hello", Version: "2.0.0"},
	}, "demo/hello@1.0.0", "demo/hello@2.0.0")
	f.writeConfig(t, `components:
  - id: demo/hello
    version: 1.0.0
  - id: demo/hello
    version: 2.0.0
`)
	f.writeOverride(t, `components:
  - id: demo/hello
    mode: local
`)

	r := runIn(t, f.Dir, "remove", "demo/hello@1.0.0")

	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)
	data, err := os.ReadFile(filepath.Join(f.Dir, "override.yaml"))
	require.NoError(t, err)
	assert.Contains(t, string(data), "- id: demo/hello")
	assert.Contains(t, string(data), "mode: local")
}

// 删掉的是外壳：override.yaml 里嵌在它下面的成员不会被一并删掉（它们本来就还在
// brickkit.yaml 里），而是被提升成顶层条目——注意这里 brickkit.yaml 本身没有声明
// servedBy：remove 只看 override.yaml 自己的嵌套结构，不需要（也不该）反过去确认
// brickkit.yaml 当时是不是真的这样声明过 servedBy——那是漂移检测的事，不是 remove
// 的事，两者刻意分开。
func TestRemoveShellPromotesMembersToTopLevel(t *testing.T) {
	f := addedProject(t, []comp{
		{ID: "infra/shell-go-core", Version: "1.0.0"},
		{ID: "mdm/customer", Version: "1.0.0"},
	}, "infra/shell-go-core@1.0.0", "mdm/customer@1.0.0")
	f.writeConfig(t, `components:
  - id: infra/shell-go-core
    version: 1.0.0
  - id: mdm/customer
    version: 1.0.0
`)
	f.writeOverride(t, `components:
  - id: infra/shell-go-core
    members:
      - id: mdm/customer
        mode: disable
`)

	r := runIn(t, f.Dir, "remove", "infra/shell-go-core")

	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)
	data, err := os.ReadFile(filepath.Join(f.Dir, "override.yaml"))
	require.NoError(t, err)
	text := string(data)
	assert.NotContains(t, text, "infra/shell-go-core")
	assert.Contains(t, text, "- id: mdm/customer")
	assert.Contains(t, text, "mode: disable", "成员自己的覆盖值要原样带过去")
	assert.NotContains(t, text, "members:", "提升之后不该再嵌套在任何外壳下面")
}

func TestRemoveSkipsOverrideCleanupWhenFileAbsent(t *testing.T) {
	f := addedProject(t, []comp{{ID: "demo/hello", Version: "1.0.0"}}, "demo/hello@1.0.0")
	f.writeConfig(t, `components:
  - id: demo/hello
    version: 1.0.0
`)

	r := runIn(t, f.Dir, "remove", "demo/hello")

	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)
	assert.NotContains(t, r.stdout, "override.yaml")
	assert.NoFileExists(t, filepath.Join(f.Dir, "override.yaml"))
}
