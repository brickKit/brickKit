package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/logging"
	"github.com/brickkit/brickkit/internal/userconfig"
)

// allCommands 是 CLI 的全部命令 + version。
var allCommands = []string{
	"init", "new", "add", "remove", "up", "down", "status",
	"fetch", "sync", "restore", "login", "publish", "version", "graph", "lint",
}

type result struct {
	stdout string
	stderr string
	code   int
}

// run 在隔离的缓冲区与临时目录上执行一次 CLI。
//
// WorkDir 必须指向临时目录：否则会写到测试进程的当前目录（源码目录）里去。
//
// 顺手隔离全局语言配置目录：NewRootCommand 每次调用都会重新解析语言
// （见 root.go），如果不隔离，测试结果会取决于跑测试的人自己的机器上
// 是否真的执行过 brickkit lang set。只在调用方还没有自己设置过的情况下
// 才覆盖——需要跨多次 run() 调用验证"设置后持久生效"的测试，可以在
// 调用 run() 之前自己先 t.Setenv(userconfig.EnvDirOverride, ...)，
// 这里就不会覆盖掉。
func run(t *testing.T, args ...string) result {
	t.Helper()
	if os.Getenv(userconfig.EnvDirOverride) == "" {
		t.Setenv(userconfig.EnvDirOverride, t.TempDir())
	}
	var out, errBuf bytes.Buffer
	opts := &Options{
		WorkDir:  t.TempDir(),
		LogLevel: logging.LevelInfo,
		Stdout:   &out,
		Stderr:   &errBuf,
		// git 源的仓库缓存放在测试自己的临时目录，绝不碰使用者的 ~/.cache
		RepoCacheDir: t.TempDir(),
		Images:       everyImagePresent{},
	}
	code := Run(NewRootCommand(opts), opts, args)
	return result{stdout: out.String(), stderr: errBuf.String(), code: code}
}

// brickkit version 输出版本号、支持的 Manifest 版本、部署目标。
func TestVersionCommand(t *testing.T) {
	r := run(t, "version")
	assert.Equal(t, clierr.ExitOK, r.code)
	assert.Contains(t, r.stdout, "BrickKit CLI v")
	assert.Contains(t, r.stdout, "Supported Manifest version: brickkit/v1")
	assert.Contains(t, r.stdout, "Supported deploy targets: docker, podman, k8s")
}

func TestVersionVerboseAddsBuildInfo(t *testing.T) {
	r := run(t, "version", "--verbose")
	assert.Equal(t, clierr.ExitOK, r.code)
	assert.Contains(t, r.stdout, "Git commit:")
	assert.Contains(t, r.stdout, "Build date:")
}

// brickkit --help 列出所有子命令。
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
	assert.Contains(t, r.stdout, "Usage:")
	for _, name := range allCommands {
		assert.Contains(t, r.stdout, name)
	}
}

func TestRootHelpIsLocalizedWhenBrickkitLangIsZH(t *testing.T) {
	t.Setenv("BRICKKIT_LANG", "zh")
	r := run(t, "--help")
	assert.Equal(t, clierr.ExitOK, r.code)
	assert.Contains(t, r.stdout, "用法：")
}

// 未知命令报错，退出码非 0。
func TestUnknownCommandFails(t *testing.T) {
	r := run(t, "nosuchcommand")
	assert.NotEqual(t, clierr.ExitOK, r.code)
	assert.Equal(t, clierr.ExitUsage, r.code)
	assert.Contains(t, r.stderr, "❌")
	assert.Contains(t, r.stderr, "unknown command nosuchcommand")
	assert.Contains(t, r.stderr, "Suggestion:")
	assert.Empty(t, r.stdout, "错误不应写入 stdout")
}

func TestUnknownFlagFails(t *testing.T) {
	r := run(t, "version", "--nosuchflag")
	assert.Equal(t, clierr.ExitUsage, r.code)
	assert.Contains(t, r.stderr, "❌ Error: invalid arguments")
	assert.Contains(t, r.stderr, "Suggestion:")
}

// 每个子命令 --help 输出帮助信息，退出码 0。
func TestEachSubcommandHelp(t *testing.T) {
	for _, name := range allCommands {
		t.Run(name, func(t *testing.T) {
			r := run(t, name, "--help")
			assert.Equal(t, clierr.ExitOK, r.code)
			assert.Contains(t, r.stdout, "Usage:")
			assert.Contains(t, r.stdout, "brickkit "+name)
			assert.NotEmpty(t, strings.TrimSpace(r.stdout))
		})
	}
}

