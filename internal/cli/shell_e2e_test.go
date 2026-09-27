package cli

// 本文件是外壳机制的端到端用例：一个真实形状的外壳项目（testdata/three-layer-shell/）
// 走完 up --dry-run，分别落到 Docker 与 K8s（提案 §8、附录 A3 / A7）。
//
//	erp/shell   外壳，shell.members 声明能承载 erp/api 与 erp/worker
//	erp/api     成员：带迁移、带一个 file:// 的 PEM 密钥（含 $ 与 "）
//	erp/worker  成员：依赖 erp/api
//	erp/portal  外壳外面的普通组件，依赖 erp/api

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"

	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/engine"
	"github.com/brickkit/brickkit/internal/envref"
	"github.com/brickkit/brickkit/internal/i18n"
	"github.com/brickkit/brickkit/internal/msgid"
)

// servedEntry 是 BRICKKIT_SERVED_MEMBERS_CONFIG 里的一个成员。
type servedEntry struct {
	ComponentID string            `json:"componentId"`
	Version     string            `json:"version"`
	HTTPPort    int               `json:"httpPort"`
	Config      map[string]string `json:"config"`
}

// shellJSONFromEnvFile 从外壳的 env 文件里取出 JSON：先按 compose 的双引号规则反转义，
// 再把 $$ 还原成 $——与 docker compose 读 env 文件时做的完全一样。
func shellJSONFromEnvFile(t *testing.T, content string) []servedEntry {
	t.Helper()
	const prefix = "BRICKKIT_SERVED_MEMBERS_CONFIG=\""
	for _, line := range strings.Split(content, "\n") {
		if !strings.HasPrefix(line, prefix) {
			continue
		}
		quoted := strings.TrimSuffix(strings.TrimPrefix(line, prefix), "\"")
		parsed := envref.ParseDotEnv("X=\"" + quoted + "\"\n")["X"]
		var entries []servedEntry
		require.NoError(t, json.Unmarshal([]byte(strings.ReplaceAll(parsed, "$$", "$")), &entries))
		return entries
	}
	require.Failf(t, "env 文件里没有 BRICKKIT_SERVED_MEMBERS_CONFIG", "%s", content)
	return nil
}

