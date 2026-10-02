// 本文件是「K8s Namespace / Deployment / Secret 生成」的业务行为测试。
//
// 与 compose 那边同样的取舍：断言落在**最终 YAML 里有什么**，不看内部结构。
// 这些文件最终要交给 kubectl，写错一个字段名 K8s 只会沉默地忽略它。
package k8s_test

import (
	"context"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"

	"github.com/brickkit/brickkit/internal/cascade"
	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/deployfile"
	"github.com/brickkit/brickkit/internal/inject"
	"github.com/brickkit/brickkit/internal/k8s"
	"github.com/brickkit/brickkit/internal/manifest"
	"github.com/brickkit/brickkit/internal/project"
	"github.com/brickkit/brickkit/internal/project/projecttest"
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
	spec     projecttest.Spec
	env      map[string]string
	proj     *project.Project
	// deployFile 不空时，模拟 -f 选了另一份部署文件（项目根目录下的文件名）。
	deployFile string
}

func newBuilder(t *testing.T) *builder {
	return &builder{
		t:        t,
		provider: stubProvider{},
		spec: projecttest.Spec{Project: "my-erp", Target: deployfile.TargetK8s,
			K8s: &deployfile.K8s{}, Files: projecttest.Files{}},
		env: map[string]string{"POSTGRES_PASSWORD": "s3cr3t"},
	}
}

func (b *builder) component(m *manifest.Manifest, entry projecttest.Entry) *builder {
	b.provider[m.Metadata.ID+"@"+m.Metadata.Version] = m
	entry.ID, entry.Version = m.Metadata.ID, m.Metadata.Version
	b.spec.Entries = append(b.spec.Entries, entry)
	b.roots = append(b.roots, resolver.Ref{ID: entry.ID, Version: entry.Version})
	return b
}