// 错误输出格式：包含 ❌ 符号、错误描述、建议。
func TestErrorOutputFormat(t *testing.T) {
	cases := []struct {
		name     string
		args     []string
		wantCode int
		contains []string
	}{
		{
			name:     "init 同时给了参数与 --name",
			args:     []string{"init", "a", "--name", "b"},
			wantCode: clierr.ExitUsage,
			contains: []string{"❌ Error: give the project name either as the argument or with --name, not both"},
		},
		{
			name:     "日志级别非法",
			args:     []string{"version", "--log-level", "verbose"},
			wantCode: clierr.ExitUsage,
			contains: []string{"❌ Error: invalid log level", "Valid values:"},
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

// 参数个数超限时走 translate 的兜底分支（cobra 的 Args 校验错误）。
func TestTooManyArgsUsesFallbackTranslation(t *testing.T) {
	r := run(t, "init", "a", "b", "c")
	assert.Equal(t, clierr.ExitUsage, r.code)
	assert.Contains(t, r.stderr, "❌ Error: incorrect command usage")
	assert.Contains(t, r.stderr, "accepts at most 1 arg(s)")
	assert.Contains(t, r.stderr, "Suggestion:")
}

// 警告（⚠️）不阻断、退出码 0，日志级别为 WARN。
// 这条契约由保留变量冲突检测使用。
func TestRunRendersWarningWithZeroExit(t *testing.T) {
	var out, errBuf bytes.Buffer
	opts := &Options{LogLevel: logging.LevelInfo, Stdout: &out, Stderr: &errBuf}

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

// 日志输出为 JSON 格式，包含 time / level / message，且只走 stderr。
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

// --config 与 up/down --context 随三层文件重构删除：部署文件用 -f 选，
// 集群用部署文件里的 k8s.context 选。写了就是未知参数，不能静默忽略。
func TestRemovedFlagsRejected(t *testing.T) {
	for _, args := range [][]string{
		{"version", "--config", "brickkit.prod.yaml"},
		{"up", "--context", "prod"},
		{"down", "--context", "prod"},
	} {
		r := run(t, args...)
		assert.Equal(t, clierr.ExitUsage, r.code, "%v 应被拒绝：%s", args, r.stderr)
		assert.Contains(t, r.stderr, "unknown flag", "%v", args)
	}
}

// -f / --no-local 只挂在读部署文件的命令上；--log-level 所有命令都继承。
func TestDeployFileFlags(t *testing.T) {
	opts := &Options{LogLevel: logging.LevelInfo, Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}}
	root := NewRootCommand(opts)
	for _, name := range []string{"up", "down", "status", "sync", "lint"} {
		sub := findCommand(root, name)
		require.NotNil(t, sub, name)
		assert.NotNil(t, sub.Flags().ShorthandLookup("f"), "%s 应有 -f", name)
		assert.NotNil(t, sub.Flags().Lookup("no-local"), "%s 应有 --no-local", name)
	}
	graph := findCommand(root, "graph")
	assert.NotNil(t, graph.Flags().ShorthandLookup("f"), "graph 应有 -f")
	assert.Nil(t, graph.Flags().Lookup("no-local"), "graph 从不读本地模式，--no-local 没有意义")
	for _, name := range allCommands {
		sub := findCommand(root, name)
		require.NotNil(t, sub, "命令树中应存在 %s", name)
		assert.NotNil(t, sub.InheritedFlags().Lookup("log-level"), "%s 应继承全局 --log-level", name)
	}
}

// 各命令的参数必须与命令参考（docs/*/07-cli-reference）写的一致。
func TestSubcommandFlags(t *testing.T) {
	root := NewRootCommand(&Options{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}})
	want := map[string][]string{
		"up":      {"dry-run", "file", "no-local"},
		"down":    {"file", "no-local"},
		"restore": {"check"},
		"publish": {"path", "visibility"},
		"version": {"verbose"},
		"graph":   {"ignore-shells"},
		"lint":    {"strict"},
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

// 根帮助是每个人最先读到的一屏：它必须讲三层文件，而不是只提 brickkit.yaml
// （从前写着"管理项目配置（brickkit.yaml）"，那是单文件时代的说法）。
// 帮助文本讲的必须是现在的行为：up 从不构建、也生成 K8s 清单并托管 mode: local；status 也查
// Podman 与 K8s；fetch 的产物落在不进 Git 的 .brickkit/ 下；示例里不写一个并不存在的公共市场。
func TestHelpTextsDescribeCurrentBehavior(t *testing.T) {
	up := run(t, "up", "--help").stdout
	for _, want := range []string{"brickkit build", "Kubernetes", "mode: local", "mode: debug"} {
		assert.Contains(t, up, want, "brickkit up --help")
	}
	for _, gone := range []string{"docker login", "compatibility check"} {
		assert.NotContains(t, up, gone, "brickkit up --help")
	}
	status := run(t, "status", "--help").stdout
	for _, want := range []string{"podman compose", "kubectl"} {
		assert.Contains(t, status, want, "brickkit status --help")
	}
	fetch := run(t, "fetch", "--help").stdout
	assert.NotContains(t, fetch, "committed with the project")
	assert.Contains(t, fetch, "not committed")
	assert.NotContains(t, run(t, "login", "--help").stdout, "brickkit.io")
}

func TestRootHelpDescribesTheThreeLayers(t *testing.T) {
	r := run(t, "--help")
	require.Equal(t, clierr.ExitOK, r.code)
	for _, want := range []string{"brickkit.yaml", "deploy.yaml", "deploy.local.yaml", "config/"} {
		assert.Contains(t, r.stdout, want)
	}
	for _, short := range []struct{ cmd, want string }{
		{"add", "three layers"},
		{"lint", "deploy files"},
		{"remove", "archive"},
	} {
		h := run(t, short.cmd, "--help")
		assert.Contains(t, r.stdout+h.stdout, short.want, "brickkit %s 的一句话简介", short.cmd)
	}
}
