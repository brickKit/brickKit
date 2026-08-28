// 本文件是 Step 19「brickkit publish」的业务行为测试，
// 覆盖开发计划 19.6–19.15，以及 004 §3.11 / 010 §7.3 的发布流程。
package cli

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/brickkit/brickkit/internal/clierr"
)

// ============================================================
// 待发布组件的夹具
// ============================================================

// writeComponentDir 在 dir 下写出一个可发布的组件（component.yaml + 产物文件）。
func writeComponentDir(t *testing.T, dir string, c comp) string {
	t.Helper()
	root := filepath.Join(dir, filepath.FromSlash(c.ID))
	writeTree(t, root, c.files())
	return root
}

// loginTo 造出一份指向该市场的有效登录凭据。
func loginTo(t *testing.T, f *projectFixture, m *fakeMarket) {
	t.Helper()
	require.Equal(t, clierr.ExitOK,
		runStdin(t, f.Dir, "zhangsan\ncorrect-horse-battery\n", "login").code)
}

// artifactEntry 是市场返回的产物列表条目。
func artifactEntry(id, artifactType, format string, files ...string) map[string]any {
	return map[string]any{"id": id, "type": artifactType, "format": format, "files": files}
}

// publishable 是一个带一份 openapi 产物的组件。
func publishable() comp {
	return comp{ID: "people/basic", Version: "1.2.0", Artifacts: []string{"api-docs:openapi.json"}}
}

// ============================================================
// 19.7 / 19.11 正常发布
// ============================================================

// 发布是三步：建 draft 版本 → 上传产物 → 转 stable（市场侧 D103）。
// 顺序错了就会出现"版本已 stable 但文件还没传齐"的半成品。
func TestPublishFollowsDraftUploadStableOrder(t *testing.T) {
	m := newFakeMarket(t)
	m.artifacts = []map[string]any{artifactEntry("art-0", "api-docs", "openapi", "openapi.json")}
	f := newMarketProject(t, m, "")
	loginTo(t, f, m)
	root := writeComponentDir(t, f.Dir, publishable())

	r := runIn(t, f.Dir, "publish", "--path", root)

	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)
	assert.Equal(t, []string{
		"POST /auth/login",
		"POST /components/people/basic/versions",
		"GET /components/people/basic/versions/1.2.0/artifacts",
		"POST /components/people/basic/versions/1.2.0/artifacts/art-0/upload",
		"PUT /components/people/basic/versions/1.2.0",
	}, m.requests())
}

// 19.11 输出要逐项报告校验结果（004 §3.11 的输出样例）。
func TestPublishOutputReportsEachCheck(t *testing.T) {
	m := newFakeMarket(t)
	m.artifacts = []map[string]any{artifactEntry("art-0", "api-docs", "openapi", "openapi.json")}
	f := newMarketProject(t, m, "")
	loginTo(t, f, m)
	root := writeComponentDir(t, f.Dir, publishable())

	r := runIn(t, f.Dir, "publish", "--path", root)

	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)
	assert.Contains(t, r.stdout, "📤 发布 people/basic@1.2.0")
	assert.Contains(t, r.stdout, "✅ Manifest 校验通过")
	assert.Contains(t, r.stdout, "✅ 镜像引用有效")
	assert.Contains(t, r.stdout, "✅ artifacts 上传成功（1 个文件）")
	assert.Contains(t, r.stdout, "🎉 发布完成")
	assert.Contains(t, r.stdout, "组件：people/basic@1.2.0")
}

