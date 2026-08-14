package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"

	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/logging"
)

// runIn 在指定目录下执行一次 CLI（不依赖进程 cwd，便于并行与隔离）。
func runIn(t *testing.T, dir string, args ...string) result {
	t.Helper()
	return runWith(t, nil, dir, args...)
}

// runWith 在执行前允许调整全局选项（注入假引擎等）。
func runWith(t *testing.T, tweak func(*Options), dir string, args ...string) result {
	t.Helper()
	var out, errBuf bytes.Buffer
	opts := &Options{
		WorkDir:    dir,
		ConfigPath: DefaultConfigFile,
		LogLevel:   logging.LevelOff,
		Stdout:     &out,
		Stderr:     &errBuf,
	}
	if tweak != nil {
		tweak(opts)
	}
	code := Run(NewRootCommand(opts), opts, args)
	return result{stdout: out.String(), stderr: errBuf.String(), code: code}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	require.NoError(t, err, "读取 %s", path)
	return string(b)
}

func requireDir(t *testing.T, path string) {
	t.Helper()
	info, err := os.Stat(path)
	require.NoError(t, err, "目录应存在：%s", path)
	assert.True(t, info.IsDir(), "%s 应是目录", path)
}

// 3.1 brickkit init 不传参数时报错。
func TestInitWithoutProjectNameFails(t *testing.T) {
	dir := t.TempDir()
	r := runIn(t, dir, "init")

	assert.NotEqual(t, clierr.ExitOK, r.code)
	assert.Contains(t, r.stderr, "❌ 请指定项目名称：brickkit init <项目名称>")

	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	assert.Empty(t, entries, "报错时不应创建任何文件")
}

// 3.2 / 3.5 / 3.10 成功创建完整目录结构。
func TestInitCreatesProjectStructure(t *testing.T) {
	dir := t.TempDir()
	r := runIn(t, dir, "init", "my-project")
	require.Equal(t, clierr.ExitOK, r.code, "stderr=%s", r.stderr)

	assert.FileExists(t, filepath.Join(dir, "brickkit.yaml"))
	for _, sub := range []string{
		".brickkit",
		".brickkit/backup",
		".brickkit/manifests",
		".brickkit/artifacts",
		".brickkit/generated",
		"components",
		"components/.archived",
	} {
		requireDir(t, filepath.Join(dir, sub))
	}
}

// 3.3 brickkit.yaml 骨架内容正确。
func TestInitConfigSkeletonContent(t *testing.T) {
	dir := t.TempDir()
	require.Equal(t, clierr.ExitOK, runIn(t, dir, "init", "my-project").code)

	raw := readFile(t, filepath.Join(dir, "brickkit.yaml"))

	var doc map[string]any
	require.NoError(t, yaml.Unmarshal([]byte(raw), &doc), "骨架必须是合法 YAML")

	assert.Equal(t, "my-project", doc["project"])
	assert.Equal(t, map[string]any{"target": "docker"}, doc["deploy"])
	require.Contains(t, doc, "components")
	require.Contains(t, doc, "resources")
	assert.Empty(t, doc["components"], "components 初始为空列表")
	assert.Empty(t, doc["resources"], "resources 初始为空列表")

	// 骨架应带注释，说明文件用途（003 §2）。
	assert.Contains(t, raw, "# brickkit.yaml")
}

// 3.4 .brickkit/backup/brickkit.yaml.initial 存在且与 brickkit.yaml 一致。
func TestInitCreatesInitialBackup(t *testing.T) {
	dir := t.TempDir()
	require.Equal(t, clierr.ExitOK, runIn(t, dir, "init", "my-project").code)

	config := readFile(t, filepath.Join(dir, "brickkit.yaml"))
	backup := readFile(t, filepath.Join(dir, ".brickkit/backup/brickkit.yaml.initial"))
	assert.Equal(t, config, backup, "初始备份内容应与 brickkit.yaml 完全一致")
}

// 3.6 .gitignore 包含 components/ 等必要条目。
func TestInitCreatesGitignore(t *testing.T) {
	dir := t.TempDir()
	require.Equal(t, clierr.ExitOK, runIn(t, dir, "init", "my-project").code)

	content := readFile(t, filepath.Join(dir, ".gitignore"))
	for _, entry := range []string{
		"components/",
		".brickkit/generated/",
		".brickkit/backup/",
		".brickkit/credentials",
		".env",
	} {
		assert.Contains(t, content, entry, ".gitignore 应包含 %s", entry)
	}
}

// 已有 .gitignore 时应追加而不是覆盖，且不重复已有条目。
func TestInitAppendsToExistingGitignore(t *testing.T) {
	dir := t.TempDir()
	existing := "# 我自己的规则\n*.log\ncomponents/\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".gitignore"), []byte(existing), 0o644))

	require.Equal(t, clierr.ExitOK, runIn(t, dir, "init", "my-project").code)

	content := readFile(t, filepath.Join(dir, ".gitignore"))
	assert.Contains(t, content, "*.log", "原有内容必须保留")
	assert.Contains(t, content, "# 我自己的规则")
	assert.Contains(t, content, ".brickkit/credentials")

	var occurrences int
	for _, line := range strings.Split(content, "\n") {
		if strings.TrimSpace(line) == "components/" {
			occurrences++
		}
	}
	assert.Equal(t, 1, occurrences, "已有条目 components/ 不应被重复追加")
}

