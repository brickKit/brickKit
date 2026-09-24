package cli

// 本文件是 Step 16-C「K8s 目标的 up / down / status 接线」的业务行为测试。
//
// 引擎是假的：命令层的职责是"生成什么、交给引擎什么、按什么顺序"，
// 而不是"怎么调 kubectl"（那一段由 internal/engine 的用例盯住）。
// 真集群验证是 P25。

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"

	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/engine"
)

// newK8sEngine 返回一个假的 kubectl 引擎。
func newK8sEngine() *fakeEngine {
	eng := newFakeEngine()
	eng.name = engine.K8s
	return eng
}

func k8sDir(f *projectFixture) string {
	return filepath.Join(f.Dir, ".brickkit", "generated", "k8s")
}

// k8sProject 造一个 deploy.target: k8s 的项目。
func k8sProject(t *testing.T) *projectFixture {
	t.Helper()
	return k8sProjectWith(t, comp{ID: "people/basic", Version: "1.0.0"}, "", "")
}

// k8sProjectWith 造一个 deploy.target: k8s 的项目。
//
// 不能用 f.writeConfig：它的头部已经写死了 deploy.target: docker。
// entryLines 是追加到组件条目里的行（如 mode: debug），extra 是追加到文件末尾的段（如 resources）。
func k8sProjectWith(t *testing.T, spec comp, entryLines, extra string) *projectFixture {
	t.Helper()

	f := addedProject(t, []comp{spec}, spec.ref())

	var b strings.Builder
	b.WriteString("project: my-erp\n\ndeploy:\n  target: k8s\n\nsources:\n")
	for _, s := range f.Sources {
		b.WriteString(s)
	}
	fmt.Fprintf(&b, "\ncomponents:\n  - id: %s\n    version: %s\n", spec.ID, spec.Version)
	b.WriteString(entryLines)
	b.WriteString(extra)
	require.NoError(t, os.WriteFile(f.Layout.ConfigPath(), []byte(b.String()), 0o644))
	return f
}

// pgResourceYAML 是一段绑定到 people/basic 的外部 PostgreSQL 声明。
func pgResourceYAML(password string) string {
	return `
resources:
  - kind: database
    engine: postgresql
    id: people-db
    host: postgres.infra.svc
    port: 5432
    username: people_user
    password: ` + password + `
    bindings:
      - componentId: people/basic
        database: people
`
}

// ============================================================
// up
// ============================================================

func TestUpK8sGeneratesManifests(t *testing.T) {
	f := k8sProject(t)

	r := runWithEngine(t, newK8sEngine(), f.Dir, "up")

	require.Equal(t, clierr.ExitOK, r.code, "%s%s", r.stdout, r.stderr)
	assert.FileExists(t, filepath.Join(k8sDir(f), "namespace.yaml"))
	assert.FileExists(t, filepath.Join(k8sDir(f), "deployments", "people-basic-1-0-0.yaml"))
	assert.FileExists(t, filepath.Join(k8sDir(f), "services", "people-basic-1-0-0.yaml"))
	assert.NoFileExists(t,
		filepath.Join(f.Dir, ".brickkit", "generated", "docker-compose.yaml"),
		"K8s 项目不该留下一份毫无意义的 compose 文件")
}

// deploymentEnvValue 从生成的 Deployment YAML 里取出某个 env 变量的值。
//
// 不用字符串 Contains 去比对多行值：YAML 库把跨行字符串渲染成块状标量
// （`|-` 那种），除第一行外每一行都会被加上统一的缩进，直接拿原始多行
// 字符串去 Contains 十有八九匹配不上，得真的按 YAML 语义解析出字段值。
func deploymentEnvValue(t *testing.T, path, varName string) string {
	t.Helper()
	var doc map[string]any
	require.NoError(t, yaml.Unmarshal([]byte(readFile(t, path)), &doc))

	spec, _ := doc["spec"].(map[string]any)
	tmpl, _ := spec["template"].(map[string]any)
	podSpec, _ := tmpl["spec"].(map[string]any)
	containers, _ := podSpec["containers"].([]any)
	require.NotEmpty(t, containers, "Deployment 里应该有容器")
	container, _ := containers[0].(map[string]any)
	envList, _ := container["env"].([]any)

	for _, e := range envList {
		entry, _ := e.(map[string]any)
		if entry["name"] == varName {
			value, _ := entry["value"].(string)
			return value
		}
	}
	t.Fatalf("Deployment 的 env 里没有变量 %s", varName)
	return ""
}

