package cli

// 本文件测「写了不生效就得出声」这条规矩（003 §3.2），两个方向都测。
//
// 起点是 replicas（005 §5.8，P35 前置）：Docker 目标下它完全不生效，
// 不提醒的话，使用者写了 replicas: 3、`up` 一切正常、然后 `docker ps` 里
// 只有一个容器——他会怀疑是不是自己写错了字段名，而字段名是对的。
//
// 后来补齐的两处：
//
//	反方向    K8s 目标下 exposePort 同样不生效，而从前一个字都不提
//	hostname  与 tlsSecret 是一对（都只服务于 Ingress），从前只报后者

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
    hostname: demo.example.com
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
		"replicas", "hostname", "tlsSecret", "serviceAccountName",
	} {
		assert.Contains(t, out, field,
			"003 §3.2：Docker 下写了 %s 必须提醒它被忽略了：%s", field, out)
	}
}

// 反方向：K8s 目标下 exposePort 不生效，同样要出声（003 §3.2）。
//
// 从前这个方向一个字都不查：`local` / `localPort` 有专门的报错挡着，
// 而 exposePort 静默失效。
func TestExposePortOnK8sWarns(t *testing.T) {
	f := addedProject(t, []comp{{ID: "demo/hello", Version: "1.0.0"}}, "demo/hello@1.0.0")
	f.writeConfig(t, `components:
  - id: demo/hello
    version: 1.0.0
    expose: true
    hostname: demo.example.com
    exposePort: 8080
`)
	body := strings.Replace(readFile(t, f.Layout.ConfigPath()),
		"target: docker", "target: k8s", 1)
	require.NoError(t, os.WriteFile(f.Layout.ConfigPath(), []byte(body), 0o644))

	r := runWithEngine(t, newFakeEngine(), f.Dir, "up", "--dry-run")

	require.Equal(t, clierr.ExitOK, r.code, "是提醒不是错误：%s", r.stderr)
	out := r.stdout + r.stderr
	assert.Contains(t, out, "只对 Docker 生效")
	assert.Contains(t, out, "exposePort")
	// 只查字段行：hostname 出现在**建议**里是对的（"对外暴露请填 hostname"），
	// 不该出现的是"它被忽略了"那份名单
	assert.NotContains(t, out, "components[].hostname",
		"hostname 在 K8s 下是生效的，不该出现在被忽略的名单里")
}

// 警告按**字段**归集，不按组件。
//
// 同一件事说 N 遍会把警告区刷满，而使用者一旦开始整块跳过警告，
// 真正要紧的那几条也一起被跳过。真跑过：4 个组件各写 replicas + tlsSecret
// 就是 8 行，20 个组件就是 40 行。
func TestTargetOnlyFieldsGroupedByField(t *testing.T) {
	specs := []comp{
		{ID: "demo/a", Version: "1.0.0"},
		{ID: "demo/b", Version: "1.0.0"},
		{ID: "demo/c", Version: "1.0.0"},
	}
	f := addedProject(t, specs, "demo/a@1.0.0", "demo/b@1.0.0", "demo/c@1.0.0")
	f.writeConfig(t, `components:
  - id: demo/a
    version: 1.0.0
    replicas: 2
  - id: demo/b
    version: 1.0.0
    replicas: 2
  - id: demo/c
    version: 1.0.0
    replicas: 2
`)

	r := runWithEngine(t, newFakeEngine(), f.Dir, "up", "--dry-run")
	require.Equal(t, clierr.ExitOK, r.code, r.stderr)

	out := r.stdout + r.stderr
	assert.Equal(t, 1, strings.Count(out, "components[].replicas"),
		"三个组件写了同一个字段，只该出现一行：%s", out)
	assert.Contains(t, out, "3 个组件：demo/a、demo/b、demo/c",
		"一行里点清是哪几个：%s", out)
}
