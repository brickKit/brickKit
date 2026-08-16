package engine

// 本文件是 P27 查出来的那个 bug 的回归测试：**命令的 stdout 与 stderr 不能混在一起**。
//
// 现象是这样暴露的：`brickkit up` 走 Podman，容器明明起来了、也 healthy，
// CLI 却报"未创建"。查下去发现 `podman compose` 会往 **stderr** 打一行横幅：
//
//	>>>> Executing external compose provider "/usr/libexec/docker/cli-plugins/docker-compose" ... <<<<
//
// 而我们把两个流合进了同一个缓冲区，于是 `ps --format json` 的输出变成
// "横幅 + JSON"，解析直接失败——**成功的部署被报成了失败**。
//
// 这不是 Podman 特有的。任何在成功路径上往 stderr 写东西的程序都会踩：
// kubectl 的弃用警告就是最常见的一个，它会同样地毁掉 `status` 与
// P38 那个按标签比对的清理逻辑。
//
// 所以规则是：**成功时只要 stdout；失败时才把 stderr 带上**——
// 因为错误信息几乎总在 stderr，报错时丢了它等于什么都没说。

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 成功时 stderr 的噪音不能混进返回值。
func TestRunKeepsStderrOutOfStdout(t *testing.T) {
	out, err := run(context.Background(), "sh", "-c",
		`echo "noise on stderr" >&2; printf '{"clean":"json"}'`)

	require.NoError(t, err)
	assert.Equal(t, `{"clean":"json"}`, string(out),
		"P27：成功时只该拿到 stdout，stderr 的横幅会毁掉后续解析")
}

// 失败时必须带上 stderr——错误信息几乎总在那里。
func TestRunIncludesStderrOnFailure(t *testing.T) {
	out, err := run(context.Background(), "sh", "-c",
		`echo "the real reason" >&2; exit 1`)

	require.Error(t, err)
	assert.Contains(t, string(out), "the real reason",
		"P27：报错时丢掉 stderr，使用者看到的就是一句没有内容的失败")
}

// 端到端复现：横幅 + JSON 的组合必须能解析出状态。
//
// 这条直接模拟 `podman compose ps --format json` 的真实行为，
// 是那个"容器 healthy 却被报成未创建"的最小重现。
func TestStatusParsesDespiteStderrBanner(t *testing.T) {
	out, err := run(context.Background(), "sh", "-c",
		`echo ">>>> Executing external compose provider ... <<<<" >&2; `+
			`printf '{"Service":"demo-hello-2-0-0","State":"running","Health":"healthy"}'`)
	require.NoError(t, err)

	statuses, err := parsePS(out)
	require.NoError(t, err, "P27：横幅混进来就会解析失败")

	require.Len(t, statuses, 1)
	assert.Equal(t, "demo-hello-2-0-0", statuses[0].Service)
	assert.Equal(t, "running", statuses[0].State,
		"P27：容器明明在跑，不能被报成未创建")
}