// v0.4.4 复核结果：readDotEnv 从前按物理行 Cut("=")，一个跨多行的双引号
// .env 值会在第一行就被切断——这条路径 K8s 和 Docker（local-debug.env）
// 共用同一个 envLookup/readDotEnv，只是落点从 env 文件换成了 Deployment
// 的 env 数组，值本身该不该完整跟"往哪写"无关。
func TestUpK8sDeploymentEnvResolvesMultilineDotEnvValue(t *testing.T) {
	clearAmbientEnvForTest(t, "APP_TOKEN_SIGNING_KEY_PEM")
	spec := comp{
		ID: "infra/iam-casdoor", Version: "1.0.0",
		ConfigSchema: []string{"appTokenSigningKeyPem:"},
	}
	f := k8sProjectWith(t, spec, `    config:
      appTokenSigningKeyPem: "${APP_TOKEN_SIGNING_KEY_PEM}"
`, "")

	pem := "-----BEGIN PRIVATE KEY-----\n" +
		"MIIBVQIBADANBgkqhkiG9w0BAQEFAASCAT8wggE7AgEAAkEA\n" +
		"-----END PRIVATE KEY-----"
	dotenv := `APP_TOKEN_SIGNING_KEY_PEM="` + pem + "\"\n"
	require.NoError(t, os.WriteFile(filepath.Join(f.Dir, ".env"), []byte(dotenv), 0o600))

	r := runWithEngine(t, newK8sEngine(), f.Dir, "up", "--dry-run")
	require.Equal(t, clierr.ExitOK, r.code, "%s%s", r.stdout, r.stderr)

	got := deploymentEnvValue(t,
		filepath.Join(k8sDir(f), "deployments", "infra-iam-casdoor-1-0-0.yaml"),
		"APP_TOKEN_SIGNING_KEY_PEM")
	assert.Equal(t, pem, got,
		"跨多行的 .env 值不能被 readDotEnv 截断成第一行")
}

// 交给引擎的是**清单目录**与**命名空间**，不是 compose 那套。
func TestUpK8sHandsDirectoryAndNamespaceToEngine(t *testing.T) {
	f := k8sProject(t)
	eng := newK8sEngine()

	runWithEngine(t, eng, f.Dir, "up")

	up := eng.lastUp(t)

	assert.Equal(t, k8sDir(f), up.File, "K8s 目标交给引擎的是目录")
	assert.Equal(t, "brickkit-my-erp", up.Project, "命名空间是 brickkit-<项目名>")
	assert.Equal(t, []string{"people-basic-1-0-0"}, up.Services)
}

// 迁移 Job 名要传给引擎：清理旧 Job 与等待完成都靠它（16.14）。
func TestUpK8sPassesMigrationJobs(t *testing.T) {
	f := k8sProjectWith(t, comp{
		ID: "people/basic", Version: "1.0.0",
		Migration: []string{"/app/server", "migrate", "up"},
	}, "", "")
	eng := newK8sEngine()

	runWithEngine(t, eng, f.Dir, "up")

	assert.Equal(t, [][]string{{"people-basic-1-0-0-migration"}}, eng.lastUp(t).MigrationGroups)
}

func TestUpK8sWithoutMigrationsPassesNoJobs(t *testing.T) {
	f := k8sProject(t)
	eng := newK8sEngine()

	runWithEngine(t, eng, f.Dir, "up")

	assert.Empty(t, eng.lastUp(t).MigrationGroups)
}

