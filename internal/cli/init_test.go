package cli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"

	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/logging"
	"github.com/brickkit/brickkit/internal/project"
)

// runIn 在指定目录下执行一次 CLI（不依赖进程 cwd，便于并行与隔离）。
func runIn(t *testing.T, dir string, args ...string) result {
	t.Helper()
	return runWith(t, nil, dir, args...)
}

// stubDigest 是测试里假装 registry 返回的 digest。
const stubDigest = "sha256:1111111111111111111111111111111111111111111111111111111111111111"

// runWith 在执行前允许调整全局选项（注入假引擎等）。
func runWith(t *testing.T, tweak func(*Options), dir string, args ...string) result {
	t.Helper()
	var out, errBuf bytes.Buffer
	opts := &Options{
		WorkDir:  dir,
		LogLevel: logging.LevelOff,
		Stdout:   &out,
		Stderr:   &errBuf,
		// 默认假装 registry 里已经有这个镜像——那是发布时的常态
		// （build → push → publish）。测试要验解析失败时自己覆盖它。
		ResolveDigest: func(context.Context, string) (string, error) {
			return stubDigest, nil
		},
		// git 源的仓库缓存放在测试自己的临时目录，绝不碰使用者的 ~/.cache
		RepoCacheDir: t.TempDir(),
		// 本机镜像：默认都在（绝不碰真的 docker）；关心镜像的用例自己换掉
		Images: everyImagePresent{},
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

// 创建式：init <name> 新建 <name>/ 目录，三层文件写在里面（提案 §11.5、命令表 1）。
// 不生成 deploy.local.yaml——那是 brickkit local on 按需复制出来的个人文件。
func TestInitCreatesNamedDirectory(t *testing.T) {
	parent := t.TempDir()
	r := runIn(t, parent, "init", "my-project")
	require.Equal(t, clierr.ExitOK, r.code, "stderr=%s", r.stderr)

	dir := filepath.Join(parent, "my-project")
	for _, file := range []string{"brickkit.yaml", "deploy.yaml", "config/vars.yaml", "config/.gitkeep", "shell/.gitkeep", ".gitignore", "BRICKKIT.md"} {
		assert.FileExists(t, filepath.Join(dir, file))
	}
	assert.NoFileExists(t, filepath.Join(dir, "deploy.local.yaml"))
	for _, sub := range []string{".brickkit", ".brickkit/manifests", ".brickkit/artifacts", ".brickkit/generated", "components", "shell"} {
		requireDir(t, filepath.Join(dir, sub))
	}
	assert.NoFileExists(t, filepath.Join(parent, "brickkit.yaml"), "不往当前目录写")
	assert.Contains(t, readFile(t, filepath.Join(dir, "BRICKKIT.md")), "<!-- brickkit:managed:begin -->")

	// 刚 init 完的项目就是一个合法、可 up 的项目
	lint := runIn(t, dir, "lint")
	assert.Equal(t, clierr.ExitOK, lint.code, lint.stdout+lint.stderr)
}

func TestInitRefusesNonEmptyNamedDirectory(t *testing.T) {
	parent := t.TempDir()
	dir := filepath.Join(parent, "my-project")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "README.md"), []byte("# hi\n"), 0o644))

	r := runIn(t, parent, "init", "my-project")
	assert.NotEqual(t, clierr.ExitOK, r.code)
	assert.Contains(t, r.stderr, "already exists and is not empty")
	assert.Contains(t, r.stderr, "cd my-project && brickkit init")
	assert.NoFileExists(t, filepath.Join(dir, "brickkit.yaml"))
}

// 已存在的空目录照常创建：那只是个还没放东西的地方。
func TestInitCreatesIntoExistingEmptyDirectory(t *testing.T) {
	parent := t.TempDir()
	require.NoError(t, os.Mkdir(filepath.Join(parent, "my-project"), 0o755))
	r := runIn(t, parent, "init", "my-project", "--no-skills")
	require.Equal(t, clierr.ExitOK, r.code, r.stderr)
	assert.FileExists(t, filepath.Join(parent, "my-project", "brickkit.yaml"))
}

