package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"

	"github.com/brickkit/brickkit/internal/logging"
	"github.com/brickkit/brickkit/internal/project"
	"github.com/brickkit/brickkit/internal/projfile"
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
	// Migration 是 migration.command。
	Migration []string
	// Image 覆盖默认的 registry.example.com/<id>:<version>（digest 用例要用）；
	// 写 "-" 表示只有 deployment.build、没有 image。
	Image string
	// ConfigSchema 是 "键名:默认值" 的列表，如 "greeting:你好"（升级摘要要用）。
	ConfigSchema []string
	// CPU / Memory 是 deployment.resources.limits（配额变更要用）。
	CPU    string
	Memory string
	// SecretConfig 是 ConfigSchema 里声明了 secret: true 的键名。
	SecretConfig []string
	// ShellMembers 是 shell.members：编进这个外壳的成员，每项写 <组件ID>@<精确版本>（附录 A24）。
	ShellMembers []string
	// Port 覆盖默认的 deployment.port（8080）——同一个外壳下的 servedBy
	// 成员测试要用不同端口，否则端口冲突校验会先一步报错。
	Port int
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
	if len(c.ShellMembers) > 0 {
		b.WriteString("shell:\n  members:\n")
		for _, m := range c.ShellMembers {
			fmt.Fprintf(&b, "    - %s\n", m)
		}
	}
	if len(c.Migration) > 0 {
		fmt.Fprintf(&b, "migration:\n  command: [%s]\n", quotedList(c.Migration))
	}
	if len(c.ConfigSchema) > 0 {
		b.WriteString("configSchema:\n  type: object\n  properties:\n")
		for _, item := range c.ConfigSchema {
			name, def, _ := strings.Cut(item, ":")
			// 旧测试写的是 camelCase 键：统一转成环境变量名（附录 A10）
			fmt.Fprintf(&b, "    %s:\n      type: string\n      default: \"%s\"\n", legacyEnvKey(name), def)
			if slices.Contains(c.SecretConfig, name) {
				b.WriteString("      secret: true\n")
			}
		}
	}
	b.WriteString("deployment:\n  type: container\n")
	if c.Image == "-" {
		// 只有 build、没有 image：镜像由 brickkit build 构建，名字由组件 ID 与版本推出来
		b.WriteString("  build:\n    dockerfile: Dockerfile\n")
	} else {
		fmt.Fprintf(&b, "  image: %s\n", c.imageRef())
	}
	port := c.Port
	if port == 0 {
		port = 8080
	}
	fmt.Fprintf(&b, "  port: %d\n", port)
	if c.CPU != "" || c.Memory != "" {
		b.WriteString("  resources:\n    limits:\n")
		if c.CPU != "" {
			fmt.Fprintf(&b, "      cpu: \"%s\"\n", c.CPU)
		}
		if c.Memory != "" {
			fmt.Fprintf(&b, "      memory: \"%s\"\n", c.Memory)
		}
	}
	b.WriteString("healthCheck:\n  type: http\n  path: /healthz\n")
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
	Layout project.Layout
	// Sources 是写入 brickkit.yaml 的安装源片段。
	Sources []string
	// legacy 是最近一次写入的旧式单文件配置原文（见 testsupport_threelayer_test.go）。
	legacy string
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
	r := runIn(t, dir, "init", "--name", "my-erp", "--yes")
	require.Equal(t, 0, r.code, "init 应成功：%s%s", r.stdout, r.stderr)

	f := &projectFixture{Dir: dir, Layout: project.NewLayout(dir), Sources: sources}
	f.writeConfig(t, "components: []\n")
	return f
}

// repoDir 是组件本地仓库的目录——代码写在这里，up 的 mode: local 从这里启动：
// 本地安装源里有它时是那个目录（与 project.LocalRepo 同一个顺序），否则是 components/<id>/。
func (f *projectFixture) repoDir(t *testing.T, id string) string {
	t.Helper()
	matches, _ := filepath.Glob(filepath.Join(f.Dir, "*", filepath.FromSlash(id), "component.yaml"))
	sort.Strings(matches)
	for _, m := range matches {
		if !strings.HasPrefix(m, f.Layout.ComponentsDir()+string(filepath.Separator)) {
			return filepath.Dir(m)
		}
	}
	return filepath.Join(f.Layout.ComponentsDir(), filepath.FromSlash(id))
}

// addedProject 建一个"已经装好"若干组件的项目：组件各自放进一个本地安装源，
// refs 及其在 comps 里能找到的依赖闭包写进三层文件（add 在 P4 重建，这里不经过它）。
func addedProject(t *testing.T, comps []comp, refs ...string) *projectFixture {
	t.Helper()
	dir := t.TempDir()
	sources := localSource(t, dir, comps...)
	f := newProjectFixtureAt(t, dir, sources...)
	if len(refs) == 0 {
		return f
	}
	byRef := map[string]comp{}
	for _, c := range comps {
		byRef[c.ref()] = c
	}
	var order []string
	seen := map[string]bool{}
	var visit func(string)
	visit = func(ref string) {
		c, ok := byRef[ref]
		if !ok || seen[ref] {
			return
		}
		seen[ref] = true
		order = append(order, ref)
		for _, dep := range append(append([]string(nil), c.Requires...), c.Optional...) {
			visit(dep)
		}
	}
	for _, ref := range refs {
		visit(ref)
	}
	var body strings.Builder
	body.WriteString("components:\n")
	for _, ref := range order {
		c := byRef[ref]
		fmt.Fprintf(&body, "  - id: %s\n    version: %s\n", c.ID, c.Version)
	}
	f.writeConfig(t, body.String())
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
	f.rewrite(t, b.String())
}

