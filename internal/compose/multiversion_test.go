package compose_test

// 本文件是 Step 29「多版本默认共存」在**生成侧**的业务行为测试，
// 覆盖开发计划 29.1、29.2、29.3。
//
// 多版本共存不是一个开关，而是 brickkit.yaml 的直接含义：
// **写了几个版本就启动几个容器**（003 §4.8）。它之所以成立，
// 靠的是服务名带版本（`people-basic-1-0-0`）——那既是容器名，
// 也是依赖方拿到的地址。
//
// 这里最要紧的是 29.3：两个调用方各依赖一个版本时，它们注入到的
// 地址必须**各指各的**。写错的表现极其隐蔽——两个调用方都能跑、
// 都能连通，只是其中一个连到了错误的版本上，
// 而那个版本的行为差异可能几周后才显形。

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/brickkit/brickkit/internal/compose"
	"github.com/brickkit/brickkit/internal/config"
)

// twoVersionProject 造一个"两个调用方各依赖 people/basic 的一个版本"的项目。
func twoVersionProject(t *testing.T) *builder {
	t.Helper()

	b := newBuilder(t)
	b.component(simple("people/basic", "1.0.0", 8080), config.Component{})
	b.component(simple("people/basic", "2.0.0", 8080), config.Component{})
	b.component(dependsOn(simple("erp/legacy", "1.0.0", 8080), "people/basic", "1.0.0"),
		config.Component{})
	b.component(dependsOn(simple("erp/backend", "1.0.0", 8080), "people/basic", "2.0.0"),
		config.Component{})
	return b
}

// 29.1 / 29.2：两个版本各生成一个 service，名字带版本。
func TestMultiVersionGeneratesOneServicePerVersion(t *testing.T) {
	b := twoVersionProject(t)
	result, err := b.build(compose.Options{})
	require.NoError(t, err)

	text := string(result.YAML)
	for _, service := range []string{"people-basic-1-0-0", "people-basic-2-0-0"} {
		assert.Contains(t, text, service+":",
			"29.1/29.2：每个版本都该有自己的 service —— brickkit.yaml 写了几个版本就启动几个")
	}
}

// 29.3 **两个调用方注入到的地址必须各指各的。**
//
// 这是整条多版本能力的关键。写错的表现不是报错，而是
// "两个调用方都跑得好好的，只是其中一个连到了错的版本上"——
// 而版本之间的行为差异可能几周后才显形，那时已经很难联想到是注入的问题。
func TestMultiVersionInjectsDistinctEndpoints(t *testing.T) {
	doc := twoVersionProject(t).parsed()

	legacy := envOf(t, serviceOf(t, doc, "erp-legacy-1-0-0"))
	backend := envOf(t, serviceOf(t, doc, "erp-backend-1-0-0"))

	assert.Equal(t, "http://people-basic-1-0-0:8080", legacy["PEOPLE_BASIC_ENDPOINT"],
		"29.3：依赖 1.0.0 的调用方必须指向 1.0.0")
	assert.Equal(t, "http://people-basic-2-0-0:8080", backend["PEOPLE_BASIC_ENDPOINT"],
		"29.3：依赖 2.0.0 的调用方必须指向 2.0.0")
	assert.NotEqual(t, legacy["PEOPLE_BASIC_ENDPOINT"], backend["PEOPLE_BASIC_ENDPOINT"],
		"29.3：两个调用方指到了同一个地方 —— 多版本共存就失去意义了")
}

// 环境变量名**不带版本**。
//
// 组件读的是 `PEOPLE_BASIC_ENDPOINT`，不是 `PEOPLE_BASIC_1_0_0_ENDPOINT`——
// 否则调用方的代码会随被依赖方的版本一起改，而那正是版本化服务名要避免的：
// 版本差异只体现在**地址的值**里，不体现在**变量名**上（004 §5.6）。
func TestMultiVersionKeepsEnvVarNameStable(t *testing.T) {
	doc := twoVersionProject(t).parsed()

	for _, service := range []string{"erp-legacy-1-0-0", "erp-backend-1-0-0"} {
		env := envOf(t, serviceOf(t, doc, service))
		require.Contains(t, env, "PEOPLE_BASIC_ENDPOINT",
			"29.3：%s 的变量名该是稳定的 PEOPLE_BASIC_ENDPOINT", service)
		for name := range env {
			assert.False(t, strings.Contains(name, "1_0_0") || strings.Contains(name, "2_0_0"),
				"29.3：变量名里不该出现版本号（%s），否则调用方代码会跟着被依赖方的版本改", name)
		}
	}
}

// 两个版本的容器互不干扰：各自的容器名、各自的端口声明。
func TestMultiVersionServicesAreIndependent(t *testing.T) {
	doc := twoVersionProject(t).parsed()

	one := envOf(t, serviceOf(t, doc, "people-basic-1-0-0"))
	two := envOf(t, serviceOf(t, doc, "people-basic-2-0-0"))

	assert.Equal(t, "people/basic", one["COMPONENT_ID"])
	assert.Equal(t, "people/basic", two["COMPONENT_ID"])
	assert.Equal(t, "1.0.0", one["COMPONENT_VERSION"], "29.2：各自知道自己是哪一版")
	assert.Equal(t, "2.0.0", two["COMPONENT_VERSION"])
}

// ============================================================
// 多版本 + 迁移：同一组件的迁移必须串行
// ============================================================

