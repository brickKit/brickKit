// 外部测试包：deployfile 经由 docpages 引用了 version，放在 package version 里会成环。
package version_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/brickkit/brickkit/internal/deployfile"
	"github.com/brickkit/brickkit/internal/version"
)

// brickkit version 报的部署目标，必须正好是部署文件的 target 能写的那几种——
// 从前这里只写了 docker、k8s，而 deploy.yaml 早就接受 target: podman。
func TestDeployTargetsMatchWhatDeployFilesAccept(t *testing.T) {
	assert.Equal(t, deployfile.Targets, version.DeployTargets)
	assert.Equal(t, "docker, podman, k8s", version.SupportedTargets())
}
