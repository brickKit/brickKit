package cli

import (
	"io"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/userconfig"
)

// Execute 是读取 os.Args 的进程入口。这里替换 os.Args 与 os.Stdout 走一遍真实路径。
// 注意：该测试操作进程级全局状态，不能并行（不加 t.Parallel）。
func TestExecuteReadsOSArgs(t *testing.T) {
	originalArgs := os.Args
	originalStdout := os.Stdout
	defer func() {
		os.Args = originalArgs
		os.Stdout = originalStdout
	}()

	r, w, err := os.Pipe()
	require.NoError(t, err)
	os.Stdout = w

	// 隔离语言解析：这条测试走的是真实 Execute()，会真的调用
	// i18n.Resolve()——不隔离的话，结果取决于跑测试的人自己的机器上
	// 是否设了 BRICKKIT_LANG、或者是否真的执行过 brickkit lang set。
	t.Setenv("BRICKKIT_LANG", "")
	t.Setenv(userconfig.EnvDirOverride, t.TempDir())

	// --log-level off 让 stderr 不产生日志，避免污染测试输出。
	os.Args = []string{"brickkit", "version", "--log-level", "off"}
	code := Execute()

	require.NoError(t, w.Close())
	out, err := io.ReadAll(r)
	require.NoError(t, err)
	require.NoError(t, r.Close())

	assert.Equal(t, clierr.ExitOK, code)
	assert.Contains(t, string(out), "BrickKit CLI v")
	assert.Contains(t, string(out), "Supported deploy targets: docker, k8s")
}
