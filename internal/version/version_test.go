package version

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/brickkit/brickkit/internal/deployfile"
)

func TestManifestAPIVersion(t *testing.T) {
	// Manifest 的 apiVersion 固定为 brickkit/v1。
	assert.Equal(t, "brickkit/v1", ManifestAPIVersion)
}

// brickkit version 报的部署目标，必须正好是部署文件的 target 能写的那几种——
// 从前这里只写了 docker、k8s，而 deploy.yaml 早就接受 target: podman。
func TestDeployTargetsMatchWhatDeployFilesAccept(t *testing.T) {
	assert.Equal(t, deployfile.Targets, DeployTargets)
	assert.Equal(t, "docker, podman, k8s", SupportedTargets())
}

// 版本号输出格式为 "v1.0.0"，已带 v 前缀时不重复添加。
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
