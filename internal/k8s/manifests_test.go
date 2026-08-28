// 本文件是 Step 16-B「Service / Ingress / 迁移 Job / 目录结构」的业务行为测试，
// 覆盖开发计划 16.3、16.4、16.5、16.6、16.7、16.12、16.13。
package k8s_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/config"
	"github.com/brickkit/brickkit/internal/k8s"
	"github.com/brickkit/brickkit/internal/manifest"
)

// ============================================================
// 16.3 / 16.4 Service
// ============================================================

func TestServiceGenerated(t *testing.T) {
	b := newBuilder(t)
	b.component(simple("people/basic", "1.0.0", 8080), config.Component{})

	doc := b.doc("services/people-basic-1-0-0.yaml")

	assert.Equal(t, "v1", doc["apiVersion"], "16.3")
	assert.Equal(t, "Service", doc["kind"], "16.3")
	assert.Equal(t, "people-basic-1-0-0", dig(t, doc, "metadata", "name"),
		"16.3 Service 名就是版本化服务名——依赖方注入的地址指的正是它")
	assert.Equal(t, "brickkit-my-erp", dig(t, doc, "metadata", "namespace"))
	assert.Equal(t, "ClusterIP", dig(t, doc, "spec", "type"), "16.3 默认不暴露到集群外")
	assert.Equal(t, map[string]any{"app": "people-basic-1-0-0"},
		dig(t, doc, "spec", "selector"), "16.3 selector")
	assert.Equal(t, []any{
		map[string]any{"name": "http", "port": 8080, "targetPort": 8080},
	}, dig(t, doc, "spec", "ports"), "16.3 ports")
}

func TestServiceIncludesExtraPorts(t *testing.T) {
	m := simple("people/basic", "1.0.0", 8080)
	m.Deployment.ExtraPorts = []manifest.ExtraPort{{Name: "grpc", Port: 9090}}

	b := newBuilder(t)
	b.component(m, config.Component{})

	ports := dig(t, b.doc("services/people-basic-1-0-0.yaml"), "spec", "ports")

	assert.Equal(t, []any{
		map[string]any{"name": "http", "port": 8080, "targetPort": 8080},
		map[string]any{"name": "grpc", "port": 9090, "targetPort": 9090},
	}, ports, "16.4 extraPorts 要出现在 Service 的 ports 数组里")
}

// ============================================================
// 16.5 / 16.6 Ingress
// ============================================================

func TestIngressGenerated(t *testing.T) {
	b := newBuilder(t)
	b.component(simple("portal/user-frontend", "1.0.0", 80),
		config.Component{Expose: true, Hostname: "portal.example.com"})

	doc := b.doc("ingress/portal-user-frontend-1-0-0.yaml")

	assert.Equal(t, "networking.k8s.io/v1", doc["apiVersion"], "16.5")
	assert.Equal(t, "Ingress", doc["kind"], "16.5")
	assert.Equal(t, "portal-user-frontend-1-0-0", dig(t, doc, "metadata", "name"))
	assert.Equal(t, "brickkit-my-erp", dig(t, doc, "metadata", "namespace"))

	rules, ok := dig(t, doc, "spec", "rules").([]any)
	require.True(t, ok && len(rules) == 1, "应有且只有一条规则：%v", rules)
	rule := rules[0]

	assert.Equal(t, "portal.example.com", dig(t, rule, "host"), "16.5 host 来自 hostname")

	paths, ok := dig(t, rule, "http", "paths").([]any)
	require.True(t, ok && len(paths) == 1, "应有且只有一条路径：%v", paths)

	assert.Equal(t, "/", dig(t, paths[0], "path"), "16.5")
	assert.Equal(t, "Prefix", dig(t, paths[0], "pathType"))
	assert.Equal(t, "portal-user-frontend-1-0-0",
		dig(t, paths[0], "backend", "service", "name"), "16.5 backend 指向自己的 Service")
	assert.Equal(t, 80, dig(t, paths[0], "backend", "service", "port", "number"),
		"16.5 backend 端口是组件的主端口")
}

