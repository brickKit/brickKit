// Package e2e 把真正的市场服务端与真正的 CLI 接起来跑一遍：发布出去的组件，
// 在另一个项目里 add 回来、能生成部署文件。
//
// CLI 自己的测试用假市场（快，而且钉住了请求的形状）；市场自己的测试直接调服务层与
// handler。两边各自都绿，并不证明"lint 通过的组件发得上去、装得回来"——两份校验规则
// 曾经各自漂移，市场一度收不下任何三层格式的组件，而两边的测试都是绿的。这个包守的
// 就是两边之间的那条缝：服务端用内存仓储与内存对象存储起在 httptest 上，CLI 走
// cli.NewRootCommand / cli.Run，与使用者敲命令是同一条路径。
package e2e

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/bcrypt"

	"github.com/brickkit/brickkit/internal/cli"
	"github.com/brickkit/brickkit/internal/project"

	"github.com/brickkit/brickkit/market-server/internal/handler"
	"github.com/brickkit/brickkit/market-server/internal/repo"
	"github.com/brickkit/brickkit/market-server/internal/service"
	"github.com/brickkit/brickkit/market-server/internal/storage"
)

const (
	publisher = "alice"
	password  = "correct-horse-battery"
	// fakeDigest 代替 registry 查询：publish 会把镜像 tag 钉成 digest，测试里没有 registry。
	fakeDigest = "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
)

// startMarket 起一个真正的市场服务端，并注册好发布者账号。返回 API 地址（含 /api/v1）。
func startMarket(t *testing.T) string {
	t.Helper()
	svc := service.New(repo.NewMemory(), storage.NewMemory(), service.Options{BcryptCost: bcrypt.MinCost})
	server := httptest.NewServer(handler.New(svc, handler.Options{Version: "e2e", Logf: t.Logf}))
	t.Cleanup(server.Close)
	api := server.URL + "/api/v1"

	resp := postJSON(t, api+"/auth/register", "", map[string]any{"username": publisher, "password": password})
	require.Equal(t, http.StatusCreated, resp.status, "注册发布者：%s", resp.body)
	return api
}

// isolate 让 CLI 不读写这台机器上的任何个人设置：语言、用户目录。
func isolate(t *testing.T) {
	t.Helper()
	t.Setenv("BRICKKIT_LANG", "en")
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
}

// brickkit 在 dir 里跑一条 CLI 命令，返回退出码与全部输出。
func brickkit(t *testing.T, dir, stdin string, args ...string) (int, string) {
	t.Helper()
	var out bytes.Buffer
	opts := cli.NewOptions()
	opts.WorkDir = dir
	opts.Stdin = strings.NewReader(stdin)
	opts.Stdout = &out
	opts.Stderr = &out
	opts.RepoCacheDir = t.TempDir()
	opts.ResolveDigest = func(context.Context, string) (string, error) { return fakeDigest, nil }
	code := cli.Run(cli.NewRootCommand(opts), opts, args)
	return code, out.String()
}

// mustRun 跑一条命令并要求它成功。
func mustRun(t *testing.T, dir string, args ...string) string {
	t.Helper()
	code, out := brickkit(t, dir, "", args...)
	require.Equal(t, 0, code, "brickkit %s\n%s", strings.Join(args, " "), out)
	return out
}

// newComponent 用 brickkit new 生成一个独立组件仓库，再像作者那样填上市场发布需要的东西：
// 镜像地址（骨架只写了 build）、一个配置项、组件文档。返回组件目录。
func newComponent(t *testing.T, id, image string, extraNew ...string) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), strings.ReplaceAll(id, "/", "-"))
	mustRun(t, t.TempDir(), append([]string{"new", id, "--path", dir}, extraNew...)...)

	path := filepath.Join(dir, "component.yaml")
	edit(t, path, "  type: container\n", "  type: container\n  image: "+image+"\n")
	writeFile(t, filepath.Join(dir, "Dockerfile"), "FROM scratch\n")
	writeFile(t, filepath.Join(dir, "BRICKKIT.md"), "# "+id+"\n\nCall GET /hello. Set GREETING to change the reply.\n")
	return dir
}