// 发布请求体必须带上完整 Manifest 与来源类型（007 §3.7）。
func TestPublishSendsManifestAndSourceType(t *testing.T) {
	m := newFakeMarket(t)
	f := newMarketProject(t, m, "")
	loginTo(t, f, m)
	root := writeComponentDir(t, f.Dir, comp{ID: "people/basic", Version: "1.2.0"})

	r := runIn(t, f.Dir, "publish", "--path", root,
		"--changelog", "新增人员状态字段", "--git-url", "https://github.com/brickkit/people-basic.git")
	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)

	var req struct {
		Version    string         `json:"version"`
		Status     string         `json:"status"`
		SourceType string         `json:"sourceType"`
		GitURL     string         `json:"gitUrl"`
		Changelog  string         `json:"changelog"`
		Manifest   map[string]any `json:"manifest"`
	}
	require.NoError(t, json.Unmarshal(m.find(t, "POST", "/versions").Body, &req))

	assert.Equal(t, "1.2.0", req.Version)
	assert.Equal(t, "draft", req.Status, "先建 draft，产物齐了再转 stable")
	assert.Equal(t, "git", req.SourceType)
	assert.Equal(t, "https://github.com/brickkit/people-basic.git", req.GitURL)
	assert.Equal(t, "新增人员状态字段", req.Changelog)
	require.NotEmpty(t, req.Manifest)
	assert.Equal(t, "brickkit/v1", req.Manifest["apiVersion"])

	metadata, ok := req.Manifest["metadata"].(map[string]any)
	require.True(t, ok, "Manifest 必须带 metadata：%v", req.Manifest)
	assert.Equal(t, "people/basic", metadata["id"])
}

// ============================================================
// 19.9 产物上传
// ============================================================

// 上传的必须是组件目录里那个文件的真实内容，路径也要对得上。
func TestPublishUploadsArtifactFiles(t *testing.T) {
	m := newFakeMarket(t)
	m.artifacts = []map[string]any{
		artifactEntry("art-0", "api-contract", "protobuf", "proto/people/v1/people.proto"),
		artifactEntry("art-1", "api-docs", "openapi", "openapi.json"),
	}
	f := newMarketProject(t, m, "")
	loginTo(t, f, m)
	root := writeComponentDir(t, f.Dir, comp{
		ID: "people/basic", Version: "1.2.0",
		Artifacts: []string{
			"api-contract:proto/people/v1/people.proto",
			"api-docs:openapi.json",
		},
	})

	r := runIn(t, f.Dir, "publish", "--path", root)
	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)

	proto := m.find(t, "POST", "/artifacts/art-0/upload")
	assert.Contains(t, proto.Query, "file=proto%2Fpeople%2Fv1%2Fpeople.proto")
	expected, err := os.ReadFile(filepath.Join(root, "proto/people/v1/people.proto"))
	require.NoError(t, err)
	assert.Equal(t, string(expected), string(proto.Body), "上传的内容必须是磁盘上那个文件")

	docs := m.find(t, "POST", "/artifacts/art-1/upload")
	assert.Contains(t, docs.Query, "file=openapi.json")
	assert.Contains(t, r.stdout, "✅ artifacts 上传成功（2 个文件）")
}

// Manifest 声明了产物文件但磁盘上没有 —— 必须在建版本之前就拦住，
// 否则市场里会留下一个永远转不了 stable 的 draft。
func TestPublishRejectsMissingArtifactFileBeforeCreatingVersion(t *testing.T) {
	m := newFakeMarket(t)
	f := newMarketProject(t, m, "")
	loginTo(t, f, m)
	root := writeComponentDir(t, f.Dir, publishable())
	require.NoError(t, os.Remove(filepath.Join(root, "openapi.json")))

	r := runIn(t, f.Dir, "publish", "--path", root)

	assert.Equal(t, clierr.ExitError, r.code)
	assert.Contains(t, r.stderr, "openapi.json")
	assert.NotContains(t, strings.Join(m.requests(), " "), "POST /components",
		"文件缺失时不该在市场里留下半截版本")
}

// ============================================================
// 19.8 未登录 / 19.6 Token 过期
// ============================================================