func TestInitNameFlagWithArgumentIsUsageError(t *testing.T) {
	r := runIn(t, t.TempDir(), "init", "a", "--name", "b")
	assert.Equal(t, clierr.ExitUsage, r.code)
	assert.Contains(t, r.stderr, "not both")
}

// 骨架内容：声明里只有项目名与两个默认本地源，部署文件默认 docker、组件列表为空。
func TestInitConfigSkeletonContent(t *testing.T) {
	dir := t.TempDir()
	require.Equal(t, clierr.ExitOK, runIn(t, dir, "init", "--name", "my-project").code)

	raw := readFile(t, filepath.Join(dir, "brickkit.yaml"))
	var decl map[string]any
	require.NoError(t, yaml.Unmarshal([]byte(raw), &decl), "骨架必须是合法 YAML")
	assert.Equal(t, "my-project", decl["project"])
	assert.Empty(t, decl["components"], "components 初始为空列表")
	assert.NotContains(t, decl, "deploy", "部署信息不在声明文件里")
	assert.NotContains(t, decl, "resources", "基础资源已从平台移除")
	assert.Contains(t, raw, "# brickkit.yaml", "骨架应带注释，说明文件用途")
	assert.Contains(t, raw, "path: ./shell")

	var deploy map[string]any
	require.NoError(t, yaml.Unmarshal([]byte(readFile(t, filepath.Join(dir, "deploy.yaml"))), &deploy))
	assert.Equal(t, "docker", deploy["target"])
	assert.Empty(t, deploy["components"])
}

// .gitignore 必须挡住个人文件与密钥（提案 §11.5）。
func TestInitCreatesGitignore(t *testing.T) {
	dir := t.TempDir()
	require.Equal(t, clierr.ExitOK, runIn(t, dir, "init", "--name", "my-project").code)

	lines := strings.Split(readFile(t, filepath.Join(dir, ".gitignore")), "\n")
	for _, entry := range []string{
		"deploy.local.yaml", "deploy.local.yaml.bak", ".secrets/", ".brickkit/", "config/.archive/", ".env", "components/",
	} {
		assert.Contains(t, lines, entry, ".gitignore 应包含 %s", entry)
	}
}

// 补全式：已有 .gitignore 只校验、绝不修改；缺的条目大声警告并逐条列出（提案 §11.5）。
func TestInitCompleteWarnsMissingGitignoreEntries(t *testing.T) {
	dir := t.TempDir()
	existing := "# 我自己的规则\n*.log\ndeploy.local.yaml\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".gitignore"), []byte(existing), 0o644))

	r := runIn(t, dir, "init", "--name", "my-project", "--yes")
	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)

	assert.Equal(t, existing, readFile(t, filepath.Join(dir, ".gitignore")), ".gitignore 一个字节都不改")
	assert.Contains(t, r.stdout, "⚠️")
	assert.Contains(t, r.stdout, ".gitignore is missing required entries")
	for _, entry := range []string{".brickkit/", ".secrets/", "config/.archive/"} {
		assert.Contains(t, r.stdout, "Missing: "+entry)
	}
	assert.NotContains(t, r.stdout, "Missing: deploy.local.yaml\n")
}

// 非空目录先打印计划，等确认；说不就什么都不写。
func TestInitCompleteAsksInNonEmptyDir(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "README.md"), []byte("# hello\n"), 0o644))

	r := runWith(t, func(o *Options) { o.Stdin = strings.NewReader("n\n") }, dir, "init", "--no-skills")
	require.Equal(t, clierr.ExitOK, r.code, r.stderr)
	assert.Contains(t, r.stdout, "This directory already has files")
	assert.Contains(t, r.stdout, "create  brickkit.yaml")
	assert.Contains(t, r.stdout, "create  components/", "计划里列出它会建的每样东西：components/ 被忽略、没有 .gitkeep，也要列")
	assert.Contains(t, r.stdout, "Cancelled")
	assert.NoFileExists(t, filepath.Join(dir, "brickkit.yaml"))

	r = runWith(t, func(o *Options) { o.Stdin = strings.NewReader("y\n") }, dir, "init", "--no-skills")
	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)
	assert.FileExists(t, filepath.Join(dir, "brickkit.yaml"))
	assert.Equal(t, "# hello\n", readFile(t, filepath.Join(dir, "README.md")))
	assert.Contains(t, r.stdout, "closing check passed")
}

