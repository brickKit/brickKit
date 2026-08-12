package cli

import (
	"bytes"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/brickkit/brickkit/internal/logging"
)

func TestNewOptionsDefaults(t *testing.T) {
	opts := NewOptions()
	require.NotNil(t, opts)
	assert.Equal(t, DefaultConfigFile, opts.ConfigPath)
	assert.Equal(t, logging.LevelInfo, opts.LogLevel)
	assert.Equal(t, os.Stdout, opts.Stdout)
	assert.Equal(t, os.Stderr, opts.Stderr)
}

// BRICKKIT_LOG_LEVEL 可覆盖默认日志级别。
func TestNewOptionsRespectsEnvLogLevel(t *testing.T) {
	t.Setenv(logging.EnvLogLevel, "debug")
	assert.Equal(t, "debug", NewOptions().LogLevel)
}

// 环境变量为空白字符串时应回落到默认级别，而不是把空串当成级别。
func TestNewOptionsFallsBackOnBlankEnv(t *testing.T) {
	t.Setenv(logging.EnvLogLevel, "   ")
	assert.Equal(t, logging.LevelInfo, NewOptions().LogLevel)
}

func TestOptionsPrintfAndPrintln(t *testing.T) {
	var out bytes.Buffer
	opts := &Options{Stdout: &out, Stderr: &bytes.Buffer{}}

	opts.Printf("✅ 项目已初始化：%s\n", "my-project")
	opts.Println("下一步：brickkit add people/basic@1.0.0")

	assert.Equal(t,
		"✅ 项目已初始化：my-project\n下一步：brickkit add people/basic@1.0.0\n",
		out.String())
}

// NewRootCommand(nil) 应回落到默认选项（只构造不执行，避免写真实 stdout）。
func TestNewRootCommandNilOptionsUsesDefaults(t *testing.T) {
	root := NewRootCommand(nil)
	require.NotNil(t, root)
	assert.Equal(t, "brickkit", root.Name())
	assert.NotEmpty(t, root.Commands())

	f := root.PersistentFlags().Lookup("config")
	require.NotNil(t, f)
	assert.Equal(t, DefaultConfigFile, f.DefValue)
}