func TestShellDryRunDockerAndK8s(t *testing.T) {
	dir := copyFixture(t, "three-layer-shell")
	pem := readFile(t, filepath.Join(dir, ".secrets", "api.pem"))
	require.Contains(t, pem, "$", "夹具的 PEM 要带 $")
	require.Contains(t, pem, "\"", "夹具的 PEM 要带 \"")

	t.Run("docker", func(t *testing.T) {
		r := runWithEngine(t, newFakeEngine(), dir, "up", "--dry-run")
		require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)

		generated := filepath.Join(dir, ".brickkit", "generated")
		compose := readFile(t, filepath.Join(generated, composeFileName))
		assert.NotContains(t, compose, "BEGIN KEY", "PEM 不进 compose.yaml")
		assert.NotContains(t, compose, "BRICKKIT_SERVED_MEMBERS_CONFIG=", "外壳 JSON 不明文进 compose.yaml")

		var doc struct {
			Services map[string]struct {
				Image       string         `yaml:"image"`
				Environment []string       `yaml:"environment"`
				DependsOn   map[string]any `yaml:"depends_on"`
			} `yaml:"services"`
		}
		require.NoError(t, yaml.Unmarshal([]byte(compose), &doc))
		assert.NotContains(t, doc.Services, "erp-api-1-0-0", "成员没有自己的主容器")
		assert.NotContains(t, doc.Services, "erp-worker-1-0-0")
		migration, ok := doc.Services["erp-api-1-0-0-migration"]
		require.True(t, ok, "成员的迁移单独跑")
		assert.Equal(t, "registry.example.com/erp-api:1.0.0", migration.Image, "用成员自己的镜像")
		assert.Contains(t, doc.Services["erp-shell-1-0-0"].DependsOn, "erp-api-1-0-0-migration")
		assert.Contains(t, doc.Services["erp-portal-1-0-0"].Environment,
			"ERP_API_ENDPOINT=http://erp-shell-1-0-0:8081", "外壳外面的依赖方拿到外壳的地址")

		entries := shellJSONFromEnvFile(t, readFile(t, filepath.Join(generated, "env", "erp-shell-1-0-0.env")))
		require.Len(t, entries, 2)
		assert.Equal(t, "erp/api", entries[0].ComponentID)
		assert.Equal(t, pem, entries[0].Config["TLS_KEY"], "PEM 经过 env 文件与 JSON 两层编码后逐字节不变")
		assert.Equal(t, "erp/worker", entries[1].ComponentID)
		assert.Equal(t, "http://erp-shell-1-0-0:8081", entries[1].Config["ERP_API_ENDPOINT"],
			"外壳里的成员互相依赖时同样指向外壳")
	})

	t.Run("k8s", func(t *testing.T) {
		r := runWithEngine(t, newK8sEngine(), dir, "up", "--dry-run", "-f", "deploy.k8s.yaml")
		require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)

		k8sDir := filepath.Join(dir, ".brickkit", "generated", "k8s")
		assert.FileExists(t, filepath.Join(k8sDir, "migrations", "erp-api-1-0-0-migration.yaml"))
		_, err := os.Stat(filepath.Join(k8sDir, "deployments", "erp-api-1-0-0.yaml"))
		assert.True(t, os.IsNotExist(err), "成员没有自己的 Deployment")

		var secretJSON string
		for _, part := range strings.Split(readFile(t, filepath.Join(k8sDir, "secrets", "config-secrets.yaml")), "\n---\n") {
			var secret struct {
				StringData map[string]string `yaml:"stringData"`
			}
			require.NoError(t, yaml.Unmarshal([]byte(part), &secret))
			if v, ok := secret.StringData["BRICKKIT_SERVED_MEMBERS_CONFIG"]; ok {
				secretJSON = v
			}
		}
		require.NotEmpty(t, secretJSON, "外壳 JSON 进 Secret")
		var entries []servedEntry
		require.NoError(t, json.Unmarshal([]byte(secretJSON), &entries))
		require.Len(t, entries, 2)
		assert.Equal(t, pem, entries[0].Config["TLS_KEY"])

		deployment := readFile(t, filepath.Join(k8sDir, "deployments", "erp-shell-1-0-0.yaml"))
		assert.NotContains(t, deployment, "BEGIN KEY")
		assert.Contains(t, deployment, "secretKeyRef")
	})
}

