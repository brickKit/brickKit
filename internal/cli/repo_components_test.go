package cli

// 本文件把仓库自带的测试组件（tests/components/，镜像由 make demo-images 构建）按使用者的做法
// 装进一个三层项目：init、放进 components/、add --local、在 config/ 里填上必填项、
// up --dry-run（Docker 与 K8s 各一次）。
//
// 这些组件是平台真实装配的夹具。模型变了而它们没跟上时，单看每个组件自己的测试是绿的——
// 那些测试只管组件的代码——真装起来才发现 component.yaml 已经不合法、或者代码读的
// 变量名与声明的配置项对不上。这条测试守的就是"它们至今还装得起来"。

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/manifest"
)

// repoComponentsDir 是仓库里的测试组件（本包在 internal/cli/ 下）。
const repoComponentsDir = "../../tests/components"

// copyRepoComponents 把每个测试组件按 <scope>/<name> 放进项目的 components/，返回组件 ID。
func copyRepoComponents(t *testing.T, project string) []string {
	t.Helper()
	entries, err := os.ReadDir(repoComponentsDir)
	require.NoError(t, err)
	var ids []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		src := filepath.Join(repoComponentsDir, e.Name())
		m, err := manifest.ParseFile(filepath.Join(src, manifest.FileName))
		require.NoError(t, err, "tests/components/%s 的 component.yaml 不合法", e.Name())
		ids = append(ids, m.Metadata.ID)
		dst := filepath.Join(project, "components", filepath.FromSlash(m.Metadata.ID))
		require.NoError(t, filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			rel, _ := filepath.Rel(src, path)
			target := filepath.Join(dst, rel)
			if d.IsDir() {
				return os.MkdirAll(target, 0o755)
			}
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			return os.WriteFile(target, data, 0o644)
		}))
	}
	require.GreaterOrEqual(t, len(ids), 10, "自检：测试组件一个都没找到")
	return ids
}

func TestRepositoryComponentsAssembleIntoAProject(t *testing.T) {
	clearAmbientEnvForTest(t, "POSTGRES_PASSWORD")
	clearAmbientEnvForTest(t, "JWT_SECRET")
	parent := t.TempDir()
	r := runIn(t, parent, "init", "demo-shop", "--no-skills")
	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)
	dir := filepath.Join(parent, "demo-shop")
	ids := copyRepoComponents(t, dir)

	r = runIn(t, dir, "add", "--local", "--yes")
	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)

	// 使用者该做的那一步：给要连数据库、Redis 的组件填上连接。公共部分放 vars.yaml
	writeTree(t, dir, map[string]string{
		".env": "POSTGRES_PASSWORD=pg-secret\nJWT_SECRET=0123456789abcdef0123456789abcdef\n",
		"config/vars.yaml": "PG_HOST: postgres\nPG_USER: postgres\nPG_PASSWORD: ${POSTGRES_PASSWORD}\nREDIS_HOST: redis\n",
	})
	database := func(name string) string {
		return "DATABASE_HOST: $var:PG_HOST\nDATABASE_NAME: " + name +
			"\nDATABASE_USER: $var:PG_USER\nDATABASE_PASSWORD: $var:PG_PASSWORD\n"
	}
	writeTree(t, dir, map[string]string{
		"config/department-tree.yaml":       database("brickkit_department"),
		"config/people-basic.yaml":          database("brickkit_people"),
		"config/authorization-rbac.yaml":    database("brickkit_rbac") + "REDIS_HOST: $var:REDIS_HOST\n",
		"config/auth-password-login.yaml":   database("brickkit_auth") + "JWT_SECRET: ${JWT_SECRET}\n",
		"config/infra-redis-event-bus.yaml": "REDIS_HOST: $var:REDIS_HOST\n",
	})

	services := make([]string, 0, len(ids))
	for _, id := range ids {
		services = append(services, strings.NewReplacer("/", "-", ".", "-").Replace(id)+"-1-0-0")
	}

	t.Run("docker", func(t *testing.T) {
		r := runWithEngine(t, newFakeEngine(), dir, "up", "--dry-run")
		require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)
		compose := readFile(t, filepath.Join(dir, ".brickkit", "generated", composeFileName))
		for _, s := range services {
			assert.Contains(t, compose, "\n  "+s+":", "每个测试组件都生成了自己的 service")
		}
		assert.Contains(t, compose, "PEOPLE_BASIC_GRPC_ENDPOINT", "people/basic 的 extraPorts 注入给 erp/backend")
		assert.NotContains(t, compose, "pg-secret", "口令不以明文进 compose.yaml")
	})

	t.Run("k8s", func(t *testing.T) {
		deploy := filepath.Join(dir, "deploy.yaml")
		replaceInFile(t, deploy, "target: docker", "target: k8s")
		r := runWithEngine(t, newFakeEngine(), dir, "up", "--dry-run")
		require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)
		generated := filepath.Join(dir, ".brickkit", "generated", "k8s")
		var all strings.Builder
		require.NoError(t, filepath.WalkDir(generated, func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return err
			}
			content := readFile(t, path)
			all.WriteString(content)
			if strings.Contains(content, "kind: Deployment") {
				assert.NotContains(t, content, "pg-secret", "%s：口令只进生成的 Secret，Deployment 里只有 secretKeyRef", path)
			}
			return nil
		}))
		for _, s := range services {
			assert.Contains(t, all.String(), "name: "+s, "每个测试组件都有自己的 Deployment")
		}
		assert.Contains(t, all.String(), "secretKeyRef", "自检：确实生成了密钥引用")
	})
}
