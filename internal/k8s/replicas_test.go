package k8s_test

// 本文件测 `replicas` 写进 Deployment。

import (
	"github.com/brickkit/brickkit/internal/project/projecttest"
	"testing"

	"github.com/stretchr/testify/assert"
)

func replicasOf(t *testing.T, count *int) any {
	t.Helper()

	b := newBuilder(t)
	b.component(simple("people/basic", "1.0.0", 8080), projecttest.Entry{Replicas: count})
	return dig(t, b.doc("deployments/people-basic-1-0-0.yaml"), "spec", "replicas")
}

func intPtr(n int) *int { return &n }

// 不写就是 1。
func TestDeploymentDefaultsToOneReplica(t *testing.T) {
	assert.Equal(t, 1, replicasOf(t, nil),
		"不写 replicas 时必须还是 1，否则升级 CLI 就会静默改变副本数")
}

// 写了就用写的。
func TestDeploymentHonorsReplicas(t *testing.T) {
	assert.Equal(t, 3, replicasOf(t, intPtr(3)))
}