// rewrite 用一份完整的旧式单文件配置重写三层文件。
//
// 旧测试常常只写 components、不写 sources：那时 add 已经把 Manifest 缓存好了，
// up 不需要安装源也能解析。这里不经过 add，所以没写 sources 时补上夹具自己的安装源。
func (f *projectFixture) rewrite(t *testing.T, text string) {
	t.Helper()
	if !strings.Contains("\n"+text, "\nsources:") && len(f.Sources) > 0 {
		text += "\nsources:\n" + strings.Join(f.Sources, "")
	}
	f.legacy = text
	writeLegacy(t, f.Dir, text)
}

// refs 返回 brickkit.yaml 里全部组件的 id@version。
func (f *projectFixture) refs(t *testing.T) []string {
	t.Helper()
	decl, err := projfile.ParseFile(f.Layout.DeclPath())
	require.NoError(t, err, "brickkit.yaml 应始终合法")
	var out []string
	for _, c := range decl.Components {
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

// oneLocalSource 把若干组件写进**同一个**本地安装源目录（add --local 的场景）。
//
// 与 localSource 的区别：那个是一个组件一个源，这个是一个源装一堆组件。
func oneLocalSource(t *testing.T, root string, comps ...comp) []string {
	t.Helper()
	for _, c := range comps {
		writeTree(t, filepath.Join(root, "shared", filepath.FromSlash(c.ID)), c.files())
	}
	return []string{"  - id: local-shared\n    type: local\n    path: ./shared\n"}
}

// ============================================================
// git 仓库
// ============================================================

func gitCmd(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
	out, err := cmd.CombinedOutput()
	require.NoErrorf(t, err, "git %s: %s", strings.Join(args, " "), out)
}

// ============================================================
// 市场 Mock（只实现 CLI 用到的端点）
// ============================================================

type mockComponent struct {
	Spec comp
	// SourceType 是 git（开源）或 registry（闭源）。
	SourceType string
	// GitURL 是开源组件的仓库地址（测试里指向本地 git 仓库）。
	GitURL string
	// FailDownload 为 true 时，产物下载端点返回 500。
	FailDownload bool
	// Status 是该版本在市场上的状态。为空时视作 stable。
	// draft / blocked 装不上，选最新版时要被跳过。
	Status string
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

// writeVersionList 实现 GET /components/{id}/versions。
func (m *mockMarket) writeVersionList(w http.ResponseWriter, componentID string) {
	list := make([]map[string]any, 0)
	for _, c := range m.comps {
		if c.Spec.ID != componentID {
			continue
		}
		status := c.Status
		if status == "" {
			status = "stable"
		}
		list = append(list, map[string]any{
			"componentId": c.Spec.ID, "version": c.Spec.Version, "status": status,
		})
	}
	if len(list) == 0 {
		writeJSONBody(w, http.StatusNotFound, map[string]any{"success": false})
		return
	}
	writeJSONBody(w, http.StatusOK, map[string]any{"success": true, "data": list})
}

// source 返回可写入 brickkit.yaml 的 sources 片段。
func (m *mockMarket) source() string {
	return fmt.Sprintf("  - id: market\n    type: market\n    url: %s/api/v1\n", m.server.URL)
}

func (m *mockMarket) handle(w http.ResponseWriter, r *http.Request) {
	rest := strings.TrimPrefix(r.URL.Path, "/api/v1/components/")
	if componentID, isList := strings.CutSuffix(rest, "/versions"); isList {
		m.writeVersionList(w, componentID)
		return
	}
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

// breakLocalManifest 把本地安装源里某个组件的 component.yaml 改坏（一处笔误）。
//
// 用来验证"Manifest 读不到时，命令还能不能干它本职的事"。本地安装源不吃缓存，
// 所以改坏这个文件就等于让依赖图解析必然失败——
// 而 down / status 的本职工作里没有一件需要依赖图。
func breakLocalManifest(t *testing.T, f *projectFixture, componentID string) {
	t.Helper()
	pattern := filepath.Join(f.Dir, "src*", filepath.FromSlash(componentID), "component.yaml")
	matches, err := filepath.Glob(pattern)
	require.NoError(t, err)
	require.NotEmpty(t, matches, "找不到 %s 的 component.yaml（%s）", componentID, pattern)

	for _, path := range matches {
		broken := strings.Replace(readFile(t, path), "  port: 8080", "  prot: 8080", 1)
		require.NoError(t, os.WriteFile(path, []byte(broken), 0o644))
	}
}

// runStdin 在指定目录执行 CLI，并喂入标准输入（用于确认提示、登录输入）。
func runStdin(t *testing.T, dir, input string, args ...string) result {
	t.Helper()
	var out, errBuf bytes.Buffer
	opts := &Options{
		WorkDir:  dir,
		LogLevel: logging.LevelOff,
		Stdin:    strings.NewReader(input),
		Stdout:   &out,
		Stderr:   &errBuf,
	}
	code := Run(NewRootCommand(opts), opts, args)
	return result{stdout: out.String(), stderr: errBuf.String(), code: code}
}

// config 返回 brickkit.yaml 的原文。
func (f *projectFixture) config(t *testing.T) string {
	t.Helper()
	return readFile(t, f.Layout.DeclPath())
}
