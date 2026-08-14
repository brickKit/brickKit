// 本文件是 Step 16-A「K8s Namespace / Deployment / Secret 生成」的业务行为测试，
// 覆盖开发计划 16.1、16.2、16.8、16.9、16.10、16.11、16.15。
//
// 与 compose 那边同样的取舍：断言落在**最终 YAML 里有什么**，不看内部结构。
// 这些文件最终要交给 kubectl，写错一个字段名 K8s 只会沉默地忽略它。
package k8s_test

import (
	"context"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"

	"github.com/brickkit/brickkit/internal/cascade"
	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/config"
	"github.com/brickkit/brickkit/internal/inject"
	"github.com/brickkit/brickkit/internal/k8s"
	"github.com/brickkit/brickkit/internal/manifest"
	"github.com/brickkit/brickkit/internal/resolver"
)

// ============================================================
// 夹具
// ============================================================

type stubProvider map[string]*manifest.Manifest

func (p stubProvider) Manifest(_ context.Context, id, version string) (*manifest.Manifest, error) {
	m, ok := p[id+"@"+version]
	if !ok {
		return nil, errNotFound{id + "@" + version}
	}
	return m, nil
}

type errNotFound struct{ ref string }

func (e errNotFound) Error() string { return "夹具里没有 " + e.ref }

type builder struct {
	t        *testing.T
	provider stubProvider
	roots    []resolver.Ref
	cfg      *config.Config
	env      map[string]string
}

func newBuilder(t *testing.T) *builder {
	return &builder{
		t:        t,
		provider: stubProvider{},
		cfg:      &config.Config{Project: "my-erp", Deploy: config.Deploy{Target: config.TargetK8s}},
		env:      map[string]string{"POSTGRES_PASSWORD": "s3cr3t"},
	}
}

func (b *builder) component(m *manifest.Manifest, entry config.Component) *builder {
	b.provider[m.Metadata.ID+"@"+m.Metadata.Version] = m
	entry.ID, entry.Version = m.Metadata.ID, m.Metadata.Version
	b.cfg.Components = append(b.cfg.Components, entry)
	b.roots = append(b.roots, resolver.Ref{ID: entry.ID, Version: entry.Version})
	return b
}

func (b *builder) resource(r config.Resource) *builder {
	b.cfg.Resources = append(b.cfg.Resources, r)
	return b
}

// build 跑完整条链路：解析 → 级联 → 注入 → 生成 K8s 清单，允许失败。
func (b *builder) build() (*k8s.Result, error) {
	b.t.Helper()

	graph, err := resolver.New(b.provider).Resolve(context.Background(), b.roots...)
	require.NoError(b.t, err)

	states, err := cascade.Compute(b.cfg, graph)
	require.NoError(b.t, err)

	env, err := inject.Build(b.cfg, graph, states)
	require.NoError(b.t, err)

	return k8s.Generate(b.cfg, graph, states, env, k8s.Options{
		Now: func() time.Time { return time.Date(2026, 8, 15, 10, 0, 0, 0, time.UTC) },
		Lookup: func(name string) (string, bool) {
			value, ok := b.env[name]
			return value, ok
		},
	})
}

func (b *builder) generate() *k8s.Result {
	b.t.Helper()

	result, err := b.build()
	require.NoError(b.t, err)
	return result
}

// doc 取出某个路径的清单并解析成通用结构。
func (b *builder) doc(path string) map[string]any {
	b.t.Helper()

	file := b.file(path)
	var out map[string]any
	require.NoError(b.t, yaml.Unmarshal(file.YAML, &out), "生成的内容必须是合法 YAML：%s", path)
	return out
}

// file 取出某份生成的文件。
func (b *builder) file(path string) k8s.File {
	b.t.Helper()

	result := b.generate()
	for _, f := range result.Files {
		if f.Path == path {
			return f
		}
	}
	require.Failf(b.t, "缺少生成文件", "期望 %s，实际有：%v", path, pathsOf(result))
	return k8s.File{}
}

func pathsOf(r *k8s.Result) []string {
	out := make([]string, 0, len(r.Files))
	for _, f := range r.Files {
		out = append(out, f.Path)
	}
	return out
}

