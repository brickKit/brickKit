package cli

// 本文件是 Step 16-D-1「面向真实集群：集群定位」的业务行为测试。
//
// 要解决的是同一类问题：**部到了错误的地方，而且部署成功了**。
// minikube 上试不出来（只有一个集群、权限也全开），真集群上是最贵的一类事故。

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/brickkit/brickkit/internal/clierr"
)

// ============================================================
// deploy.context：钉住集群
// ============================================================

// 配置里写了 context，就必须部到那个集群上。
//
// 不校验的后果：kubectl 用的是 `kubectl config current-context`——
// 切了 context 忘记切回来，一份写着生产的 brickkit.yaml 会被部到预发，
// 或者反过来。而且**会成功**，没有任何一处提示你部错了地方。
func TestUpK8sRefusesWrongContext(t *testing.T) {
	f := k8sProject(t)
	setDeployField(t, f, "context", "prod-cluster")
	eng := newK8sEngine()
	eng.currentContext = "dev-cluster"

	r := runWithEngine(t, eng, f.Dir, "up")

	assert.Equal(t, clierr.ExitError, r.code)
	assert.Contains(t, r.stderr, "prod-cluster", "要说清期望的是哪个")
	assert.Contains(t, r.stderr, "dev-cluster", "以及当前实际是哪个")
	assert.Empty(t, eng.ups, "一条 kubectl apply 都不能执行")
	assert.NoDirExists(t, k8sDir(f), "更不能留下生成物")
}

func TestUpK8sAcceptsMatchingContext(t *testing.T) {
	f := k8sProject(t)
	setDeployField(t, f, "context", "prod-cluster")
	eng := newK8sEngine()
	eng.currentContext = "prod-cluster"

	r := runWithEngine(t, eng, f.Dir, "up")

	require.Equal(t, clierr.ExitOK, r.code, "%s%s", r.stdout, r.stderr)
	assert.Contains(t, r.stdout, "prod-cluster", "部到哪个集群要写在输出里")
	assert.Len(t, eng.ups, 1)
}

// 没写 context 就不校验——不是所有人都需要钉住。
func TestUpK8sWithoutContextDoesNotCheck(t *testing.T) {
	f := k8sProject(t)
	eng := newK8sEngine()
	eng.currentContext = "whatever"

	r := runWithEngine(t, eng, f.Dir, "up")

	require.Equal(t, clierr.ExitOK, r.code, "%s%s", r.stdout, r.stderr)
	assert.Len(t, eng.ups, 1)
}

// 钉住的 context 也要传给 kubectl。
//
// 只做校验不传参数是不够的：校验与执行之间有时间差，而且使用者可能在
// 另一个终端切走了 context。显式 --context 才是"部到这个集群"的保证。
func TestUpK8sPassesContextToEngine(t *testing.T) {
	f := k8sProject(t)
	setDeployField(t, f, "context", "prod-cluster")
	eng := newK8sEngine()
	eng.currentContext = "prod-cluster"

	runWithEngine(t, eng, f.Dir, "up")

	assert.Equal(t, "prod-cluster", eng.lastUp(t).Context)
}

// --context 参数可以临时覆盖配置（一次性部到别的集群）。
func TestUpK8sContextFlagOverridesConfig(t *testing.T) {
	f := k8sProject(t)
	setDeployField(t, f, "context", "prod-cluster")
	eng := newK8sEngine()
	eng.currentContext = "dev-cluster"

	r := runWithEngine(t, eng, f.Dir, "up", "--context", "dev-cluster")

	require.Equal(t, clierr.ExitOK, r.code, "%s%s", r.stdout, r.stderr)
	assert.Equal(t, "dev-cluster", eng.lastUp(t).Context)
}

// down / status 同样要守住这条线：停错集群和部错集群一样糟。
func TestDownK8sRefusesWrongContext(t *testing.T) {
	f := k8sProject(t)
	eng := newK8sEngine()
	eng.currentContext = "prod-cluster"
	require.Equal(t, clierr.ExitOK, runWithEngine(t, eng, f.Dir, "up").code)

	setDeployField(t, f, "context", "prod-cluster")
	eng.currentContext = "dev-cluster"
	r := runWithEngine(t, eng, f.Dir, "down")

	assert.Equal(t, clierr.ExitError, r.code)
	assert.Empty(t, eng.downs)
}

// ============================================================
// deploy.namespace / createNamespace
// ============================================================

