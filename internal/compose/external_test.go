package compose_test

// 本文件测 `external:` 在 Docker 目标下的生成（P39）。
//
// # 核心行为：不生成它，但依赖方要连得上
//
// external 组件由**别的项目**部署，所以本项目不为它生成 service、
// 不生成它的迁移容器、`down` 时也碰不到它（碰不到是"没生成"的自然结果）。
//
// 但依赖它的组件必须拿到**能用的地址**——这在 Docker 上不是白来的：
//
//	K8s    跨命名空间 DNS 原生可用，拼个后缀就行
//	Docker 服务名**只在同一个网络里**解析
//
// 所以 Docker 这边要多做一件事：把依赖方接进对方项目的网络。
// 少了这一步，注入的地址长得完全正确，容器里却 `no such host`——
// 而使用者会去查那个组件是不是没起来，查半天发现它好好地跑着。

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/brickkit/brickkit/internal/config"
)

// externalSetup 造一个「本地 caller 依赖外部 notifier」的项目。
func externalSetup(t *testing.T) *builder {
	t.Helper()

	notifier := simple("infra/notifier", "1.0.0", 8080)
	caller := dependsOn(simple("demo/caller", "1.0.0", 8080), "infra/notifier", "1.0.0")

	b := newBuilder(t)
	b.component(caller, config.Component{})
	b.component(notifier, config.Component{
		External: &config.External{Project: "platform-shared"},
	})
	return b
}

// 不为 external 组件生成 service。
//
// 生成了才是灾难：两个项目各跑一份"权威"组件，
// 而这类组件之所以要共享，正是因为跑两份就是错的（定时任务发双倍邮件）。
func TestExternalComponentHasNoService(t *testing.T) {
	doc := externalSetup(t).parsed()

	services := servicesOf(t, doc)
	assert.NotContains(t, services, "infra-notifier-1-0-0",
		"P39：external 组件由别的项目部署，这边再生成一份就是跑了两份权威组件")
	assert.Contains(t, services, "demo-caller-1-0-0", "本项目自己的组件照常生成")
}

// 依赖方照常拿到地址。
func TestDependentStillGetsExternalEndpoint(t *testing.T) {
	doc := externalSetup(t).parsed()

	env := envOf(t, serviceOf(t, doc, "demo-caller-1-0-0"))
	assert.Equal(t, "http://infra-notifier-1-0-0:8080", env["INFRA_NOTIFIER_ENDPOINT"],
		"P39：地址仍用版本化服务名——它带着版本，连错版本会当场解析失败而不是静默连上")
}

// 依赖方要接进对方项目的网络，否则地址对了也连不上。
//
// 这条是 Docker 侧 external 能不能用的**全部关键**。
// 少了它，注入的地址长得完全正确，容器里却 `no such host`。
func TestDependentJoinsExternalProjectNetwork(t *testing.T) {
	doc := externalSetup(t).parsed()

	networks := doc["networks"].(map[string]any)
	shared, ok := networks["brickkit-platform-shared-net"]
	require.True(t, ok,
		"P39：要声明对方项目的网络，否则服务名在本项目网络里根本解析不出来：%v", networks)

	spec := shared.(map[string]any)
	assert.Equal(t, true, spec["external"],
		"P39：那张网络由对方项目创建，本项目只能引用**不能创建**——"+
			"抢着创建会在对方 down 时把网络一起带走")
	assert.Equal(t, "brickkit-platform-shared-net", spec["name"])

	caller := serviceOf(t, doc, "demo-caller-1-0-0")
	assert.Contains(t, caller["networks"], "brickkit-platform-shared-net",
		"P39：依赖方要同时在两张网络里")
}

// 不依赖 external 组件的服务不该被拉进那张网络。
//
// 网络是可达性边界。让所有组件都接进去，等于把"谁能访问共享服务"
// 这件事从声明依赖变成了默认全通——那正是 NetworkPolicy 要防的东西。
func TestUnrelatedServiceDoesNotJoinExternalNetwork(t *testing.T) {
	b := externalSetup(t)
	b.component(simple("demo/lonely", "1.0.0", 8080), config.Component{})

	doc := b.parsed()
	lonely := serviceOf(t, doc, "demo-lonely-1-0-0")

	assert.NotContains(t, lonely["networks"], "brickkit-platform-shared-net",
		"P39：没声明依赖的组件不该被顺手接进共享网络——"+
			"那等于把可达性从'按依赖声明'变成'默认全通'")
}

// 没有 external 组件时，不该冒出多余的网络。
func TestNoExternalNetworkWithoutExternalComponents(t *testing.T) {
	b := newBuilder(t)
	b.component(simple("demo/hello", "1.0.0", 8080), config.Component{})

	doc := b.parsed()
	networks := doc["networks"].(map[string]any)

	assert.Len(t, networks, 1, "P39：没用到 external 就只该有本项目那一张网：%v", networks)
}

// external 组件声明了迁移也不执行。
//
// 迁移属于**部署它的那个项目**。这边再跑一次，轻则重复执行，
// 重则两个项目的迁移互相打架——而数据库只有一个。
func TestExternalComponentMigrationIsNotRun(t *testing.T) {
	notifier := simple("infra/notifier", "1.0.0", 8080)
	notifier = withMigration(notifier)

	caller := dependsOn(simple("demo/caller", "1.0.0", 8080), "infra/notifier", "1.0.0")

	b := newBuilder(t)
	b.component(caller, config.Component{})
	b.component(notifier, config.Component{
		External: &config.External{Project: "platform-shared"},
	})

	doc := b.parsed()
	services := servicesOf(t, doc)

	assert.NotContains(t, services, "infra-notifier-1-0-0-migration",
		"P39：迁移属于部署它的那个项目，这边再跑一次会和对方打架——而数据库只有一个")
}

// 多个 external 组件指向同一个项目时，网络只声明一次。
func TestTwoExternalsInSameProjectShareOneNetwork(t *testing.T) {
	notifier := simple("infra/notifier", "1.0.0", 8080)
	audit := simple("infra/audit", "1.0.0", 8080)
	caller := dependsOn(
		dependsOn(simple("demo/caller", "1.0.0", 8080), "infra/notifier", "1.0.0"),
		"infra/audit", "1.0.0")

	b := newBuilder(t)
	b.component(caller, config.Component{})
	b.component(notifier, config.Component{External: &config.External{Project: "platform-shared"}})
	b.component(audit, config.Component{External: &config.External{Project: "platform-shared"}})

	doc := b.parsed()
	networks := doc["networks"].(map[string]any)

	assert.Len(t, networks, 2, "P39：本项目一张 + 共享一张，不该重复：%v", networks)
}
