package compose_test

// 本文件锁住 002 §7.8：**升级一个组件，不会把它的调用方一起切过去**
// （开发计划 Step 31 第 42 项）。
//
// 场景是升级最常见的形态：`brickkit.yaml` 里把 people/basic 从 1.0.0 改成 2.0.0，
// 而 erp/backend 的 Manifest 里写着它依赖 people/basic@1.0.0。
//
// 正确行为是 **1.0.0 照样起来，erp/backend 照样指向它**——
// 因为"我依赖哪个版本"是**组件作者**在 Manifest 里做的兼容性决定，
// 不是使用者改一行 brickkit.yaml 就能替他改的。
//
// 反过来（悄悄把调用方切到 2.0.0）的后果是最难查的一类：升级当天一切正常，
// 直到某个只在 2.0.0 里变了的行为被触发——而那时没人会想到是几周前那次
// "只升了 people/basic" 的改动。
//
// 这条行为在试用指南 08.2 里真跑验过，但一直没有自动化测试兜着。

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/brickkit/brickkit/internal/compose"
	"github.com/brickkit/brickkit/internal/config"
)

// upgradedProject 模拟"使用者升级了 people/basic，而调用方仍声明旧版本"。
//
// brickkit.yaml 里只写 2.0.0；1.0.0 是被 erp/backend 的依赖关系拉进来的。
func upgradedProject(t *testing.T) *builder {
	t.Helper()

	b := newBuilder(t)
	// 使用者把版本改成了 2.0.0
	b.component(simple("people/basic", "2.0.0", 8080), config.Component{})
	// 而调用方的 Manifest 里写着 1.0.0
	b.component(dependsOn(simple("erp/backend", "1.0.0", 8080), "people/basic", "1.0.0"),
		config.Component{})
	// 1.0.0 只出现在依赖图里，不在 brickkit.yaml 的 components 列表里——
	// 它是被 erp/backend 的依赖关系拉进来的
	old := simple("people/basic", "1.0.0", 8080)
	b.provider[old.Metadata.ID+"@"+old.Metadata.Version] = old
	return b
}

// 调用方仍然指向它自己声明的那一版。
func TestUpgradeDoesNotSwitchCallers(t *testing.T) {
	doc := upgradedProject(t).parsed()

	env := envOf(t, serviceOf(t, doc, "erp-backend-1-0-0"))

	assert.Equal(t, "http://people-basic-1-0-0:8080", env["PEOPLE_BASIC_ENDPOINT"],
		"31.42 / 002 §7.8：调用方的 Manifest 写的是 1.0.0，"+
			"升级 brickkit.yaml 不该把它悄悄切到 2.0.0——"+
			"依赖哪个版本是组件作者的兼容性决定")
}

// 被依赖的旧版本必须真的起来。
//
// 只是"地址还指着 1.0.0"不够——如果 1.0.0 没被部署，
// 调用方拿到的是一个指向不存在服务的地址，表现成连接超时。
func TestUpgradeKeepsDependedOldVersionRunning(t *testing.T) {
	result, err := upgradedProject(t).build(compose.Options{})
	require.NoError(t, err)

	text := string(result.YAML)
	assert.Contains(t, text, "people-basic-1-0-0:",
		"31.42：调用方还依赖着 1.0.0，它就必须被部署——"+
			"否则那个地址指向一个不存在的服务，表现成连接超时")
	assert.Contains(t, text, "people-basic-2-0-0:",
		"使用者升级到的 2.0.0 同样要起来")
}
