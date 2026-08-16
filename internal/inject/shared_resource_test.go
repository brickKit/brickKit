package inject_test

// 本文件测**多个组件共享同一个资源实例**（006 §7，开发计划验收项 A6.5）。
//
// # 为什么单独补
//
// Step 39 逐条查证时发现：全项目**没有任何用例让两个组件绑同一个资源**——
// 所有 bindings 夹具都只有一条。而 006 §7 把这件事列为推荐做法，
// 试用指南里的真实项目也正是这么用的（一个 postgres 同时服务四个组件）。
//
// 这里最容易出的错是**串到别人的库上**：注入时如果按"资源"而不是
// 按"这条 binding"取 database，两个组件会拿到同一个库名。
// 那不会报任何错——两个组件都连得上、都能建表，直到某天
// people/basic 读到了 department/tree 写的行。
//
// 这类 bug 的代价极不对称：它不崩、不报警，只是悄悄破坏数据边界（006 §7.2）。

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/brickkit/brickkit/internal/config"
	"github.com/brickkit/brickkit/internal/inject"
	"github.com/brickkit/brickkit/internal/manifest"
)

// needsDatabase 造一个声明了数据库依赖的组件。
func needsDatabase(id string) *manifest.Manifest {
	m := simple(id, "1.0.0", 8080)
	m.Dependencies = &manifest.Dependencies{
		Resources: []manifest.ResourceDep{{Kind: "database", Engine: "postgresql"}},
	}
	return m
}

// sharedPostgres 造一个被若干组件共享的 PostgreSQL 实例。
func sharedPostgres(bindings ...config.Binding) config.Resource {
	return config.Resource{
		Kind: "database", Engine: "postgresql", ID: "postgres-main",
		Host: "postgres-main", Port: 5432, Username: "dev", Password: "secret",
		Bindings: bindings,
	}
}

// twoComponentsSharing 让 people/basic 与 department/tree 共用一个资源。
func twoComponentsSharing(t *testing.T, res config.Resource) *inject.Result {
	t.Helper()

	b := newBuilder(t)
	b.component(needsDatabase("people/basic"), config.Component{})
	b.component(needsDatabase("department/tree"), config.Component{})
	b.resource(res)
	return b.build()
}

// 共享同一个实例时，每个组件必须拿到**自己那一条 binding** 的 database。
//
// 这是 006 §7.2 数据边界的全部依据。串了不会报错，只会悄悄读到别人的数据。
func TestSharedResourceGivesEachComponentItsOwnDatabase(t *testing.T) {
	result := twoComponentsSharing(t, sharedPostgres(
		config.Binding{ComponentID: "people/basic", Database: "people"},
		config.Binding{ComponentID: "department/tree", Database: "department"},
	))

	people := envOf(t, result, "people/basic")
	department := envOf(t, result, "department/tree")

	assert.Equal(t, "people", people["DATABASE_NAME"],
		"A6.5：people/basic 要拿到自己那条 binding 的库")
	assert.Equal(t, "department", department["DATABASE_NAME"],
		"A6.5：department/tree 拿到的库串了——两个组件会共用一个库，"+
			"不报错、不崩，只是悄悄破坏数据边界（006 §7.2）")
}

// 共享的是同一个实例：主机、端口、账号必须完全一致。
//
// 与上一条正好相反的方向——库名必须各不相同，连接目标必须完全相同。
// 两条一起才说明"共享"这件事做对了。
func TestSharedResourceKeepsOneInstanceForEveryone(t *testing.T) {
	result := twoComponentsSharing(t, sharedPostgres(
		config.Binding{ComponentID: "people/basic", Database: "people"},
		config.Binding{ComponentID: "department/tree", Database: "department"},
	))

	people := envOf(t, result, "people/basic")
	department := envOf(t, result, "department/tree")

	// 先确认这些变量**确实存在**再比。写错一个名字的话，两边都会取到空字符串，
	// assert.Equal("", "") 照样通过——那种绿灯什么也没证明。
	// （这条注释来自实事：初版写的是 DATABASE_USERNAME，而真名是 DATABASE_USER，
	//   直到拿真生成物对照才发现，测试一直空过。）
	for _, key := range []string{"DATABASE_HOST", "DATABASE_PORT", "DATABASE_USER"} {
		require.Contains(t, people, key, "A6.5：%s 根本不存在，下面的比较毫无意义", key)
		require.NotEmpty(t, people[key], "A6.5：%s 是空的，比较等于没比", key)
		assert.Equal(t, people[key], department[key],
			"A6.5：%s 不一致就不叫共享同一个实例了", key)
	}
	assert.Equal(t, "postgres-main", people["DATABASE_HOST"])
	assert.Equal(t, "dev", people["DATABASE_USER"])
}

// 没被绑定的组件，什么都不该拿到。
//
// 资源在 brickkit.yaml 里是全局声明的，只有 bindings 决定谁能用它。
// 漏了这条判断等于把数据库凭据发给了每一个组件。
func TestUnboundComponentGetsNothingFromSharedResource(t *testing.T) {
	result := twoComponentsSharing(t, sharedPostgres(
		config.Binding{ComponentID: "people/basic", Database: "people"},
	))

	department := envOf(t, result, "department/tree")
	for key := range department {
		assert.NotContains(t, key, "DATABASE_",
			"A6.5：department/tree 没被绑定，不该拿到任何数据库变量（拿到了 %s）", key)
	}
}

// 共享时也支持各自的 envPrefix。
//
// 006 §7 的场景里，一个组件可能同时连主库与共享库；
// 靠 envPrefix 区分，所以共享路径上它必须照样生效。
func TestSharedResourceRespectsPerBindingEnvPrefix(t *testing.T) {
	result := twoComponentsSharing(t, sharedPostgres(
		config.Binding{ComponentID: "people/basic", Database: "people", EnvPrefix: "SHARED"},
		config.Binding{ComponentID: "department/tree", Database: "department"},
	))

	people := envOf(t, result, "people/basic")
	department := envOf(t, result, "department/tree")

	assert.Equal(t, "people", people["SHARED_DATABASE_NAME"],
		"A6.5：带 envPrefix 的那条 binding 要走前缀")
	assert.Equal(t, "department", department["DATABASE_NAME"],
		"A6.5：另一条没写前缀，不该被邻居的前缀影响")
	require.NotContains(t, department, "SHARED_DATABASE_NAME")
}