// 不声明 expose: true 就不生成 Ingress。安全是默认的。
func TestNoIngressWithoutExpose(t *testing.T) {
	b := newBuilder(t)
	b.component(simple("people/basic", "1.0.0", 8080), config.Component{})

	assert.False(t, hasFile(b.generate(), "ingress/people-basic-1-0-0.yaml"), "16.6")
}

// expose: true 但没写 hostname：K8s 下必须报错。
//
// 生成一条没有 host 的 Ingress，等于把这个组件挂到**所有**进入集群的域名上，
// 谁先匹配上谁生效——一个内部组件可能就这样顶掉了门户站点。
func TestExposeWithoutHostnameIsAnError(t *testing.T) {
	b := newBuilder(t)
	b.component(simple("portal/user-frontend", "1.0.0", 80), config.Component{Expose: true})

	_, err := b.build()

	require.Error(t, err)
	assert.Equal(t, clierr.CodeConfigInvalid, clierr.As(err).Code)
	assert.Contains(t, err.Error(), "hostname")
	assert.Contains(t, err.Error(), "portal/user-frontend")
}

// 两个组件写同一个 hostname：必须报错，不能安静地生成两份 Ingress。
//
// # 为什么这是错的
//
// 平台生成的每条 Ingress 规则都是 `host: <hostname>` + `path: /`（§5.5）。
// 两个组件共用一个 hostname，就是两条一模一样的规则指向不同的后端——
// K8s 对此没有定义行为（nginx-ingress 取创建时间最早的那份并记一条冲突日志），
// 表现是外面打进来的请求随机落到其中一个，而 `kubectl apply` 一句抱怨都没有。
//
// # 为什么是报错而不是警告
//
// 与 Docker 侧对称：那边两个组件抢同一个宿主机端口同样是硬错误
// （compose.checkExposePorts），而且两者的出路形状完全一样——给其中一个换个值。
// 一个"生成成功、apply 成功、路由随机"的部署，比一次生成期的失败难查得多。
//
// # 多版本共存时几乎必然踩到
//
// 照 003 §8.3 加第二个版本时，那一整个条目是复制出来的，hostname 会跟着复制。
func TestDuplicateHostnameIsAnError(t *testing.T) {
	b := newBuilder(t)
	b.component(simple("portal/user-frontend", "1.0.0", 80),
		config.Component{Expose: true, Hostname: "shop.example.com"})
	b.component(simple("erp/backend", "1.0.0", 8080),
		config.Component{Expose: true, Hostname: "shop.example.com"})

	_, err := b.build()

	require.Error(t, err, "两个组件占同一个域名，不该安静地生成两份 Ingress")
	assert.Equal(t, clierr.CodeConfigInvalid, clierr.As(err).Code)
	text := err.Error()
	assert.Contains(t, text, "shop.example.com")
	assert.Contains(t, text, "portal/user-frontend", "要点名是哪两个组件")
	assert.Contains(t, text, "erp/backend")
	// 出路必须给出来：使用者未必想得到"一个组件一个子域名"这条约定
	assert.Contains(t, strings.Join(clierr.As(err).Hints, "\n"), "hostname")
}

// 同一个组件的两个版本共用一个 hostname 同样报错。
//
// 这是最常撞的一种：003 §8.3 的多版本条目是复制出来的。
// 而且这里没有"让它俩轮流服务"这种解释——两份 Ingress 不是负载均衡，
// 是未定义行为，请求会稳定落到其中一个版本上。
func TestDuplicateHostnameAcrossVersionsIsAnError(t *testing.T) {
	b := newBuilder(t)
	b.component(simple("portal/user-frontend", "1.0.0", 80),
		config.Component{Expose: true, Hostname: "shop.example.com"})
	b.component(simple("portal/user-frontend", "2.0.0", 80),
		config.Component{Expose: true, Hostname: "shop.example.com"})

	_, err := b.build()

	require.Error(t, err)
	assert.Contains(t, err.Error(), "1.0.0")
	assert.Contains(t, err.Error(), "2.0.0", "要带版本号，否则两行看起来一模一样")
}