func hasFile(r *k8s.Result, path string) bool {
	for _, f := range r.Files {
		if f.Path == path {
			return true
		}
	}
	return false
}

// container 取出 Deployment 里唯一的那个容器。
func (b *builder) container(service string) map[string]any {
	b.t.Helper()

	doc := b.doc("deployments/" + service + ".yaml")
	containers := dig(b.t, doc, "spec", "template", "spec", "containers")
	list, ok := containers.([]any)
	require.True(b.t, ok && len(list) == 1, "应有且只有一个容器：%v", containers)

	c, ok := list[0].(map[string]any)
	require.True(b.t, ok, "容器必须是一个对象：%v", list[0])
	return c
}

// dig 沿路径取值，任何一层缺失都直接让测试失败。
func dig(t *testing.T, doc any, path ...string) any {
	t.Helper()

	current := doc
	for i, key := range path {
		m, ok := current.(map[string]any)
		require.True(t, ok, "第 %d 层 %q 不是对象：%v", i, key, current)
		current, ok = m[key]
		require.True(t, ok, "缺少字段 %s（在 %v 中）", strings.Join(path[:i+1], "."), m)
	}
	return current
}

// envOf 把容器的 env 数组转成 map：普通变量取 value，Secret 变量取引用描述。
func envOf(t *testing.T, container map[string]any) map[string]any {
	t.Helper()

	out := map[string]any{}
	raw, ok := container["env"].([]any)
	if !ok {
		return out
	}
	for _, item := range raw {
		entry, ok := item.(map[string]any)
		require.True(t, ok, "env 每一项都必须是对象：%v", item)
		name, ok := entry["name"].(string)
		require.True(t, ok, "env 每一项都必须有 name：%v", entry)

		if value, plain := entry["value"]; plain {
			out[name] = value
			continue
		}
		out[name] = entry["valueFrom"]
	}
	return out
}

// simple 造一个最小组件。
func simple(id, version string, port int) *manifest.Manifest {
	return &manifest.Manifest{
		APIVersion: manifest.APIVersion,
		Kind:       manifest.Kind,
		Metadata:   manifest.Metadata{ID: id, Name: id, Version: version},
		Deployment: manifest.Deployment{
			Type:  manifest.DeploymentTypeContainer,
			Image: "registry.example.com/" + strings.ReplaceAll(id, "/", "-") + ":" + version,
			Port:  port,
		},
		HealthCheck: manifest.HealthCheck{Type: manifest.HealthCheckHTTP, Path: "/healthz"},
	}
}

func withDatabase(m *manifest.Manifest) *manifest.Manifest {
	if m.Dependencies == nil {
		m.Dependencies = &manifest.Dependencies{}
	}
	m.Dependencies.Resources = append(m.Dependencies.Resources,
		manifest.ResourceDep{Kind: "database", Engine: "postgresql"})
	return m
}

func dependsOn(m *manifest.Manifest, id, version string) *manifest.Manifest {
	if m.Dependencies == nil {
		m.Dependencies = &manifest.Dependencies{}
	}
	m.Dependencies.Components = append(m.Dependencies.Components,
		manifest.ComponentDep{ID: id, Version: version})
	return m
}

// pgResource 是一个 PostgreSQL 资源（K8s 环境下由运维部署，CLI 只注入连接信息）。
func pgResource(bindings ...config.Binding) config.Resource {
	return config.Resource{
		Kind: config.ResourceKindDatabase, Engine: "postgresql", ID: "people-db",
		Host: "postgres.infra.svc", Port: 5432, Username: "people_user",
		Password: "${POSTGRES_PASSWORD}", Bindings: bindings,
	}
}

// ============================================================
// 16.1 Namespace
// ============================================================

func TestNamespaceGenerated(t *testing.T) {
	b := newBuilder(t)
	b.component(simple("people/basic", "1.0.0", 8080), config.Component{})

	doc := b.doc("namespace.yaml")

	assert.Equal(t, "v1", doc["apiVersion"], "16.1")
	assert.Equal(t, "Namespace", doc["kind"], "16.1")
	assert.Equal(t, "brickkit-my-erp", dig(t, doc, "metadata", "name"), "16.1 名称是 brickkit-<项目名>")
	assert.Equal(t, "my-erp", dig(t, doc, "metadata", "labels", "brickkit.io/project"), "16.1 labels")
}

