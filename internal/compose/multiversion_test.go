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
