package k8s_test

// 本文件测 `external:` 在 K8s 目标下的生成（P39）。
//
// # K8s 侧比 Docker 侧简单
//
// 跨命名空间 DNS 是原生能力，寻址只要带上后缀（那部分在 inject 里）。
// 这里要做的只有一件事：**什么都不为它生成**。
//
//	不生成 Deployment  否则两个命名空间各跑一份"权威"组件
//	不生成 Service     否则本命名空间会有一个指向零个 Pod 的 Service，
//	                   而它会**抢在**跨命名空间 DNS 之前被解析到——
//	                   结果是连上一个永远没有后端的地址，503 且无从查起
//	不生成 Job         迁移属于部署它的那个项目，两边都跑会互相打架
//
// 第二条是这里最隐蔽的：多生成一个 Service 不只是浪费，
// 它会**主动劫持**本该走出去的流量。

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/brickkit/brickkit/internal/config"
)

// externalBuilder 造「caller 依赖外部 notifier」。
func externalBuilder(t *testing.T) *builder {
	t.Helper()

	b := newBuilder(t)
	b.component(dependsOn(simple("demo/caller", "1.0.0", 8080), "infra/notifier", "1.0.0"),
		config.Component{})
	b.component(simple("infra/notifier", "1.0.0", 8080), config.Component{
		External: &config.External{Project: "platform-shared"},
	})
	return b
}

// 不为 external 组件生成任何清单。
func TestExternalComponentGeneratesNothing(t *testing.T) {
	result := externalBuilder(t).generate()

	for _, path := range pathsOf(result) {
		assert.NotContains(t, path, "infra-notifier",
			"P39：它由别的项目部署，这边一份清单都不该生成（多出来的是 %s）", path)
	}
	assert.True(t, hasFile(result, "deployments/demo-caller-1-0-0.yaml"),
		"本项目自己的组件照常生成")
}

// 尤其不能生成 Service。
//
// 这条单独列出来，因为它的后果与"多生成一个 Deployment"完全不同：
// 本命名空间里的同名 Service 会**抢在**跨命名空间解析之前命中，
// 而它背后一个 Pod 都没有。表现是稳定的 503，
// 且从依赖方看一切正常——地址对、DNS 通、Service 存在。
func TestExternalComponentHasNoServiceObject(t *testing.T) {
	result := externalBuilder(t).generate()

	assert.False(t, hasFile(result, "services/infra-notifier-1-0-0.yaml"),
		"P39：同名 Service 会劫持本该走出去的流量，且背后没有任何 Pod")
}

// 声明了迁移也不生成 Job。
func TestExternalComponentGeneratesNoMigrationJob(t *testing.T) {
	b := newBuilder(t)
	b.component(dependsOn(simple("demo/caller", "1.0.0", 8080), "infra/notifier", "1.0.0"),
		config.Component{})
	b.component(migrating(simple("infra/notifier", "1.0.0", 8080)), config.Component{
		External: &config.External{Project: "platform-shared"},
	})

	result := b.generate()

	assert.False(t, hasFile(result, "migrations/infra-notifier-1-0-0-migration.yaml"),
		"P39：迁移属于部署它的那个项目，两边都跑会互相打架——而数据库只有一个")
}

// 依赖方拿到的是带命名空间的地址。
//
// 这条是端到端的：inject 算出来的值要真的落进 Deployment 的 env 里。
func TestDependentEnvHasNamespaceQualifiedAddress(t *testing.T) {
	doc := externalBuilder(t).doc("deployments/demo-caller-1-0-0.yaml")

	containers, ok := dig(t, doc, "spec", "template", "spec", "containers").([]any)
	require.True(t, ok && len(containers) == 1)
	env := map[string]string{}
	for _, item := range containers[0].(map[string]any)["env"].([]any) {
		e := item.(map[string]any)
		if v, ok := e["value"].(string); ok {
			env[e["name"].(string)] = v
		}
	}

	require.Contains(t, env, "INFRA_NOTIFIER_ENDPOINT")
	assert.Equal(t, "http://infra-notifier-1-0-0.brickkit-platform-shared:8080",
		env["INFRA_NOTIFIER_ENDPOINT"],
		"P39：跨命名空间必须带后缀，否则解析的是本命名空间里一个不存在的名字")
}