func TestPublishWithoutLoginFails(t *testing.T) {
	m := newFakeMarket(t)
	f := newMarketProject(t, m, "")
	root := writeComponentDir(t, f.Dir, publishable())

	r := runIn(t, f.Dir, "publish", "--path", root)

	assert.Equal(t, clierr.ExitError, r.code)
	assert.Contains(t, r.stderr, "未登录")
	assert.Contains(t, r.stderr, "brickkit login")
	assert.Empty(t, m.requests(), "未登录时不该向市场发任何请求")
}

// 19.6 Token 过期要明确说"过期了，重新登录"，而不是笼统的认证失败。
func TestPublishWithExpiredTokenAsksToLoginAgain(t *testing.T) {
	m := newFakeMarket(t)
	f := newMarketProject(t, m, "")
	loginTo(t, f, m)
	root := writeComponentDir(t, f.Dir, publishable())

	// 把凭据改成昨天过期
	expireCredentials(t, f.Dir, time.Now().Add(-24*time.Hour))

	r := runIn(t, f.Dir, "publish", "--path", root)

	assert.Equal(t, clierr.ExitError, r.code)
	assert.Contains(t, r.stderr, "过期")
	assert.Contains(t, r.stderr, "brickkit login")
}

// expireCredentials 把凭据的过期时间改成给定时刻。
func expireCredentials(t *testing.T, dir string, at time.Time) {
	t.Helper()
	path := credentialsPath(dir)
	data, err := os.ReadFile(path)
	require.NoError(t, err)

	var creds map[string]any
	require.NoError(t, json.Unmarshal(data, &creds))
	creds["expiresAt"] = at.UTC().Format(time.RFC3339)

	updated, err := json.Marshal(creds)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, updated, 0o600))
}

// ============================================================
// 19.14 / 19.15 Token 优先级
// ============================================================

// 004 §5.3：登录态优先于配置文件里的 authToken。
func TestPublishPrefersCredentialsOverAuthToken(t *testing.T) {
	m := newFakeMarket(t)
	f := newMarketProject(t, m, "token-from-config")
	loginTo(t, f, m)
	root := writeComponentDir(t, f.Dir, comp{ID: "people/basic", Version: "1.2.0"})

	require.Equal(t, clierr.ExitOK, runIn(t, f.Dir, "publish", "--path", root).code)

	assert.Equal(t, m.token, m.find(t, "POST", "/versions").Token,
		"19.14：两者都在时用 credentials 里的 Token")
}

// 19.15 没登录过就回落到配置里的 authToken。
func TestPublishFallsBackToAuthToken(t *testing.T) {
	m := newFakeMarket(t)
	f := newMarketProject(t, m, "token-from-config")
	root := writeComponentDir(t, f.Dir, comp{ID: "people/basic", Version: "1.2.0"})

	r := runIn(t, f.Dir, "publish", "--path", root)

	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)
	assert.Equal(t, "token-from-config", m.find(t, "POST", "/versions").Token)
}

// A 市场的 Token 绝不能发给 B 市场（008 安全边界）。
func TestPublishIgnoresCredentialsOfAnotherMarket(t *testing.T) {
	other := newFakeMarket(t)
	target := newFakeMarket(t)

	dir := t.TempDir()
	f := newProjectFixtureAt(t, dir, marketSourceFragment("target", target.url(), "token-from-config"))
	f.writeConfig(t, "components: []\nresources: []\n")
	// 登录的是另一个市场
	require.Equal(t, clierr.ExitOK,
		runStdin(t, f.Dir, "zhangsan\ncorrect-horse-battery\n",
			"login", "--market", other.url()).code)

	root := writeComponentDir(t, f.Dir, comp{ID: "people/basic", Version: "1.2.0"})
	require.Equal(t, clierr.ExitOK, runIn(t, f.Dir, "publish", "--path", root).code)

	assert.Equal(t, "token-from-config", target.find(t, "POST", "/versions").Token,
		"另一个市场的登录态不能用在这个市场上")
}