// 外壳以 mode: debug 跑在宿主机上（本地开发一开始就是这样）：成员跟着在宿主机上，
// 外壳的本地 env 文件里有 JSON（PEM 逐字节不变），成员的迁移在容器起来之后单独跑完，
// 外壳外面的容器经 extra_hosts 连到宿主机上的外壳。
func TestBareShellEndToEnd(t *testing.T) {
	dir := copyFixture(t, "three-layer-shell")
	pem := readFile(t, filepath.Join(dir, ".secrets", "api.pem"))
	local := strings.Replace(readFile(t, filepath.Join(dir, "deploy.yaml")),
		"  - id: erp/shell\n", "  - id: erp/shell\n    mode: debug\n    localPort: 18000\n", 1)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "deploy.local.yaml"), []byte(local), 0o644))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, ".brickkit"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".brickkit", "local-mode"), []byte("on\n"), 0o644))

	eng := newFakeEngine()
	r := runWithEngine(t, eng, dir, "up")
	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)

	require.Len(t, eng.ups, 1)
	assert.Equal(t, []string{"erp-api-1-0-0-migration"}, eng.ups[0].RunAfter, "成员迁移在容器起来之后单独跑")
	assert.NotContains(t, eng.ups[0].Services, "erp-shell-1-0-0", "裸进程外壳没有容器")

	generated := filepath.Join(dir, ".brickkit", "generated")
	envFile := readFile(t, filepath.Join(generated, "local-debug.erp-shell-1-0-0.env"))
	env := map[string]string{}
	for k, v := range envref.ParseDotEnv(envFile) {
		env[k] = v
	}
	var entries []servedEntry
	require.NoError(t, json.Unmarshal([]byte(env["BRICKKIT_SERVED_MEMBERS_CONFIG"]), &entries))
	require.Len(t, entries, 2)
	assert.Equal(t, pem, entries[0].Config["TLS_KEY"])
	assert.Equal(t, "http://localhost:8081", entries[1].Config["ERP_API_ENDPOINT"],
		"外壳里的成员互相调用：都在宿主机上")

	compose := readFile(t, filepath.Join(generated, composeFileName))
	var doc struct {
		Services map[string]struct {
			ExtraHosts  []string `yaml:"extra_hosts"`
			Environment []string `yaml:"environment"`
		} `yaml:"services"`
	}
	require.NoError(t, yaml.Unmarshal([]byte(compose), &doc))
	portal := doc.Services["erp-portal-1-0-0"]
	assert.Contains(t, portal.Environment, "ERP_API_ENDPOINT=http://erp-shell-1-0-0:8081")
	assert.Contains(t, portal.ExtraHosts, "erp-shell-1-0-0:host-gateway")
	assert.NotContains(t, compose, "BEGIN KEY")
}

// status：外壳承载的成员没有自己的容器，它跑没跑就是外壳容器跑没跑——不能报成"未创建"，
// 那会让使用者去查一个不存在的故障。外壳是裸进程时，成员跟外壳一样不进容器表。
func TestStatusReportsHostedMembersThroughTheirShell(t *testing.T) {
	dir := copyFixture(t, "three-layer-shell")
	eng := newFakeEngine()
	eng.statuses = []engine.Status{
		{Service: "erp-shell-1-0-0", State: "running", Health: "healthy"},
		{Service: "erp-portal-1-0-0", State: "running", Health: "healthy"},
	}
	r := runWithEngine(t, eng, dir, "status")
	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)
	assert.NotContains(t, r.stdout, "not created")
	for _, line := range strings.Split(r.stdout, "\n") {
		if strings.Contains(line, "erp/api") {
			assert.Contains(t, line, "erp/shell@1.0.0", "成员那一行点名承载它的外壳")
			assert.Contains(t, line, "healthy")
		}
	}

	local := strings.Replace(readFile(t, filepath.Join(dir, "deploy.yaml")),
		"  - id: erp/shell\n", "  - id: erp/shell\n    mode: debug\n    localPort: 18000\n", 1)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "deploy.local.yaml"), []byte(local), 0o644))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, ".brickkit"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".brickkit", "local-mode"), []byte("on\n"), 0o644))
	eng.statuses = []engine.Status{{Service: "erp-portal-1-0-0", State: "running", Health: "healthy"}}
	r = runWithEngine(t, eng, dir, "status")
	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)
	assert.NotContains(t, r.stdout, "not created", "裸进程外壳的成员同样没有容器")
}

// 成员条目的字段是它"自己跑"时用的（附录 A21）：外壳在跑时它们不生效、也不警告；
// 外壳一关，成员按自己的条目独立部署（erp/api 发布自己的端口）。
func TestShellDisabledMembersRunStandalone(t *testing.T) {
	dir := copyFixture(t, "three-layer-shell")
	type composeDoc struct {
		Services map[string]struct {
			Ports []string `yaml:"ports"`
		} `yaml:"services"`
	}
	read := func() composeDoc {
		var doc composeDoc
		require.NoError(t, yaml.Unmarshal([]byte(readFile(t, filepath.Join(dir, ".brickkit", "generated", composeFileName))), &doc))
		return doc
	}

	r := runWithEngine(t, newFakeEngine(), dir, "up", "--dry-run")
	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)
	assert.NotContains(t, read().Services, "erp-api-1-0-0")
	assert.NotContains(t, r.stdout+r.stderr, "no effect", "外壳在跑时成员自己的部署字段不警告")

	deploy := strings.Replace(readFile(t, filepath.Join(dir, "deploy.yaml")),
		"  - id: erp/shell\n", "  - id: erp/shell\n    mode: disable\n", 1)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "deploy.yaml"), []byte(deploy), 0o644))
	r = runWithEngine(t, newFakeEngine(), dir, "up", "--dry-run")
	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)
	api, ok := read().Services["erp-api-1-0-0"]
	require.True(t, ok, "外壳不跑：成员独立部署")
	assert.Contains(t, api.Ports, "18081:8081", "用的是它自己条目里的 exposePort")
}

