package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/brickkit/brickkit/internal/clierr"
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