// ============================================================
// 19.12 Manifest 校验 / 19.13 镜像引用
// ============================================================

func TestPublishRejectsInvalidManifest(t *testing.T) {
	m := newFakeMarket(t)
	f := newMarketProject(t, m, "")
	loginTo(t, f, m)

	root := filepath.Join(f.Dir, "broken")
	require.NoError(t, os.MkdirAll(root, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "component.yaml"),
		[]byte("apiVersion: brickkit/v1\nkind: Component\nmetadata:\n  id: 不合法的ID\n"), 0o644))

	r := runIn(t, f.Dir, "publish", "--path", root)

	assert.Equal(t, clierr.ExitError, r.code)
	assert.Contains(t, r.stderr, "Manifest")
	assert.NotContains(t, strings.Join(m.requests(), " "), "POST /components")
}

func TestPublishFailsWhenComponentYamlMissing(t *testing.T) {
	m := newFakeMarket(t)
	f := newMarketProject(t, m, "")
	loginTo(t, f, m)

	r := runIn(t, f.Dir, "publish", "--path", filepath.Join(f.Dir, "nowhere"))

	assert.Equal(t, clierr.ExitError, r.code)
	assert.Contains(t, r.stderr, "component.yaml")
}

// 19.13 镜像引用无效时报错。生产环境不接受 latest（010 §5）。
func TestPublishRejectsInvalidImageReference(t *testing.T) {
	cases := []struct {
		name   string
		image  string
		reason string
	}{
		{"没有标签", "registry.example.com/people-basic", "标签"},
		{"用了 latest", "registry.example.com/people-basic:latest", "latest"},
		{"带空格", "registry.example.com/people basic:1.2.0", "不合法"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m := newFakeMarket(t)
			f := newMarketProject(t, m, "")
			loginTo(t, f, m)

			component := comp{ID: "people/basic", Version: "1.2.0"}
			root := writeComponentDir(t, f.Dir, component)
			replaceInFile(t, filepath.Join(root, "component.yaml"),
				"registry.example.com/people-basic:1.2.0", c.image)

			r := runIn(t, f.Dir, "publish", "--path", root)

			assert.Equal(t, clierr.ExitError, r.code, r.stdout)
			assert.Contains(t, r.stderr, "镜像")
			assert.Contains(t, r.stderr, c.reason)
			assert.NotContains(t, strings.Join(m.requests(), " "), "POST /components")
		})
	}
}

func replaceInFile(t *testing.T, path, old, replacement string) {
	t.Helper()
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	updated := strings.ReplaceAll(string(data), old, replacement)
	require.NotEqual(t, string(data), updated, "夹具里应存在待替换的字符串 %q", old)
	require.NoError(t, os.WriteFile(path, []byte(updated), 0o644))
}

// ============================================================
// 19.10 从归档目录发布
// ============================================================

// 010 §7.3：被 sync 归档到 .archived/ 的组件依然可以直接发布。
func TestPublishFromArchivedDirectory(t *testing.T) {
	m := newFakeMarket(t)
	f := newMarketProject(t, m, "")
	loginTo(t, f, m)

	archived := filepath.Join(f.Dir, "components", ".archived")
	root := writeComponentDir(t, archived, publishable())
	m.artifacts = []map[string]any{artifactEntry("art-0", "api-docs", "openapi", "openapi.json")}

	r := runIn(t, f.Dir, "publish", "--path", root)

	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)
	assert.Contains(t, r.stdout, "🎉 发布完成")
	assert.Contains(t, m.requests(), "POST /components/people/basic/versions")
}

// ============================================================
// 可见性
// ============================================================

