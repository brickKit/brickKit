package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/i18n"
	"github.com/brickkit/brickkit/internal/msgid"
)

// localRepoFixture 是外壳夹具的一份拷贝；edit 改其中一个文件。
func localRepoFixture(t *testing.T) string {
	t.Helper()
	return copyFixture(t, "three-layer-shell")
}

func setMode(t *testing.T, dir, entry, mode string) {
	t.Helper()
	editFile(t, filepath.Join(dir, "deploy.yaml"), "  - id: "+entry+"\n", "  - id: "+entry+"\n    mode: "+mode+"\n")
}

// bumpRepo 把本地仓库里的组件升到 to（使用者在仓库里改了版本、或切到了别的 tag），
// 旧版本的 Manifest 留在缓存里——之前一次 up 取过它。
func bumpRepo(t *testing.T, dir, repoDir, id, from, to string) {
	t.Helper()
	path := filepath.Join(dir, repoDir, filepath.FromSlash(id), "component.yaml")
	old := readFile(t, path)
	cacheManifest(t, dir, id, from, old)
	require.NoError(t, os.WriteFile(path, []byte(strings.ReplaceAll(old, "version: "+from, "version: "+to)), 0o644))
}

// 附录 A22：代码从本地仓库运行（mode: local）时，仓库版本必须是这次运行的版本。
func TestLocalRepoVersionMustMatchDefault(t *testing.T) {
	dir := localRepoFixture(t)
	setMode(t, dir, "erp/portal", "local")
	bumpRepo(t, dir, "components", "erp/portal", "1.0.0", "1.1.0")

	r := runWithEngine(t, newFakeEngine(), dir, "up", "--dry-run")
	require.Equal(t, clierr.ExitError, r.code, r.stdout+r.stderr)
	assert.Contains(t, r.stderr, "erp/portal@1.0.0")
	assert.Contains(t, r.stderr, "1.1.0")
	assert.Contains(t, r.stderr, "brickkit upgrade erp/portal@1.1.0")
	assert.Contains(t, r.stderr, "checkout 1.0.0")
}

// 附录 A24：外壳以裸进程运行时，成员的代码从成员的本地仓库来——它的版本也要对。
func TestBareShellMemberRepoMustMatchDeclared(t *testing.T) {
	dir := localRepoFixture(t)
	setMode(t, dir, "erp/shell", "local")
	bumpRepo(t, dir, "components", "erp/api", "1.0.0", "1.1.0")

	r := runWithEngine(t, newFakeEngine(), dir, "up", "--dry-run")
	require.Equal(t, clierr.ExitError, r.code, r.stdout+r.stderr)
	assert.Contains(t, r.stderr, "erp/api@1.0.0")
	assert.Contains(t, r.stderr, "1.1.0")
}

// 附录 A22：裸进程外壳承载的不是默认版本——本地仓库里是默认版本的代码，拦下。
func TestBareShellHostingNonDefaultBlocked(t *testing.T) {
	dir := localRepoFixture(t)
	setMode(t, dir, "erp/shell", "local")
	api := filepath.Join(dir, "components", "erp", "api", "component.yaml")
	old := readFile(t, api)
	cacheManifest(t, dir, "erp/api", "1.0.0", old)
	require.NoError(t, os.WriteFile(api, []byte(strings.ReplaceAll(old, "1.0.0", "1.1.0")), 0o644))
	editFile(t, filepath.Join(dir, "brickkit.yaml"), "  - id: erp/api\n    version: 1.0.0\n",
		"  - id: erp/api\n    version: 1.1.0\n  - id: erp/api\n    version: 1.0.0\n    requiredBy: [erp/shell, erp/portal]\n")
	editFile(t, filepath.Join(dir, "deploy.yaml"), "      - id: erp/api\n", "      - id: erp/api@1.0.0\n")
	editFile(t, filepath.Join(dir, "deploy.yaml"), "  - id: erp/portal\n", "  - id: erp/portal\n  - id: erp/api\n")

	r := runWithEngine(t, newFakeEngine(), dir, "up", "--dry-run")
	require.Equal(t, clierr.ExitError, r.code, r.stdout+r.stderr)
	assert.Contains(t, r.stderr, "erp/api@1.0.0")
	assert.Contains(t, r.stderr, "default")
}

// 容器部署拉的是镜像，不看本地仓库（附录 A22）。
func TestContainerDeploymentNotChecked(t *testing.T) {
	dir := localRepoFixture(t)
	bumpRepo(t, dir, "components", "erp/portal", "1.0.0", "1.1.0")
	r := runWithEngine(t, newFakeEngine(), dir, "up", "--dry-run")
	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)
}

// 本地仓库按安装源找：shell/ 下的外壳也找得到（从前只看 components/）。
func TestLocalRepoFindsShellDirectory(t *testing.T) {
	dir := localRepoFixture(t)
	setMode(t, dir, "erp/shell", "local")
	writeTree(t, filepath.Join(dir, "shell", "erp", "shell"), map[string]string{
		"go.mod":  "module example.com/shell\n\ngo 1.22\n",
		"main.go": "package main\n\nfunc main() {}\n",
	})
	r := runWithEngine(t, newFakeEngine(), dir, "up", "--dry-run")
	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)
}

// 本地仓库的 component.yaml 读不出版本号（改到一半、YAML 写坏了）：说出来，不能当成版本对得上。
// 本地安装源里的组件会被安装源自己拦下；这里是从 git 装、--repo 克隆出来的那份。
func TestLocalRepoUnreadableVersionIsReported(t *testing.T) {
	g := newGitOrgProject(t)
	g.release(comp{ID: "erp/api", Version: "1.0.0"})
	dir := g.project()
	g.mustRun(dir, "add", "erp/api@1.0.0", "--repo")
	setMode(t, dir, "erp/api", "local")
	repo := filepath.Join(dir, "components", "erp", "api")
	writeTree(t, repo, map[string]string{"go.mod": "module example.com/api\n\ngo 1.22\n", "main.go": "package main\n\nfunc main() {}\n"})
	require.NoError(t, os.WriteFile(filepath.Join(repo, "component.yaml"), []byte("metadata: [this is: not: valid\n"), 0o644))

	r := g.run(dir, "up", "--dry-run")
	require.Equal(t, clierr.ExitError, r.code, r.stdout+r.stderr)
	// 原因（YAML 解析器的原话）在消息末尾：只核对它前面的部分
	assert.Contains(t, r.stderr, i18n.T(msgid.CliUpLocalRepoUnreadable, "erp/api@1.0.0", "components/erp/api", ""))
}
