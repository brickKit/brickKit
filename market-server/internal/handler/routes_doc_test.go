package handler_test

// 本文件守着 007 §9 那张 API 表与真实路由表一致，**两个方向都守**。
//
// # 为什么需要它
//
// 007 §9 是**对外契约**：它自称"定稿"，读者会照着它写客户端。
// 而复核时实测两个方向都已经漂了：
//
//	文档写了、没实现（5 个）  POST /components、PUT /components/{id}、
//	                          DELETE /components/{id}、GET .../versions/{ver}、
//	                          POST .../artifacts（真实路径带 {artifactId}/upload）
//	实现了、文档没写（3 个）  GET /health、POST /auth/logout、GET /audit
//
// 两个方向的代价不一样，但都是真的：照着写客户端的人会撞 404；
// 而 `/api/v1/health` 在《市场部署与运维指南》里出现了四次
// （compose 的 healthcheck 探的就是它），却在这份定义市场 API 的文档里
// 一个字都没有——没人知道有这个端点。
//
// 已有的文档守卫一个都抓不到它：check-docs 查的是小节引用与断链，
// check-cli-docs 查的是 **CLI** 的命令与参数，docfields 查的是 YAML 字段名。
// HTTP 路径在它们眼里只是一段普通文本。
//
// # 真相来源是路由注册函数，不是又抄一份清单
//
// handler.Routes() 走的是 New() 用的**同一个** registerRoutes——
// 抄一张表就又多一份会漂的真相，而那正是这个测试要解决的问题。

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/brickkit/market-server/internal/handler"
)

// designDoc 是 007 组件市场设计的路径（本包在 market-server/internal/handler/ 下）。
const designDoc = "../../../design/007-组件市场设计.md"

// apiChapter 截出 007 §9 那一章。
//
// 只看这一章：别处（§3.7 发布请求示例、§17 交互流程）也会出现路径，
// 那些是叙述，不是清单——拿它们当"文档写了"会让守卫失去意义。
func apiChapter(t *testing.T) string {
	t.Helper()

	body, err := os.ReadFile(filepath.FromSlash(designDoc))
	require.NoError(t, err, "读不到设计书就没法比对，这本身就该让测试失败")
	text := string(body)

	start := strings.Index(text, "\n## 9. 市场 API 设计")
	require.Positive(t, start, "007 §9 那一章不见了——是不是改了标题？")
	end := strings.Index(text[start+1:], "\n## ")
	require.Positive(t, end, "找不到 §9 的结尾")
	return text[start : start+1+end]
}

// docRow 匹配 API 表里的一行：| GET | /api/v1/... | 说明 |
var docRow = regexp.MustCompile(`(?m)^\|\s*(GET|POST|PUT|DELETE)\s*\|\s*(/api/v1/\S*?)\s*\|`)

// documentedRoutes 收集 §9 表格里列出的端点，归一化成与 Routes() 同一种写法。
func documentedRoutes(t *testing.T) []string {
	t.Helper()

	var out []string
	for _, m := range docRow.FindAllStringSubmatch(apiChapter(t), -1) {
		out = append(out, m[1]+" "+normalize(m[2]))
	}
	require.NotEmpty(t, out, "一条都没解析出来——正则与表格写法对不上了，结论不可信")
	return out
}

// pathParam 匹配两种参数写法：文档的 {id} 与路由的 :id。
var pathParam = regexp.MustCompile(`\{[^}]+\}|:[^/]+`)

// normalize 把路径里的参数名抹平。
//
// 文档写 `{componentId}`、路由写 `:scope/:name`，比对的是**形状**不是命名。
// 组件 ID 是两段式 scope/name（002 §10.3），路由里因此是两段参数，
// 而文档写成一个 {componentId}——这不是分叉，是同一个东西的两种写法。
func normalize(path string) string {
	segments := strings.Split(strings.Trim(path, "/"), "/")

	out := make([]string, 0, len(segments))
	previousParam := false
	for _, seg := range segments {
		if !pathParam.MatchString(seg) {
			out = append(out, seg)
			previousParam = false
			continue
		}
		// 连续的参数段折成一个：{componentId} ≡ :scope/:name
		if !previousParam {
			out = append(out, "{}")
		}
		previousParam = true
	}
	return "/" + strings.Join(out, "/")
}

// 实现了的端点，007 §9 必须写进去。
func TestEveryRouteIsDocumented(t *testing.T) {
	documented := map[string]bool{}
	for _, r := range documentedRoutes(t) {
		documented[r] = true
	}

	var missing []string
	for _, route := range handler.Routes() {
		method, path, _ := strings.Cut(route, " ")
		if !documented[method+" "+normalize(path)] {
			missing = append(missing, route)
		}
	}
	sort.Strings(missing)

	assert.Empty(t, missing,
		"这些端点实现了，但 007 §9 里没有——没人知道它们存在：\n   %s",
		strings.Join(missing, "\n   "))
}

// 007 §9 写了的端点，必须真的实现。
//
// 反方向同样要守：照着一份"定稿"规范书写客户端的人，撞 404 时
// 第一反应是自己写错了，而不是文档错了。
func TestEveryDocumentedRouteExists(t *testing.T) {
	implemented := map[string]bool{}
	for _, route := range handler.Routes() {
		method, path, _ := strings.Cut(route, " ")
		implemented[method+" "+normalize(path)] = true
	}

	var phantom []string
	for _, r := range documentedRoutes(t) {
		if !implemented[r] {
			phantom = append(phantom, r)
		}
	}
	sort.Strings(phantom)

	assert.Empty(t, phantom,
		"007 §9 列了这些端点，但服务端没有实现——照着写客户端的人会撞 404：\n   %s\n"+
			"   要么实现它，要么把它从表里挪进「故意不做」那一节并写清理由",
		strings.Join(phantom, "\n   "))
}

// 自检：解析没坏。
//
// 照着 check-docs.py 的做法——一个永远返回"没问题"的守卫比没有守卫更糟，
// 因为它会让人以为这件事有人管着。
func TestRouteDocParsingSelfCheck(t *testing.T) {
	routes := handler.Routes()
	require.NotEmpty(t, routes, "一条路由都没取到")
	assert.Contains(t, routes, "GET /api/v1/health", "自检：这条一定存在")

	documented := documentedRoutes(t)
	assert.Contains(t, documented, "GET /api/v1/health",
		"自检：§9.7 里一定有它")

	assert.Equal(t, "/api/v1/components/{}/versions/{}/manifest",
		normalize("/api/v1/components/:scope/:name/versions/:version/manifest"),
		"自检：连续参数段要折成一个")
	assert.Equal(t, "/api/v1/components/{}/versions/{}/manifest",
		normalize("/api/v1/components/{id}/versions/{ver}/manifest"),
		"自检：文档写法与路由写法要归一到同一个形状")
}