// 没 expose 的组件即使写了 hostname 也不占域名——它根本不生成 Ingress。
func TestHostnameOnUnexposedComponentDoesNotConflict(t *testing.T) {
	b := newBuilder(t)
	b.component(simple("portal/user-frontend", "1.0.0", 80),
		config.Component{Expose: true, Hostname: "shop.example.com"})
	b.component(simple("erp/backend", "1.0.0", 8080),
		config.Component{Hostname: "shop.example.com"})

	_, err := b.build()

	require.NoError(t, err, "没 expose 就不生成 Ingress，谈不上抢域名")
}

// ============================================================
// 16.12 K8s 不需要 exposePort
// ============================================================

// K8s 通过 Ingress + 域名路由，两个组件共用 80 端口也不冲突，
// 因此 exposePort 在这个目标下完全不参与生成——写了也没有任何影响。
func TestExposePortIgnoredOnK8s(t *testing.T) {
	withPort := func(exposePort int) []k8s.File {
		b := newBuilder(t)
		b.component(simple("portal/user-frontend", "1.0.0", 80),
			config.Component{Expose: true, Hostname: "portal.example.com", ExposePort: exposePort})
		return b.generate().Files
	}

	assert.Equal(t, withPort(0), withPort(18080), "16.12 exposePort 不该改变任何一份 K8s 清单")
}

// ============================================================
// 16.7 迁移 Job
// ============================================================

func migrating(m *manifest.Manifest) *manifest.Manifest {
	m.Migration = &manifest.Migration{Command: []string{"/app/server", "migrate", "up"}}
	return m
}

func TestMigrationJobGenerated(t *testing.T) {
	b := newBuilder(t)
	b.component(migrating(withDatabase(simple("people/basic", "1.0.0", 8080))), config.Component{})
	b.resource(pgResource(config.Binding{ComponentID: "people/basic", Database: "people"}))

	doc := b.doc("migrations/people-basic-1-0-0-migration.yaml")

	assert.Equal(t, "batch/v1", doc["apiVersion"], "16.7")
	assert.Equal(t, "Job", doc["kind"], "16.7")
	assert.Equal(t, "people-basic-1-0-0-migration", dig(t, doc, "metadata", "name"))
	assert.Equal(t, "brickkit-my-erp", dig(t, doc, "metadata", "namespace"))
	assert.Equal(t, 0, dig(t, doc, "spec", "backoffLimit"),
		"16.7 迁移失败不重试：重试只会把同一个坏脚本再跑几遍")
	assert.Equal(t, "Never", dig(t, doc, "spec", "template", "spec", "restartPolicy"))
}

func TestMigrationJobContainer(t *testing.T) {
	b := newBuilder(t)
	b.component(migrating(withDatabase(simple("people/basic", "1.0.0", 8080))), config.Component{})
	b.resource(pgResource(config.Binding{ComponentID: "people/basic", Database: "people"}))

	doc := b.doc("migrations/people-basic-1-0-0-migration.yaml")
	containers, ok := dig(t, doc, "spec", "template", "spec", "containers").([]any)
	require.True(t, ok && len(containers) == 1, "应有且只有一个容器：%v", containers)
	c, _ := containers[0].(map[string]any)

	assert.Equal(t, "registry.example.com/people-basic:1.0.0", c["image"],
		"16.7 用组件自己的镜像，迁移脚本与业务代码同版本")
	assert.Equal(t, []any{"/app/server", "migrate", "up"}, c["command"],
		"16.7 K8s 的 command 整体替换 ENTRYPOINT，不用像 compose 那样拆成两半")
	assert.NotContains(t, c, "livenessProbe", "一次性任务不该有探针")
	assert.NotContains(t, c, "ports", "迁移容器不监听端口")
}