// 把成员并进外壳可能造出一个组件层面没有的环：erp/worker（在外壳里）依赖独立部署的 erp/pay，
// erp/pay 又依赖 erp/api（也在外壳里）。于是外壳要等 erp/pay、erp/pay 要等外壳。
//
//   - Docker：compose 的 depends_on 真的成环，起不来——生成前就说清是哪几条成员依赖造成的；
//   - K8s：Pod 之间没有 depends_on，这样部署完全可行，不能因此拦下（启动顺序退回按组件排）。
func TestShellMergeCycle(t *testing.T) {
	dir := mergeCycleFixture(t)

	r := runWithEngine(t, newFakeEngine(), dir, "up", "--dry-run")
	require.Equal(t, clierr.ExitError, r.code, r.stdout+r.stderr)
	assert.Contains(t, r.stderr, i18n.T(msgid.ShellMergeCycle, "erp/shell@1.0.0"))
	for _, want := range []string{"erp/shell@1.0.0", "erp/worker@1.0.0", "erp/pay@1.0.0", "erp/api@1.0.0"} {
		assert.Contains(t, r.stderr, want, "要点名造成环的成员依赖")
	}
	assert.Contains(t, r.stderr, "skipWaitFor: [erp/pay]", "第三条出路要给出能直接照抄的那一行")

	r = runWithEngine(t, newK8sEngine(), dir, "up", "--dry-run", "-f", "deploy.k8s.yaml")
	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)

	// 外壳以裸进程运行：compose 里没有外壳这个 service，也就没有 depends_on 环，不拦
	local := strings.Replace(readFile(t, filepath.Join(dir, "deploy.yaml")),
		"  - id: erp/shell\n", "  - id: erp/shell\n    mode: debug\n    localPort: 18000\n", 1)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "deploy.local.yaml"), []byte(local), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".brickkit", "local-mode"), []byte("on\n"), 0o644))
	r = runWithEngine(t, newFakeEngine(), dir, "up", "--dry-run")
	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)
}

// mergeCycleFixture 在外壳夹具上造出"并进外壳才有的环"：erp/worker（外壳里）→ erp/pay（独立）
// → erp/api（外壳里）。
func mergeCycleFixture(t *testing.T) string {
	t.Helper()
	dir := copyFixture(t, "three-layer-shell")
	writeTree(t, filepath.Join(dir, "components", "erp", "pay"), map[string]string{"component.yaml": `apiVersion: brickkit/v1
kind: Component
metadata: {id: erp/pay, name: Pay, version: 1.0.0, description: 独立部署，依赖外壳里的 erp/api}
dependencies:
  components: [erp/api@1.0.0]
deployment: {type: container, image: registry.example.com/erp-pay:1.0.0, port: 8095}
healthCheck: {type: tcp}
`})
	worker := filepath.Join(dir, "components", "erp", "worker", "component.yaml")
	require.NoError(t, os.WriteFile(worker, []byte(strings.Replace(readFile(t, worker),
		"    - erp/api@1.0.0\n", "    - erp/api@1.0.0\n    - erp/pay@1.0.0\n", 1)), 0o644))
	decl := filepath.Join(dir, "brickkit.yaml")
	require.NoError(t, os.WriteFile(decl, []byte(readFile(t, decl)+"  - id: erp/pay\n    version: 1.0.0\n"), 0o644))
	for _, name := range []string{"deploy.yaml", "deploy.k8s.yaml"} {
		path := filepath.Join(dir, name)
		require.NoError(t, os.WriteFile(path, []byte(readFile(t, path)+"  - id: erp/pay\n"), 0o644))
	}

	return dir
}

