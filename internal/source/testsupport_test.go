package source

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"

	"github.com/brickkit/brickkit/internal/config"
)

// ============================================================
// 组件构造辅助：生成合法的 component.yaml 与组件仓库目录
// ============================================================

// artifactSpec 描述 Manifest 中的一条 artifacts 声明。
type artifactSpec struct {
	Type   string
	Format string
	Files  []string
}

// componentSpec 描述一个用于测试的组件（Manifest + 仓库中的文件）。
type componentSpec struct {
	ID          string
	Version     string
	Description string // 用于区分"同一组件来自不同安装源"的场景
	Image       string
	Artifacts   []artifactSpec
	// Files 是组件仓库根目录下的文件内容（相对路径 → 内容）。
	// artifacts 声明的文件都应在此提供，否则 local / git 源下载时会失败。
	Files map[string]string
}

// yamlText 渲染出一份合法的 component.yaml（字段依据 002 §2.2）。
func (s componentSpec) yamlText() string {
	desc := s.Description
	if desc == "" {
		desc = "测试组件"
	}
	image := s.Image
	if image == "" {
		image = "registry.example.com/" + strings.ReplaceAll(s.ID, "/", "-") + ":" + s.Version
	}

	var b strings.Builder
	b.WriteString("apiVersion: brickkit/v1\n")
	b.WriteString("kind: Component\n")
	b.WriteString("metadata:\n")
	fmt.Fprintf(&b, "  id: %s\n", s.ID)
	fmt.Fprintf(&b, "  name: %s\n", "测试组件 "+s.ID)
	fmt.Fprintf(&b, "  version: %s\n", s.Version)
	fmt.Fprintf(&b, "  description: %s\n", desc)
	if len(s.Artifacts) > 0 {
		b.WriteString("artifacts:\n")
		for _, a := range s.Artifacts {
			fmt.Fprintf(&b, "  - type: %s\n", a.Type)
			if a.Format != "" {
				fmt.Fprintf(&b, "    format: %s\n", a.Format)
			}
			b.WriteString("    files:\n")
			for _, f := range a.Files {
				fmt.Fprintf(&b, "      - %s\n", f)
			}
		}
	}
	b.WriteString("deployment:\n")
	b.WriteString("  type: container\n")
	fmt.Fprintf(&b, "  image: %s\n", image)
	b.WriteString("  port: 8080\n")
	b.WriteString("healthCheck:\n")
	b.WriteString("  type: http\n")
	b.WriteString("  path: /healthz\n")
	return b.String()
}

// componentDir 返回该组件在安装源目录中的相对路径：<scope>/<name>/。
func (s componentSpec) componentDir() string {
	return filepath.FromSlash(s.ID)
}

// writeComponent 把组件写入安装源目录（local 源目录或 git 仓库工作区）。
func writeComponent(t *testing.T, sourceRoot string, s componentSpec) string {
	t.Helper()
	dir := filepath.Join(sourceRoot, s.componentDir())
	require.NoError(t, os.MkdirAll(dir, 0o755))
	writeFile(t, filepath.Join(dir, "component.yaml"), s.yamlText())
	for name, content := range s.Files {
		writeFile(t, filepath.Join(dir, filepath.FromSlash(name)), content)
	}
	return dir
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	require.NoError(t, err, "读取 %s", path)
	return string(data)
}

// protoSpec 是一个带两类 artifacts 的典型组件（对齐 002 §12.5 department/tree）。
func protoSpec(id, version string) componentSpec {
	return componentSpec{
		ID:      id,
		Version: version,
		Artifacts: []artifactSpec{
			{Type: "api-contract", Format: "protobuf", Files: []string{"proto/department/v1/department.proto"}},
			{Type: "api-docs", Format: "openapi", Files: []string{"openapi.json"}},
		},
		Files: map[string]string{
			"proto/department/v1/department.proto": "// proto of " + id + "@" + version + "\n",
			"openapi.json":                         `{"openapi":"3.0.0","info":{"version":"` + version + `"}}`,
		},
	}
}

// ============================================================
// 项目辅助
// ============================================================

// newProject 建一个空项目根目录并返回布局。
func newProject(t *testing.T) config.Layout {
	t.Helper()
	return config.NewLayout(t.TempDir(), "")
}

// newClient 构造被测客户端，并保证测试结束时释放临时 clone。
func newClient(t *testing.T, layout config.Layout, cfg *config.Config, opts Options) *Client {
	t.Helper()
	c, err := New(layout, cfg, opts)
	require.NoError(t, err)
	t.Cleanup(func() { _ = c.Close() })
	return c
}

// cfgWithSources 构造只含安装源的最小项目配置。
func cfgWithSources(sources ...config.Source) *config.Config {
	return &config.Config{
		Project: "test-project",
		Deploy:  config.Deploy{Target: config.TargetDocker},
		Sources: sources,
	}
}

func boolPtr(b bool) *bool { return &b }

// at 返回一个固定时刻的时钟，用于 Token 过期判定。
func at(ts string) func() time.Time {
	t, err := time.Parse(time.RFC3339, ts)
	if err != nil {
		panic(err)
	}
	return func() time.Time { return t }
}

// ============================================================
// git 辅助
// ============================================================

// newGitRepo 在 dir 中初始化一个 git 仓库并提交当前内容，返回可用于 clone 的 URL。
func newGitRepo(t *testing.T, dir string) string {
	t.Helper()
	git(t, dir, "init", "-q", "-b", "main")
	git(t, dir, "add", "-A")
	git(t, dir,
		"-c", "user.email=test@example.com",
		"-c", "user.name=BrickKit Test",
		"commit", "-q", "-m", "init",
	)
	return dir
}

