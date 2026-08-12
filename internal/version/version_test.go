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

// 004 §11.3：版本号输出格式为 "v1.0.0"，已带 v 前缀时不重复添加。
func TestDisplay(t *testing.T) {
	original := Version
	defer func() { Version = original }()

	cases := map[string]string{
		"1.2.3":      "v1.2.3",
		"v1.2.3":     "v1.2.3",
		"0.1.0-dev":  "v0.1.0-dev",
		"v0.1.0-dev": "v0.1.0-dev",
	}
	for in, want := range cases {
		Version = in
		assert.Equal(t, want, Display(), "Display() with Version=%q", in)
	}
}

func TestVersionDefaults(t *testing.T) {
	// 未通过 ldflags 注入时应有占位值，不能为空。
	assert.NotEmpty(t, Version)
	assert.NotEmpty(t, Commit)
	assert.NotEmpty(t, BuildDate)
}