// skipWaitFor 写在成员条目上：外壳不再等 erp/pay（环断了，Docker 起得来），erp/pay 照样等外壳；
// 启动顺序那一行说清外壳不等谁。等待去掉了，连接不去掉：K8s 的出站网络策略照样放行 erp/pay。
func TestSkipWaitForBreaksMergeCycle(t *testing.T) {
	dir := mergeCycleFixture(t)
	for _, name := range []string{"deploy.yaml", "deploy.k8s.yaml"} {
		path := filepath.Join(dir, name)
		require.NoError(t, os.WriteFile(path, []byte(strings.Replace(readFile(t, path),
			"      - id: erp/worker\n", "      - id: erp/worker\n        skipWaitFor: [erp/pay]\n", 1)), 0o644))
	}

	r := runWithEngine(t, newFakeEngine(), dir, "up", "--dry-run")
	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)
	var doc struct {
		Services map[string]struct {
			DependsOn map[string]any `yaml:"depends_on"`
		} `yaml:"services"`
	}
	require.NoError(t, yaml.Unmarshal([]byte(readFile(t, filepath.Join(dir, ".brickkit", "generated", composeFileName))), &doc))
	assert.NotContains(t, doc.Services["erp-shell-1-0-0"].DependsOn, "erp-pay-1-0-0", "外壳不等 erp/pay")
	assert.Contains(t, doc.Services["erp-pay-1-0-0"].DependsOn, "erp-shell-1-0-0", "erp/pay 照样等外壳里的 erp/api")
	assert.Contains(t, r.stdout, i18n.T(msgid.CliRenderOrderSkipsWaitFor, "erp/pay@1.0.0"))

	k8s := filepath.Join(dir, "deploy.k8s.yaml")
	require.NoError(t, os.WriteFile(k8s, []byte(readFile(t, k8s)+"k8s:\n  networkPolicy:\n    enabled: true\n    egress:\n      enabled: true\n"), 0o644))
	r = runWithEngine(t, newK8sEngine(), dir, "up", "--dry-run", "-f", "deploy.k8s.yaml")
	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)
	assert.NotContains(t, r.stdout, i18n.T(msgid.CliRenderOrderSkipsWaitFor, "erp/pay@1.0.0"),
		"K8s 下 skipWaitFor 不起作用：启动顺序也不能说它起了作用")
	policy := readFile(t, filepath.Join(dir, ".brickkit", "generated", "k8s", "networkpolicies", "erp-shell-1-0-0.yaml"))
	egress := policy[strings.Index(policy, "egress:"):strings.Index(policy, "ingress:")]
	assert.Contains(t, egress, "erp-pay-1-0-0", "外壳的出站策略照样放行 erp/pay：不等它，不等于不连它")
}

// 裸进程没有 depends_on：skipWaitFor 写在它（或裸进程外壳的成员）上不起作用，说一声。
func TestSkipWaitForOnBareProcessWarns(t *testing.T) {
	dir := mergeCycleFixture(t)
	local := strings.Replace(readFile(t, filepath.Join(dir, "deploy.yaml")),
		"  - id: erp/shell\n", "  - id: erp/shell\n    mode: debug\n    localPort: 18000\n", 1)
	local = strings.Replace(local, "      - id: erp/worker\n", "      - id: erp/worker\n        skipWaitFor: [erp/pay]\n", 1)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "deploy.local.yaml"), []byte(local), 0o644))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, ".brickkit"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".brickkit", "local-mode"), []byte("on\n"), 0o644))

	r := runWithEngine(t, newFakeEngine(), dir, "up", "--dry-run")
	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)
	assert.Contains(t, r.stdout+r.stderr, i18n.T(msgid.ComposeSkipWaitForOnBareProcess, "erp/worker@1.0.0"))
	assert.NotContains(t, r.stdout, i18n.T(msgid.CliRenderOrderSkipsWaitFor, "erp/pay@1.0.0"),
		"说了不起作用，启动顺序就不能照它排")
}