func git(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
	out, err := cmd.CombinedOutput()
	require.NoErrorf(t, err, "git %s: %s", strings.Join(args, " "), out)
}

// ============================================================
// 市场 API Mock（007 §9.1 端点表）
// ============================================================

type recordedRequest struct {
	Method string
	Path   string
	Query  string
	Auth   string
}

type marketMock struct {
	server *httptest.Server
	// components 的键是 "<id>@<version>"
	components map[string]componentSpec

	// token 非空时，未携带 "Bearer <token>" 的请求返回 401。
	token string
	// rawYAML 为 true 时，manifest 端点直接返回 YAML 正文（不带 JSON 信封）。
	rawYAML bool
	// failDownload 为 true 时，产物下载端点返回 500。
	failDownload bool
	// failManifest 为 true 时，Manifest 端点返回 500。
	failManifest bool
	// failArtifactList 为 true 时，产物列表端点返回 503。
	failArtifactList bool
	// garbageArtifactList 为 true 时，产物列表端点返回无法解析的正文。
	garbageArtifactList bool

	mu       sync.Mutex
	requests []recordedRequest
}

func newMarketMock(t *testing.T, specs ...componentSpec) *marketMock {
	t.Helper()
	m := &marketMock{components: map[string]componentSpec{}}
	for _, s := range specs {
		m.components[s.ID+"@"+s.Version] = s
	}
	m.server = httptest.NewServer(http.HandlerFunc(m.handle))
	t.Cleanup(m.server.Close)
	return m
}

// URL 返回市场 API 基地址（含 /api/v1 前缀，对齐 003 §6.2）。
func (m *marketMock) URL() string { return m.server.URL + "/api/v1" }

func (m *marketMock) recorded() []recordedRequest {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]recordedRequest(nil), m.requests...)
}

func (m *marketMock) handle(w http.ResponseWriter, r *http.Request) {
	m.mu.Lock()
	m.requests = append(m.requests, recordedRequest{
		Method: r.Method, Path: r.URL.Path, Query: r.URL.RawQuery,
		Auth: r.Header.Get("Authorization"),
	})
	m.mu.Unlock()

	if m.token != "" && r.Header.Get("Authorization") != "Bearer "+m.token {
		writeJSON(w, http.StatusUnauthorized, map[string]any{
			"success": false,
			"error":   map[string]any{"code": "UNAUTHORIZED", "message": "认证失败"},
		})
		return
	}

	rest := strings.TrimPrefix(r.URL.Path, "/api/v1/components/")
	idPart, tail, ok := strings.Cut(rest, "/versions/")
	if !ok {
		http.NotFound(w, r)
		return
	}
	version, action, _ := strings.Cut(tail, "/")
	spec, found := m.components[idPart+"@"+version]
	if !found {
		writeJSON(w, http.StatusNotFound, map[string]any{
			"success": false,
			"error":   map[string]any{"code": "NOT_FOUND", "message": "组件版本不存在"},
		})
		return
	}

	switch {
	case action == "manifest":
		m.writeManifest(w, spec)
	case action == "artifacts":
		m.writeArtifactList(w, spec)
	case strings.HasPrefix(action, "artifacts/"):
		m.writeArtifactFile(w, r, spec, strings.TrimSuffix(strings.TrimPrefix(action, "artifacts/"), "/download"))
	default:
		http.NotFound(w, r)
	}
}

func (m *marketMock) writeManifest(w http.ResponseWriter, spec componentSpec) {
	if m.failManifest {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	if m.rawYAML {
		w.Header().Set("Content-Type", "application/yaml")
		_, _ = w.Write([]byte(spec.yamlText()))
		return
	}
	var doc map[string]any
	if err := yaml.Unmarshal([]byte(spec.yamlText()), &doc); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"success": true,
		"data": map[string]any{
			"manifest":   doc,
			"sourceType": "git",
			"gitUrl":     "https://example.com/" + strings.ReplaceAll(spec.ID, "/", "-") + ".git",
		},
	})
}

func (m *marketMock) writeArtifactList(w http.ResponseWriter, spec componentSpec) {
	if m.failArtifactList {
		http.Error(w, "service unavailable", http.StatusServiceUnavailable)
		return
	}
	if m.garbageArtifactList {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte("<html>proxy error</html>"))
		return
	}
	list := make([]map[string]any, 0, len(spec.Artifacts))
	for i, a := range spec.Artifacts {
		list = append(list, map[string]any{
			"id":     artifactID(i),
			"type":   a.Type,
			"format": a.Format,
			"files":  a.Files,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true, "data": list})
}

func (m *marketMock) writeArtifactFile(w http.ResponseWriter, r *http.Request, spec componentSpec, id string) {
	if m.failDownload {
		http.Error(w, "storage unavailable", http.StatusInternalServerError)
		return
	}
	file := r.URL.Query().Get("file")
	for i, a := range spec.Artifacts {
		if artifactID(i) != id {
			continue
		}
		for _, f := range a.Files {
			if f != file {
				continue
			}
			content, ok := spec.Files[f]
			if !ok {
				http.NotFound(w, r)
				return
			}
			w.Header().Set("Content-Type", "application/octet-stream")
			_, _ = w.Write([]byte(content))
			return
		}
	}
	http.NotFound(w, r)
}

func artifactID(i int) string { return fmt.Sprintf("art-%d", i) }

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}