// 命名空间名要能回填到 Result 里：后续 kubectl 的每条命令都要 -n 它。
func TestResultCarriesNamespace(t *testing.T) {
	b := newBuilder(t)
	b.component(simple("people/basic", "1.0.0", 8080), config.Component{})

	assert.Equal(t, "brickkit-my-erp", b.generate().Namespace)
}

// ============================================================
// 16.2 Deployment
// ============================================================

func TestDeploymentBasics(t *testing.T) {
	b := newBuilder(t)
	b.component(simple("people/basic", "1.0.0", 8080), config.Component{})

	doc := b.doc("deployments/people-basic-1-0-0.yaml")

	assert.Equal(t, "apps/v1", doc["apiVersion"], "16.2")
	assert.Equal(t, "Deployment", doc["kind"], "16.2")
	assert.Equal(t, "people-basic-1-0-0", dig(t, doc, "metadata", "name"), "名字是版本化服务名")
	assert.Equal(t, "brickkit-my-erp", dig(t, doc, "metadata", "namespace"))
	assert.Equal(t, 1, dig(t, doc, "spec", "replicas"), "多实例是后期能力，先固定 1")
	assert.Equal(t, "people-basic-1-0-0",
		dig(t, doc, "spec", "selector", "matchLabels", "app"), "selector 必须选得中自己的 Pod")
	assert.Equal(t, "people-basic-1-0-0",
		dig(t, doc, "spec", "template", "metadata", "labels", "app"))
}

func TestDeploymentLabels(t *testing.T) {
	b := newBuilder(t)
	b.component(simple("people/basic", "1.0.0", 8080), config.Component{})

	doc := b.doc("deployments/people-basic-1-0-0.yaml")

	assert.Equal(t, map[string]any{
		"app":                           "people-basic-1-0-0",
		"brickkit.io/component":         "people-basic",
		"brickkit.io/component-version": "1.0.0",
		"brickkit.io/project":           "my-erp",
	}, dig(t, doc, "metadata", "labels"), "16.2 labels（005 §5.3）")
	assert.Equal(t, "people/basic",
		dig(t, doc, "metadata", "annotations", "brickkit.io/component-id"),
		"原样的组件 ID 放注解里——标签值放不下带斜杠的写法")
}

// 标签值里绝不能出现斜杠。
//
// K8s 的标签**值**只允许字母数字与 - _ .（斜杠只在标签**键**的前缀里合法）。
// 设计书 005 §5.3 原来的样例写的是 `brickkit.io/component-id: people/basic`，
// 那份 Deployment 会被 API Server 整份拒绝——错误信息还只提"a valid label must…"，
// 完全看不出是组件 ID 的锅。
func TestLabelValuesAreValid(t *testing.T) {
	b := newBuilder(t)
	b.component(simple("people/basic", "1.0.0", 8080), config.Component{})

	valid := regexp.MustCompile(`^[a-zA-Z0-9]([-_.a-zA-Z0-9]*[a-zA-Z0-9])?$`)
	for _, f := range b.generate().Files {
		var doc map[string]any
		require.NoError(t, yaml.Unmarshal(f.YAML, &doc))
		for _, labels := range labelSetsOf(doc) {
			for key, value := range labels {
				text, ok := value.(string)
				require.True(t, ok, "%s 的标签 %s 必须是字符串：%v", f.Path, key, value)
				assert.Regexp(t, valid, text, "%s 的标签 %s 值不合法", f.Path, key)
				assert.LessOrEqual(t, len(text), 63, "%s 的标签 %s 超长", f.Path, key)
			}
		}
	}
}

// labelSetsOf 递归找出文档里所有的 labels 段。
func labelSetsOf(node any) []map[string]any {
	var out []map[string]any
	switch value := node.(type) {
	case map[string]any:
		if labels, ok := value["labels"].(map[string]any); ok {
			out = append(out, labels)
		}
		for _, child := range value {
			out = append(out, labelSetsOf(child)...)
		}
	case []any:
		for _, child := range value {
			out = append(out, labelSetsOf(child)...)
		}
	}
	return out
}

