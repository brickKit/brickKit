package cli

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"

	"github.com/brickkit/brickkit/internal/config"
)

// ============================================================
// 组件构造
// ============================================================

// comp 描述一个用于测试的组件。
type comp struct {
	ID      string
	Version string
	// Requires / Optional 的写法为 "department/tree@1.0.0"。
	Requires []string
	Optional []string
	// Artifacts 是 "type:文件路径" 的列表，如 "api-docs:openapi.json"。
	Artifacts []string
	// Migration 是 migration.command（002 §8.2）。
	Migration []string
	// Image 覆盖默认的 registry.example.com/<id>:<version>（P29 的 digest 用例要用）。
	Image string
}

// imageRef 是该组件的 deployment.image。
func (c comp) imageRef() string {
	if c.Image != "" {
		return c.Image
	}
	return "registry.example.com/" + strings.ReplaceAll(c.ID, "/", "-") + ":" + c.Version
}

func (c comp) ref() string { return c.ID + "@" + c.Version }

func (c comp) yamlText() string {
	var b strings.Builder
	b.WriteString("apiVersion: brickkit/v1\nkind: Component\nmetadata:\n")
	fmt.Fprintf(&b, "  id: %s\n", c.ID)
	fmt.Fprintf(&b, "  name: 测试组件 %s\n", c.ID)
	fmt.Fprintf(&b, "  version: %s\n", c.Version)
	fmt.Fprintf(&b, "  description: 用于 add / remove 测试的组件\n")
	if len(c.Artifacts) > 0 {
		b.WriteString("artifacts:\n")
		for _, a := range c.Artifacts {
			typ, file, _ := strings.Cut(a, ":")
			fmt.Fprintf(&b, "  - type: %s\n    files:\n      - %s\n", typ, file)
		}
	}
	if len(c.Requires) > 0 || len(c.Optional) > 0 {
		b.WriteString("dependencies:\n  components:\n")
		for _, r := range c.Requires {
			fmt.Fprintf(&b, "    - %s\n", r)
		}
		for _, o := range c.Optional {
			fmt.Fprintf(&b, "    - id: %s\n      optional: true\n", o)
		}
	}
	if len(c.Migration) > 0 {
		fmt.Fprintf(&b, "migration:\n  command: [%s]\n", quotedList(c.Migration))
	}
	b.WriteString("deployment:\n  type: container\n")
	fmt.Fprintf(&b, "  image: %s\n", c.imageRef())
	b.WriteString("  port: 8080\nhealthCheck:\n  type: http\n  path: /healthz\n")
	return b.String()
}

// quotedList 把命令渲染成 YAML 的行内数组："a", "b"。
func quotedList(items []string) string {
	quoted := make([]string, 0, len(items))
	for _, item := range items {
		quoted = append(quoted, `"`+item+`"`)
	}
	return strings.Join(quoted, ", ")
}

// files 返回该组件仓库中的文件内容（component.yaml + 各产物文件）。
func (c comp) files() map[string]string {
	out := map[string]string{"component.yaml": c.yamlText()}
	for _, a := range c.Artifacts {
		_, file, _ := strings.Cut(a, ":")
		out[file] = "// " + file + " of " + c.ref() + "\n"
	}
	return out
}

// writeTree 把一组文件写到 dir 下。
func writeTree(t *testing.T, dir string, files map[string]string) {
	t.Helper()
	for name, content := range files {
		path := filepath.Join(dir, filepath.FromSlash(name))
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
		require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
	}
}

// ============================================================
// 项目脚手架
// ============================================================

// projectFixture 是一个已初始化、且配置好安装源的测试项目。
type projectFixture struct {
	Dir    string
	Layout config.Layout
	// Sources 是写入 brickkit.yaml 的安装源片段。
	Sources []string
}

// configWithComment 是带注释与 ${ENV_VAR} 的配置，用于验证 add / remove 不破坏用户内容。
const configHeader = `# brickkit.yaml - BrickKit 项目配置
# 这一行注释必须在 add / remove 之后依然存在
project: my-erp

deploy:
  target: docker          # docker | k8s
`

// newProjectFixture 在新的临时目录里初始化项目并写入指定的安装源。
func newProjectFixture(t *testing.T, sources ...string) *projectFixture {
	t.Helper()
	return newProjectFixtureAt(t, t.TempDir(), sources...)
}

// newProjectFixtureAt 在指定目录初始化项目：本地安装源的相对路径要与项目根一致，
// 因此写组件的目录和项目目录必须是同一个。
func newProjectFixtureAt(t *testing.T, dir string, sources ...string) *projectFixture {
	t.Helper()
	r := runIn(t, dir, "init", "my-erp")
	require.Equal(t, 0, r.code, "init 应成功：%s%s", r.stdout, r.stderr)

	f := &projectFixture{Dir: dir, Layout: config.NewLayout(dir, ""), Sources: sources}
	f.writeConfig(t, "components: []\nresources: []\n")
	return f
}

// writeConfig 用"注释 + 安装源 + body"重写 brickkit.yaml。
func (f *projectFixture) writeConfig(t *testing.T, body string) {
	t.Helper()
	var b strings.Builder
	b.WriteString(configHeader)
	if len(f.Sources) > 0 {
		b.WriteString("\nsources:\n")
		for _, s := range f.Sources {
			b.WriteString(s)
		}
	}
	b.WriteString("\n")
	b.WriteString(body)
	require.NoError(t, os.WriteFile(f.Layout.ConfigPath(), []byte(b.String()), 0o644))
}