// --visibility 是一次额外的调用，且必须在版本转 stable 之后 ——
// 先改可见性再发布，中间那段时间组件是"存在但可见性未定"的。
func TestPublishSetsVisibilityAfterStable(t *testing.T) {
	m := newFakeMarket(t)
	f := newMarketProject(t, m, "")
	loginTo(t, f, m)
	root := writeComponentDir(t, f.Dir, comp{ID: "people/basic", Version: "1.2.0"})

	r := runIn(t, f.Dir, "publish", "--path", root, "--visibility", "private")
	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)

	requests := m.requests()
	stable := requestIndex(requests, "PUT /components/people/basic/versions/1.2.0")
	visibility := requestIndex(requests, "PUT /components/people/basic/visibility")
	require.NotEqual(t, -1, visibility, "应调用可见性接口：%v", requests)
	assert.Greater(t, visibility, stable, "可见性应在转 stable 之后设置")

	var body struct {
		Visibility string `json:"visibility"`
	}
	require.NoError(t, json.Unmarshal(m.find(t, "PUT", "/visibility").Body, &body))
	assert.Equal(t, "private", body.Visibility)
	assert.Contains(t, r.stdout, "可见性：private")
}

// 不传 --visibility 时不动市场上已有的可见性设置。
func TestPublishWithoutVisibilityFlagDoesNotTouchIt(t *testing.T) {
	m := newFakeMarket(t)
	f := newMarketProject(t, m, "")
	loginTo(t, f, m)
	root := writeComponentDir(t, f.Dir, comp{ID: "people/basic", Version: "1.2.0"})

	require.Equal(t, clierr.ExitOK, runIn(t, f.Dir, "publish", "--path", root).code)

	assert.NotContains(t, strings.Join(m.requests(), " "), "/visibility")
}

func TestPublishRejectsUnknownVisibility(t *testing.T) {
	m := newFakeMarket(t)
	f := newMarketProject(t, m, "")
	loginTo(t, f, m)
	root := writeComponentDir(t, f.Dir, comp{ID: "people/basic", Version: "1.2.0"})

	r := runIn(t, f.Dir, "publish", "--path", root, "--visibility", "semi-public")

	assert.Equal(t, clierr.ExitUsage, r.code)
	assert.Contains(t, r.stderr, "public")
}

// requestIndex 返回请求在调用序列中的位置，找不到返回 -1。
func requestIndex(items []string, target string) int {
	for i, item := range items {
		if item == target {
			return i
		}
	}
	return -1
}

// ============================================================
// 市场返回错误时的表现
// ============================================================

// 版本已存在是最常见的发布失败，要直说，别让人去猜。
func TestPublishReportsDuplicateVersionClearly(t *testing.T) {
	m := newFakeMarket(t)
	m.overrides["/versions"] = marketResponse{
		status: 409,
		body: `{"success":false,"error":{"code":"VERSION_ALREADY_EXISTS",` +
			`"message":"版本已存在：people/basic@1.2.0"}}`,
	}
	f := newMarketProject(t, m, "")
	loginTo(t, f, m)
	root := writeComponentDir(t, f.Dir, comp{ID: "people/basic", Version: "1.2.0"})

	r := runIn(t, f.Dir, "publish", "--path", root)

	assert.Equal(t, clierr.ExitError, r.code)
	assert.Contains(t, r.stderr, "版本已存在")
	assert.Contains(t, r.stderr, "people/basic@1.2.0")
}

// 市场拒绝了 Manifest（比如保留变量冲突），要把市场给的原因显示出来，
// 而不是只说一句"发布失败"。
func TestPublishSurfacesMarketValidationDetails(t *testing.T) {
	m := newFakeMarket(t)
	m.overrides["/versions"] = marketResponse{
		status: 400,
		body: `{"success":false,"error":{"code":"CONFIG_SCHEMA_RESERVED_VARIABLE_CONFLICT",` +
			`"message":"配置项与保留环境变量冲突","details":{"conflicts":[` +
			`{"configKey":"databaseUrl","envVarName":"DATABASE_URL"}]}}}`,
	}
	f := newMarketProject(t, m, "")
	loginTo(t, f, m)
	root := writeComponentDir(t, f.Dir, comp{ID: "people/basic", Version: "1.2.0"})

	r := runIn(t, f.Dir, "publish", "--path", root)

	assert.Equal(t, clierr.ExitError, r.code)
	assert.Contains(t, r.stderr, "配置项与保留环境变量冲突")
	assert.Contains(t, r.stderr, "DATABASE_URL")
}

