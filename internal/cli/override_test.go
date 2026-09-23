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

func TestOverrideCreatesFileOnFirstRun(t *testing.T) {
	f := addedProject(t, []comp{
		{ID: "demo/hello", Version: "1.0.0"},
		{ID: "demo/caller", Version: "1.0.0", Requires: []string{"demo/hello@1.0.0"}},
	}, "demo/hello@1.0.0", "demo/caller@1.0.0")
	f.writeConfig(t, `components:
  - id: demo/hello
    version: 1.0.0
  - id: demo/caller
    version: 1.0.0
`)

	r := runIn(t, f.Dir, "override")

	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)
	path := filepath.Join(f.Dir, "override.yaml")
	require.FileExists(t, path)

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Contains(t, string(data), "- id: demo/hello")
	assert.Contains(t, string(data), "- id: demo/caller")
}

func TestOverrideAddsFileToGitignore(t *testing.T) {
	f := addedProject(t, []comp{{ID: "demo/hello", Version: "1.0.0"}}, "demo/hello@1.0.0")
	f.writeConfig(t, `components:
  - id: demo/hello
    version: 1.0.0
`)

	r := runIn(t, f.Dir, "override")
	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)

	data, err := os.ReadFile(filepath.Join(f.Dir, ".gitignore"))
	require.NoError(t, err)
	assert.Contains(t, string(data), "override.yaml")
}

func TestOverrideNestsMembersUnderTheirShell(t *testing.T) {
	f := addedProject(t, []comp{
		{ID: "infra/shell-go-core", Version: "1.0.0"},
		{ID: "mdm/customer", Version: "1.0.0"},
	}, "infra/shell-go-core@1.0.0", "mdm/customer@1.0.0")
	f.writeConfig(t, `components:
  - id: infra/shell-go-core
    version: 1.0.0
  - id: mdm/customer
    version: 1.0.0
    servedBy: infra/shell-go-core@1.0.0
`)

	r := runIn(t, f.Dir, "override")
	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)

	data, err := os.ReadFile(filepath.Join(f.Dir, "override.yaml"))
	require.NoError(t, err)
	text := string(data)
	assert.Contains(t, text, "- id: infra/shell-go-core")
	assert.Contains(t, text, "members:")
	assert.Contains(t, text, "- id: mdm/customer")
}

func TestOverrideRefreshPreservesExistingCustomizations(t *testing.T) {
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

	r := runIn(t, f.Dir, "override")
	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)

	data, err := os.ReadFile(filepath.Join(f.Dir, "override.yaml"))
	require.NoError(t, err)
	text := string(data)
	assert.Contains(t, text, "mode: debug")
	assert.Contains(t, text, "9001")
}

func TestOverrideRefreshAddsLineForNewComponent(t *testing.T) {
	f := addedProject(t, []comp{{ID: "demo/hello", Version: "1.0.0"}}, "demo/hello@1.0.0")
	f.writeConfig(t, `components:
  - id: demo/hello
    version: 1.0.0
`)
	f.writeOverride(t, `components:
  - id: demo/hello
`)
	f.writeConfig(t, `components:
  - id: demo/hello
    version: 1.0.0
  - id: demo/caller
    version: 1.0.0
`)

	r := runIn(t, f.Dir, "override")
	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)

	data, err := os.ReadFile(filepath.Join(f.Dir, "override.yaml"))
	require.NoError(t, err)
	assert.Contains(t, string(data), "- id: demo/caller")
}

func TestOverrideRefreshDropsLineForRemovedComponent(t *testing.T) {
	f := addedProject(t, []comp{{ID: "demo/hello", Version: "1.0.0"}}, "demo/hello@1.0.0")
	f.writeConfig(t, `components:
  - id: demo/hello
    version: 1.0.0
`)
	f.writeOverride(t, `components:
  - id: demo/hello
  - id: demo/gone
`)

	r := runIn(t, f.Dir, "override")
	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)

	data, err := os.ReadFile(filepath.Join(f.Dir, "override.yaml"))
	require.NoError(t, err)
	assert.NotContains(t, string(data), "demo/gone")
}

// 多版本共存：一个组件 ID 在 brickkit.yaml 里出现两次（不同版本）时，
// override.yaml 只该生成一条覆盖条目——它按 ID 索引，不认版本（设计书 §8：
// "没有版本号，一个 ID 一行"），applyOverride 本来就是按 ID 匹配、对所有版本
// 一视同仁。生成两条同 ID 的条目，写下去的文件下一次读回来就会因为
// "重复组件 ID" 校验报错——生成命令自己把 override.yaml 写坏了。
func TestOverrideDedupesMultiVersionComponentIntoOneEntry(t *testing.T) {
	comps := []comp{
		{ID: "demo/hello", Version: "1.0.0"},
		{ID: "demo/hello", Version: "2.0.0"},
	}
	f := addedProject(t, comps, "demo/hello@1.0.0", "demo/hello@2.0.0")
	f.writeConfig(t, `components:
  - id: demo/hello
    version: 1.0.0
  - id: demo/hello
    version: 2.0.0
`)

	r := runIn(t, f.Dir, "override")
	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)

	ov, err := override.ParseOverrideFile(filepath.Join(f.Dir, "override.yaml"))
	require.NoError(t, err, "override.yaml 应该能被自己写出来的文件解析回去，不该报重复组件 ID")
	require.Len(t, ov.Components, 1, "同一个组件 ID 的多个版本只该生成一条覆盖条目")
	assert.Equal(t, "demo/hello", ov.Components[0].ID)
}

// 同一个组件 ID，一个版本被外壳收编、另一个版本独立部署（真实场景：
// resolver_edge_test.go 的 TestServingShellIDMatchesExactVersionOnly）——
// 嵌进外壳的 members 会造成"这个组件只属于这个外壳"的错误印象，而它明明
// 还有一个版本是独立跑的，所以该按 standalone 处理，不嵌套。
func TestOverridePlacesStandaloneWhenAnyVersionIsStandalone(t *testing.T) {
	comps := []comp{
		{ID: "infra/shell-go-core", Version: "1.0.0"},
		{ID: "mdm/customer", Version: "1.0.7", Port: 8081},
		{ID: "mdm/customer", Version: "2.0.0", Port: 8082},
	}
	f := addedProject(t, comps,
		"infra/shell-go-core@1.0.0", "mdm/customer@1.0.7", "mdm/customer@2.0.0")
	f.writeConfig(t, `components:
  - id: infra/shell-go-core
    version: 1.0.0
  - id: mdm/customer
    version: 1.0.7
    servedBy: infra/shell-go-core@1.0.0
  - id: mdm/customer
    version: 2.0.0
`)

	r := runIn(t, f.Dir, "override")
	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)

	ov, err := override.ParseOverrideFile(filepath.Join(f.Dir, "override.yaml"))
	require.NoError(t, err, r.stdout+r.stderr)
	require.Len(t, ov.Components, 2)

	var customer *override.ComponentOverride
	for i := range ov.Components {
		if ov.Components[i].ID == "mdm/customer" {
			customer = &ov.Components[i]
		}
		assert.Empty(t, ov.Components[i].Members, "mdm/customer 不该被嵌进任何外壳的 members 下面")
	}
	require.NotNil(t, customer, "mdm/customer 该是顶层条目")
}

func TestOverridePrintsDriftNote(t *testing.T) {
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

	r := runIn(t, f.Dir, "override")
	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)
	assert.Contains(t, r.stdout, "demo/hello")
}