func TestDeploymentContainer(t *testing.T) {
	b := newBuilder(t)
	b.component(simple("people/basic", "1.0.0", 8080), config.Component{})

	c := b.container("people-basic-1-0-0")

	assert.Equal(t, "people-basic", c["name"], "16.2 容器名是组件 ID（不带版本）")
	assert.Equal(t, "registry.example.com/people-basic:1.0.0", c["image"], "16.2 image")
	assert.Equal(t, []any{map[string]any{"name": "http", "containerPort": 8080}},
		c["ports"], "16.2 主端口")
}

// 环境变量与 compose 那边同源（inject），K8s 只是换个写法。
func TestDeploymentEnv(t *testing.T) {
	b := newBuilder(t)
	b.component(simple("department/tree", "1.0.0", 8080), config.Component{})
	b.component(dependsOn(simple("people/basic", "1.0.0", 8080), "department/tree", "1.0.0"),
		config.Component{})

	env := envOf(t, b.container("people-basic-1-0-0"))

	assert.Equal(t, "people/basic", env["COMPONENT_ID"], "16.2 平台变量")
	assert.Equal(t, "1.0.0", env["COMPONENT_VERSION"])
	assert.Equal(t, "http://department-tree-1-0-0:8080", env["DEPARTMENT_TREE_ENDPOINT"],
		"16.2 依赖地址指向 K8s Service 名")
}

// 环境变量值必须是字符串：K8s 的 env.value 只接受字符串，
// 写成数字会被 API Server 直接拒绝（`cannot unmarshal number into field value`）。
func TestEnvValuesAreStrings(t *testing.T) {
	b := newBuilder(t)
	b.component(withDatabase(simple("people/basic", "1.0.0", 8080)), config.Component{})
	b.resource(pgResource(config.Binding{ComponentID: "people/basic", Database: "people"}))

	env := envOf(t, b.container("people-basic-1-0-0"))

	assert.Equal(t, "5432", env["DATABASE_PORT"], "端口是数字，但在 env 里必须写成字符串")
}

// ============================================================
// 16.9 / 16.10 探针
// ============================================================

func TestLivenessProbe(t *testing.T) {
	b := newBuilder(t)
	b.component(simple("people/basic", "1.0.0", 8080), config.Component{})

	probe := b.container("people-basic-1-0-0")["livenessProbe"]

	assert.Equal(t, "/healthz", dig(t, probe, "httpGet", "path"), "16.9")
	assert.Equal(t, 8080, dig(t, probe, "httpGet", "port"), "16.9 探主端口")
	assert.Equal(t, 10, dig(t, probe, "initialDelaySeconds"), "16.9（005 §5.3）")
	assert.Equal(t, 10, dig(t, probe, "periodSeconds"))
	assert.Equal(t, 3, dig(t, probe, "timeoutSeconds"))
	assert.Equal(t, 3, dig(t, probe, "failureThreshold"))
}

func TestReadinessProbe(t *testing.T) {
	b := newBuilder(t)
	b.component(simple("people/basic", "1.0.0", 8080), config.Component{})

	probe := b.container("people-basic-1-0-0")["readinessProbe"]

	assert.Equal(t, "/healthz", dig(t, probe, "httpGet", "path"), "16.10")
	assert.Equal(t, 8080, dig(t, probe, "httpGet", "port"), "16.10")
	assert.Equal(t, 5, dig(t, probe, "initialDelaySeconds"), "16.10 就绪探针比存活探针早（005 §5.3）")
	assert.Equal(t, 5, dig(t, probe, "periodSeconds"))
}

// healthCheck.type: tcp → tcpSocket 探针。
func TestTCPProbe(t *testing.T) {
	m := simple("infra/queue", "1.0.0", 5672)
	m.HealthCheck = manifest.HealthCheck{Type: manifest.HealthCheckTCP}

	b := newBuilder(t)
	b.component(m, config.Component{})

	c := b.container("infra-queue-1-0-0")

	assert.Equal(t, 5672, dig(t, c["livenessProbe"], "tcpSocket", "port"))
	assert.Equal(t, 5672, dig(t, c["readinessProbe"], "tcpSocket", "port"))
}

