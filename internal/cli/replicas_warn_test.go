package cli

// 本文件测「replicas 是 K8s 专属字段」的提醒（005 §5.8，P35 前置）。
//
// Docker 目标下 replicas 完全不生效。不提醒的话，使用者写了 replicas: 3、
// `up` 一切正常、然后 `docker ps` 里只有一个容器——他会怀疑是不是自己写错了字段名，
// 而字段名是对的。

import (
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/brickkit/brickkit/internal/clierr"
)

// replicasProject 造一个 target 可指定、组件写了 replicas 的项目。
func replicasProject(t *testing.T, target string) *projectFixture {
	t.Helper()

	f := addedProject(t, []comp{{ID: "demo/hello", Version: "1.0.0"}}, "demo/hello@1.0.0")
	f.writeConfig(t, "components:\n  - id: demo/hello\n    version: 1.0.0\n    replicas: 3\n")

	body := strings.Replace(readFile(t, f.Layout.ConfigPath()),
		"target: docker", "target: "+target, 1)
	require.NoError(t, os.WriteFile(f.Layout.ConfigPath(), []byte(body), 0o644))
	return f
}

// Docker 目标下写 replicas 要警告，但不阻断。
func TestReplicasOnDockerWarns(t *testing.T) {
	f := replicasProject(t, "docker")

	r := runWithEngine(t, newFakeEngine(), f.Dir, "up", "--dry-run")

	require.Equal(t, clierr.ExitOK, r.code, "P35：是提醒不是错误：%s", r.stderr)
	out := r.stdout + r.stderr
	assert.Contains(t, out, "replicas",
		"P35：Docker 下它完全不生效，不说的话使用者会以为是自己写错了字段名：%s", out)
}

// K8s 目标下不该有这条警告。
func TestReplicasOnK8sDoesNotWarn(t *testing.T) {
	f := replicasProject(t, "k8s")

	r := runWithEngine(t, newFakeEngine(), f.Dir, "up", "--dry-run")

	require.Equal(t, clierr.ExitOK, r.code, r.stderr)
	assert.NotContains(t, r.stdout+r.stderr, "只对 K8s 生效",
		"P35：K8s 下它是生效的，再提醒就是噪音")
}
