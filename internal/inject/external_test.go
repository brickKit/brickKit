package inject_test

// 本文件测 external 组件的**地址注入**（P39）。
//
// # 为什么这里必须区分部署目标
//
// 到今天为止注入完全不看 `deploy.target`——因为同一个组件在两种目标下
// 服务名一模一样（`infra-notifier-1-0-0`），拼出来的地址两边都对。
//
// external 把这个巧合打破了：被引用的组件在**另一个项目**里。
//
//	Docker  另一个项目 = 另一张网络。把依赖方接进那张网之后，
//	        裸服务名照常解析——地址不用变。
//	K8s     另一个项目 = 另一个命名空间。裸服务名只在**本命名空间**解析，
//	        必须写成 `<服务名>.<对方命名空间>`。
//
// 所以这是第一处注入必须知道目标的地方。搞错的后果不对称：
// K8s 上少写后缀会得到一个查不到的 DNS 名，容器起来了、健康检查也绿，
// 直到第一次真的去调它——而那可能是发布之后很久。

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/brickkit/brickkit/internal/config"
	"github.com/brickkit/brickkit/internal/manifest"
)

// externalBuilder 造「caller 依赖外部 notifier」，target 由调用方指定。
func externalBuilder(t *testing.T, target string) *builder {
	t.Helper()

	b := newBuilder(t)
	b.cfg.Deploy.Target = target
	b.component(dependsOn(simple("demo/caller", "1.0.0", 8080), "infra/notifier", "1.0.0"),
		config.Component{})
	b.component(simple("infra/notifier", "1.0.0", 8080), config.Component{
		External: &config.External{Project: "platform-shared"},
	})
	return b
}

// Docker：裸服务名。依赖方已被接进对方项目的网络，服务名照常解析。
func TestExternalEndpointOnDockerUsesPlainServiceName(t *testing.T) {
	env := envOf(t, externalBuilder(t, config.TargetDocker).build(), "demo/caller")

	assert.Equal(t, "http://infra-notifier-1-0-0:8080", env["INFRA_NOTIFIER_ENDPOINT"],
		"P39：Docker 上依赖方与对方在同一张网络里，裸服务名就能解析")
}

// K8s：必须带上对方的命名空间。
//
// 少了后缀，DNS 会在**本命名空间**里找一个不存在的名字。
// 容器照样起来、健康检查照样绿——直到第一次真的去调它。
func TestExternalEndpointOnK8sIsNamespaceQualified(t *testing.T) {
	env := envOf(t, externalBuilder(t, config.TargetK8s).build(), "demo/caller")

	assert.Equal(t, "http://infra-notifier-1-0-0.brickkit-platform-shared:8080",
		env["INFRA_NOTIFIER_ENDPOINT"],
		"P39：K8s 上裸服务名只在本命名空间解析，跨命名空间必须带后缀")
}

// 非 external 的依赖，两种目标下都不带后缀。
//
// 这条守的是"别顺手给所有人加后缀"：本项目内部的依赖加了后缀虽然也能解析，
// 但会把一个本可以随命名空间迁移的地址钉死。
func TestOrdinaryEndpointIsNeverQualified(t *testing.T) {
	for _, target := range []string{config.TargetDocker, config.TargetK8s} {
		b := newBuilder(t)
		b.cfg.Deploy.Target = target
		b.component(dependsOn(simple("demo/caller", "1.0.0", 8080), "demo/hello", "1.0.0"),
			config.Component{})
		b.component(simple("demo/hello", "1.0.0", 8080), config.Component{})

		env := envOf(t, b.build(), "demo/caller")
		assert.Equal(t, "http://demo-hello-1-0-0:8080", env["DEMO_HELLO_ENDPOINT"],
			"P39：%s —— 本项目内部的依赖不该被加上命名空间后缀", target)
	}
}

// external 组件的额外端口也要带后缀。
//
// 漏掉额外端口是很自然的错：主端口测过了就以为完事，
// 而 gRPC 往往正在额外端口上（people/basic 的 9090）。
func TestExternalExtraPortEndpointIsAlsoQualified(t *testing.T) {
	b := newBuilder(t)
	b.cfg.Deploy.Target = config.TargetK8s
	b.component(dependsOn(simple("demo/caller", "1.0.0", 8080), "infra/notifier", "1.0.0"),
		config.Component{})

	notifier := simple("infra/notifier", "1.0.0", 8080)
	notifier.Deployment.ExtraPorts = []manifest.ExtraPort{{Name: "grpc", Port: 9090}}
	b.component(notifier, config.Component{
		External: &config.External{Project: "platform-shared"},
	})

	env := envOf(t, b.build(), "demo/caller")
	assert.Equal(t, "http://infra-notifier-1-0-0.brickkit-platform-shared:9090",
		env["INFRA_NOTIFIER_GRPC_ENDPOINT"],
		"P39：额外端口最容易漏——gRPC 往往正在这上面")
}
