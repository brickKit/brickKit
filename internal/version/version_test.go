package version

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestManifestAPIVersion(t *testing.T) {
	// 002 §2：Manifest 的 apiVersion 固定为 brickkit/v1。
	assert.Equal(t, "brickkit/v1", ManifestAPIVersion)
}

func TestDeployTargets(t *testing.T) {
	// 003 §3：部署目标只有 docker 与 k8s 两种。
	assert.Equal(t, []string{"docker", "k8s"}, DeployTargets)
	assert.Equal(t, "docker, k8s", SupportedTargets())
}

func TestVersionDefaults(t *testing.T) {
	// 未通过 ldflags 注入时应有占位值，不能为空。
	assert.NotEmpty(t, Version)
	assert.NotEmpty(t, Commit)
	assert.NotEmpty(t, BuildDate)
}