// 同一个组件的两个版本都有迁移时，它们必须按版本号先后跑，不能同时跑。
//
// # 为什么这是平台的责任
//
// 资源绑定按**组件 ID** 记（不带版本，003 §5.3），所以同一组件的多个版本
// 拿到的 DATABASE_NAME 必然是同一个；而迁移状态表的主键是
// (component_id, version)（002 §8.11），两个版本的 component_id 也是同一个。
// 于是"两个迁移容器同时对同一个库、用同一个身份跑迁移"这件事，
// 完全是平台自己生成出来的——使用者在 brickkit.yaml 里只是照 003 §8.3
// 写了两行版本号。
//
// # 撞的恰好是超集里重合的那部分
//
// 迁移只增不改（002 §8.10），所以 2.0.0 的迁移集合是 1.0.0 的超集。
// 这在**老库**上没问题：1.0.0 发现 0001 已应用就跳过、干净退出。
// 但在**空库**上两个容器都会去跑 0001——一个成功，另一个撞主键退出，
// 那个版本的主服务永远停在 Created。
//
// 而且它只在空库上撞、重跑一次就好（那时 0001 已经写进去了），
// 错误信息指向数据库主键冲突，与"我配了多版本"看不出任何关系。
// 间歇性 + 自愈 + 报错指向别处，是最费时间的那一类。
func TestSameComponentMigrationsAreChainedByVersion(t *testing.T) {
	b := newBuilder(t)
	b.component(withMigration(withDatabase(simple("demo/hello", "1.0.0", 8080))), config.Component{})
	b.component(withMigration(withDatabase(simple("demo/hello", "2.0.0", 8080))), config.Component{})
	b.resource(pgResource(config.Binding{ComponentID: "demo/hello", Database: "hello"}))

	doc := b.parsed()

	// 低版本先跑：它不等任何人
	first := serviceOf(t, doc, "demo-hello-1-0-0-migration")
	assert.NotContains(t, first, "depends_on", "版本最低的那个迁移不该等任何人")

	// 高版本等低版本**成功结束**
	second := serviceOf(t, doc, "demo-hello-2-0-0-migration")
	dependsOn, ok := second["depends_on"].(map[string]any)
	require.True(t, ok, "2.0.0 的迁移要等 1.0.0 的迁移：%v", second)
	condition, ok := dependsOn["demo-hello-1-0-0-migration"].(map[string]any)
	require.True(t, ok, "应当依赖 demo-hello-1-0-0-migration，实际是 %v", keysOf(dependsOn))
	assert.Equal(t, "service_completed_successfully", condition["condition"],
		"要等它**成功**结束——失败了还往下跑，等于拿半个 schema 去跑下一批迁移")
}

// 顺序按版本号，不是按服务名的字典序。
//
// 字典序会把 10.0.0 排在 2.0.0 前面，于是先跑的是**更新**的那一版——
// 迁移是只增不改的，顺序反了会让旧版本的迁移在新 schema 上执行。
func TestMigrationChainOrdersByVersionNotByName(t *testing.T) {
	b := newBuilder(t)
	b.component(withMigration(withDatabase(simple("demo/hello", "2.0.0", 8080))), config.Component{})
	b.component(withMigration(withDatabase(simple("demo/hello", "10.0.0", 8080))), config.Component{})
	b.resource(pgResource(config.Binding{ComponentID: "demo/hello", Database: "hello"}))

	doc := b.parsed()

	assert.NotContains(t, serviceOf(t, doc, "demo-hello-2-0-0-migration"), "depends_on",
		"2.0.0 才是版本号最小的那个（字典序会把 10.0.0 排前面）")
	dependsOn, ok := serviceOf(t, doc, "demo-hello-10-0-0-migration")["depends_on"].(map[string]any)
	require.True(t, ok, "10.0.0 的迁移要等 2.0.0 的迁移")
	assert.Contains(t, dependsOn, "demo-hello-2-0-0-migration")
}

// 不同组件之间不串：它们的 component_id 不同，迁移状态表的主键
// (component_id, version) 已经让它们互不相干（002 §8.11 的设计目的）。
// 串起来只会平白拖慢 up，而 up 里迁移是所有组件的前置阻塞步骤。
func TestDifferentComponentsMigrationsStayParallel(t *testing.T) {
	b := newBuilder(t)
	b.component(withMigration(withDatabase(simple("demo/hello", "1.0.0", 8080))), config.Component{})
	b.component(withMigration(withDatabase(simple("people/basic", "1.0.0", 8080))), config.Component{})
	b.resource(pgResource(
		config.Binding{ComponentID: "demo/hello", Database: "shared"},
		config.Binding{ComponentID: "people/basic", Database: "shared"}))

	doc := b.parsed()

	assert.NotContains(t, serviceOf(t, doc, "demo-hello-1-0-0-migration"), "depends_on")
	assert.NotContains(t, serviceOf(t, doc, "people-basic-1-0-0-migration"), "depends_on")
}

// 只有一个版本有迁移时，不该凭空生成一条指向不存在 service 的依赖
// （compose 遇到不存在的 depends_on 会直接报错，整个项目起不来）。
func TestMigrationChainSkipsVersionsWithoutMigration(t *testing.T) {
	b := newBuilder(t)
	b.component(withDatabase(simple("demo/hello", "1.0.0", 8080)), config.Component{})
	b.component(withMigration(withDatabase(simple("demo/hello", "2.0.0", 8080))), config.Component{})
	b.resource(pgResource(config.Binding{ComponentID: "demo/hello", Database: "hello"}))

	doc := b.parsed()
	services := doc["services"].(map[string]any)

	require.NotContains(t, services, "demo-hello-1-0-0-migration", "1.0.0 没有迁移")
	assert.NotContains(t, serviceOf(t, doc, "demo-hello-2-0-0-migration"), "depends_on",
		"前面没有别的迁移可等，就不该有 depends_on")
}