// withConfig 给组件加一个带默认值的配置项（configSchema 的键就是环境变量名）。
func withConfig(t *testing.T, dir string) {
	t.Helper()
	appendFile(t, filepath.Join(dir, "component.yaml"),
		"\nconfigSchema:\n  properties:\n    GREETING:\n      type: string\n      default: hello\n")
}

// publish 在组件目录里登录并发布（凭据写在组件目录的 .brickkit/ 下）。
func publish(t *testing.T, api, dir string) string {
	t.Helper()
	code, out := brickkit(t, dir, password+"\n", "login", "--market", api, "--username", publisher, "--password-stdin")
	require.Equal(t, 0, code, "login\n%s", out)
	return mustRun(t, dir, "publish", "--path", dir, "--market", api,
		"--git-url", "https://git.example.com/"+filepath.Base(dir)+".git")
}

// consumer 用 brickkit init 建一个项目，并加上指向市场的安装源。返回项目目录。
func consumer(t *testing.T, api, name string) string {
	t.Helper()
	parent := t.TempDir()
	mustRun(t, parent, "init", name, "--no-skills")
	dir := filepath.Join(parent, name)
	edit(t, filepath.Join(dir, project.FileDecl), "sources:\n",
		"sources:\n  - name: market\n    type: market\n    url: "+api+"\n")
	return dir
}

// 主路径：lint 通过的组件发布到真的市场，在另一个项目里 add 回来，文档随之缓存，
// 配置骨架写好，up --dry-run 生成部署文件。
func TestPublishAndAddBack(t *testing.T) {
	isolate(t)
	api := startMarket(t)

	comp := newComponent(t, "people/basic", "registry.example.com/people-basic:0.1.0")
	withConfig(t, comp)
	mustRun(t, comp, "lint")
	out := publish(t, api, comp)
	assert.Contains(t, out, "BRICKKIT.md")

	proj := consumer(t, api, "shop")
	mustRun(t, proj, "add", "people/basic@0.1.0", "--yes")

	layout := project.NewLayout(proj)
	doc, err := os.ReadFile(layout.CachedDocPath("people/basic", "0.1.0"))
	require.NoError(t, err, "市场给的 BRICKKIT.md 应当缓存在 Manifest 旁边")
	assert.Equal(t, readFile(t, filepath.Join(comp, "BRICKKIT.md")), string(doc))

	config := readFile(t, filepath.Join(proj, project.DirConfig, "people-basic.yaml"))
	assert.Contains(t, config, "GREETING", "add 写出这个组件的配置骨架")

	out = mustRun(t, proj, "up", "--dry-run")
	compose := readFile(t, filepath.Join(layout.GeneratedDir(), "compose.yaml"))
	assert.Contains(t, compose, "people-basic-0-1-0", out)
	assert.Contains(t, compose, fakeDigest, "部署的是发布时钉住的那个镜像")
}