func TestInitCompleteYesSkipsPrompt(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "README.md"), []byte("# hello\n"), 0o644))
	r := runIn(t, dir, "init", "--yes", "--no-skills")
	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)
	assert.NotContains(t, r.stdout, "Continue?")
	assert.FileExists(t, filepath.Join(dir, "deploy.yaml"))
	name, err := project.ProjectNameFor(project.NewLayout(dir), "")
	require.NoError(t, err)
	assert.Contains(t, readFile(t, filepath.Join(dir, "brickkit.yaml")), "project: "+name+"\n", "项目名取自目录名")
}

// 已有的文件跳过、一个字节不动；项目名沿用已有 brickkit.yaml 的 project。
func TestInitCompleteSkipsExistingAndKeepsProjectName(t *testing.T) {
	dir := t.TempDir()
	decl := "project: old\ncomponents: []\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, "brickkit.yaml"), []byte(decl), 0o644))

	r := runIn(t, dir, "init", "--yes", "--no-skills")
	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)
	assert.Equal(t, decl, readFile(t, filepath.Join(dir, "brickkit.yaml")))
	assert.Contains(t, r.stdout, "skip    brickkit.yaml")
	assert.Contains(t, r.stdout, "Project completed: old")
	assert.Contains(t, readFile(t, filepath.Join(dir, "BRICKKIT.md")), "# old")
}

// 收尾校验：补全了但装载不了（brickkit.yaml 有组件、部署文件没有它的条目），命令失败并说清为什么。
func TestInitCompleteClosingCheckFails(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "brickkit.yaml"),
		[]byte("project: old\ncomponents:\n  - id: erp/backend\n    version: 1.0.0\n"), 0o644))

	r := runIn(t, dir, "init", "--yes", "--no-skills")
	assert.Equal(t, clierr.ExitError, r.code)
	assert.Contains(t, r.stdout, "erp/backend", "装载的问题原样打印出来")
	assert.Contains(t, r.stderr, "the project was completed but does not load yet")
	assert.FileExists(t, filepath.Join(dir, "deploy.yaml"), "补全本身已经做完")
}

// 项目名称非法时报错并给出命名规则。
func TestInitRejectsInvalidProjectNames(t *testing.T) {
	cases := []struct {
		name     string
		args     []string
		contains []string
	}{
		{"含空格", []string{"my project"}, []string{"invalid project name", "lowercase letters, digits and hyphens"}},
		{"含大写", []string{"MyProject"}, []string{"invalid project name", "all lowercase", "myproject"}},
		{"含下划线", []string{"my_project"}, []string{"invalid project name"}},
		{"含中文", []string{"我的项目"}, []string{"invalid project name"}},
		// 以中划线开头必须用 -- 分隔，否则 cobra 会当成 flag 解析（这是正确的 CLI 行为）
		{"以中划线开头", []string{"--", "-my-project"}, []string{"invalid project name", "start or end with a hyphen"}},
		{"以中划线结尾", []string{"my-project-"}, []string{"invalid project name", "hyphen"}},
		{"空字符串", []string{""}, []string{"Please specify a project name"}},
		{"含斜杠", []string{"my/project"}, []string{"invalid project name"}},
		{"超长名称", []string{strings.Repeat("a", 55)}, []string{"invalid project name", "length"}},
		{"--name 非法", []string{"--name", "Bad Name"}, []string{"invalid project name"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dir := t.TempDir()
			r := runIn(t, dir, append([]string{"init"}, c.args...)...)

			assert.NotEqual(t, clierr.ExitOK, r.code)
			for _, want := range c.contains {
				assert.Contains(t, r.stderr, want)
			}
			entries, err := os.ReadDir(dir)
			require.NoError(t, err)
			assert.Empty(t, entries, "报错时一个文件、一个目录都不写")
		})
	}
}

