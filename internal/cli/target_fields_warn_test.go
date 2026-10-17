package cli

// 本文件测「写了不生效就得出声」这条规矩，两个方向都测：
//
//	Docker / Podman  只服务于 K8s 的字段（k8s: 块、hostname / tlsSecret 这对 Ingress 字段……）
//	K8s              只对 Docker / Podman 有意义的 exposePort
//
// 不生效却不说，使用者会怀疑是自己写错了字段名，而字段名是对的。
// replicas 不在名单里：三种目标下它都生效。

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/brickkit/brickkit/internal/clierr"
)

// replicas 在 Docker 下生效（compose 的 deploy.replicas），不该再被报成"被忽略"。
func TestReplicasOnDockerIsNotIgnored(t *testing.T) {
	f := addedProject(t, []comp{{ID: "demo/hello", Version: "1.0.0"}}, "demo/hello@1.0.0")
	f.writeConfig(t, "components:\n  - id: demo/hello\n    version: 1.0.0\n    replicas: 3\n")

	r := runWithEngine(t, newFakeEngine(), f.Dir, "up", "--dry-run")

	require.Equal(t, clierr.ExitOK, r.code, r.stderr)
	assert.NotContains(t, r.stdout+r.stderr, "replicas has no effect")
	assert.Contains(t, generatedCompose(t, f.Dir), "replicas: 3")
}

// 平台承诺的是"部署文件里只对 K8s 生效的字段**全部**会在 Docker 下
// 提醒本次被忽略"。这条承诺一度只兑现了四个字段。
//
// 最要紧的是 networkPolicy：写了它的人以为自己收紧了网络边界，
// 而 Docker 下一条策略都不会生成，网络照旧全通——而且毫无提示。
func TestAllK8sOnlyFieldsWarnOnDocker(t *testing.T) {
	f := addedProject(t, []comp{{ID: "demo/hello", Version: "1.0.0"}}, "demo/hello@1.0.0")
	f.writeConfig(t, `components:
  - id: demo/hello
    version: 1.0.0
    tlsSecret: demo-tls
    serviceAccountName: demo-sa
    expose: true
    hostname: demo.example.com
`)
	body := strings.Replace(f.legacy, "  target: docker", `  target: docker
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
	f.rewrite(t, body)

	r := runWithEngine(t, newFakeEngine(), f.Dir, "up", "--dry-run")
	require.Equal(t, clierr.ExitOK, r.code, "是提醒不是错误：%s", r.stderr)

	out := r.stdout + r.stderr
	for _, field := range []string{
		"k8s.context", "k8s.namespace", "k8s.createNamespace",
		"k8s.podSecurity", "k8s.imagePullSecrets", "k8s.ingressClass",
		"k8s.ingressAnnotations", "k8s.networkPolicy", "k8s.serviceAccount",
		"hostname", "tlsSecret", "serviceAccountName",
	} {
		assert.Contains(t, out, field,
			"Docker 下写了 %s 必须提醒它被忽略了：%s", field, out)
	}
}

// 反方向：K8s 目标下 exposePort 不生效，同样要出声。
//
// 从前这个方向一个字都不查：`mode: debug` / `localPort` 有专门的报错挡着，
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
	body := strings.Replace(f.legacy,
		"target: docker", "target: k8s", 1)
	f.rewrite(t, body)

	r := runWithEngine(t, newFakeEngine(), f.Dir, "up", "--dry-run")

	require.Equal(t, clierr.ExitOK, r.code, "是提醒不是错误：%s", r.stderr)
	out := r.stdout + r.stderr
	assert.Contains(t, out, "exposePort has no effect with target: k8s")
	assert.NotContains(t, out, "hostname has no effect",
		"hostname 在 K8s 下是生效的，不该出现在被忽略的名单里")
}

// 警告按**字段**归集，不按组件。
//
// 同一件事说 N 遍会把警告区刷满，而使用者一旦开始整块跳过警告，
// 真正要紧的那几条也一起被跳过。真跑过：4 个组件各写两个被忽略的字段
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
    serviceAccountName: a-sa
  - id: demo/b
    version: 1.0.0
    serviceAccountName: b-sa
  - id: demo/c
    version: 1.0.0
    serviceAccountName: c-sa
`)

	r := runWithEngine(t, newFakeEngine(), f.Dir, "up", "--dry-run")
	require.Equal(t, clierr.ExitOK, r.code, r.stderr)

	out := r.stdout + r.stderr
	assert.Equal(t, 1, strings.Count(out, "serviceAccountName has no effect"),
		"三个组件写了同一个字段，只该出现一行：%s", out)
	assert.Contains(t, out, "demo/a@1.0.0, demo/b@1.0.0, demo/c@1.0.0",
		"一行里点清是哪几个：%s", out)
}
