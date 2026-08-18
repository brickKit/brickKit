package k8s_test

// 本文件测 `replicas` 写进 Deployment（005 §5.8，P35 的前置）。

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/brickkit/brickkit/internal/config"
)

func replicasOf(t *testing.T, count *int) any {
	t.Helper()

	b := newBuilder(t)
	b.component(simple("people/basic", "1.0.0", 8080), config.Component{Replicas: count})
	return dig(t, b.doc("deployments/people-basic-1-0-0.yaml"), "spec", "replicas")
}

func intPtr(n int) *int { return &n }

// 不写就是 1。
func TestDeploymentDefaultsToOneReplica(t *testing.T) {
	assert.Equal(t, 1, replicasOf(t, nil),
		"P35：不写 replicas 时必须还是 1，否则升级 CLI 就会静默改变副本数")
}

// 写了就用写的。
func TestDeploymentHonorsReplicas(t *testing.T) {
	assert.Equal(t, 3, replicasOf(t, intPtr(3)))
}
