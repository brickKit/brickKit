package runcmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// 测试里统一用的端口：它出现在 Django / Rails / Spring Boot 的命令与环境变量里。
const testPort = 18080

const (
	mainGo = "package main\n\nfunc main() {}\n"
	libGo  = "package lib\n"
)

// write 在临时目录里按 名字→内容 造出一棵源码树，返回根目录。
func write(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, content := range files {
		path := filepath.Join(dir, filepath.FromSlash(name))
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
		require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
	}
	return dir
}

func makeExecutable(t *testing.T, dir, name string) {
	t.Helper()
	require.NoError(t, os.Chmod(filepath.Join(dir, filepath.FromSlash(name)), 0o755))
}

// detectAs 在指定的目标操作系统下探测（生产代码里 goos 来自 runtime.GOOS）。
func detectAs(dir, goos string, hints Hints) (Command, error) {
	return detect(dir, hints, Params{Port: testPort}, goos)
}

// mustDetect 在 Linux 下探测，期望成功。
func mustDetect(t *testing.T, dir string, hints Hints) Command {
	t.Helper()
	cmd, err := detectAs(dir, "linux", hints)
	require.NoError(t, err)
	return cmd
}

// problemsOf 期望探测以 *NoCommandError 失败，返回它的 Problems。
func problemsOf(t *testing.T, dir string, hints Hints) []Problem {
	t.Helper()
	_, err := detectAs(dir, "linux", hints)
	var nce *NoCommandError
	require.ErrorAs(t, err, &nce)
	return nce.Problems
}