// 未登录（Token 被市场拒绝）时提示重新登录。
func TestPublishReportsUnauthorizedFromMarket(t *testing.T) {
	m := newFakeMarket(t)
	m.overrides["/versions"] = marketResponse{
		status: 401,
		body:   `{"success":false,"error":{"code":"UNAUTHORIZED","message":"令牌无效，请重新登录"}}`,
	}
	f := newMarketProject(t, m, "")
	loginTo(t, f, m)
	root := writeComponentDir(t, f.Dir, comp{ID: "people/basic", Version: "1.2.0"})

	r := runIn(t, f.Dir, "publish", "--path", root)

	assert.Equal(t, clierr.ExitError, r.code)
	assert.Contains(t, r.stderr, "brickkit login")
}

// ============================================================
// 签名（Step 20）
// ============================================================

// --sign 不带 --key 时找项目根目录下的 cosign.key（generate-key-pair 的默认名字）。
// 找不到就必须说清楚找的是哪个路径、以及怎么生成——绝不能假装签过了，
// 那会让使用者以为组件带签名，而市场里其实什么都没有。
//
// 本用例前身是 Step 20 之前的 TestPublishSignFlagReportsNotImplemented
// （断言 --sign 报"未实现"）。功能落地后按新行为重写。
func TestPublishSignWithoutKeyReportsWhereItLooked(t *testing.T) {
	m := newFakeMarket(t)
	f := newMarketProject(t, m, "")
	loginTo(t, f, m)
	root := writeComponentDir(t, f.Dir, comp{ID: "people/basic", Version: "1.2.0"})

	r := runIn(t, f.Dir, "publish", "--path", root, "--sign")

	assert.Equal(t, clierr.ExitError, r.code)
	assert.Contains(t, r.stderr, "cosign.key", "要说明默认找的是哪个文件")
	assert.Contains(t, r.stderr, "generate-key-pair", "要给出生成密钥对的命令")
	assert.NotContains(t, strings.Join(m.requests(), " "), "POST /components",
		"签不成就不该先把版本建出来——版本号不可回收")
}

// ============================================================
// 来源类型推断（007 §11）
// ============================================================

// 组件目录是个有 origin 的 Git 仓库 → 开源组件，仓库地址自动带上，
// 用户不用每次发布都手打一遍 --git-url。
func TestPublishInfersGitOriginFromComponentRepo(t *testing.T) {
	m := newFakeMarket(t)
	f := newMarketProject(t, m, "")
	loginTo(t, f, m)

	root := writeComponentDir(t, f.Dir, comp{ID: "people/basic", Version: "1.2.0"})
	gitCmd(t, root, "init", "-q", "-b", "main")
	gitCmd(t, root, "remote", "add", "origin", "https://github.com/brickkit/people-basic.git")

	r := runIn(t, f.Dir, "publish", "--path", root)
	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)

	var req struct {
		SourceType string `json:"sourceType"`
		GitURL     string `json:"gitUrl"`
	}
	require.NoError(t, json.Unmarshal(m.find(t, "POST", "/versions").Body, &req))
	assert.Equal(t, "git", req.SourceType)
	assert.Equal(t, "https://github.com/brickkit/people-basic.git", req.GitURL)
	assert.Contains(t, r.stdout, "来源类型：git")
	assert.Contains(t, r.stdout, "Git 仓库：https://github.com/brickkit/people-basic.git")
}