// 迁移 Pod 绝不能带上组件 Service 的 selector 标签。
//
// 带上的后果：迁移 Pod 被登记成该 Service 的一个后端，而它没有就绪探针，
// K8s 认为它随时可用——迁移期间打到这个组件的请求会有一部分被转发给
// 一个根本不监听端口的 Pod，表现成偶发的 connection refused。
func TestMigrationPodIsNotAServiceEndpoint(t *testing.T) {
	b := newBuilder(t)
	b.component(migrating(withDatabase(simple("people/basic", "1.0.0", 8080))), config.Component{})
	b.resource(pgResource(config.Binding{ComponentID: "people/basic", Database: "people"}))

	selector := dig(t, b.doc("services/people-basic-1-0-0.yaml"), "spec", "selector")
	podLabels := dig(t, b.doc("migrations/people-basic-1-0-0-migration.yaml"),
		"spec", "template", "metadata", "labels")

	labels, ok := podLabels.(map[string]any)
	require.True(t, ok, "Pod 标签必须是对象：%v", podLabels)
	for key, want := range selector.(map[string]any) {
		assert.NotEqual(t, want, labels[key],
			"迁移 Pod 的标签 %s 不能与 Service 的 selector 相同", key)
	}
	assert.Equal(t, "migration", labels["brickkit.io/role"], "迁移 Pod 要标出自己的角色")
}

// 002 §8.5：迁移容器的环境变量与主容器完全一致，密码同样走 Secret。
func TestMigrationJobEnvMatchesComponent(t *testing.T) {
	b := newBuilder(t)
	b.component(migrating(withDatabase(simple("people/basic", "1.0.0", 8080))), config.Component{})
	b.resource(pgResource(config.Binding{ComponentID: "people/basic", Database: "people"}))

	doc := b.doc("migrations/people-basic-1-0-0-migration.yaml")
	containers, _ := dig(t, doc, "spec", "template", "spec", "containers").([]any)
	job, _ := containers[0].(map[string]any)

	assert.Equal(t, envOf(t, b.container("people-basic-1-0-0")), envOf(t, job),
		"16.7 迁移容器与主容器的环境变量必须一模一样")
}

// 没有 migration 字段的组件不生成 Job。
func TestNoMigrationJobWithoutMigration(t *testing.T) {
	b := newBuilder(t)
	b.component(simple("people/basic", "1.0.0", 8080), config.Component{})

	assert.False(t, hasFile(b.generate(), "migrations/people-basic-1-0-0-migration.yaml"))
}

// 要清理的旧 Job 名字必须回填给命令层（16.14 的输入）。
func TestResultCarriesMigrationJobs(t *testing.T) {
	b := newBuilder(t)
	b.component(migrating(withDatabase(simple("people/basic", "1.0.0", 8080))), config.Component{})
	b.component(simple("department/tree", "1.0.0", 8080), config.Component{})
	b.resource(pgResource(config.Binding{ComponentID: "people/basic", Database: "people"}))

	assert.Equal(t, [][]string{{"people-basic-1-0-0-migration"}}, b.generate().MigrationGroups)
}

// ============================================================
// 16.13 目录结构
// ============================================================

func TestDirectoryLayout(t *testing.T) {
	b := newBuilder(t)
	b.component(migrating(withDatabase(simple("people/basic", "1.0.0", 8080))), config.Component{})
	b.component(simple("portal/user-frontend", "1.0.0", 80),
		config.Component{Expose: true, Hostname: "portal.example.com"})
	b.resource(pgResource(config.Binding{ComponentID: "people/basic", Database: "people"}))

	assert.Equal(t, []string{
		"deployments/people-basic-1-0-0.yaml",
		"deployments/portal-user-frontend-1-0-0.yaml",
		"ingress/portal-user-frontend-1-0-0.yaml",
		"migrations/people-basic-1-0-0-migration.yaml",
		"namespace.yaml",
		"secrets/resource-secrets.yaml",
		"services/people-basic-1-0-0.yaml",
		"services/portal-user-frontend-1-0-0.yaml",
	}, pathsOf(b.generate()), "16.13 目录结构（005 §5）")
}

