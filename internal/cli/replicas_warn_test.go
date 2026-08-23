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

// 003 §3.2 承诺的是"`deploy` 下除 target 外**全部**只对 K8s 生效，
// 写了会提醒本次被忽略"。这条承诺一度只兑现了四个字段。
//
// 最要紧的是 networkPolicy：写了它的人以为自己收紧了网络边界，
// 而 Docker 下一条策略都不会生成，网络照旧全通——而且毫无提示。
func TestAllK8sOnlyFieldsWarnOnDocker(t *testing.T) {
	f := addedProject(t, []comp{{ID: "demo/hello", Version: "1.0.0"}}, "demo/hello@1.0.0")
	f.writeConfig(t, `components:
  - id: demo/hello
    version: 1.0.0
    replicas: 2
    tlsSecret: demo-tls
    serviceAccountName: demo-sa
    expose: true
`)
	body := strings.Replace(readFile(t, f.Layout.ConfigPath()), "  target: docker", `  target: docker
  context: prod-cluster
  namespace: team-a
  createNamespace: false
  podSecurity: restricted
  imagePullSecrets: [regcred]
  ingressClass: nginx
  ingressAnnotations:
    cert-manager.io/cluster-issuer: letsencrypt
  serviceAccount:
    enabled: true
  networkPolicy:
    enabled: true`, 1)
	require.NoError(t, os.WriteFile(f.Layout.ConfigPath(), []byte(body), 0o644))

	r := runWithEngine(t, newFakeEngine(), f.Dir, "up", "--dry-run")
	require.Equal(t, clierr.ExitOK, r.code, "是提醒不是错误：%s", r.stderr)

	out := r.stdout + r.stderr
	for _, field := range []string{
		"deploy.context", "deploy.namespace", "deploy.createNamespace",
		"deploy.podSecurity", "deploy.imagePullSecrets", "deploy.ingressClass",
		"deploy.ingressAnnotations", "deploy.serviceAccount", "deploy.networkPolicy",
		"replicas", "tlsSecret", "serviceAccountName",
	} {
		assert.Contains(t, out, field,
			"003 §3.2：Docker 下写了 %s 必须提醒它被忽略了：%s", field, out)
	}
}