// build 跑完整条链路：解析 → 级联 → 注入 → 生成 K8s 清单，允许失败。
func (b *builder) build() (*k8s.Result, error) {
	b.t.Helper()

	b.proj = projecttest.Build(b.t, b.spec)
	if b.deployFile != "" {
		b.proj.DeployPath = filepath.Join(b.proj.Layout.Root, b.deployFile)
	}
	projecttest.FillShellCapability(b.spec, b.provider)
	graph, err := resolver.New(b.provider).Resolve(context.Background(), b.roots...)
	require.NoError(b.t, err)

	states, err := cascade.Compute(b.proj, graph)
	require.NoError(b.t, err)

	env, err := inject.Build(b.proj, graph, states)
	require.NoError(b.t, err)

	return k8s.Generate(b.proj, graph, states, env, k8s.Options{
		Root: b.proj.Layout.Root,
		Now:  func() time.Time { return time.Date(2026, 8, 15, 10, 0, 0, 0, time.UTC) },
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

// withDatabase 给组件加一个密钥配置项 DB_PASSWORD（默认值 s3cr3t）——
// 资源废除之后，"连库要一个密码"就是组件自己 configSchema 里的一项。
func withDatabase(m *manifest.Manifest) *manifest.Manifest {
	if m.ConfigSchema == nil {
		m.ConfigSchema = &manifest.ConfigSchema{Properties: map[string]manifest.ConfigProperty{}}
	}
	m.ConfigSchema.Properties["DB_PASSWORD"] = manifest.ConfigProperty{
		Type: "string", Secret: true, Default: "s3cr3t",
	}
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

// ============================================================
// Namespace
// ============================================================

func TestNamespaceGenerated(t *testing.T) {
	b := newBuilder(t)
	b.component(simple("people/basic", "1.0.0", 8080), projecttest.Entry{})

	doc := b.doc("namespace.yaml")

	assert.Equal(t, "v1", doc["apiVersion"], "16.1")
	assert.Equal(t, "Namespace", doc["kind"], "16.1")
	assert.Equal(t, "brickkit-my-erp", dig(t, doc, "metadata", "name"), "16.1 名称是 brickkit-<项目名>")
	assert.Equal(t, "my-erp", dig(t, doc, "metadata", "labels", "brickkit.io/project"), "16.1 labels")
}

// 命名空间名要能回填到 Result 里：后续 kubectl 的每条命令都要 -n 它。
func TestResultCarriesNamespace(t *testing.T) {
	b := newBuilder(t)
	b.component(simple("people/basic", "1.0.0", 8080), projecttest.Entry{})

	assert.Equal(t, "brickkit-my-erp", b.generate().Namespace)
}

// ============================================================
// Deployment
// ============================================================

func TestDeploymentBasics(t *testing.T) {
	b := newBuilder(t)
	b.component(simple("people/basic", "1.0.0", 8080), projecttest.Entry{})

	doc := b.doc("deployments/people-basic-1-0-0.yaml")

	assert.Equal(t, "apps/v1", doc["apiVersion"], "16.2")
	assert.Equal(t, "Deployment", doc["kind"], "16.2")
	assert.Equal(t, "people-basic-1-0-0", dig(t, doc, "metadata", "name"), "名字是版本化服务名")
	assert.Equal(t, "brickkit-my-erp", dig(t, doc, "metadata", "namespace"))
	assert.Equal(t, 1, dig(t, doc, "spec", "replicas"), "没写 replicas 时的默认值")
	assert.Equal(t, "people-basic-1-0-0",
		dig(t, doc, "spec", "selector", "matchLabels", "app"), "selector 必须选得中自己的 Pod")
	assert.Equal(t, "people-basic-1-0-0",
		dig(t, doc, "spec", "template", "metadata", "labels", "app"))
}

func TestDeploymentLabels(t *testing.T) {
	b := newBuilder(t)
	b.component(simple("people/basic", "1.0.0", 8080), projecttest.Entry{})

	doc := b.doc("deployments/people-basic-1-0-0.yaml")

	assert.Equal(t, map[string]any{
		"app":                           "people-basic-1-0-0",
		"brickkit.io/component":         "people-basic",
		"brickkit.io/component-version": "1.0.0",
		"brickkit.io/project":           "my-erp",
	}, dig(t, doc, "metadata", "labels"), "16.2 labels")
	assert.Equal(t, "people/basic",
		dig(t, doc, "metadata", "annotations", "brickkit.io/component-id"),
		"原样的组件 ID 放注解里——标签值放不下带斜杠的写法")
}

// 标签值里绝不能出现斜杠。
//
// K8s 的标签**值**只允许字母数字与 - _ .（斜杠只在标签**键**的前缀里合法）。
// 早先设计里的样例写的是 `brickkit.io/component-id: people/basic`，
// 那份 Deployment 会被 API Server 整份拒绝——错误信息还只提"a valid label must…"，
// 完全看不出是组件 ID 的锅。
func TestLabelValuesAreValid(t *testing.T) {
	b := newBuilder(t)
	b.component(simple("people/basic", "1.0.0", 8080), projecttest.Entry{})

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
	b.component(simple("people/basic", "1.0.0", 8080), projecttest.Entry{})

	c := b.container("people-basic-1-0-0")

	assert.Equal(t, "people-basic", c["name"], "16.2 容器名是组件 ID（不带版本）")
	assert.Equal(t, "registry.example.com/people-basic:1.0.0", c["image"], "16.2 image")
	assert.Equal(t, []any{map[string]any{"name": "http", "containerPort": 8080}},
		c["ports"], "16.2 主端口")
}

// 环境变量与 compose 那边同源（inject），K8s 只是换个写法。
func TestDeploymentEnv(t *testing.T) {
	b := newBuilder(t)
	b.component(simple("department/tree", "1.0.0", 8080), projecttest.Entry{})
	b.component(dependsOn(simple("people/basic", "1.0.0", 8080), "department/tree", "1.0.0"),
		projecttest.Entry{})

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
	m := simple("people/basic", "1.0.0", 8080)
	m.ConfigSchema = &manifest.ConfigSchema{Properties: map[string]manifest.ConfigProperty{
		"DB_PORT": {Type: "integer", Default: 5432},
	}}
	b.component(m, projecttest.Entry{})

	env := envOf(t, b.container("people-basic-1-0-0"))

	assert.Equal(t, "5432", env["DB_PORT"], "端口是数字，但在 env 里必须写成字符串")
}

// ============================================================
// 探针
// ============================================================

func TestLivenessProbe(t *testing.T) {
	b := newBuilder(t)
	b.component(simple("people/basic", "1.0.0", 8080), projecttest.Entry{})

	probe := b.container("people-basic-1-0-0")["livenessProbe"]

	assert.Equal(t, "/healthz", dig(t, probe, "httpGet", "path"), "16.9")
	assert.Equal(t, 8080, dig(t, probe, "httpGet", "port"), "16.9 探主端口")
	assert.Equal(t, 10, dig(t, probe, "initialDelaySeconds"), "16.9")
	assert.Equal(t, 10, dig(t, probe, "periodSeconds"))
	assert.Equal(t, 3, dig(t, probe, "timeoutSeconds"))
	assert.Equal(t, 3, dig(t, probe, "failureThreshold"))
}

func TestReadinessProbe(t *testing.T) {
	b := newBuilder(t)
	b.component(simple("people/basic", "1.0.0", 8080), projecttest.Entry{})

	probe := b.container("people-basic-1-0-0")["readinessProbe"]

	assert.Equal(t, "/healthz", dig(t, probe, "httpGet", "path"), "16.10")
	assert.Equal(t, 8080, dig(t, probe, "httpGet", "port"), "16.10")
	assert.Equal(t, 5, dig(t, probe, "initialDelaySeconds"), "16.10 就绪探针比存活探针早")
	assert.Equal(t, 5, dig(t, probe, "periodSeconds"))
}

// 启动探针。
//
// 没有它时，冷启动 45 秒的组件会在 t≈30s 被 livenessProbe 判死
// （initialDelay 10 + period 10 × failureThreshold 3）→ kill → 重启 →
// 再走一遍同样的 30 秒 → 永久 CrashLoopBackOff。
func TestStartupProbe(t *testing.T) {
	b := newBuilder(t)
	b.component(simple("people/basic", "1.0.0", 8080), projecttest.Entry{})

	probe := b.container("people-basic-1-0-0")["startupProbe"]

	assert.Equal(t, "/healthz", dig(t, probe, "httpGet", "path"))
	assert.Equal(t, 8080, dig(t, probe, "httpGet", "port"))
	assert.Equal(t, 5, dig(t, probe, "periodSeconds"))
	// 默认宽限期 60 秒 ÷ 5 秒一次 = 12 次
	assert.Equal(t, 12, dig(t, probe, "failureThreshold"))
	assert.NotContains(t, probe, "initialDelaySeconds",
		"启动探针不该有初始延迟：它要尽早发现组件已经起来了")
}

// 组件声明的宽限期换算成失败次数，除不尽时向上取整——
// 宁可多给几秒，也不要比组件声明的少。
func TestStartupProbeFailureThresholdFollowsManifest(t *testing.T) {
	for _, tc := range []struct {
		seconds   int
		threshold int
	}{
		{seconds: 180, threshold: 36},
		{seconds: 300, threshold: 60},
		{seconds: 7, threshold: 2}, // 7 ÷ 5 = 1.4 → 2（给 10 秒，不是 5 秒）
		{seconds: 1, threshold: 1},
	} {
		m := simple("java/monolith", "1.0.0", 8080)
		m.HealthCheck.StartPeriodSeconds = tc.seconds

		b := newBuilder(t)
		b.component(m, projecttest.Entry{})

		probe := b.container("java-monolith-1-0-0")["startupProbe"]
		assert.Equal(t, tc.threshold, dig(t, probe, "failureThreshold"),
			"startPeriodSeconds: %d", tc.seconds)
	}
}

// healthCheck.type: tcp → tcpSocket 探针。
func TestTCPProbe(t *testing.T) {
	m := simple("infra/queue", "1.0.0", 5672)
	m.HealthCheck = manifest.HealthCheck{Type: manifest.HealthCheckTCP}

	b := newBuilder(t)
	b.component(m, projecttest.Entry{})

	c := b.container("infra-queue-1-0-0")

	assert.Equal(t, 5672, dig(t, c["livenessProbe"], "tcpSocket", "port"))
	assert.Equal(t, 5672, dig(t, c["readinessProbe"], "tcpSocket", "port"))
	assert.Equal(t, 5672, dig(t, c["startupProbe"], "tcpSocket", "port"))
}

// healthCheck.type: none → 一个探针都不生成。
//
// 生成一个探不通的探针，K8s 会反复 kill 掉一个其实健康的 Pod。
func TestNoProbeWhenHealthCheckNone(t *testing.T) {
	m := simple("infra/job", "1.0.0", 8080)
	m.HealthCheck = manifest.HealthCheck{Type: manifest.HealthCheckNone}

	b := newBuilder(t)
	b.component(m, projecttest.Entry{})

	c := b.container("infra-job-1-0-0")

	assert.NotContains(t, c, "livenessProbe")
	assert.NotContains(t, c, "readinessProbe")
	assert.NotContains(t, c, "startupProbe")
}

// ============================================================
// 资源配额
// ============================================================

func TestResourcesUseCLIDefaults(t *testing.T) {
	b := newBuilder(t)
	b.component(simple("people/basic", "1.0.0", 8080), projecttest.Entry{})

	resources := b.container("people-basic-1-0-0")["resources"]

	assert.Equal(t, inject.DefaultRequestCPU, dig(t, resources, "requests", "cpu"), "16.11")
	assert.Equal(t, inject.DefaultRequestMemory, dig(t, resources, "requests", "memory"))
	// 没人写上限就不生成 limits：CPU limit 会让组件在节点空闲时也被 CFS 限流，
	// 内存 limit 是平台凭空猜的一个 OOMKill 阈值
	assert.NotContains(t, resources, "limits", "没人写上限就不该有 limits")
}

// K8s 的写法就是 Manifest 的写法（100m / 128Mi），不需要 compose 那样的换算。
func TestResourcesMergedFromConfig(t *testing.T) {
	m := simple("people/basic", "1.0.0", 8080)
	m.Deployment.Resources = &manifest.Resources{
		Requests: &manifest.ResourceSpec{CPU: "200m", Memory: "256Mi"},
		Limits:   &manifest.ResourceSpec{CPU: "1", Memory: "1Gi"},
	}

	b := newBuilder(t)
	b.component(m, projecttest.Entry{
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
// Secret
// ============================================================

func TestSecretReferencedFromEnv(t *testing.T) {
	b := newBuilder(t)
	b.component(withDatabase(simple("people/basic", "1.0.0", 8080)), projecttest.Entry{})

	env := envOf(t, b.container("people-basic-1-0-0"))

	assert.Equal(t, map[string]any{"secretKeyRef": map[string]any{
		"name": "people-basic-1-0-0-config-secret", "key": "DB_PASSWORD",
	}}, env["DB_PASSWORD"], "16.15 密码只能通过 secretKeyRef 引用")
}

// 密码在 Deployment 里绝不能出现明文——那份 YAML 会进 git、进 CI 日志。
func TestPasswordNeverAppearsInDeployment(t *testing.T) {
	b := newBuilder(t)
	b.component(withDatabase(simple("people/basic", "1.0.0", 8080)), projecttest.Entry{})

	text := string(b.file("deployments/people-basic-1-0-0.yaml").YAML)

	assert.NotContains(t, text, "s3cr3t", "16.15 明文密码不能出现在 Deployment 里")
	assert.NotContains(t, text, "${POSTGRES_PASSWORD}", "占位符同样不该出现")
}

// ============================================================
// 声明了 secret: true 的配置项
// ============================================================

func secretConfigManifest() *manifest.Manifest {
	m := simple("acme/hello", "0.1.0", 8080)
	m.ConfigSchema = &manifest.ConfigSchema{Properties: map[string]manifest.ConfigProperty{
		"API_KEY": {Type: "string", Secret: true},
		"REGION":  {Type: "string"},
	}}
	return m
}

// 值进 Secret，Deployment 里只留引用：Secret 与 Deployment 在 K8s 里是分开授权的，
// 密钥明文写进 Deployment，能读 Deployment 的人就都读得到。
func TestSecretConfigGoesThroughSecret(t *testing.T) {
	b := newBuilder(t)
	b.component(secretConfigManifest(), projecttest.Entry{Config: map[string]any{
		"API_KEY": "${THIRD_PARTY_KEY}", "REGION": "eu-west-1",
	}})
	b.env["THIRD_PARTY_KEY"] = "sk-live-SECRET123"

	doc := b.doc("secrets/config-secrets.yaml")
	env := envOf(t, b.container("acme-hello-0-1-0"))
	deployment := string(b.file("deployments/acme-hello-0-1-0.yaml").YAML)

	assert.Equal(t, "acme-hello-0-1-0-config-secret", dig(t, doc, "metadata", "name"),
		"<版本化服务名>-config-secret，与资源 Secret（<资源ID>-secret）永不撞名")
	assert.Equal(t, "sk-live-SECRET123", dig(t, doc, "stringData", "API_KEY"),
		"${VAR} 在生成时求值——kubectl 不做变量替换")
	assert.Equal(t, map[string]any{"secretKeyRef": map[string]any{
		"name": "acme-hello-0-1-0-config-secret", "key": "API_KEY",
	}}, env["API_KEY"])
	assert.Equal(t, "eu-west-1", env["REGION"], "没声明 secret 的配置项照常明文")
	assert.NotContains(t, deployment, "sk-live-SECRET123", "密钥绝不能出现在 Deployment 里")
}

// 没有声明 secret 的配置项时不生成 config-secrets 文件（空文件只会让人以为漏了什么）。
func TestNoConfigSecretFileWithoutDeclaredSecrets(t *testing.T) {
	b := newBuilder(t)
	m := simple("acme/hello", "0.1.0", 8080)
	m.ConfigSchema = &manifest.ConfigSchema{Properties: map[string]manifest.ConfigProperty{
		"REGION": {Type: "string"},
	}}
	b.component(m, projecttest.Entry{Config: map[string]any{"REGION": "eu-west-1"}})

	assert.False(t, hasFile(b.generate(), "secrets/config-secrets.yaml"))
}

// 值文件权限同样是 0600：Secret 目录整个是。
func TestConfigSecretFileIsNotWorldReadable(t *testing.T) {
	b := newBuilder(t)
	b.component(secretConfigManifest(), projecttest.Entry{Config: map[string]any{"API_KEY": "${THIRD_PARTY_KEY}"}})
	b.env["THIRD_PARTY_KEY"] = "sk-live-SECRET123"

	dir := t.TempDir()
	require.NoError(t, k8s.WriteFiles(dir, b.generate().Files))

	info, err := os.Stat(filepath.Join(dir, "secrets", "config-secrets.yaml"))
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())
}

// 变量没定义要报错，与资源密码同一条规则：放过去就是把字面量 "${THIRD_PARTY_KEY}" 当密钥部署上去。
func TestUnresolvedSecretConfigIsAnError(t *testing.T) {
	b := newBuilder(t)
	b.component(secretConfigManifest(), projecttest.Entry{Config: map[string]any{"API_KEY": "${THIRD_PARTY_KEY}"}})

	_, err := b.build()

	require.Error(t, err)
	assert.Equal(t, clierr.CodeConfigInvalid, clierr.As(err).Code)
	assert.Contains(t, err.Error(), "THIRD_PARTY_KEY")
}

// ============================================================
// existingSecret：引用外部已建好的 Secret（K8s 下的 existingSecret 写法）
// ============================================================

// 组件声明的配置密钥写成 existingSecret 形状：secretKeyRef 指向使用者给的名字与 key，
// 不是平台按 <服务名>-config-secret 算出来的那个。
func TestConfigExistingSecretReferencesGivenNameAndKey(t *testing.T) {
	m := secretConfigManifest() // apiKey 声明了 secret: true
	b := newBuilder(t)
	b.component(m, projecttest.Entry{Config: map[string]any{
		"API_KEY": map[string]any{"existingSecret": "acme-hello-vault-synced", "key": "api-key"},
	}})

	env := envOf(t, b.container("acme-hello-0-1-0"))

	assert.Equal(t, map[string]any{"secretKeyRef": map[string]any{
		"name": "acme-hello-vault-synced", "key": "api-key",
	}}, env["API_KEY"])
	assert.False(t, hasFile(b.generate(), "secrets/config-secrets.yaml"),
		"值在外部已建好的 Secret 里，平台没有值可以生成一份自己的 Secret")
}

// ${VAR} 没定义时必须报错。
//
// 放过去的后果是把字面量 "${POSTGRES_PASSWORD}" 当成密码部署上去：
// kubectl 不做变量替换，Pod 会以认证失败反复重启，而 YAML 看上去完全正常。
func TestUnresolvedEnvVarIsAnError(t *testing.T) {
	b := newBuilder(t)
	m := simple("people/basic", "1.0.0", 8080)
	m.ConfigSchema = &manifest.ConfigSchema{Properties: map[string]manifest.ConfigProperty{
		"DB_PASSWORD": {Type: "string", Secret: true},
	}}
	b.component(m, projecttest.Entry{Config: map[string]any{"DB_PASSWORD": "${POSTGRES_PASSWORD}"}})
	delete(b.env, "POSTGRES_PASSWORD")

	_, err := b.build()

	require.Error(t, err)
	assert.Equal(t, clierr.CodeConfigInvalid, clierr.As(err).Code)
	assert.Contains(t, err.Error(), "POSTGRES_PASSWORD", "要说清楚是哪个变量没定义")
}

// ============================================================
// 只生成本次真的会跑的东西
// ============================================================

func TestSkippedComponentsAreNotGenerated(t *testing.T) {
	b := newBuilder(t)
	b.component(simple("people/basic", "1.0.0", 8080), projecttest.Entry{})
	b.component(simple("legacy/thing", "1.0.0", 8080), projecttest.Entry{Mode: deployfile.ModeDisable})

	result := b.generate()

	assert.True(t, hasFile(result, "deployments/people-basic-1-0-0.yaml"))
	assert.False(t, hasFile(result, "deployments/legacy-thing-1-0-0.yaml"),
		"mode: disable 的组件不该出现在清单里")
}

// 同一份配置生成两次，内容必须逐字节相同（否则 git diff 全是噪音）。
func TestGenerationIsDeterministic(t *testing.T) {
	build := func() []k8s.File {
		b := newBuilder(t)
		b.component(withDatabase(simple("people/basic", "1.0.0", 8080)), projecttest.Entry{})
		b.component(simple("department/tree", "1.0.0", 8080), projecttest.Entry{})
		return b.generate().Files
	}

	assert.Equal(t, build(), build())
}

// 生成的文件要写清楚"谁生成的、别手改"。
func TestFilesHaveHeaderComment(t *testing.T) {
	b := newBuilder(t)
	b.component(simple("people/basic", "1.0.0", 8080), projecttest.Entry{})

	text := string(b.file("deployments/people-basic-1-0-0.yaml").YAML)

	assert.Contains(t, text, "Generated automatically by the BrickKit CLI")
	assert.Contains(t, text, "do not edit by hand")
	assert.Contains(t, text, "2026-08-15T10:00:00Z")
}

// Deployment 与迁移 Job 用同一个镜像名（manifest.ImageRef）：不带 tag 的 image 补上组件版本，
// 只有 build 的组件用推出来的 <scope>-<name>:<版本>——从前 Deployment 原样写 image，
// Pod 跑 :latest 而迁移跑 :1.0.0，只有 build 的组件干脆是空镜像名。
func TestDeploymentImageMatchesImageRef(t *testing.T) {
	untagged := simple("erp/api", "1.0.0", 8080)
	untagged.Deployment.Image = "ghcr.io/org/erp-api"
	buildOnly := simple("erp/web", "2.0.0", 8081)
	buildOnly.Deployment.Image = ""
	buildOnly.Deployment.Build = &manifest.Build{}

	b := newBuilder(t)
	b.component(untagged, projecttest.Entry{})
	b.component(buildOnly, projecttest.Entry{})
	assert.Equal(t, "ghcr.io/org/erp-api:1.0.0", b.container("erp-api-1-0-0")["image"])
	assert.Equal(t, "erp-web:2.0.0", b.container("erp-web-2-0-0")["image"])
}

// K8s 上同一个值写进 Pod 的 terminationGracePeriodSeconds；没声明时不写，用集群的默认值。
func TestStopGracePeriodBecomesTerminationGracePeriod(t *testing.T) {
	b := newBuilder(t)
	worker := simple("erp/worker", "1.0.0", 8081)
	worker.Deployment.StopGracePeriodSeconds = 25
	b.component(worker, projecttest.Entry{})
	b.component(simple("erp/api", "1.0.0", 8083), projecttest.Entry{})

	assert.Equal(t, 25, dig(t, b.doc("deployments/erp-worker-1-0-0.yaml"), "spec", "template", "spec", "terminationGracePeriodSeconds"))
	podSpec := dig(t, b.doc("deployments/erp-api-1-0-0.yaml"), "spec", "template", "spec").(map[string]any)
	assert.NotContains(t, podSpec, "terminationGracePeriodSeconds")
}

// 就绪探针探 readinessCheck，存活与启动探针仍探 healthCheck：还没就绪的 Pod 不收流量，但也不会因此被杀。
func TestReadinessProbeUsesReadinessCheck(t *testing.T) {
	b := newBuilder(t)
	api := simple("erp/api", "1.0.0", 8080)
	api.ReadinessCheck = &manifest.ReadinessCheck{Type: manifest.HealthCheckHTTP, Path: "/readyz"}
	b.component(api, projecttest.Entry{})

	container := dig(t, b.doc("deployments/erp-api-1-0-0.yaml"), "spec", "template", "spec", "containers").([]any)[0].(map[string]any)
	assert.Equal(t, "/readyz", dig(t, container, "readinessProbe", "httpGet", "path"))
	assert.Equal(t, "/healthz", dig(t, container, "livenessProbe", "httpGet", "path"))
	assert.Equal(t, "/healthz", dig(t, container, "startupProbe", "httpGet", "path"))
}
