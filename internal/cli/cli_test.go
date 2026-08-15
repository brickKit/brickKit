package cli

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/logging"
)

// allCommands 是设计书 004 §3.1 定义的 11 个命令 + version。
var allCommands = []string{
	"init", "add", "remove", "up", "down", "status",
	"order", "sync", "reset", "login", "publish", "version",
}

type result struct {
	stdout string
	stderr string
	code   int
}

// run 在隔离的缓冲区与临时目录上执行一次 CLI。
//
// WorkDir 必须指向临时目录：否则会写到测试进程的当前目录（源码目录）里去。
func run(t *testing.T, args ...string) result {
	t.Helper()
	var out, errBuf bytes.Buffer
	opts := &Options{
		WorkDir:    t.TempDir(),
		ConfigPath: DefaultConfigFile,
		LogLevel:   logging.LevelInfo,
		Stdout:     &out,
		Stderr:     &errBuf,
	}
	code := Run(NewRootCommand(opts), opts, args)
	return result{stdout: out.String(), stderr: errBuf.String(), code: code}
}

// 2.1 brickkit version 输出版本号、支持的 Manifest 版本、部署目标。
func TestVersionCommand(t *testing.T) {
	r := run(t, "version")
	assert.Equal(t, clierr.ExitOK, r.code)
	assert.Contains(t, r.stdout, "BrickKit CLI v")
	assert.Contains(t, r.stdout, "支持 Manifest 版本：brickkit/v1")
	assert.Contains(t, r.stdout, "支持部署目标：docker, k8s")
}

func TestVersionVerboseAddsBuildInfo(t *testing.T) {
	r := run(t, "version", "--verbose")
	assert.Equal(t, clierr.ExitOK, r.code)
	assert.Contains(t, r.stdout, "Git commit：")
	assert.Contains(t, r.stdout, "构建时间：")
}

// 2.2 brickkit --help 列出所有子命令。
func TestRootHelpListsAllCommands(t *testing.T) {
	r := run(t, "--help")
	assert.Equal(t, clierr.ExitOK, r.code)
	for _, name := range allCommands {
		assert.Contains(t, r.stdout, name, "帮助信息应包含子命令 %s", name)
	}
}

func TestNoArgsPrintsHelp(t *testing.T) {
	r := run(t)
	assert.Equal(t, clierr.ExitOK, r.code)
	assert.Contains(t, r.stdout, "用法：")
	for _, name := range allCommands {
		assert.Contains(t, r.stdout, name)
	}
}

// 2.3 未知命令报错，退出码非 0。
func TestUnknownCommandFails(t *testing.T) {
	r := run(t, "nosuchcommand")
	assert.NotEqual(t, clierr.ExitOK, r.code)
	assert.Equal(t, clierr.ExitUsage, r.code)
	assert.Contains(t, r.stderr, "❌")
	assert.Contains(t, r.stderr, "未知命令 nosuchcommand")
	assert.Contains(t, r.stderr, "建议：")
	assert.Empty(t, r.stdout, "错误不应写入 stdout")
}

func TestUnknownFlagFails(t *testing.T) {
	r := run(t, "version", "--nosuchflag")
	assert.Equal(t, clierr.ExitUsage, r.code)
	assert.Contains(t, r.stderr, "❌ 错误：参数不合法")
	assert.Contains(t, r.stderr, "建议：")
}

// 2.4 每个子命令 --help 输出帮助信息，退出码 0。
func TestEachSubcommandHelp(t *testing.T) {
	for _, name := range allCommands {
		t.Run(name, func(t *testing.T) {
			r := run(t, name, "--help")
			assert.Equal(t, clierr.ExitOK, r.code)
			assert.Contains(t, r.stdout, "用法：")
			assert.Contains(t, r.stdout, "brickkit "+name)
			assert.NotEmpty(t, strings.TrimSpace(r.stdout))
		})
	}
}

// 2.5 错误输出格式：包含 ❌ 符号、错误描述、建议。
func TestErrorOutputFormat(t *testing.T) {
	cases := []struct {
		name     string
		args     []string
		wantCode int
		contains []string
	}{
		{
			name:     "init 缺少项目名称",
			args:     []string{"init"},
			wantCode: clierr.ExitUsage,
			contains: []string{"❌ 请指定项目名称：brickkit init <项目名称>"},
		},
		{
			name:     "add 缺少组件",
			args:     []string{"add"},
			wantCode: clierr.ExitUsage,
			contains: []string{"❌ 请指定要添加的组件", "brickkit add <组件ID>@<精确版本>"},
		},
		{
			name:     "remove 缺少组件",
			args:     []string{"remove"},
			wantCode: clierr.ExitUsage,
			contains: []string{"❌ 请指定要移除的组件"},
		},
		{
			name:     "日志级别非法",
			args:     []string{"version", "--log-level", "verbose"},
			wantCode: clierr.ExitUsage,
			contains: []string{"❌ 错误：日志级别不合法", "合法值："},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := run(t, c.args...)
			assert.Equal(t, c.wantCode, r.code)
			for _, want := range c.contains {
				assert.Contains(t, r.stderr, want)
			}
		})
	}
}