// 不是 Git 仓库（比如只有构建产物的闭源组件）→ 按 registry 走镜像分发。
func TestPublishDefaultsToRegistryWithoutGitRemote(t *testing.T) {
	m := newFakeMarket(t)
	f := newMarketProject(t, m, "")
	loginTo(t, f, m)
	root := writeComponentDir(t, f.Dir, comp{ID: "people/basic", Version: "1.2.0"})

	r := runIn(t, f.Dir, "publish", "--path", root)
	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)

	var req struct {
		SourceType string `json:"sourceType"`
		GitURL     string `json:"gitUrl"`
	}
	require.NoError(t, json.Unmarshal(m.find(t, "POST", "/versions").Body, &req))
	assert.Equal(t, "registry", req.SourceType)
	assert.Empty(t, req.GitURL)
	assert.Contains(t, r.stdout, "来源类型：registry")
}

// 显式声明闭源时，即使目录里有 git remote 也不把仓库地址交出去。
func TestPublishAsRegistryDoesNotLeakGitURL(t *testing.T) {
	m := newFakeMarket(t)
	f := newMarketProject(t, m, "")
	loginTo(t, f, m)

	root := writeComponentDir(t, f.Dir, comp{ID: "mycompany/approval", Version: "1.0.0"})
	gitCmd(t, root, "init", "-q", "-b", "main")
	gitCmd(t, root, "remote", "add", "origin", "git@internal.example.com:secret/approval.git")

	r := runIn(t, f.Dir, "publish", "--path", root, "--source-type", "registry")
	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)

	body := string(m.find(t, "POST", "/versions").Body)
	assert.NotContains(t, body, "internal.example.com", "闭源组件不该把内网仓库地址发出去")
	assert.Contains(t, r.stdout, "来源类型：registry")
}

// 没有市场安装源也没有 --market 时，提示要说清楚怎么补。
func TestPublishWithoutMarketTellsHowToSpecifyIt(t *testing.T) {
	dir := t.TempDir()
	f := newProjectFixtureAt(t, dir)
	root := writeComponentDir(t, dir, comp{ID: "people/basic", Version: "1.2.0"})

	r := runIn(t, f.Dir, "publish", "--path", root)

	assert.Equal(t, clierr.ExitError, r.code)
	assert.Contains(t, r.stderr, "--market")
}

// registry 带端口时冒号不是标签分隔符，不能误判成"没有标签"。
func TestPublishAcceptsRegistryWithPortAndDigest(t *testing.T) {
	for _, image := range []string{
		"registry.example.com:5000/people-basic:1.2.0",
		"registry.example.com/people-basic@sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
	} {
		t.Run(image, func(t *testing.T) {
			m := newFakeMarket(t)
			f := newMarketProject(t, m, "")
			loginTo(t, f, m)

			root := writeComponentDir(t, f.Dir, comp{ID: "people/basic", Version: "1.2.0"})
			replaceInFile(t, filepath.Join(root, "component.yaml"),
				"registry.example.com/people-basic:1.2.0", image)

			r := runIn(t, f.Dir, "publish", "--path", root)
			assert.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)
		})
	}
}

// ============================================================
// 上一次没发完留下的 draft：能续传就续传
// ============================================================

// interruptedPublish 造出"上一次发布中断在上传产物这一步"的现场。
//
// 走的是**真实路径**：让 /upload 返回 500，publish 在第二步失败，而市场那边
// 已经留下一个 draft 版本。手工编一份 Manifest 塞进去是构造不出来的——
// publish 会先把 image tag 钉成 digest 再发（P29），编的那份对不上。
func interruptedPublish(t *testing.T, spec comp) (*projectFixture, *fakeMarket, string) {
	t.Helper()
	dir := t.TempDir()
	writeTree(t, dir, spec.files())

	market := newFakeMarket(t)
	market.artifacts = []map[string]any{
		{"id": "a1", "type": "api-docs", "files": []string{"openapi.json"}},
	}
	market.overrides = map[string]marketResponse{
		"/upload": {status: 500, body: `{"success":false,"error":{"code":"INTERNAL","message":"存储抖了一下"}}`},
	}

	f := newProjectFixtureAt(t, t.TempDir(), marketSourceFragment("m", market.url(), "tok"))
	r := runIn(t, f.Dir, "publish", "--path", dir)
	require.NotEqual(t, clierr.ExitOK, r.code, "上传产物这一步应当失败：%s", r.stdout)
	require.Equal(t, "draft", market.storedStatus, "而版本已经建出来了")

	market.overrides = nil // 网络恢复
	return f, market, dir
}