// healthCheck.type: none → 一个探针都不生成。
//
// 生成一个探不通的探针，K8s 会反复 kill 掉一个其实健康的 Pod。
func TestNoProbeWhenHealthCheckNone(t *testing.T) {
	m := simple("infra/job", "1.0.0", 8080)
	m.HealthCheck = manifest.HealthCheck{Type: manifest.HealthCheckNone}

	b := newBuilder(t)
	b.component(m, config.Component{})

	c := b.container("infra-job-1-0-0")

	assert.NotContains(t, c, "livenessProbe")
	assert.NotContains(t, c, "readinessProbe")
}

// ============================================================
// 16.11 资源配额
// ============================================================

func TestResourcesUseCLIDefaults(t *testing.T) {
	b := newBuilder(t)
	b.component(simple("people/basic", "1.0.0", 8080), config.Component{})

	resources := b.container("people-basic-1-0-0")["resources"]

	assert.Equal(t, inject.DefaultRequestCPU, dig(t, resources, "requests", "cpu"), "16.11")
	assert.Equal(t, inject.DefaultRequestMemory, dig(t, resources, "requests", "memory"))
	assert.Equal(t, inject.DefaultLimitCPU, dig(t, resources, "limits", "cpu"))
	assert.Equal(t, inject.DefaultLimitMemory, dig(t, resources, "limits", "memory"))
}

// K8s 的写法就是 Manifest 的写法（100m / 128Mi），不需要 compose 那样的换算。
func TestResourcesMergedFromConfig(t *testing.T) {
	m := simple("people/basic", "1.0.0", 8080)
	m.Deployment.Resources = &manifest.Resources{
		Requests: &manifest.ResourceSpec{CPU: "200m", Memory: "256Mi"},
		Limits:   &manifest.ResourceSpec{CPU: "1", Memory: "1Gi"},
	}

	b := newBuilder(t)
	b.component(m, config.Component{
		// 使用者只想调大内存上限，其余保持组件的推荐值
		Resources: &manifest.Resources{Limits: &manifest.ResourceSpec{Memory: "2Gi"}},
	})

	resources := b.container("people-basic-1-0-0")["resources"]

	assert.Equal(t, "200m", dig(t, resources, "requests", "cpu"), "16.11 组件推荐值保留")
	assert.Equal(t, "256Mi", dig(t, resources, "requests", "memory"))
	assert.Equal(t, "1", dig(t, resources, "limits", "cpu"), "16.11 没被覆盖的字段不能丢")
	assert.Equal(t, "2Gi", dig(t, resources, "limits", "memory"), "16.11 覆盖值优先")
}

// ============================================================
// 16.8 / 16.15 Secret
// ============================================================

func TestSecretGenerated(t *testing.T) {
	b := newBuilder(t)
	b.component(withDatabase(simple("people/basic", "1.0.0", 8080)), config.Component{})
	b.resource(pgResource(config.Binding{ComponentID: "people/basic", Database: "people"}))

	doc := b.doc("secrets/resource-secrets.yaml")

	assert.Equal(t, "v1", doc["apiVersion"], "16.8")
	assert.Equal(t, "Secret", doc["kind"], "16.8")
	assert.Equal(t, "people-db-secret", dig(t, doc, "metadata", "name"), "16.8 <资源ID>-secret")
	assert.Equal(t, "brickkit-my-erp", dig(t, doc, "metadata", "namespace"))
	assert.Equal(t, "Opaque", doc["type"], "16.8")
	assert.Equal(t, "s3cr3t", dig(t, doc, "stringData", "password"),
		"16.8 ${POSTGRES_PASSWORD} 必须在生成时求值——kubectl 不做变量替换")
}

func TestSecretReferencedFromEnv(t *testing.T) {
	b := newBuilder(t)
	b.component(withDatabase(simple("people/basic", "1.0.0", 8080)), config.Component{})
	b.resource(pgResource(config.Binding{ComponentID: "people/basic", Database: "people"}))

	env := envOf(t, b.container("people-basic-1-0-0"))

	assert.Equal(t, map[string]any{"secretKeyRef": map[string]any{
		"name": "people-db-secret", "key": "password",
	}}, env["DATABASE_PASSWORD"], "16.15 密码只能通过 secretKeyRef 引用")
	assert.Equal(t, "people_user", env["DATABASE_USER"], "非敏感的连接信息照常明文")
}

