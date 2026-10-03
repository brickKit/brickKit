package k8s_test

// 这些用例原来借资源（resources）搭夹具；资源废除后改用组件自己的密钥配置项
// （withDatabase 现在给组件加一个 secret: true 的 DB_PASSWORD），守的是同一批行为。

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"

	"github.com/brickkit/brickkit/internal/deployfile"
	"github.com/brickkit/brickkit/internal/k8s"
	"github.com/brickkit/brickkit/internal/manifest"
	"github.com/brickkit/brickkit/internal/project/projecttest"
)

func fullFeatured(t *testing.T) *k8s.Result {
	t.Helper()

	portal := withDatabase(simple("portal/web", "1.0.0", 8080))
	portal.Migration = &manifest.Migration{Command: []string{"/app/portal", "migrate"}}

	b := newBuilder(t)
	b.spec.K8s.NetworkPolicy = &deployfile.NetworkPolicy{
		Enabled:           true,
		IngressController: &deployfile.IngressControllerSource{Namespace: "ingress-nginx"},
	}
	b.spec.K8s.ServiceAccount = &deployfile.ServiceAccount{Enabled: true}

	return b.
		component(portal, projecttest.Entry{
			Expose: true, Hostname: "portal.example.com", Replicas: projecttest.IntPtr(3),
		}).
		generate()
}

func refsInFiles(t *testing.T, result *k8s.Result) []string {
	t.Helper()

	var out []string
	for _, file := range result.Files {
		decoder := yaml.NewDecoder(strings.NewReader(string(file.YAML)))
		for {
			var doc map[string]any
			if err := decoder.Decode(&doc); err != nil {
				break
			}
			if doc == nil {
				continue
			}
			kind, _ := doc["kind"].(string)
			metadata, _ := doc["metadata"].(map[string]any)
			name, _ := metadata["name"].(string)
			require.NotEmpty(t, kind, "每份清单都必须有 kind：%s", file.Path)
			require.NotEmpty(t, name, "每份清单都必须有 metadata.name：%s", file.Path)
			out = append(out, strings.ToLower(kind)+"/"+name)
		}
	}
	return out
}

func TestDirectoryLayout(t *testing.T) {
	b := newBuilder(t)
	b.component(migrating(withDatabase(simple("people/basic", "1.0.0", 8080))), projecttest.Entry{})
	b.component(simple("portal/user-frontend", "1.0.0", 80),
		projecttest.Entry{Expose: true, Hostname: "portal.example.com"})

	assert.Equal(t, []string{
		"deployments/people-basic-1-0-0.yaml",
		"deployments/portal-user-frontend-1-0-0.yaml",
		"ingress/portal-user-frontend.yaml",
		"migrations/people-basic-1-0-0-migration.yaml",
		"namespace.yaml",
		"secrets/config-secrets.yaml",
		"services/people-basic-1-0-0.yaml",
		"services/portal-user-frontend-1-0-0.yaml",
	}, pathsOf(b.generate()), "16.13 目录结构")
}

func TestSecretFileIsNotWorldReadable(t *testing.T) {
	b := newBuilder(t)
	b.component(withDatabase(simple("people/basic", "1.0.0", 8080)), projecttest.Entry{})

	dir := filepath.Join(t.TempDir(), "k8s")
	require.NoError(t, k8s.WriteFiles(dir, b.generate().Files))

	info, err := os.Stat(filepath.Join(dir, "secrets", "config-secrets.yaml"))
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm(), "Secret 文件只有自己能读")

	info, err = os.Stat(filepath.Join(dir, "deployments", "people-basic-1-0-0.yaml"))
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o644), info.Mode().Perm(), "其余清单照常")
}

func TestMigrationJobsAreGroupedByComponent(t *testing.T) {
	b := newBuilder(t)
	b.component(migrating(simple("demo/hello", "2.0.0", 8080)), projecttest.Entry{})
	b.component(migrating(simple("demo/hello", "10.0.0", 8080)), projecttest.Entry{})
	b.component(migrating(simple("people/basic", "1.0.0", 8080)), projecttest.Entry{})

	groups := b.generate().MigrationGroups

	require.Len(t, groups, 2, "两个组件 ID → 两组：%v", groups)
	assert.Equal(t, [][]string{
		// 按版本号排，不是服务名的字典序：10.0.0 必须排在 2.0.0 后面
		{"demo-hello-2-0-0-migration", "demo-hello-10-0-0-migration"},
		{"people-basic-1-0-0-migration"},
	}, groups, "组内按版本升序；组间按组件 ID 字典序")
}

func TestLabelsNotOnServiceIngressOrJob(t *testing.T) {
	m := simple("erp/sales", "1.0.0", 8080)
	m.Migration = &manifest.Migration{Command: []string{"./migrate"}}

	b := newBuilder(t)
	b.component(m, projecttest.Entry{
		Expose: true, Hostname: "sales.example.com",
		Labels: map[string]string{"prometheus.io/scrape": "true"},
	})

	for _, path := range []string{
		"services/erp-sales-1-0-0.yaml",
		"ingress/erp-sales.yaml",
		"migrations/erp-sales-1-0-0-migration.yaml",
	} {
		annotations, ok := dig(t, b.doc(path), "metadata", "annotations").(map[string]any)
		require.True(t, ok, "%s 的 annotations 段读不出来", path)
		assert.NotContains(t, annotations, "prometheus.io/scrape", path)
	}
}

// 明文变量里的 ${VAR} 同样在生成时展开：kubectl 不做任何变量替换。
func TestPlainValuesAreExpandedToo(t *testing.T) {
	m := simple("people/basic", "1.0.0", 8080)
	m.ConfigSchema = &manifest.ConfigSchema{Properties: map[string]manifest.ConfigProperty{
		"DB_HOST": {Type: "string"},
	}}
	b := newBuilder(t)
	b.component(m, projecttest.Entry{Config: map[string]any{"DB_HOST": "${PG_HOST}"}})
	b.env["PG_HOST"] = "pg.prod.svc"

	env := envOf(t, b.container("people-basic-1-0-0"))

	assert.Equal(t, "pg.prod.svc", env["DB_HOST"])
}
