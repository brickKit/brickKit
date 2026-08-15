package config

// 本文件是 P26 两个新配置段（deploy.networkPolicy / deploy.serviceAccount）
// 在**配置层**的行为：解析得对不对、写错时报得清不清楚。
//
// 生成什么清单是 k8s 包的事（internal/k8s/hardening_test.go）。

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const hardenedConfig = `
project: my-erp
deploy:
  target: k8s
  networkPolicy:
    enabled: true
    ingressController:
      namespace: ingress-nginx
      podSelector:
        app.kubernetes.io/name: ingress-nginx
  serviceAccount:
    enabled: true
components:
  - id: portal/user-frontend
    version: 1.0.0
    expose: true
    hostname: portal.example.com
    serviceAccountName: portal-s3-reader
`

func TestParseHardeningConfig(t *testing.T) {
	c, err := ParseConfig([]byte(hardenedConfig), "brickkit.yaml")
	require.NoError(t, err)

	require.NotNil(t, c.Deploy.NetworkPolicy)
	assert.True(t, c.Deploy.NetworkPolicyEnabled(), "P26")

	controller := c.Deploy.NetworkPolicy.IngressController
	require.NotNil(t, controller)
	assert.Equal(t, "ingress-nginx", controller.Namespace)
	assert.Equal(t, map[string]string{"app.kubernetes.io/name": "ingress-nginx"},
		controller.PodSelector)

	assert.True(t, c.Deploy.ServiceAccountEnabled(), "P26")
	assert.Equal(t, "portal-s3-reader", c.Components[0].ServiceAccountName, "P26")
}

// 两个开关都是 opt-in：不写就是关的，而不是"写了 networkPolicy 段就算开"。
//
// 分成两个字段（有没有这个段 / enabled 是不是 true）是有用的：
// 想临时关掉网络策略时可以只把 enabled 改成 false，
// ingressController 那几行留着，下次再打开不用重新查一遍 controller 在哪。
func TestHardeningIsOptIn(t *testing.T) {
	c, err := ParseConfig([]byte(minimalConfig), "brickkit.yaml")
	require.NoError(t, err)

	assert.False(t, c.Deploy.NetworkPolicyEnabled(), "不写就是关的")
	assert.False(t, c.Deploy.ServiceAccountEnabled(), "不写就是关的")

	c, err = ParseConfig([]byte(`
project: my-erp
deploy:
  target: k8s
  networkPolicy:
    enabled: false
    ingressController:
      namespace: ingress-nginx
`), "brickkit.yaml")
	require.NoError(t, err)
	assert.False(t, c.Deploy.NetworkPolicyEnabled(),
		"enabled: false 时即使写了 ingressController 也不该生成")
}

// 写了 ingressController 却没写 namespace → 阻断。
//
// 空命名空间会生成 `kubernetes.io/metadata.name: ""`，那是一条谁也匹配不上的规则：
// 策略照样 apply 成功，ingress controller 却照样被挡在外面。
func TestIngressControllerNamespaceRequired(t *testing.T) {
	_, err := ParseConfig([]byte(`
project: my-erp
deploy:
  target: k8s
  networkPolicy:
    enabled: true
    ingressController:
      podSelector:
        app.kubernetes.io/name: ingress-nginx
`), "brickkit.yaml")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "deploy.networkPolicy.ingressController.namespace",
		"错误要指到具体字段：%v", err)
}

// 新配置段里的错别字也要被认出来（P33 那套未知字段检查要能走进新结构）。
//
// 这条不是形式主义：`enabled` 打错成 `enable` 时，YAML 解析不会报任何错，
// 结果是**网络策略静悄悄地没生成**——而使用者以为已经收紧了。
// 这正是 P33 当初要解决的那类问题，新加的嵌套结构必须一并覆盖到。
func TestTypoInHardeningConfigIsCaught(t *testing.T) {
	_, err := ParseConfig([]byte(`
project: my-erp
deploy:
  target: k8s
  networkPolicy:
    enable: true
`), "brickkit.yaml")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "enable", "%v", err)
	assert.Contains(t, err.Error(), "enabled", "应该给出正确写法的建议：%v", err)
}

// 嵌套两层的错别字同样要认出来。
func TestTypoInIngressControllerIsCaught(t *testing.T) {
	_, err := ParseConfig([]byte(`
project: my-erp
deploy:
  target: k8s
  networkPolicy:
    enabled: true
    ingressController:
      namesapce: ingress-nginx
`), "brickkit.yaml")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "namesapce", "%v", err)
	assert.Contains(t, err.Error(), "namespace", "应该给出正确写法的建议：%v", err)
}

// podSelector 底下是使用者自己定的标签，不能当未知字段拦。
func TestPodSelectorLabelsAreNotChecked(t *testing.T) {
	_, err := ParseConfig([]byte(`
project: my-erp
deploy:
  target: k8s
  networkPolicy:
    enabled: true
    ingressController:
      namespace: ingress-nginx
      podSelector:
        whatever.example.com/anything: yes-this-is-fine
`), "brickkit.yaml")

	assert.NoError(t, err, "标签键是使用者的自由，出现什么都合法")
}