// --dry-run 只生成清单，不碰集群。
func TestUpK8sDryRunDoesNotApply(t *testing.T) {
	f := k8sProject(t)
	eng := newK8sEngine()

	r := runWithEngine(t, eng, f.Dir, "up", "--dry-run")

	require.Equal(t, clierr.ExitOK, r.code, "%s%s", r.stdout, r.stderr)
	assert.FileExists(t, filepath.Join(k8sDir(f), "namespace.yaml"), "文件照常生成")
	assert.Empty(t, eng.ups, "但一条 kubectl 都不该执行")
}

// 输出里要告诉使用者清单在哪、命名空间叫什么。
func TestUpK8sOutputMentionsNamespaceAndPath(t *testing.T) {
	f := k8sProject(t)

	r := runWithEngine(t, newK8sEngine(), f.Dir, "up")

	assert.Contains(t, r.stdout, filepath.Join(".brickkit", "generated", "k8s"))
	assert.Contains(t, r.stdout, "brickkit-my-erp")
}

// 建库提示两种目标都要有：K8s 下资源由运维部署，但库照样得先建出来。
func TestUpK8sReportsDatabaseRequirements(t *testing.T) {
	t.Setenv("PEOPLE_DB_PASSWORD", "s3cr3t")
	f := k8sProjectWith(t, comp{ID: "people/basic", Version: "1.0.0"}, "",
		pgResourceYAML("${PEOPLE_DB_PASSWORD}"))

	r := runWithEngine(t, newK8sEngine(), f.Dir, "up")

	require.Equal(t, clierr.ExitOK, r.code, "%s%s", r.stdout, r.stderr)
	assert.Contains(t, r.stdout, `CREATE DATABASE "people"`)
}

// ${VAR} 没定义时必须阻断，且不能留下半份清单。
func TestUpK8sBlocksOnUnresolvedEnvVar(t *testing.T) {
	f := k8sProjectWith(t, comp{ID: "people/basic", Version: "1.0.0"}, "",
		pgResourceYAML("${PEOPLE_DB_PASSWORD_NOT_SET}"))
	eng := newK8sEngine()

	r := runWithEngine(t, eng, f.Dir, "up")

	assert.Equal(t, clierr.ExitError, r.code)
	assert.Contains(t, r.stderr, "PEOPLE_DB_PASSWORD_NOT_SET")
	assert.Empty(t, eng.ups)
}

// ============================================================
// down / status
// ============================================================

func TestDownK8s(t *testing.T) {
	f := k8sProject(t)
	eng := newK8sEngine()
	require.Equal(t, clierr.ExitOK, runWithEngine(t, eng, f.Dir, "up").code)

	r := runWithEngine(t, eng, f.Dir, "down")

	require.Equal(t, clierr.ExitOK, r.code, "%s%s", r.stdout, r.stderr)
	require.Len(t, eng.downs, 1)
	// 交给引擎的只有项目名（这里就是命名空间）：down 不认生成目录，
	// 那份目录会被 up --dry-run 重写（005 §5.9.3）
	assert.Equal(t, "brickkit-my-erp", eng.downs[0].Project)
}

// 还没 up 过时 down 不该报错。
//
// 但它**照样会问集群**：命名空间在不在、里面有没有东西，只有集群知道。
// 从前这里看的是生成目录在不在，不在就一条 kubectl 都不发——而那个目录
// 在 .gitignore 里，随时可能被清掉。kubectl delete 本来就带 --ignore-not-found，
// 对着一个不存在的命名空间执行是 exit 0，没有代价。
func TestDownK8sBeforeUp(t *testing.T) {
	f := k8sProject(t)
	eng := newK8sEngine()

	r := runWithEngine(t, eng, f.Dir, "down")

	assert.Equal(t, clierr.ExitOK, r.code)
	require.Len(t, eng.downs, 1, "该问的还是要问集群，不能靠生成目录猜")
	assert.Contains(t, r.stdout, "no containers running")
}

func TestStatusK8s(t *testing.T) {
	f := k8sProject(t)
	eng := newK8sEngine()
	require.Equal(t, clierr.ExitOK, runWithEngine(t, eng, f.Dir, "up").code)

	r := runWithEngine(t, eng, f.Dir, "status")

	require.Equal(t, clierr.ExitOK, r.code, "%s%s", r.stdout, r.stderr)
	assert.Contains(t, r.stdout, "people/basic")
	assert.Contains(t, r.stdout, "Running")
}

