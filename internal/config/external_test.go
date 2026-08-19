package config_test

// 本文件测 `external:`（P39）：引用一个**已经由别人部署好**的组件。
//
// # 这条能力解决什么
//
// 有些组件本身就是权威，复制一份就是错的：定时任务跑两份等于双倍邮件、
// 大模型推理服务复制不起、别的团队运维的服务只许调用（试用指南 19.4）。
//
// 这时候需要的不是"把它也部一份"，而是"**它已经有人部好了，我只连它**"。
//
// # 为什么不是"给组件指定命名空间"
//
// 共享的东西**不属于任何一个消费方项目**：它有自己的负责人、自己的升级节奏。
// 一旦把它写进 A 的 brickkit.yaml（因而由 A 负责部署），
// **A 执行一次 `brickkit down`，B 就挂了**——这跟它部在哪个命名空间毫无关系。
//
// 所以正解是：共享的那些东西自己就是一个普通的 BrickKit 项目，
// 消费方只声明"我连它"。`external.project` 指的就是那个项目。

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/brickkit/brickkit/internal/config"
)

func parseCfg(t *testing.T, body string) (*config.Config, error) {
	t.Helper()
	return config.ParseConfig([]byte("project: my-erp\ndeploy:\n  target: docker\n"+body), "brickkit.yaml")
}

// 最小可用形态：声明它由哪个项目部署。
func TestExternalComponentParses(t *testing.T) {
	cfg, err := parseCfg(t, `components:
  - id: infra/notifier
    version: 1.0.0
    external:
      project: platform-shared
`)

	require.NoError(t, err)
	require.Len(t, cfg.Components, 1)

	c := cfg.Components[0]
	require.NotNil(t, c.External, "P39：external 段要被解析出来")
	assert.Equal(t, "platform-shared", c.External.Project)
	assert.True(t, c.IsExternal())
}

// 没写 external 的组件就是普通组件。
func TestOrdinaryComponentIsNotExternal(t *testing.T) {
	cfg, err := parseCfg(t, `components:
  - id: demo/hello
    version: 1.0.0
`)

	require.NoError(t, err)
	assert.False(t, cfg.Components[0].IsExternal())
	assert.Nil(t, cfg.Components[0].External)
}

// `project` 必填——它就是"去哪找"这个问题的全部答案。
func TestExternalWithoutProjectIsAnError(t *testing.T) {
	_, err := parseCfg(t, `components:
  - id: infra/notifier
    version: 1.0.0
    external: {}
`)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "external.project",
		"P39：不知道对方项目叫什么，就既拼不出地址也连不上网络")
}

// 项目名要合法——它会变成命名空间名（K8s）与网络名（Docker）。
func TestExternalProjectNameIsValidated(t *testing.T) {
	_, err := parseCfg(t, `components:
  - id: infra/notifier
    version: 1.0.0
    external:
      project: "Platform Shared!"
`)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "external.project",
		"P39：项目名会变成 K8s 命名空间与 Docker 网络名，非法字符要当场拦下")
}

// external 与 local 互斥。
//
// 这两件事的含义正好相反：local 是"这个组件在我的 IDE 里跑"，
// external 是"这个组件在别人那儿跑"。同时写只能说明使用者没想清楚，
// 而平台若挑一个执行，挑哪个都是猜。
func TestExternalAndLocalConflict(t *testing.T) {
	_, err := parseCfg(t, `components:
  - id: infra/notifier
    version: 1.0.0
    local: true
    external:
      project: platform-shared
`)

	require.Error(t, err)
	text := err.Error()
	assert.Contains(t, text, "local")
	assert.Contains(t, text, "external")
}

// external 组件不能声明 expose。
//
// expose 是"把**我部署的**这个组件暴露出去"。而 external 组件不由我部署，
// 它的入口该由它自己的项目决定。允许写只会生成一个指向不存在的 Service 的
// Ingress——K8s 上表现为 503，而使用者会去查那个组件，查不出任何问题。
func TestExternalWithExposeIsAnError(t *testing.T) {
	_, err := parseCfg(t, `components:
  - id: infra/notifier
    version: 1.0.0
    expose: true
    external:
      project: platform-shared
`)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "expose")
}

// 引用自己的项目是错的。
//
// 写成自己就成了"我依赖一个由我部署、但我不部署的组件"——自相矛盾，
// 而且真按它执行会得到一个谁也不会去部署的空洞依赖。
func TestExternalPointingAtOwnProjectIsAnError(t *testing.T) {
	_, err := parseCfg(t, `components:
  - id: infra/notifier
    version: 1.0.0
    external:
      project: my-erp
`)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "external.project",
		"P39：指向自己等于说'我不部署它，但它由我部署'")
}

// external 组件也不该声明资源覆盖与配置覆盖。
//
// 它跑在别人的项目里，用的是**别人那份 brickkit.yaml** 的配置。
// 在这边写 config 不会有任何效果，而使用者会以为生效了——
// 这正是最难查的一类问题：改了、没报错、没作用。
func TestExternalWithConfigIsAnError(t *testing.T) {
	_, err := parseCfg(t, `components:
  - id: infra/notifier
    version: 1.0.0
    config:
      greeting: 你好
    external:
      project: platform-shared
`)

	require.Error(t, err)
	text := err.Error()
	assert.Contains(t, text, "config")
	assert.Contains(t, text, "external")
}