// 合法名称：中划线、数字、纯数字。
func TestInitAcceptsValidProjectNames(t *testing.T) {
	for _, name := range []string{"my-project", "project123", "123", "a", "my-erp-dev"} {
		t.Run(name, func(t *testing.T) {
			parent := t.TempDir()
			r := runIn(t, parent, "init", name, "--no-skills")

			require.Equal(t, clierr.ExitOK, r.code, "stderr=%s", r.stderr)
			assert.Contains(t, readFile(t, filepath.Join(parent, name, "brickkit.yaml")), "project: "+name)
		})
	}
}

func TestInitOutputMatchesDesignDocs(t *testing.T) {
	r := runIn(t, t.TempDir(), "init", "my-project")
	require.Equal(t, clierr.ExitOK, r.code, "stderr=%s", r.stderr)

	want := "✅ Project initialized: my-project\n" +
		"   📄 brickkit.yaml        Project config\n" +
		"   📄 deploy.yaml          How it is deployed (team file, committed)\n" +
		"   📁 config/              Component configuration and shared vars\n" +
		"   📁 components/          Component source (configured as the local install source local-dev)\n" +
		"   📁 shell/               Shells (kind: shell), project code (the local install source local-shells)\n" +
		"   📁 .brickkit/           CLI working directory\n" +
		"   📄 BRICKKIT.md          Project map: components and where their docs are\n" +
		"   📁 .claude/skills/      AI assistant skills (4)\n" +
		"   📄 AGENTS.md            AI assistant project guide\n" +
		"   💡 If component source goes into Git with the project: brickkit init --hooks installs the pre-commit check\n" +
		"\n" +
		"Next steps:\n" +
		"  cd my-project\n" +
		"  brickkit add --local               add every component under components/\n" +
		"  brickkit add people/basic@1.0.0    add a component from an install source\n" +
		"  brickkit up                        start everything in one go\n"
	assert.Equal(t, want, r.stdout)
}

// init 只接受一个参数（多余参数走 translate 兜底）。
func TestInitRejectsTooManyArgs(t *testing.T) {
	dir := t.TempDir()
	r := runIn(t, dir, "init", "a", "b")
	assert.Equal(t, clierr.ExitUsage, r.code)
	assert.NoFileExists(t, filepath.Join(dir, "brickkit.yaml"))
}

func TestInitInstallsSkills(t *testing.T) {
	dir := t.TempDir()
	r := runIn(t, dir, "init", "--name", "my-project", "--yes")
	require.Equal(t, 0, r.code, r.stderr)

	for _, rel := range []string{
		"AGENTS.md",
		filepath.Join(".claude", "skills", "brickkit-assemble", "SKILL.md"),
		filepath.Join(".claude", "skills", "brickkit-component", "SKILL.md"),
		filepath.Join(".claude", "skills", "brickkit-deploy", "SKILL.md"),
		filepath.Join(".claude", "skills", "brickkit-troubleshoot", "SKILL.md"),
		filepath.Join(".brickkit", "skills.lock"),
	} {
		_, err := os.Stat(filepath.Join(dir, rel))
		assert.NoError(t, err, "init 没装出 %s", rel)
	}
	assert.Contains(t, r.stdout, ".claude/skills/")
}

func TestInitNoSkillsInstallsNothing(t *testing.T) {
	dir := t.TempDir()
	r := runIn(t, dir, "init", "--name", "my-project", "--yes", "--no-skills")
	require.Equal(t, 0, r.code, r.stderr)

	for _, rel := range []string{"AGENTS.md", ".claude",
		filepath.Join(".brickkit", "skills.lock")} {
		_, err := os.Stat(filepath.Join(dir, rel))
		assert.True(t, os.IsNotExist(err), "--no-skills 却产生了 %s", rel)
	}
	assert.NotContains(t, r.stdout, ".claude/skills/")
}