// 骨架阶段：未实现的命令给出明确的 NOT_IMPLEMENTED 错误与 Step 编号。
//
// 现在这张表是**空的**——命令树上已经没有未实现的入口了：
// init（Step 3）、reset（Step 8）、add / remove（Step 9）、order（Step 10）、
// up / down / status（Step 15）、sync（Step 17）、login / publish（Step 19）、
// publish --sign（Step 20）。
//
// 表空了也保留这个用例：将来再往命令树上加占位命令时，把它填回来即可，
// 那条"占位必须明确报错、不能假装成功"的约束就还在。
func TestNotImplementedCommands(t *testing.T) {
	cases := map[string][]string{}

	for name, args := range cases {
		t.Run(name, func(t *testing.T) {
			r := run(t, args...)
			assert.Equal(t, clierr.ExitError, r.code)
			assert.Contains(t, r.stderr, "尚未实现")
			assert.Contains(t, r.stderr, "开发计划 Step ")
			assert.Contains(t, r.stderr, "\"error_code\":\"NOT_IMPLEMENTED\"")
		})
	}
}

// 参数个数超限时走 translate 的兜底分支（cobra 的 Args 校验错误）。
func TestTooManyArgsUsesFallbackTranslation(t *testing.T) {
	r := run(t, "init", "a", "b", "c")
	assert.Equal(t, clierr.ExitUsage, r.code)
	assert.Contains(t, r.stderr, "❌ 错误：命令用法不正确")
	assert.Contains(t, r.stderr, "accepts at most 1 arg(s)")
	assert.Contains(t, r.stderr, "建议：")
}

// 警告（⚠️）不阻断、退出码 0，日志级别为 WARN。
// 这条契约由 Step 11 的保留变量冲突检测使用（004 §5.6.1、开发计划 33.15）。
func TestRunRendersWarningWithZeroExit(t *testing.T) {
	var out, errBuf bytes.Buffer
	opts := &Options{ConfigPath: DefaultConfigFile, LogLevel: logging.LevelInfo, Stdout: &out, Stderr: &errBuf}

	root := &cobra.Command{
		Use:           "warn-only",
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return clierr.Warn(clierr.CodeConfigConflict, "配置冲突（警告，不阻断）：").
				WithDetail("配置项", "departmentTreeEndpoint")
		},
	}

	code := Run(root, opts, nil)

	assert.Equal(t, clierr.ExitOK, code, "警告不应改变退出码")
	assert.Contains(t, errBuf.String(), "⚠️ 配置冲突（警告，不阻断）：")
	assert.Contains(t, errBuf.String(), "\"level\":\"WARN\"")
	assert.Contains(t, errBuf.String(), "\"error_code\":\"CONFIG_CONFLICT\"")
	assert.NotContains(t, errBuf.String(), "❌")
}

// 2.6 日志输出为 JSON 格式，包含 time / level / message，且只走 stderr。
func TestLogsAreJSONOnStderr(t *testing.T) {
	r := run(t, "version")
	require.NotEmpty(t, r.stderr, "stderr 应包含 JSON 日志")

	var count int
	for _, line := range strings.Split(strings.TrimSpace(r.stderr), "\n") {
		if line == "" {
			continue
		}
		var entry map[string]any
		require.NoError(t, json.Unmarshal([]byte(line), &entry), "stderr 每行都应是 JSON：%q", line)
		for _, key := range []string{"time", "level", "message"} {
			assert.Contains(t, entry, key)
		}
		count++
	}
	assert.GreaterOrEqual(t, count, 2, "至少包含命令开始与结束两条日志")

	// 人类可读输出与日志严格分流。
	assert.NotContains(t, r.stdout, "{\"time\"")
	assert.NotContains(t, r.stderr, "BrickKit CLI v")
}

func TestLogRecordsExecutedSubcommand(t *testing.T) {
	r := run(t, "version")
	assert.Contains(t, r.stderr, "\"command\":\"brickkit version\"")
}

func TestLogLevelOffSilencesLogs(t *testing.T) {
	r := run(t, "version", "--log-level", "off")
	assert.Equal(t, clierr.ExitOK, r.code)
	assert.Empty(t, r.stderr)
	assert.Contains(t, r.stdout, "BrickKit CLI v")
}

// 全局 flag --config 在所有子命令上都可用（004 §3.5）。
func TestGlobalConfigFlag(t *testing.T) {
	var out, errBuf bytes.Buffer
	opts := &Options{ConfigPath: DefaultConfigFile, LogLevel: logging.LevelInfo, Stdout: &out, Stderr: &errBuf}
	root := NewRootCommand(opts)
	code := Run(root, opts, []string{"version", "--config", "brickkit.prod.yaml"})

	assert.Equal(t, clierr.ExitOK, code)
	assert.Equal(t, "brickkit.prod.yaml", opts.ConfigPath)
	assert.Contains(t, errBuf.String(), "\"config\":\"brickkit.prod.yaml\"")

	for _, name := range allCommands {
		sub := findCommand(root, name)
		require.NotNil(t, sub, "命令树中应存在 %s", name)
		assert.NotNil(t, sub.InheritedFlags().Lookup("config"), "%s 应继承全局 --config", name)
		assert.NotNil(t, sub.InheritedFlags().Lookup("log-level"), "%s 应继承全局 --log-level", name)
	}
}

// 各命令的参数必须与 004 定义一致。
func TestSubcommandFlags(t *testing.T) {
	root := NewRootCommand(&Options{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}})
	want := map[string][]string{
		"add":     {"yes", "refresh", "repo", "repo-all"},
		"up":      {"only", "dry-run", "check-resources"},
		"down":    {"only"},
		"reset":   {"last"},
		"publish": {"path", "visibility"},
		"version": {"verbose"},
	}
	for name, flags := range want {
		sub := findCommand(root, name)
		require.NotNil(t, sub, name)
		for _, f := range flags {
			assert.NotNil(t, sub.Flags().Lookup(f), "brickkit %s 应有 --%s 参数", name, f)
		}
	}
}

func findCommand(root *cobra.Command, name string) *cobra.Command {
	for _, c := range root.Commands() {
		if c.Name() == name {
			return c
		}
	}
	return nil
}