// 中途失败之后再 publish，从"上传产物"接着做，而不是让版本号作废。
//
// # 为什么这条重要
//
// 发布是三步：建版本（draft）→ 逐个上传产物 → 转 stable（004 §3.11）。第一步一旦
// 成功，那个版本号就**永久占住了**——版本不可回收，软删除也占位（007 §6.4）。
// 于是网络在第二步抖一下，使用者就只剩"跳一个版本号"这一条路，而中断的原因
// 跟他毫无关系。
//
// 服务端本来就支持接着发：draft 可以继续上传产物再转 stable
// （SetVersionStatus 会先 ensureArtifactsUploaded）。缺的只是 CLI 这一侧。
func TestPublishResumesUnfinishedDraft(t *testing.T) {
	f, market, dir := interruptedPublish(t, comp{
		ID: "people/basic", Version: "1.0.0", Artifacts: []string{"api-docs:openapi.json"},
	})

	r := runIn(t, f.Dir, "publish", "--path", dir)

	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)
	assert.Contains(t, r.stdout, "续传", "要说清这是在接着上次那次往下做")
	assert.Equal(t, "stable", market.storedStatus, "这次要真的发出去")
	market.find(t, http.MethodPost, "/upload")
}

// 版本已经是 stable：那是真的"已经发过了"，照旧拦下。
func TestPublishRejectsAlreadyPublishedVersion(t *testing.T) {
	f, market, dir := interruptedPublish(t, comp{
		ID: "people/basic", Version: "1.0.0", Artifacts: []string{"api-docs:openapi.json"},
	})
	require.Equal(t, clierr.ExitOK, runIn(t, f.Dir, "publish", "--path", dir).code)
	require.Equal(t, "stable", market.storedStatus)

	r := runIn(t, f.Dir, "publish", "--path", dir)

	require.NotEqual(t, clierr.ExitOK, r.code, r.stdout)
	assert.Contains(t, r.stderr, "已经发布过", "要说清它不是半成品")
	assert.Contains(t, r.stderr, "换一个版本号")
}

// draft 里登记的 Manifest 与本地不一样 → **绝不续传**。
//
// 这是续传唯一危险的地方：draft 里那份 Manifest 是上一次登记的。组件改过之后
// 用同一个版本号再发，闷头续传会把**旧 Manifest** 配上**新产物**发出去——
// 比烧掉版本号更糟，因为它悄无声息地成功了。
func TestPublishRefusesToResumeWhenManifestChanged(t *testing.T) {
	f, _, dir := interruptedPublish(t, comp{
		ID: "people/basic", Version: "1.0.0", Artifacts: []string{"api-docs:openapi.json"},
	})

	// 改了组件，但版本号没动
	changed := comp{ID: "people/basic", Version: "1.0.0",
		Artifacts: []string{"api-docs:openapi.json"}, Migration: []string{"/app/migrate"}}
	writeTree(t, dir, changed.files())

	r := runIn(t, f.Dir, "publish", "--path", dir)

	require.NotEqual(t, clierr.ExitOK, r.code, r.stdout)
	assert.Contains(t, r.stderr, "不一样", "要说清是 Manifest 变了")
	assert.Contains(t, r.stderr, "换一个版本号")
}