// 技能文件是要提交进 Git 的（团队共享），所以 .gitignore 里绝不能出现它们。
// 这条没人守着：下一个人「顺手」把 .claude/ 加进忽略规则，共享就静默失效了。
func TestInitDoesNotIgnoreSkillFiles(t *testing.T) {
	dir := t.TempDir()
	require.Equal(t, 0, runIn(t, dir, "init", "--name", "my-project", "--yes").code)

	b, err := os.ReadFile(filepath.Join(dir, ".gitignore"))
	require.NoError(t, err)
	for _, forbidden := range []string{".claude", "AGENTS.md", "skills.lock"} {
		assert.NotContains(t, string(b), forbidden,
			".gitignore 忽略了技能文件，团队共享会静默失效")
	}
}

// CLAUDE.md 是使用者自己的流程文件：既不新建，也不往已有的里面加东西。
func TestInitNeverTouchesClaudeMd(t *testing.T) {
	dir := t.TempDir()
	r := runIn(t, dir, "init", "--name", "my-project", "--yes")
	require.Equal(t, 0, r.code, r.stderr)
	_, err := os.Stat(filepath.Join(dir, "CLAUDE.md"))
	assert.True(t, os.IsNotExist(err), "不该建出 CLAUDE.md")

	dir2 := t.TempDir()
	mine := "# 我自己的规则\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir2, "CLAUDE.md"), []byte(mine), 0o644))
	require.Equal(t, 0, runIn(t, dir2, "init", "--name", "my-project", "--yes").code)

	after, err := os.ReadFile(filepath.Join(dir2, "CLAUDE.md"))
	require.NoError(t, err)
	assert.Equal(t, mine, string(after), "已有的 CLAUDE.md 被改了")
}

func TestInitInstallsHookWhenProjectIsRepoRoot(t *testing.T) {
	dir := newTestRepo(t)

	r := runIn(t, dir, "init", "--name", "my-erp", "--yes")
	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)

	hook := filepath.Join(dir, ".git", "hooks", "pre-commit")
	assert.FileExists(t, hook)
	assert.Contains(t, r.stdout, "pre-commit")
	// `.git/hooks/pre-commit` 正好 21 列，占满了对齐用的 %-21s——说明文字曾经
	// 直接粘在路径后面（"…pre-commit提交前检查组件结构"）。
	assert.Contains(t, r.stdout, ".git/hooks/pre-commit Check the component layout before committing",
		"路径与说明之间至少要有一个空格")

	script, err := os.ReadFile(hook)
	require.NoError(t, err)
	assert.Contains(t, string(script), hookMarker)
}

func TestInitSkipsHookOutsideGitRepo(t *testing.T) {
	dir := t.TempDir()

	r := runIn(t, dir, "init", "--name", "my-erp", "--yes")
	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)
	assert.Contains(t, r.stdout, "brickkit init --hooks",
		"装不上要提示怎么补装——init 常常跑在 git init 之前")
}

// init 完全可能跑在一个跟本项目无关的仓库的子目录里（本仓库的
// docs/archive/guide/playground/ 就是）。那时自动装等于往别人的 .git/hooks 里写东西。
func TestInitDoesNotWriteHookIntoAnUnrelatedRepo(t *testing.T) {
	repo := newTestRepo(t)
	nested := filepath.Join(repo, "sub", "project")
	require.NoError(t, os.MkdirAll(nested, 0o755))

	r := runIn(t, nested, "init", "--name", "my-erp", "--yes")
	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)

	assert.NoFileExists(t, filepath.Join(repo, ".git", "hooks", "pre-commit"),
		"嵌套项目要装 hook，得自己显式说一句")
	assert.Contains(t, r.stdout, "brickkit init --hooks")
}

func TestInitHooksOnlyInstallsIntoExistingProject(t *testing.T) {
	repo := newTestRepo(t)
	nested := filepath.Join(repo, "sub", "project")
	require.NoError(t, os.MkdirAll(nested, 0o755))
	require.Equal(t, clierr.ExitOK, runIn(t, nested, "init", "--name", "my-erp", "--yes").code)

	r := runIn(t, nested, "init", "--hooks")
	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)

	hook := filepath.Join(repo, ".git", "hooks", "pre-commit")
	require.FileExists(t, hook)
	script, err := os.ReadFile(hook)
	require.NoError(t, err)
	assert.Contains(t, string(script), "\nsub/project\n",
		"清单里记的是项目根相对仓库根的路径")
}