// 跳过同一个外壳里的依赖：两边在同一个进程里，本来就没有等待——写了等于没写，说一声。
func TestSkipWaitForOnCoHostedDependencyWarns(t *testing.T) {
	dir := copyFixture(t, "three-layer-shell")
	path := filepath.Join(dir, "deploy.yaml")
	require.NoError(t, os.WriteFile(path, []byte(strings.Replace(readFile(t, path),
		"      - id: erp/worker\n", "      - id: erp/worker\n        skipWaitFor: [erp/api]\n", 1)), 0o644))
	r := runWithEngine(t, newFakeEngine(), dir, "up", "--dry-run")
	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)
	assert.Contains(t, r.stdout+r.stderr,
		i18n.T(msgid.ComposeSkipWaitForCoHosted, "erp/worker@1.0.0", "erp/api@1.0.0", "erp/shell@1.0.0"))
}

// 外壳 component.yaml 声明编进的成员版本（附录 A24）与这次承载的版本对不上：up --dry-run 与
// graph 都在生成前失败，给出三条带具体取值的出路。
func TestShellMemberVersionMismatchEndToEnd(t *testing.T) {
	dir := copyFixture(t, "three-layer-shell")
	shellYAML := filepath.Join(dir, "shell", "erp", "shell", "component.yaml")
	require.NoError(t, os.WriteFile(shellYAML, []byte(strings.Replace(readFile(t, shellYAML),
		"erp/api@1.0.0", "erp/api@0.9.0", 1)), 0o644))

	for _, args := range [][]string{{"up", "--dry-run"}, {"graph"}} {
		r := runWithEngine(t, newFakeEngine(), dir, args...)
		require.Equal(t, clierr.ExitError, r.code, "%v\n%s", args, r.stdout+r.stderr)
		assert.Contains(t, r.stderr, i18n.T(msgid.ShellMemberVersionMismatch, "erp/shell@1.0.0", "erp/api@0.9.0", "erp/api@1.0.0"), args)
		assert.Contains(t, r.stderr, "components[0].members[0]", args)
		assert.Contains(t, r.stderr, i18n.T(msgid.ShellHintUpgradeShell, "erp/shell", "erp/api@1.0.0"), args)
		assert.Contains(t, r.stderr, i18n.T(msgid.ShellHintMoveMemberOut, "erp/api", "erp/shell"), args)
		assert.Contains(t, r.stderr, "`- {id: erp/api, version: 0.9.0, requiredBy: [erp/shell]}`", args)
		assert.Contains(t, r.stderr, "`- id: erp/api@0.9.0`", args)
	}
}