// 密码在 Deployment 里绝不能出现明文——那份 YAML 会进 git、进 CI 日志。
func TestPasswordNeverAppearsInDeployment(t *testing.T) {
	b := newBuilder(t)
	b.component(withDatabase(simple("people/basic", "1.0.0", 8080)), config.Component{})
	b.resource(pgResource(config.Binding{ComponentID: "people/basic", Database: "people"}))

	text := string(b.file("deployments/people-basic-1-0-0.yaml").YAML)

	assert.NotContains(t, text, "s3cr3t", "16.15 明文密码不能出现在 Deployment 里")
	assert.NotContains(t, text, "${POSTGRES_PASSWORD}", "占位符同样不该出现")
}

// 一个组件绑定两个同类资源（envPrefix 区分）时，每个资源一份 Secret。
func TestSecretPerResource(t *testing.T) {
	b := newBuilder(t)
	b.component(withDatabase(simple("people/basic", "1.0.0", 8080)), config.Component{})
	b.resource(config.Resource{
		Kind: config.ResourceKindDatabase, Engine: "postgresql", ID: "primary-db",
		Host: "pg-primary.infra.svc", Port: 5432, Username: "u", Password: "${PRIMARY_PASSWORD}",
		Bindings: []config.Binding{{ComponentID: "people/basic", Database: "people", EnvPrefix: "primary"}},
	})
	b.resource(config.Resource{
		Kind: config.ResourceKindDatabase, Engine: "postgresql", ID: "archive-db",
		Host: "pg-archive.infra.svc", Port: 5432, Username: "u", Password: "${ARCHIVE_PASSWORD}",
		Bindings: []config.Binding{{ComponentID: "people/basic", Database: "people", EnvPrefix: "archive"}},
	})
	b.env["PRIMARY_PASSWORD"] = "p1"
	b.env["ARCHIVE_PASSWORD"] = "p2"

	text := string(b.file("secrets/resource-secrets.yaml").YAML)
	env := envOf(t, b.container("people-basic-1-0-0"))

	assert.Contains(t, text, "primary-db-secret", "16.8 每个资源一份 Secret")
	assert.Contains(t, text, "archive-db-secret")
	assert.Equal(t, map[string]any{"secretKeyRef": map[string]any{
		"name": "primary-db-secret", "key": "password",
	}}, env["PRIMARY_DATABASE_PASSWORD"], "16.15 envPrefix 变量指向对应资源的 Secret")
	assert.Equal(t, map[string]any{"secretKeyRef": map[string]any{
		"name": "archive-db-secret", "key": "password",
	}}, env["ARCHIVE_DATABASE_PASSWORD"])
}

// 对象存储的密钥叫 secret-key，不叫 password。
func TestStorageSecretKeyName(t *testing.T) {
	m := simple("media/store", "1.0.0", 8080)
	m.Dependencies = &manifest.Dependencies{
		Resources: []manifest.ResourceDep{{Kind: "storage", Engine: "s3"}},
	}

	b := newBuilder(t)
	b.component(m, config.Component{})
	b.resource(config.Resource{
		Kind: config.ResourceKindStorage, Engine: "s3", ID: "assets",
		Host: "rustfs.infra.svc", Port: 9000, Username: "ak", Password: "${STORAGE_SECRET}",
		Bindings: []config.Binding{{ComponentID: "media/store", Database: "assets"}},
	})
	b.env["STORAGE_SECRET"] = "sk"

	env := envOf(t, b.container("media-store-1-0-0"))

	assert.Equal(t, map[string]any{"secretKeyRef": map[string]any{
		"name": "assets-secret", "key": "secret-key",
	}}, env["STORAGE_SECRET_KEY"], "16.15")
	assert.Equal(t, "ak", env["STORAGE_ACCESS_KEY"], "access key 不是密钥，照常明文")
}

// 没有任何敏感变量时不生成 Secret 文件（空 Secret 只会让人以为漏了什么）。
func TestNoSecretFileWithoutSensitiveVars(t *testing.T) {
	b := newBuilder(t)
	b.component(simple("people/basic", "1.0.0", 8080), config.Component{})

	assert.False(t, hasFile(b.generate(), "secrets/resource-secrets.yaml"))
}