func (f *projectFixture) config(t *testing.T) string {
	t.Helper()
	return readFile(t, f.Layout.ConfigPath())
}

// parsed 解析当前 brickkit.yaml。
func (f *projectFixture) parsed(t *testing.T) *config.Config {
	t.Helper()
	c, err := config.ParseConfigFile(f.Layout.ConfigPath())
	require.NoError(t, err, "brickkit.yaml 应始终是合法配置")
	return c
}

// refs 返回配置中全部组件的 id@version。
func (f *projectFixture) refs(t *testing.T) []string {
	t.Helper()
	var out []string
	for _, c := range f.parsed(t).Components {
		out = append(out, c.Ref())
	}
	return out
}

// localSource 把若干组件写进一个本地安装源目录（每个版本一个目录）。
//
// 返回可写入 brickkit.yaml 的 sources 片段。
func localSource(t *testing.T, root string, comps ...comp) []string {
	t.Helper()
	var fragments []string
	for i, c := range comps {
		name := "src" + strconv.Itoa(i)
		writeTree(t, filepath.Join(root, name, filepath.FromSlash(c.ID)), c.files())
		fragments = append(fragments, fmt.Sprintf("  - id: local-%d\n    type: local\n    path: ./%s\n", i, name))
	}
	return fragments
}

// ============================================================
// git 仓库
// ============================================================

// newComponentRepo 建一个包含该组件源码的 git 仓库，返回仓库路径（可作为 clone URL）。
func newComponentRepo(t *testing.T, c comp) string {
	t.Helper()
	dir := t.TempDir()
	writeTree(t, dir, c.files())
	writeTree(t, dir, map[string]string{"README.md": "# " + c.ID + "\n"})

	gitCmd(t, dir, "init", "-q", "-b", "main")
	gitCmd(t, dir, "add", "-A")
	gitCmd(t, dir, "-c", "user.email=test@example.com", "-c", "user.name=BrickKit Test",
		"commit", "-q", "-m", "init")
	return dir
}

func gitCmd(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
	out, err := cmd.CombinedOutput()
	require.NoErrorf(t, err, "git %s: %s", strings.Join(args, " "), out)
}

// ============================================================
// 市场 Mock（007 §9.1；只实现 CLI 用到的三个端点）
// ============================================================

type mockComponent struct {
	Spec comp
	// SourceType 是 git（开源）或 registry（闭源），007 §11。
	SourceType string
	// GitURL 是开源组件的仓库地址（测试里指向本地 git 仓库）。
	GitURL string
	// FailDownload 为 true 时，产物下载端点返回 500。
	FailDownload bool
}

type mockMarket struct {
	server *httptest.Server
	comps  map[string]*mockComponent
}

func newMockMarket(t *testing.T, comps ...*mockComponent) *mockMarket {
	t.Helper()
	m := &mockMarket{comps: map[string]*mockComponent{}}
	for _, c := range comps {
		m.comps[c.Spec.ref()] = c
	}
	m.server = httptest.NewServer(http.HandlerFunc(m.handle))
	t.Cleanup(m.server.Close)
	return m
}

// source 返回可写入 brickkit.yaml 的 sources 片段。
func (m *mockMarket) source() string {
	return fmt.Sprintf("  - id: market\n    type: market\n    url: %s/api/v1\n", m.server.URL)
}

func (m *mockMarket) handle(w http.ResponseWriter, r *http.Request) {
	rest := strings.TrimPrefix(r.URL.Path, "/api/v1/components/")
	id, tail, ok := strings.Cut(rest, "/versions/")
	if !ok {
		http.NotFound(w, r)
		return
	}
	version, action, _ := strings.Cut(tail, "/")
	c, found := m.comps[id+"@"+version]
	if !found {
		writeJSONBody(w, http.StatusNotFound, map[string]any{"success": false})
		return
	}

	switch {
	case action == "manifest":
		var doc map[string]any
		if err := yaml.Unmarshal([]byte(c.Spec.yamlText()), &doc); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		data := map[string]any{"manifest": doc, "sourceType": c.SourceType}
		if c.GitURL != "" {
			data["gitUrl"] = c.GitURL
		}
		writeJSONBody(w, http.StatusOK, map[string]any{"success": true, "data": data})
	case action == "artifacts":
		list := make([]map[string]any, 0, len(c.Spec.Artifacts))
		for i, a := range c.Spec.Artifacts {
			typ, file, _ := strings.Cut(a, ":")
			list = append(list, map[string]any{
				"id": "art-" + strconv.Itoa(i), "type": typ, "files": []string{file},
			})
		}
		writeJSONBody(w, http.StatusOK, map[string]any{"success": true, "data": list})
	case strings.HasPrefix(action, "artifacts/"):
		if c.FailDownload {
			http.Error(w, "storage unavailable", http.StatusInternalServerError)
			return
		}
		file := r.URL.Query().Get("file")
		content, ok := c.Spec.files()[file]
		if !ok {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(content))
	default:
		http.NotFound(w, r)
	}
}

func writeJSONBody(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}