// 第三条出路走通（附录 A24）：默认版本 1.1.0 在本地仓库、独立运行；外壳编进的 1.0.0 在
// brickkit.yaml 里以 requiredBy 保留，外壳下写 erp/api@1.0.0。依赖 1.0.0 的 erp/portal
// 拿到外壳地址，外壳 JSON 里是 1.0.0，两个版本的迁移按版本号串行。
func TestShellHostsDeclaredVersionWhileDefaultRunsStandalone(t *testing.T) {
	dir := copyFixture(t, "three-layer-shell")
	apiYAML := filepath.Join(dir, "components", "erp", "api", "component.yaml")
	old := readFile(t, apiYAML)
	cache := filepath.Join(dir, ".brickkit", "manifests")
	require.NoError(t, os.MkdirAll(cache, 0o755))
	// 本地源一个目录只放一个版本：1.0.0 在缓存里（连同信封，与之前一次 up 写下的一样）
	require.NoError(t, os.WriteFile(filepath.Join(cache, "erp-api-1.0.0.yaml"), []byte(old), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(cache, "erp-api-1.0.0.sig.json"), []byte(`{"sourceKind":"local"}`), 0o644))
	require.NoError(t, os.WriteFile(apiYAML, []byte(strings.ReplaceAll(old, "1.0.0", "1.1.0")), 0o644))

	decl := filepath.Join(dir, "brickkit.yaml")
	require.NoError(t, os.WriteFile(decl, []byte(strings.Replace(readFile(t, decl),
		"  - id: erp/api\n    version: 1.0.0\n",
		"  - id: erp/api\n    version: 1.1.0\n  - id: erp/api\n    version: 1.0.0\n    requiredBy: [erp/shell, erp/portal]\n", 1)), 0o644))
	deploy := filepath.Join(dir, "deploy.yaml")
	require.NoError(t, os.WriteFile(deploy, []byte(strings.Replace(readFile(t, deploy),
		"      - id: erp/api\n", "      - id: erp/api@1.0.0\n", 1)+"  - id: erp/api\n"), 0o644))

	r := runWithEngine(t, newFakeEngine(), dir, "up", "--dry-run")
	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)

	generated := filepath.Join(dir, ".brickkit", "generated")
	var doc struct {
		Services map[string]struct {
			Environment []string       `yaml:"environment"`
			DependsOn   map[string]any `yaml:"depends_on"`
		} `yaml:"services"`
	}
	require.NoError(t, yaml.Unmarshal([]byte(readFile(t, filepath.Join(generated, composeFileName))), &doc))
	assert.Contains(t, doc.Services, "erp-api-1-1-0", "默认版本独立运行")
	assert.NotContains(t, doc.Services, "erp-api-1-0-0", "1.0.0 在外壳里")
	assert.Contains(t, doc.Services["erp-portal-1-0-0"].Environment, "ERP_API_ENDPOINT=http://erp-shell-1-0-0:8081")
	assert.Contains(t, doc.Services["erp-api-1-1-0-migration"].DependsOn, "erp-api-1-0-0-migration", "同一个库的两个版本迁移串行")

	entries := shellJSONFromEnvFile(t, readFile(t, filepath.Join(generated, "env", "erp-shell-1-0-0.env")))
	require.NotEmpty(t, entries)
	assert.Equal(t, "erp/api", entries[0].ComponentID)
	assert.Equal(t, "1.0.0", entries[0].Version)
}

// shellCompiledOlderFixture：外壳编进的是 erp/api@0.9.0，本地仓库（默认版本）是 1.0.0；
// 0.9.0 的 Manifest 在缓存里（本地源一个目录只放一个版本）。
func shellCompiledOlderFixture(t *testing.T) string {
	t.Helper()
	dir := copyFixture(t, "three-layer-shell")
	shellYAML := filepath.Join(dir, "shell", "erp", "shell", "component.yaml")
	require.NoError(t, os.WriteFile(shellYAML, []byte(strings.Replace(readFile(t, shellYAML),
		"erp/api@1.0.0", "erp/api@0.9.0", 1)), 0o644))
	api := readFile(t, filepath.Join(dir, "components", "erp", "api", "component.yaml"))
	cache := filepath.Join(dir, ".brickkit", "manifests")
	require.NoError(t, os.MkdirAll(cache, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(cache, "erp-api-0.9.0.yaml"), []byte(strings.ReplaceAll(api, "1.0.0", "0.9.0")), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(cache, "erp-api-0.9.0.sig.json"), []byte(`{"sourceKind":"local"}`), 0o644))
	return dir
}

// editFile 把 path 里的 old 换成 new（必须出现）。
func editFile(t *testing.T, path, old, new string) {
	t.Helper()
	content := readFile(t, path)
	require.Contains(t, content, old)
	require.NoError(t, os.WriteFile(path, []byte(strings.Replace(content, old, new, 1)), 0o644))
}

