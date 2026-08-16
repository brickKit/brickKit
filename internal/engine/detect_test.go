package engine

// 本文件测 `Detect()` 的三个分支（开发计划 34.11 / 34.12）。
//
// 这个函数一直没有测试，而它决定了后面的一切：选错引擎的后果是
// "文件生成了、命令也成功了，只是整个项目跑在了错误的编排器上"
// （Step 16 之前真撞到过一次）。
//
// 它直接调 `exec.LookPath`，没有注入点——所以这里用 **PATH 本身**做夹具：
// 临时目录里放几个假的可执行文件，想让它看见什么就放什么。
// 比加一层注入更贴近真实：`LookPath` 的行为（可执行位、PATH 顺序）
// 也一并被覆盖到了。

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/brickkit/brickkit/internal/clierr"
)

// pathWith 造一个只含指定命令的 PATH。
func pathWith(t *testing.T, names ...string) {
	t.Helper()

	dir := t.TempDir()
	for _, name := range names {
		path := filepath.Join(dir, name)
		require.NoError(t, os.WriteFile(path, []byte("#!/bin/sh\nexit 0\n"), 0o755))
	}
	t.Setenv("PATH", dir)
}

// 有 Docker 就用 Docker。
func TestDetectPrefersDocker(t *testing.T) {
	pathWith(t, "docker", "podman")

	eng, err := Detect()

	require.NoError(t, err)
	assert.Equal(t, Docker, eng.Name(),
		"34.11：两个都在时用 Docker——Podman 那条路已经移除（005 §7）")
}

// 只有 Podman 时**不是**"找不到引擎"，而是"暂不支持 Podman"。
//
// 这两件事该做的下一步完全不同：后者装个 Docker 就好；
// 前者说明问题不在他的机器上，装了也未必有用。
func TestDetectReportsPodmanSpecifically(t *testing.T) {
	pathWith(t, "podman")

	_, err := Detect()

	require.Error(t, err)
	text := clierr.As(err).Format()
	assert.Contains(t, text, "暂不支持 Podman", "34.11：%s", text)
	assert.NotContains(t, text, "没有找到可用的容器引擎",
		"34.11：不能report成笼统的'找不到引擎'——那会让人白装一遍 Docker 之外的东西")
}

// 两个都没有时报"没有找到可用的容器引擎"，并给出不需要引擎的那条出路。
func TestDetectWithoutAnyEngine(t *testing.T) {
	pathWith(t)

	_, err := Detect()

	require.Error(t, err)
	e := clierr.As(err)
	assert.Equal(t, clierr.CodeEngineMissing, e.Code, "34.12")
	text := e.Format()
	assert.Contains(t, text, "没有找到可用的容器引擎", "34.12")
	assert.Contains(t, text, "--dry-run",
		"34.12：生成部署文件不需要任何引擎，这条出路必须给出来——"+
			"否则没装 Docker 的人会以为连看一眼生成物都做不到")
}
