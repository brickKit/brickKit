package handler_test

// 本文件守着市场 API 参考文档（docs/{zh,en}/11-reference/06-market-api.md）的端点表
// 与真实路由表一致，**两个方向都守**，并且中英两份列的端点一模一样。
//
// # 为什么需要它
//
// 那张表是对外契约，读者会照着它写客户端。两个方向的漂移代价不一样，但都是真的：
// 文档写了、没实现，照着写客户端的人撞 404；实现了、文档没写，使用者无从知道端点存在——
// `/api/v1/health` 曾经就是这样：自托管的 healthcheck 探的是它，API 文档里一个字都没有。
//
// 别的文档守卫抓不到它：它们查小节引用、断链、CLI 命令与参数、YAML 字段名；
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

	"github.com/brickkit/brickkit/market-server/internal/handler"
)

// referenceDocs 是两种语言的市场 API 参考（本包在 market-server/internal/handler/ 下）。
var referenceDocs = []string{
	"../../../docs/zh/11-reference/06-market-api.md",
	"../../../docs/en/11-reference/06-market-api.md",
}

// docRow 匹配端点表里的一行：| GET | `/api/v1/...` | 说明 |（路径两边的反引号可有可无）。
//
// 方法单独占一列才算：「故意不做」那张表把方法和路径写在同一格里，是叙述，不是清单。
var docRow = regexp.MustCompile("(?m)^\\|\\s*(GET|POST|PUT|DELETE)\\s*\\|\\s*`?(/api/v1/[^`|\\s]*)`?\\s*\\|")

// routesIn 收集一份文档端点表里的端点，归一化成与 Routes() 同一种写法。
func routesIn(t *testing.T, path string) []string {
	t.Helper()

	body, err := os.ReadFile(filepath.FromSlash(path))
	require.NoError(t, err, "读不到 API 参考就没法比对，这本身就该让测试失败")
	var out []string
	for _, m := range docRow.FindAllStringSubmatch(string(body), -1) {
		out = append(out, m[1]+" "+normalize(m[2]))
	}
	require.NotEmpty(t, out, "%s 一条都没解析出来——正则与表格写法对不上了，结论不可信", path)
	sort.Strings(out)
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

// 参考文档写了的端点，必须真的实现。
//
// 反方向同样要守：照着一份"定稿"规范书写客户端的人，撞 404 时
// 第一反应是自己写错了，而不是文档错了。
func TestEveryDocumentedRouteExists(t *testing.T) {
	for _, doc := range referenceDocs {
		t.Run(filepath.Base(filepath.Dir(filepath.Dir(doc))), func(t *testing.T) {
			documentedRoutesExist(t, doc)
		})
	}
}

func documentedRoutesExist(t *testing.T, doc string) {
	implemented := map[string]bool{}
	for _, route := range handler.Routes() {
		method, path, _ := strings.Cut(route, " ")
		implemented[method+" "+normalize(path)] = true
	}

	var phantom []string
	for _, r := range routesIn(t, doc) {
		if !implemented[r] {
			phantom = append(phantom, r)
		}
	}
	sort.Strings(phantom)

	assert.Empty(t, phantom,
		"%s 列了这些端点，但服务端没有实现——照着写客户端的人会撞 404：\n   %s\n"+
			"   要么实现它，要么把它从表里挪进「故意不做」那一节并写清理由",
		doc, strings.Join(phantom, "\n   "))
}

// 实现了的端点，必须真的写进 API 参考。一个只活在代码里的端点，使用者无从知道
// 它存在，也就无从知道能不能依赖它。
func TestEveryRouteIsDocumented(t *testing.T) {
	for _, doc := range referenceDocs {
		t.Run(filepath.Base(filepath.Dir(filepath.Dir(doc))), func(t *testing.T) {
			everyRouteDocumentedIn(t, doc)
		})
	}
}

func everyRouteDocumentedIn(t *testing.T, doc string) {
	documented := map[string]bool{}
	for _, r := range routesIn(t, doc) {
		documented[r] = true
	}

	var undocumented []string
	for _, route := range handler.Routes() {
		method, path, _ := strings.Cut(route, " ")
		normalized := method + " " + normalize(path)
		if !documented[normalized] {
			undocumented = append(undocumented, route)
		}
	}
	sort.Strings(undocumented)

	assert.Empty(t, undocumented,
		"服务端实现了这些端点，但 %s 没写——没人知道它们存在：\n   %s\n"+
			"   补进参考文档（中英两份）对应的小节，或者如果它不该对外，说清楚为什么",
		doc, strings.Join(undocumented, "\n   "))
}

// 自检：解析没坏。
//
// 照着 check-docs.py 的做法——一个永远返回"没问题"的守卫比没有守卫更糟，
// 因为它会让人以为这件事有人管着。
func TestRouteDocParsingSelfCheck(t *testing.T) {
	routes := handler.Routes()
	require.NotEmpty(t, routes, "一条路由都没取到")
	assert.Contains(t, routes, "GET /api/v1/health", "自检：这条一定存在")

	for _, path := range referenceDocs {
		assert.Contains(t, routesIn(t, path), "GET /api/v1/health", "自检：%s 里一定有它", path)
	}

	assert.Equal(t, "/api/v1/components/{}/versions/{}/manifest",
		normalize("/api/v1/components/:scope/:name/versions/:version/manifest"),
		"自检：连续参数段要折成一个")
	assert.Equal(t, "/api/v1/components/{}/versions/{}/manifest",
		normalize("/api/v1/components/{id}/versions/{ver}/manifest"),
		"自检：文档写法与路由写法要归一到同一个形状")
}

// 中英两份是各自独立写的，但列的端点必须一模一样：一边漏写的端点，读那种语言的人就不知道它。
func TestRouteDocsAgreeAcrossLanguages(t *testing.T) {
	assert.Equal(t, routesIn(t, referenceDocs[0]), routesIn(t, referenceDocs[1]),
		"%s 与 %s 的端点表不一致", referenceDocs[0], referenceDocs[1])
}