// 照着第三条出路（两个版本都留）一字不差地改完，up 就要能过：brickkit.yaml 加上外壳编进的版本，
// 成员条目钉到那个版本，默认版本在部署文件顶层有自己的条目、独立运行。
func TestKeepBothVersionsHintWorksWhenFollowed(t *testing.T) {
	dir := shellCompiledOlderFixture(t)
	r := runWithEngine(t, newFakeEngine(), dir, "up", "--dry-run")
	require.Equal(t, clierr.ExitError, r.code, r.stdout+r.stderr)
	for _, line := range []string{"`- {id: erp/api, version: 0.9.0, requiredBy: [erp/shell]}`", "`- id: erp/api@0.9.0`", "`- id: erp/api`"} {
		require.Contains(t, r.stderr, line)
	}

	editFile(t, filepath.Join(dir, "brickkit.yaml"), "  - id: erp/worker\n",
		"  - {id: erp/api, version: 0.9.0, requiredBy: [erp/shell]}\n  - id: erp/worker\n")
	editFile(t, filepath.Join(dir, "deploy.yaml"), "      - id: erp/api\n", "      - id: erp/api@0.9.0\n")
	editFile(t, filepath.Join(dir, "deploy.yaml"), "  - id: erp/portal\n", "  - id: erp/portal\n  - id: erp/api\n")

	r = runWithEngine(t, newFakeEngine(), dir, "up", "--dry-run")
	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)

	// 只写 requiredBy: [erp/shell]（附录 A24）：外壳承载 0.9.0，默认版本 1.0.0 独立运行
	generated := filepath.Join(dir, ".brickkit", "generated")
	entries := shellJSONFromEnvFile(t, readFile(t, filepath.Join(generated, "env", "erp-shell-1-0-0.env")))
	require.NotEmpty(t, entries)
	assert.Equal(t, "erp/api", entries[0].ComponentID)
	assert.Equal(t, "0.9.0", entries[0].Version)
	assert.Contains(t, readFile(t, filepath.Join(generated, composeFileName)), "erp-api-1-0-0:")
}

// brickkit.yaml 已经留着外壳编进的 0.9.0（部署文件顶层也有它的条目）：第三条出路是把两个
// 条目对调——照着改完 up 要能过，而不是撞上"同一个版本两个条目"。
func TestPinDeclaredVersionHintWorksWhenFollowed(t *testing.T) {
	dir := shellCompiledOlderFixture(t)
	editFile(t, filepath.Join(dir, "brickkit.yaml"), "  - id: erp/worker\n",
		"  - {id: erp/api, version: 0.9.0, requiredBy: [erp/portal]}\n  - id: erp/worker\n")
	editFile(t, filepath.Join(dir, "deploy.yaml"), "  - id: erp/portal\n", "  - id: erp/portal\n  - id: erp/api@0.9.0\n")

	r := runWithEngine(t, newFakeEngine(), dir, "up", "--dry-run")
	require.Equal(t, clierr.ExitError, r.code, r.stdout+r.stderr)
	require.Contains(t, r.stderr, "`- id: erp/api@0.9.0`")
	require.Contains(t, r.stderr, "components[2]", "指出顶层那个 0.9.0 条目")
	require.NotContains(t, r.stderr, "requiredBy: [erp/shell]", "brickkit.yaml 里已经有 0.9.0")

	editFile(t, filepath.Join(dir, "deploy.yaml"), "  - id: erp/portal\n  - id: erp/api@0.9.0\n", "  - id: erp/portal\n  - id: erp/api\n")
	editFile(t, filepath.Join(dir, "deploy.yaml"), "      - id: erp/api\n", "      - id: erp/api@0.9.0\n")

	r = runWithEngine(t, newFakeEngine(), dir, "up", "--dry-run")
	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)
}