// 升级 CLI 之后重跑 brickkit init --hooks：文件确实被重写（版本戳与写死在里面的
// 可执行文件路径都刷新），所以输出必须说"已刷新"，而不是"已经装过了"——
// 后者会让人以为什么都没发生，于是继续用着旧 hook。
func TestInitHooksOnlyReportsRefreshOnReinstall(t *testing.T) {
	dir := newTestRepo(t)
	require.Equal(t, clierr.ExitOK, runIn(t, dir, "init", "--name", "my-erp", "--yes").code)

	r := runIn(t, dir, "init", "--hooks")
	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)
	assert.Contains(t, r.stdout, "was refreshed to the current version")
	assert.NotContains(t, r.stdout, "already installed")
}

func TestInitHooksOnlyRejectsProjectName(t *testing.T) {
	r := runIn(t, newTestRepo(t), "init", "--hooks", "my-erp")
	assert.Equal(t, clierr.ExitUsage, r.code)
	assert.Contains(t, r.stderr, "needs no project name")
}

// brickkit init --hooks 在没有 brickkit.yaml 的目录里必须报错，不能一声不响装上。
//
// 装上去的那个 hook 在那儿永远不会响——它要比对的意图声明根本不存在。使用者
// 敲完命令却以为「闸门开了」，这正是这套设计一直在防的那种静默误判。
// 多项目仓库里从仓库根跑这条命令，撞的就是这一条。
func TestInitHooksOnlyRequiresAProject(t *testing.T) {
	dir := newTestRepo(t)

	r := runIn(t, dir, "init", "--hooks")
	assert.Equal(t, clierr.ExitError, r.code, r.stdout+r.stderr)
	assert.Contains(t, r.stderr, "so this is not a BrickKit project")
	assert.NoFileExists(t, filepath.Join(dir, ".git", "hooks", "pre-commit"),
		"报错了就不该留下一个永远不会响的 hook")
}

func TestInitHooksOnlyFailsLoudlyOutsideGitRepo(t *testing.T) {
	dir := t.TempDir()
	require.Equal(t, clierr.ExitOK, runIn(t, dir, "init", "--name", "my-erp", "--yes").code)

	r := runIn(t, dir, "init", "--hooks")
	assert.Equal(t, clierr.ExitError, r.code, "显式要求装就得装上，装不上是错误")
	assert.Contains(t, r.stderr, "git")
}

// 目录里预先有一份自己的 AGENTS.md：跳过它，并且说出来。
func TestInitKeepsExistingAgentsMd(t *testing.T) {
	dir := t.TempDir()
	mine := "# 我自己写的导读\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, "AGENTS.md"), []byte(mine), 0o644))

	r := runIn(t, dir, "init", "--name", "my-project", "--yes")
	require.Equal(t, 0, r.code, r.stderr)

	after, err := os.ReadFile(filepath.Join(dir, "AGENTS.md"))
	require.NoError(t, err)
	assert.Equal(t, mine, string(after), "已有的 AGENTS.md 被覆盖了")
	assert.Contains(t, r.stdout, "AGENTS.md", "跳过了要说出来")
}

// 已有的 BRICKKIT.md 没有维护区：不动它，说一声组件表不会自动更新。
func TestInitCompleteNotesUnmanagedProjectDoc(t *testing.T) {
	dir := t.TempDir()
	mine := "# my notes\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, "BRICKKIT.md"), []byte(mine), 0o644))
	r := runIn(t, dir, "init", "--name", "shop", "--yes", "--no-skills")
	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)
	assert.Equal(t, mine, readFile(t, filepath.Join(dir, "BRICKKIT.md")))
	assert.Contains(t, r.stdout, "without the brickkit-managed block")
}

func TestInitNamedPathIsAFile(t *testing.T) {
	parent := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(parent, "my-project"), []byte("x"), 0o644))
	r := runIn(t, parent, "init", "my-project")
	assert.NotEqual(t, clierr.ExitOK, r.code)
	assert.Contains(t, r.stderr, "not a directory")
}