// 排障命令必须是 kubectl 的，不能给一条 docker compose。
func TestK8sLogsCommandIsKubectl(t *testing.T) {
	command := logsCommand(engine.K8s, "brickkit-my-erp", "people-basic-1-0-0")

	assert.Contains(t, command, "kubectl logs")
	assert.Contains(t, command, "deployment/people-basic-1-0-0")
	assert.Contains(t, command, "-n brickkit-my-erp")
	assert.NotContains(t, command, "docker")
}

// ============================================================
// 资源可达性：K8s 下不能从本机拨号
// ============================================================

// K8s 下的资源地址是**集群内**的 DNS 名（postgres.infra），
// 开发者本机根本解析不了它。照 Docker 那套拨一次号，
// 会对一个完全健康的部署报"不可达"——组件正连着这个库跑得好好的。
// 这是接上真集群（minikube）之后第一时间撞到的。
func TestStatusK8sDoesNotProbeResourcesFromHost(t *testing.T) {
	f := k8sProjectWith(t, comp{ID: "people/basic", Version: "1.0.0"}, "",
		pgResourceYAML("plain-password"))
	eng := newK8sEngine()
	require.Equal(t, clierr.ExitOK, runWithEngine(t, eng, f.Dir, "up").code)

	r := runWithEngine(t, eng, f.Dir, "status")

	require.Equal(t, clierr.ExitOK, r.code, "%s%s", r.stdout, r.stderr)
	assert.Contains(t, r.stdout, "postgres.infra.svc:5432", "地址照常列出来")
	assert.NotContains(t, r.stdout, "unreachable", "本机解析不了集群内地址，不能据此判不可达")
	assert.Contains(t, r.stdout, "inside the cluster", "要说清为什么不下结论")
}

// 生成了 NetworkPolicy 就必须提醒"生不生效取决于集群的 CNI"（O1）。
//
// 不支持执行 NetworkPolicy 的集群上，apply 会成功、get networkpolicy 看得见、
// 而流量完全不受限制——没有任何报错。而 CLI 测不出来（K8s 没有这个 API）。
// 既然测不出来就必须说出来：从前这句话只写在 005 §5.13.0 与 003 §3.2 里，
// 而打开这个开关的人多半是从附录 D 抄了个字段，不会回去读那两节。
func TestNetworkPolicyNoticeIsPrinted(t *testing.T) {
	f := k8sProjectWith(t, comp{ID: "people/basic", Version: "1.0.0"}, "", "")

	// networkPolicy 挂在 deploy 下，而 extra 是追加到文件末尾的，所以在这里插
	body := strings.Replace(readFile(t, f.Layout.ConfigPath()), "  target: k8s\n",
		"  target: k8s\n  networkPolicy:\n    enabled: true\n"+
			"    ingressController:\n      namespace: ingress-nginx\n", 1)
	require.NoError(t, os.WriteFile(f.Layout.ConfigPath(), []byte(body), 0o644))

	r := runWithEngine(t, newFakeEngine(), f.Dir, "up", "--dry-run")
	out := r.stdout + r.stderr

	require.Equal(t, clierr.ExitOK, r.code, out)
	assert.Contains(t, out, "NetworkPolicy", out)
	assert.Contains(t, out, "CNI", "要点名是 CNI 决定的")
	assert.Contains(t, out, "no error whatsoever", "要说清失败是无声的")
}

// 没打开 networkPolicy 时不该有这条——每次 up 都多几行与自己无关的话，
// 会让人开始整块跳过输出。
func TestNetworkPolicyNoticeAbsentWhenDisabled(t *testing.T) {
	f := k8sProjectWith(t, comp{ID: "people/basic", Version: "1.0.0"}, "", "")

	r := runWithEngine(t, newFakeEngine(), f.Dir, "up", "--dry-run")

	require.Equal(t, clierr.ExitOK, r.code, r.stderr)
	assert.NotContains(t, r.stdout+r.stderr, "NetworkPolicy")
}