func TestWriteFiles(t *testing.T) {
	b := newBuilder(t)
	b.component(migrating(withDatabase(simple("people/basic", "1.0.0", 8080))), config.Component{})
	b.resource(pgResource(config.Binding{ComponentID: "people/basic", Database: "people"}))
	result := b.generate()

	dir := filepath.Join(t.TempDir(), "k8s")
	require.NoError(t, k8s.WriteFiles(dir, result.Files))

	for _, f := range result.Files {
		content, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(f.Path)))
		require.NoError(t, err, "16.13 %s 应被写出来", f.Path)
		assert.Equal(t, string(f.YAML), string(content))
	}
}

// 重新生成要先清干净：组件删掉后，上次留下的清单如果还在，
// kubectl apply -f 整个目录会把它又部署一遍。
func TestWriteFilesClearsStaleManifests(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "k8s")
	stale := filepath.Join(dir, "deployments", "legacy-thing-1-0-0.yaml")
	require.NoError(t, os.MkdirAll(filepath.Dir(stale), 0o755))
	require.NoError(t, os.WriteFile(stale, []byte("kind: Deployment\n"), 0o644))

	b := newBuilder(t)
	b.component(simple("people/basic", "1.0.0", 8080), config.Component{})
	require.NoError(t, k8s.WriteFiles(dir, b.generate().Files))

	_, err := os.Stat(stale)
	assert.True(t, os.IsNotExist(err), "上一次生成的残留清单必须被清掉")
	_, err = os.Stat(filepath.Join(dir, "deployments", "people-basic-1-0-0.yaml"))
	assert.NoError(t, err)
}

// Secret 文件的权限要收紧：里面是明文密码。
func TestSecretFileIsNotWorldReadable(t *testing.T) {
	b := newBuilder(t)
	b.component(withDatabase(simple("people/basic", "1.0.0", 8080)), config.Component{})
	b.resource(pgResource(config.Binding{ComponentID: "people/basic", Database: "people"}))

	dir := filepath.Join(t.TempDir(), "k8s")
	require.NoError(t, k8s.WriteFiles(dir, b.generate().Files))

	info, err := os.Stat(filepath.Join(dir, "secrets", "resource-secrets.yaml"))
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm(), "Secret 文件只有自己能读")

	info, err = os.Stat(filepath.Join(dir, "deployments", "people-basic-1-0-0.yaml"))
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o644), info.Mode().Perm(), "其余清单照常")
}

// 同一个组件的多个版本，迁移 Job 归到同一组并按版本号排序。
//
// 分组是给引擎用的执行顺序：组内串行、组间并行。理由与 compose 侧的
// chainMigrations 完全一样——同一组件 ID 的多个版本共用一个库、共用一个
// component_id，同时跑会在空库上撞主键（002 §8.11、§8.10）。
func TestMigrationJobsAreGroupedByComponent(t *testing.T) {
	b := newBuilder(t)
	b.component(migrating(withDatabase(simple("demo/hello", "2.0.0", 8080))), config.Component{})
	b.component(migrating(withDatabase(simple("demo/hello", "10.0.0", 8080))), config.Component{})
	b.component(migrating(withDatabase(simple("people/basic", "1.0.0", 8080))), config.Component{})
	b.resource(pgResource(
		config.Binding{ComponentID: "demo/hello", Database: "hello"},
		config.Binding{ComponentID: "people/basic", Database: "people"}))

	groups := b.generate().MigrationGroups

	require.Len(t, groups, 2, "两个组件 ID → 两组：%v", groups)
	assert.Equal(t, [][]string{
		// 按版本号排，不是服务名的字典序：10.0.0 必须排在 2.0.0 后面
		{"demo-hello-2-0-0-migration", "demo-hello-10-0-0-migration"},
		{"people-basic-1-0-0-migration"},
	}, groups, "组内按版本升序；组间按组件 ID 字典序（同一份配置每次都要给出同一个顺序）")
}