// 外壳：成员与外壳各自发布，add 外壳时成员一起进来，部署文件里成员嵌在外壳条目下。
func TestPublishAndAddBackShell(t *testing.T) {
	isolate(t)
	api := startMarket(t)

	member := newComponent(t, "people/basic", "registry.example.com/people-basic:0.1.0")
	mustRun(t, member, "lint")
	publish(t, api, member)

	shell := newComponent(t, "shop/gateway", "registry.example.com/shop-gateway:0.1.0", "--shell")
	edit(t, filepath.Join(shell, "component.yaml"), "example/member@0.1.0", "people/basic@0.1.0")
	edit(t, filepath.Join(shell, "component.yaml"), "  port: 8080", "  port: 9000")
	mustRun(t, shell, "lint")
	publish(t, api, shell)

	proj := consumer(t, api, "shop")
	mustRun(t, proj, "add", "shop/gateway@0.1.0", "--yes")

	decl := readFile(t, filepath.Join(proj, project.FileDecl))
	assert.Contains(t, decl, "kind: shell", "brickkit.yaml 记下外壳")
	assert.Contains(t, decl, "people/basic", "成员随外壳一起加进来")
	deploy := readFile(t, filepath.Join(proj, project.FileDeploy))
	gateway := strings.Index(deploy, "shop/gateway")
	members := strings.Index(deploy, "members:")
	require.True(t, gateway >= 0 && members > gateway, "deploy.yaml 里成员嵌在外壳条目下：\n%s", deploy)
	assert.Contains(t, deploy[members:], "people/basic")

	out := mustRun(t, proj, "up", "--dry-run")
	compose := readFile(t, filepath.Join(project.NewLayout(proj).GeneratedDir(), "compose.yaml"))
	assert.Contains(t, compose, "shop-gateway-0-1-0", out)
	assert.NotContains(t, compose, "people-basic-0-1-0:", "成员由外壳承载，没有自己的 service")
}

// 绕过 CLI 直接调 API：市场自己做同一份校验，每个问题单独给出字段。
func TestRawPublishIsValidatedByTheMarket(t *testing.T) {
	isolate(t)
	api := startMarket(t)

	login := postJSON(t, api+"/auth/login", "", map[string]any{"username": publisher, "password": password})
	require.Equal(t, http.StatusOK, login.status, "%s", login.body)
	var session struct {
		Data struct {
			Token string `json:"token"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(login.body, &session))

	manifest := map[string]any{
		"apiVersion":  "brickkit/v1",
		"kind":        "Component",
		"metadata":    map[string]any{"id": "people/basic", "name": "People", "version": "0.1.0", "description": "x"},
		"deployment":  map[string]any{"type": "container", "image": "registry.example.com/people-basic:0.1.0", "port": 8080},
		"healthCheck": map[string]any{"type": "http", "path": "/healthz"},
		"futureField": true,
	}
	resp := postJSON(t, api+"/components/people/basic/versions", session.Data.Token, map[string]any{
		"version": "0.1.0", "manifest": manifest, "sourceType": "git", "gitUrl": "https://git.example.com/p.git",
	})

	require.Equal(t, http.StatusBadRequest, resp.status, "%s", resp.body)
	var body struct {
		Error struct {
			Code    string `json:"code"`
			Details struct {
				Problems []struct {
					Field  string `json:"field"`
					Reason string `json:"reason"`
				} `json:"problems"`
			} `json:"details"`
		} `json:"error"`
	}
	require.NoError(t, json.Unmarshal(resp.body, &body))
	assert.Equal(t, "MANIFEST_INVALID", body.Error.Code)
	require.Len(t, body.Error.Details.Problems, 1, "%s", resp.body)
	assert.Equal(t, "futureField", body.Error.Details.Problems[0].Field)
	assert.NotEmpty(t, body.Error.Details.Problems[0].Reason)
}

// ============================================================
// 辅助
// ============================================================

type httpResult struct {
	status int
	body   []byte
}

func postJSON(t *testing.T, url, token string, body any) httpResult {
	t.Helper()
	raw, err := json.Marshal(body)
	require.NoError(t, err)
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(raw))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	var buf bytes.Buffer
	_, err = buf.ReadFrom(resp.Body)
	require.NoError(t, err)
	return httpResult{status: resp.StatusCode, body: buf.Bytes()}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	return string(data)
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
}

func appendFile(t *testing.T, path, content string) {
	t.Helper()
	writeFile(t, path, readFile(t, path)+content)
}

// edit 把文件里的 old 换成 replacement；old 必须存在——骨架变了要让测试响亮地失败，
// 而不是悄悄地什么也没改。
func edit(t *testing.T, path, old, replacement string) {
	t.Helper()
	content := readFile(t, path)
	require.Contains(t, content, old, "%s 里没有 %q", path, old)
	writeFile(t, path, strings.Replace(content, old, replacement, 1))
}
