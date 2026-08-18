package config_test

// 本文件测 `replicas`（P35 的前置，005 §5.8）。
//
// # 为什么要先做它
//
// P35 是"生成 PodDisruptionBudget"。但在 `replicas` 写死为 1 的前提下，
// PDB 无论怎么写都是死路——这是在 calico 集群上真跑确认过的：
//
//	minAvailable: 1     ALLOWED DISRUPTIONS 恒为 0，`kubectl drain` 永远排不空
//	maxUnavailable: 1   等于没写，一个副本随时可被驱逐
//
// 所以 PDB 有意义的前提是**先能配多副本**。这是同一件事的两半，
// 分开做只会得到一个必然有害的 PDB。

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/brickkit/brickkit/internal/config"
)

func parseReplicas(t *testing.T, body string) (*config.Config, error) {
	t.Helper()
	return config.ParseConfig([]byte("project: my-erp\ndeploy:\n  target: k8s\n"+body), "brickkit.yaml")
}

// 不写就是 1：与此前写死的行为完全一致。
//
// 这条守的是"加了字段不会悄悄改变既有项目的行为"——
// 副本数从 1 变成别的值，是任何人都不该在升级 CLI 时被动收到的变化。
func TestReplicasDefaultsToOne(t *testing.T) {
	cfg, err := parseReplicas(t, "components:\n  - id: demo/hello\n    version: 1.0.0\n")

	require.NoError(t, err)
	assert.Equal(t, 1, cfg.Components[0].ReplicaCount(),
		"P35：不写 replicas 时必须还是 1，否则升级 CLI 就会静默改变副本数")
}

// 写了就用写的。
func TestReplicasIsParsed(t *testing.T) {
	cfg, err := parseReplicas(t, `components:
  - id: demo/hello
    version: 1.0.0
    replicas: 3
`)

	require.NoError(t, err)
	assert.Equal(t, 3, cfg.Components[0].ReplicaCount())
}

// 0 副本要报错，而不是当成"关闭"。
//
// 关闭组件已经有 `enabled: false`，而且它会走级联计算、会提醒依赖方。
// 用 replicas: 0 关组件则绕过了这一切：依赖它的组件照常启动、照常拿到地址，
// 然后连一个不存在的后端——表现是 503，且状态表里那个组件显示"正常"。
func TestZeroReplicasIsAnError(t *testing.T) {
	_, err := parseReplicas(t, `components:
  - id: demo/hello
    version: 1.0.0
    replicas: 0
`)

	require.Error(t, err)
	text := err.Error()
	assert.Contains(t, text, "replicas")
	assert.Contains(t, text, "enabled",
		"P35：要指向 enabled: false 这条正路——它会走级联、会提醒依赖方，而 replicas: 0 不会")
}

// 负数同样报错。
func TestNegativeReplicasIsAnError(t *testing.T) {
	_, err := parseReplicas(t, `components:
  - id: demo/hello
    version: 1.0.0
    replicas: -1
`)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "replicas")
}

// external 组件不能写 replicas。
//
// 它由别的项目部署，副本数是那边的决定。在这边写不会有任何效果，
// 而使用者会以为生效了（P39 同款理由）。
func TestExternalWithReplicasIsAnError(t *testing.T) {
	_, err := parseReplicas(t, `components:
  - id: infra/notifier
    version: 1.0.0
    replicas: 3
    external:
      project: platform-shared
`)

	require.Error(t, err)
	text := err.Error()
	assert.Contains(t, text, "replicas")
	assert.Contains(t, text, "external")
}

// local 组件不能写 replicas。
//
// local 是"这个组件在我的 IDE 里跑"——IDE 里只有一个进程。
// 写 replicas: 3 表达不了任何东西，只能说明使用者没想清楚。
func TestLocalWithReplicasIsAnError(t *testing.T) {
	_, err := parseReplicas(t, `components:
  - id: demo/hello
    version: 1.0.0
    local: true
    replicas: 3
`)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "local")
}
