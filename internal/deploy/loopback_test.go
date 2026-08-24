// 本文件盯 `host: localhost` 那条警告。
//
// 它守的是一条**长期没有任何东西执行的规矩**：006 §10.2 早就写着"不要写
// localhost"，而生成的部署文件完全正常、`DATABASE_HOST=localhost` 老老实实
// 注进去、容器起来就去连自己——表现是启动之后才出现的 connection refused，
// 配置却挑不出毛病。规范书自己的示例长期写的还就是 `host: localhost`。
package deploy_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/brickkit/brickkit/internal/config"
	"github.com/brickkit/brickkit/internal/deploy"
)

// pgOn 造一条绑给 people/basic 的数据库资源，host 由调用方指定。
func pgOn(host string) *config.Config {
	return &config.Config{
		Project: "my-erp",
		Components: []config.Component{
			{ID: "people/basic", Version: "1.0.0"},
		},
		Resources: []config.Resource{{
			Kind: "database", Engine: "postgresql", ID: "pg-main",
			Host: host, Port: 5432,
			Bindings: []config.Binding{{ComponentID: "people/basic", Database: "people"}},
		}},
	}
}

// 容器组件绑了 localhost → 警告，并给出 host.docker.internal。
func TestLocalhostWarnsWhenAContainerComponentBindsIt(t *testing.T) {
	warnings := deploy.LocalhostResourceWarnings(
		pgOn("localhost"), []string{"people/basic"}, config.TargetDocker)

	require.Len(t, warnings, 1)
	out := warnings[0].Format()
	assert.Contains(t, out, "pg-main")
	assert.Contains(t, out, "people/basic", "要说清是谁连不上")
	assert.Contains(t, out, deploy.HostMachineAlias, "要给出该写什么")
}

// 回环地址的几种写法都算。
func TestLocalhostWarnsForEveryLoopbackSpelling(t *testing.T) {
	for _, host := range []string{"localhost", "LocalHost", " 127.0.0.1 ", "::1", "[::1]"} {
		warnings := deploy.LocalhostResourceWarnings(
			pgOn(host), []string{"people/basic"}, config.TargetDocker)
		assert.Len(t, warnings, 1, "host=%q 应当警告", host)
	}
}

// 正常写法不该有警告。
func TestLocalhostSilentForRealAddresses(t *testing.T) {
	for _, host := range []string{
		deploy.HostMachineAlias, "postgres.infra", "10.0.0.9", "db.example.com",
	} {
		warnings := deploy.LocalhostResourceWarnings(
			pgOn(host), []string{"people/basic"}, config.TargetDocker)
		assert.Empty(t, warnings, "host=%q 不该警告", host)
	}
}

// **关键的例外**：绑它的组件全是 local: true 时，localhost 恰恰是对的。
//
// 那些进程就跑在宿主机上，平台也只把这个地址写进 local-debug.*.env，
// 一个容器都碰不到。调用方因此只传"会生成容器"的组件——这里模拟的正是
// 那种情形：容器组件一个都没有。
func TestLocalhostSilentWhenOnlyLocalComponentsUseIt(t *testing.T) {
	warnings := deploy.LocalhostResourceWarnings(
		pgOn("localhost"), nil, config.TargetDocker)

	assert.Empty(t, warnings, "纯本地调试的项目不该被这条警告打扰")
}

// 这次不启动的组件绑了它，也不该警告。
func TestLocalhostSilentWhenBoundComponentIsNotRunning(t *testing.T) {
	warnings := deploy.LocalhostResourceWarnings(
		pgOn("localhost"), []string{"someone/else"}, config.TargetDocker)

	assert.Empty(t, warnings)
}

// K8s 下建议的写法不一样：那里没有宿主机可指，只有集群内地址。
func TestLocalhostHintDiffersOnK8s(t *testing.T) {
	warnings := deploy.LocalhostResourceWarnings(
		pgOn("localhost"), []string{"people/basic"}, config.TargetK8s)

	require.Len(t, warnings, 1)
	out := warnings[0].Format()
	assert.Contains(t, out, "集群")
	assert.NotContains(t, out, deploy.HostMachineAlias,
		"K8s 下 host.docker.internal 毫无意义，给了只会误导")
}