// ${VAR} 没定义时必须报错。
//
// 放过去的后果是把字面量 "${POSTGRES_PASSWORD}" 当成密码部署上去：
// kubectl 不做变量替换，Pod 会以认证失败反复重启，而 YAML 看上去完全正常。
func TestUnresolvedEnvVarIsAnError(t *testing.T) {
	b := newBuilder(t)
	b.component(withDatabase(simple("people/basic", "1.0.0", 8080)), config.Component{})
	b.resource(pgResource(config.Binding{ComponentID: "people/basic", Database: "people"}))
	delete(b.env, "POSTGRES_PASSWORD")

	_, err := b.build()

	require.Error(t, err)
	assert.Equal(t, clierr.CodeConfigInvalid, clierr.As(err).Code)
	assert.Contains(t, err.Error(), "POSTGRES_PASSWORD", "要说清楚是哪个变量没定义")
}

// 非敏感字段里的 ${VAR} 同样要求值。
func TestPlainValuesAreExpandedToo(t *testing.T) {
	r := pgResource(config.Binding{ComponentID: "people/basic", Database: "people"})
	r.Host = "${PG_HOST}"

	b := newBuilder(t)
	b.component(withDatabase(simple("people/basic", "1.0.0", 8080)), config.Component{})
	b.resource(r)
	b.env["PG_HOST"] = "pg.prod.svc"

	env := envOf(t, b.container("people-basic-1-0-0"))

	assert.Equal(t, "pg.prod.svc", env["DATABASE_HOST"])
}

// ============================================================
// 只生成本次真的会跑的东西
// ============================================================

func TestSkippedComponentsAreNotGenerated(t *testing.T) {
	off := false
	b := newBuilder(t)
	b.component(simple("people/basic", "1.0.0", 8080), config.Component{})
	b.component(simple("legacy/thing", "1.0.0", 8080), config.Component{Enabled: &off})

	result := b.generate()

	assert.True(t, hasFile(result, "deployments/people-basic-1-0-0.yaml"))
	assert.False(t, hasFile(result, "deployments/legacy-thing-1-0-0.yaml"),
		"enabled: false 的组件不该出现在清单里")
}

// local: true 在 K8s 下必须报错。
//
// 它的语义是"这个组件跑在你的 IDE 里，其他组件通过宿主机地址访问它"——
// 集群里的 Pod 根本连不到开发者的笔记本。悄悄跳过会让依赖方拿到一个
// 指向不存在 Service 的地址，表现成随机的连接超时。
func TestLocalTrueRejectedOnK8s(t *testing.T) {
	b := newBuilder(t)
	b.component(simple("people/basic", "1.0.0", 8080), config.Component{Local: true})

	_, err := b.build()

	require.Error(t, err)
	assert.Equal(t, clierr.CodeConfigInvalid, clierr.As(err).Code)
	assert.Contains(t, err.Error(), "local")
	assert.Contains(t, err.Error(), "people/basic")
}

// 同一份配置生成两次，内容必须逐字节相同（否则 git diff 全是噪音）。
func TestGenerationIsDeterministic(t *testing.T) {
	build := func() []k8s.File {
		b := newBuilder(t)
		b.component(withDatabase(simple("people/basic", "1.0.0", 8080)), config.Component{})
		b.component(simple("department/tree", "1.0.0", 8080), config.Component{})
		b.resource(pgResource(config.Binding{ComponentID: "people/basic", Database: "people"}))
		return b.generate().Files
	}

	assert.Equal(t, build(), build())
}

// 生成的文件要写清楚"谁生成的、别手改"。
func TestFilesHaveHeaderComment(t *testing.T) {
	b := newBuilder(t)
	b.component(simple("people/basic", "1.0.0", 8080), config.Component{})

	text := string(b.file("deployments/people-basic-1-0-0.yaml").YAML)

	assert.Contains(t, text, "由 BrickKit CLI 自动生成")
	assert.Contains(t, text, "请勿手动编辑")
	assert.Contains(t, text, "2026-08-15T10:00:00Z")
}