// 3.7 / 3.12 重复 init（或目录已有 brickkit.yaml）时报错，且不破坏已有配置。
func TestInitTwiceFails(t *testing.T) {
	dir := t.TempDir()
	require.Equal(t, clierr.ExitOK, runIn(t, dir, "init", "my-project").code)
	before := readFile(t, filepath.Join(dir, "brickkit.yaml"))

	r := runIn(t, dir, "init", "other-project")

	assert.NotEqual(t, clierr.ExitOK, r.code)
	assert.Contains(t, r.stderr, "❌")
	assert.Contains(t, r.stderr, "已存在")
	assert.Equal(t, before, readFile(t, filepath.Join(dir, "brickkit.yaml")),
		"已有 brickkit.yaml 不能被覆盖")
}

func TestInitFailsWhenConfigFileAlreadyExists(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "brickkit.yaml"), []byte("project: old\n"), 0o644))

	r := runIn(t, dir, "init", "my-project")

	assert.NotEqual(t, clierr.ExitOK, r.code)
	assert.Contains(t, r.stderr, "brickkit.yaml")
	assert.Equal(t, "project: old\n", readFile(t, filepath.Join(dir, "brickkit.yaml")))
}

// 3.8 / 3.9 项目名称非法时报错并给出命名规则。
func TestInitRejectsInvalidProjectNames(t *testing.T) {
	cases := []struct {
		name     string
		args     []string
		contains []string
	}{
		{"含空格", []string{"my project"}, []string{"项目名称不合法", "小写字母、数字与中划线"}},
		{"含大写", []string{"MyProject"}, []string{"项目名称不合法", "全部小写", "myproject"}},
		{"含下划线", []string{"my_project"}, []string{"项目名称不合法"}},
		{"含中文", []string{"我的项目"}, []string{"项目名称不合法"}},
		// 以中划线开头必须用 -- 分隔，否则 cobra 会当成 flag 解析（这是正确的 CLI 行为）
		{"以中划线开头", []string{"--", "-my-project"}, []string{"项目名称不合法", "中划线开头"}},
		{"以中划线结尾", []string{"my-project-"}, []string{"项目名称不合法", "中划线"}},
		{"空字符串", []string{""}, []string{"请指定项目名称"}},
		{"含斜杠", []string{"my/project"}, []string{"项目名称不合法"}},
		{"超长名称", []string{strings.Repeat("a", 55)}, []string{"项目名称不合法", "长度"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dir := t.TempDir()
			r := runIn(t, dir, append([]string{"init"}, c.args...)...)

			assert.NotEqual(t, clierr.ExitOK, r.code)
			for _, want := range c.contains {
				assert.Contains(t, r.stderr, want)
			}
			assert.NoFileExists(t, filepath.Join(dir, "brickkit.yaml"))
			assert.NoDirExists(t, filepath.Join(dir, ".brickkit"))
		})
	}
}

// 32.20–32.22 合法名称：中划线、数字、纯数字。
func TestInitAcceptsValidProjectNames(t *testing.T) {
	for _, name := range []string{"my-project", "project123", "123", "a", "my-erp-dev"} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			r := runIn(t, dir, "init", name)

			require.Equal(t, clierr.ExitOK, r.code, "stderr=%s", r.stderr)
			assert.Contains(t, readFile(t, filepath.Join(dir, "brickkit.yaml")), "project: "+name)
		})
	}
}

// 3.11 非空目录（无 brickkit.yaml）中 init 成功，且不影响已有文件。
func TestInitInNonEmptyDirectorySucceeds(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "README.md"), []byte("# hello\n"), 0o644))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "src"), 0o755))

	r := runIn(t, dir, "init", "my-project")

	require.Equal(t, clierr.ExitOK, r.code, "stderr=%s", r.stderr)
	assert.FileExists(t, filepath.Join(dir, "brickkit.yaml"))
	assert.Equal(t, "# hello\n", readFile(t, filepath.Join(dir, "README.md")))
	requireDir(t, filepath.Join(dir, "src"))
}

// 输出必须与 004 §3.2 / 009 §2 / 011 §3 中的样例逐字一致（Step 40 文档验证依赖）。
func TestInitOutputMatchesDesignDocs(t *testing.T) {
	dir := t.TempDir()
	r := runIn(t, dir, "init", "my-project")
	require.Equal(t, clierr.ExitOK, r.code, "stderr=%s", r.stderr)

	want := "✅ 项目已初始化：my-project\n" +
		"   📁 brickkit.yaml        项目配置\n" +
		"   📁 .brickkit/           CLI 工作目录\n" +
		"   📁 .brickkit/backup/    配置备份\n" +
		"\n" +
		"下一步：\n" +
		"  brickkit add people/basic@1.0.0    添加组件\n" +
		"  brickkit up                        一键启动\n"
	assert.Equal(t, want, r.stdout)
}

// --config 指定的配置文件名应被 init 采用（多环境初始化，004 §3.5）。
func TestInitHonorsConfigFlag(t *testing.T) {
	dir := t.TempDir()
	r := runIn(t, dir, "init", "my-project", "--config", "brickkit.prod.yaml")
	require.Equal(t, clierr.ExitOK, r.code, "stderr=%s", r.stderr)

	assert.FileExists(t, filepath.Join(dir, "brickkit.prod.yaml"))
	assert.NoFileExists(t, filepath.Join(dir, "brickkit.yaml"))
	assert.FileExists(t, filepath.Join(dir, ".brickkit/backup/brickkit.prod.yaml.initial"))
	assert.Contains(t, r.stdout, "brickkit.prod.yaml")
}

// init 只接受一个参数（多余参数走 translate 兜底）。
func TestInitRejectsTooManyArgs(t *testing.T) {
	dir := t.TempDir()
	r := runIn(t, dir, "init", "a", "b")
	assert.Equal(t, clierr.ExitUsage, r.code)
	assert.NoFileExists(t, filepath.Join(dir, "brickkit.yaml"))
}
