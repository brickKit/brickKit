package release_test

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/release"
)

func script(t *testing.T, dir, name, body string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, filepath.Dir(name)), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte("#!/bin/sh\n"+body), 0o755))
}

func TestRunChecksRunsEachInTheComponentDirAndStreamsOutput(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell scripts")
	}
	dir := t.TempDir()
	script(t, dir, "scripts/one.sh", "echo one in $(basename $(pwd))\n")
	script(t, dir, "scripts/two.sh", "echo two \"$1\" >&2\n")
	var out, errOut bytes.Buffer
	var ran [][]string
	err := release.RunChecks(dir, "comp", "erp/api@1.0.0",
		[][]string{{"./scripts/one.sh"}, {"scripts/two.sh", "a b"}},
		func(argv []string) { ran = append(ran, argv) }, &out, &errOut)
	require.NoError(t, err)
	assert.Equal(t, [][]string{{"./scripts/one.sh"}, {"scripts/two.sh", "a b"}}, ran)
	assert.Equal(t, "one in "+filepath.Base(dir)+"\n", out.String(), "在组件目录下跑，路径相对组件目录")
	assert.Equal(t, "two a b\n", errOut.String(), "argv 原样传，不经 shell 拆分")
}

// 第一条失败就停：后面的不跑，错误说清楚是哪条、退出码多少。
func TestRunChecksStopsAtTheFirstFailure(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell scripts")
	}
	dir := t.TempDir()
	script(t, dir, "fail.sh", "echo broken >&2\nexit 3\n")
	var ran [][]string
	err := release.RunChecks(dir, "comp", "erp/api@1.0.0",
		[][]string{{"./fail.sh", "--strict"}, {"true"}},
		func(argv []string) { ran = append(ran, argv) }, &bytes.Buffer{}, &bytes.Buffer{})
	require.Error(t, err)
	e := clierr.As(err)
	assert.Equal(t, clierr.CodeReleaseCheckFailed, e.Code)
	out := e.Format()
	assert.Contains(t, out, "erp/api@1.0.0")
	assert.Contains(t, out, "./fail.sh --strict")
	assert.Contains(t, out, "3")
	assert.Len(t, ran, 1, "失败之后的检查不再跑")
}

func TestRunChecksCommandNotFound(t *testing.T) {
	err := release.RunChecks(t.TempDir(), "comp", "erp/api@1.0.0",
		[][]string{{"brickkit-no-such-command-xyz"}}, func([]string) {}, &bytes.Buffer{}, &bytes.Buffer{})
	require.Error(t, err)
	e := clierr.As(err)
	assert.Equal(t, clierr.CodeReleaseCheckFailed, e.Code)
	assert.Contains(t, e.Format(), "brickkit-no-such-command-xyz")
}
