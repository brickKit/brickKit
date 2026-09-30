package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 一次改名要同时改代码里的 msgid.X 与每份目录里的 key，文案一个字节都不动。
func TestApplyRenamesCodeAndCatalogs(t *testing.T) {
	root := t.TempDir()
	write := func(p, s string) {
		require.NoError(t, os.MkdirAll(filepath.Dir(filepath.Join(root, p)), 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(root, p), []byte(s), 0o644))
	}
	write("internal/i18n/locales/en.yaml", "cli.up.local_debugging_local_true: \"Local debugging\"\n")
	write("internal/i18n/locales/zh.yaml", "cli.up.local_debugging_local_true: \"本地调试\"\n")
	write("internal/cli/x.go", "package cli\n\nvar _ = i18n.T(msgid.CliUpLocalDebuggingLocalTrue)\n")

	require.NoError(t, apply(root, []rename{{Old: "cli.up.local_debugging_local_true", New: "cli.up.local_debugging_title"}}))

	assert.Equal(t, "cli.up.local_debugging_title: \"Local debugging\"\n", read(t, root, "internal/i18n/locales/en.yaml"))
	assert.Equal(t, "cli.up.local_debugging_title: \"本地调试\"\n", read(t, root, "internal/i18n/locales/zh.yaml"))
	assert.Contains(t, read(t, root, "internal/cli/x.go"), "msgid.CliUpLocalDebuggingTitle")
}

// 新 key 已经存在：整批拒绝，什么都不写。
func TestApplyRefusesACollisionAndWritesNothing(t *testing.T) {
	root := t.TempDir()
	en := "a.old: \"Old\"\na.new: \"New\"\n"
	code := "package cli\n\nvar _ = i18n.T(msgid.AOld)\n"
	require.NoError(t, os.MkdirAll(filepath.Join(root, "internal/i18n/locales"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(root, "internal/cli"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "internal/i18n/locales/en.yaml"), []byte(en), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "internal/cli/x.go"), []byte(code), 0o644))

	err := apply(root, []rename{{Old: "a.old", New: "a.new"}})
	require.ErrorContains(t, err, "a.new")
	assert.Equal(t, en, read(t, root, "internal/i18n/locales/en.yaml"))
	assert.Equal(t, code, read(t, root, "internal/cli/x.go"))
}

// 有单数形式的 key 改名时，单数那一条（.one）跟着改，否则 TN 在 n == 1 时再也找不到它。
func TestApplyRenamesThePluralOneFormToo(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "internal/i18n/locales"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "internal/i18n/locales/en.yaml"),
		[]byte("count.thing: \"%[1]d things\"\ncount.thing.one: \"%[1]d thing\"\n"), 0o644))

	require.NoError(t, apply(root, []rename{{Old: "count.thing", New: "count.widgets"}}))
	assert.Equal(t, "count.widgets: \"%[1]d things\"\ncount.widgets.one: \"%[1]d thing\"\n",
		read(t, root, "internal/i18n/locales/en.yaml"))
}

func read(t *testing.T, root, p string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(root, p))
	require.NoError(t, err)
	return string(b)
}