// 很多组织只给命名空间级权限，命名空间名也是他们定的。
func TestUpK8sUsesConfiguredNamespace(t *testing.T) {
	f := k8sProject(t)
	setDeployField(t, f, "namespace", "team-a-prod")
	eng := newK8sEngine()

	r := runWithEngine(t, eng, f.Dir, "up")

	require.Equal(t, clierr.ExitOK, r.code, "%s%s", r.stdout, r.stderr)
	assert.Equal(t, "team-a-prod", eng.lastUp(t).Project)
	assert.Contains(t, readFile(t, filepath.Join(k8sDir(f), "deployments", "people-basic-1-0-0.yaml")),
		"namespace: team-a-prod")
}

// createNamespace: false —— 不生成也不 apply namespace.yaml。
//
// 只有命名空间级权限时，`kubectl apply -f namespace.yaml` 会 Forbidden，
// 整个 up 在**第一条命令**就挂掉，而命名空间其实早就由运维建好了。
func TestUpK8sCanSkipNamespaceCreation(t *testing.T) {
	f := k8sProject(t)
	setDeployField(t, f, "namespace", "team-a-prod")
	setDeployField(t, f, "createNamespace", "false")
	eng := newK8sEngine()

	r := runWithEngine(t, eng, f.Dir, "up")

	require.Equal(t, clierr.ExitOK, r.code, "%s%s", r.stdout, r.stderr)
	assert.NoFileExists(t, filepath.Join(k8sDir(f), "namespace.yaml"))
	assert.FileExists(t, filepath.Join(k8sDir(f), "deployments", "people-basic-1-0-0.yaml"))
}

// 不建命名空间时，down 也不能把它删掉——那是别人的命名空间。
func TestDownK8sKeepsForeignNamespace(t *testing.T) {
	f := k8sProject(t)
	setDeployField(t, f, "namespace", "team-a-prod")
	setDeployField(t, f, "createNamespace", "false")
	eng := newK8sEngine()
	require.Equal(t, clierr.ExitOK, runWithEngine(t, eng, f.Dir, "up").code)

	r := runWithEngine(t, eng, f.Dir, "down")

	require.Equal(t, clierr.ExitOK, r.code, "%s%s", r.stdout, r.stderr)
	require.Len(t, eng.downs, 1)
	assert.False(t, eng.downs[0].DeleteNamespace,
		"命名空间不是我们建的，就不能由我们删")
}

// 默认（自己建命名空间）时，down 照旧连命名空间一起删干净。
func TestDownK8sDeletesOwnNamespace(t *testing.T) {
	f := k8sProject(t)
	eng := newK8sEngine()
	require.Equal(t, clierr.ExitOK, runWithEngine(t, eng, f.Dir, "up").code)

	require.Equal(t, clierr.ExitOK, runWithEngine(t, eng, f.Dir, "down").code)

	require.Len(t, eng.downs, 1)
	assert.True(t, eng.downs[0].DeleteNamespace)
}

// ============================================================
// Docker 目标下这些字段是无效的，要说一声
// ============================================================

// 它们是 K8s 专用的。写了却没生效，比报错更让人困惑。
func TestDockerTargetWarnsAboutK8sOnlyFields(t *testing.T) {
	f := k8sProject(t)
	setDeployField(t, f, "context", "prod-cluster")
	// 把目标改回 docker，但 K8s 专用字段留着
	text := readFile(t, f.Layout.ConfigPath())
	require.NoError(t, os.WriteFile(f.Layout.ConfigPath(),
		[]byte(replaceOnce(text, "target: k8s", "target: docker")), 0o644))

	r := runWithEngine(t, newFakeEngine(), f.Dir, "up", "--dry-run")

	assert.Contains(t, r.stdout+r.stderr, "deploy.context")
	assert.Contains(t, r.stdout+r.stderr, "只在 deploy.target: k8s 下生效")
}

// ============================================================
// 夹具
// ============================================================

// setDeployField 往 brickkit.yaml 的 deploy 段里塞一个字段。
func setDeployField(t *testing.T, f *projectFixture, key, value string) {
	t.Helper()

	text := readFile(t, f.Layout.ConfigPath())
	replaced := replaceOnce(text, "deploy:\n  target: k8s\n",
		"deploy:\n  target: k8s\n  "+key+": "+value+"\n")
	require.NotEqual(t, text, replaced, "夹具里应有 deploy.target: k8s")
	require.NoError(t, os.WriteFile(f.Layout.ConfigPath(), []byte(replaced), 0o644))
}

func replaceOnce(text, old, new string) string {
	idx := indexOfSub(text, old)
	if idx < 0 {
		return text
	}
	return text[:idx] + new + text[idx+len(old):]
}

func indexOfSub(text, sub string) int {
	for i := 0; i+len(sub) <= len(text); i++ {
		if text[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
