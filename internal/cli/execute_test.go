package cli

import (
	"io"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/brickkit/brickkit/internal/clierr"
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

	// --log-level off 让 stderr 不产生日志，避免污染测试输出。
	os.Args = []string{"brickkit", "version", "--log-level", "off"}
	code := Execute()

	require.NoError(t, w.Close())
	out, err := io.ReadAll(r)
	require.NoError(t, err)
	require.NoError(t, r.Close())

	assert.Equal(t, clierr.ExitOK, code)
	assert.Contains(t, string(out), "BrickKit CLI v")
	assert.Contains(t, string(out), "支持部署目标：docker, k8s")
}
