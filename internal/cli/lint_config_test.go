package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/brickkit/brickkit/internal/clierr"
)

// schemaComp 是带 configSchema 的组件：DB_HOST 必填且没有默认值。
func schemaComp(id string) string {
	return "apiVersion: brickkit/v1\nkind: Component\nmetadata:\n  id: " + id + "\n  name: x\n  version: 1.0.0\n  description: x\n" +
		"deployment:\n  type: container\n  image: registry.example.com/x:1.0.0\n  port: 8080\n" +
		"healthCheck:\n  type: http\n  path: /healthz\n" +
		"configSchema:\n  type: object\n  properties:\n    DB_HOST:\n      type: string\n    TOKEN:\n      type: string\n    CERT:\n      type: string\n  required: [DB_HOST]\n"
}

// lintConfigProject：本地源 components/ 里的 erp/api 已加进项目；config 由调用方写。
func lintConfigProject(t *testing.T, config string, extra map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	files := map[string]string{
		"brickkit.yaml":                     "project: shop\nsources:\n  - name: local-dev\n    type: local\n    path: ./components\ncomponents:\n  - id: erp/api\n    version: 1.0.0\n",
		"deploy.yaml":                       "target: docker\ncomponents:\n  - id: erp/api\n",
		"components/erp/api/component.yaml": schemaComp("erp/api"),
	}
	if config != "" {
		files["config/erp-api.yaml"] = config
	}
	for k, v := range extra {
		files[k] = v
	}
	writeTree(t, dir, files)
	return dir
}

func TestLintRequiredValueMissing(t *testing.T) {
	dir := lintConfigProject(t, "DB_HOST: \"\"\n", nil)
	r := runIn(t, dir, "lint")
	assert.Equal(t, clierr.ExitError, r.code)
	assert.Contains(t, r.stdout, "erp/api@1.0.0")
	assert.Contains(t, r.stdout, "DB_HOST")

	dir = lintConfigProject(t, "DB_HOST: db.local\n", nil)
	r = runIn(t, dir, "lint")
	assert.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)
}

// 缓存与本地源里都没有 Manifest 的组件：离线查不了，说一声，不算错。
func TestLintNotesUncachedManifests(t *testing.T) {
	dir := lintConfigProject(t, "DB_HOST: db.local\n", map[string]string{
		"brickkit.yaml": "project: shop\nsources:\n  - name: local-dev\n    type: local\n    path: ./components\ncomponents:\n  - id: erp/api\n    version: 1.0.0\n  - id: crm/web\n    version: 2.0.0\n",
		"deploy.yaml":   "target: docker\ncomponents:\n  - id: erp/api\n  - id: crm/web\n",
	})
	r := runIn(t, dir, "lint")
	assert.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)
	assert.Contains(t, r.stdout, "crm/web@2.0.0")
	assert.Contains(t, r.stdout, "not checked")
}

// 缓存里的 Manifest 同样拿来查（git / market 组件 add 之后都在那里）。
func TestLintUsesCachedManifest(t *testing.T) {
	dir := lintConfigProject(t, "", map[string]string{
		"brickkit.yaml": "project: shop\ncomponents:\n  - id: crm/web\n    version: 2.0.0\n",
		"deploy.yaml":   "target: docker\ncomponents:\n  - id: crm/web\n",
		".brickkit/manifests/crm/web/2.0.0/component.yaml": schemaComp("crm/web"),
	})
	require.NoError(t, os.RemoveAll(filepath.Join(dir, "components")))
	r := runIn(t, dir, "lint")
	assert.Equal(t, clierr.ExitError, r.code)
	assert.Contains(t, r.stdout, "crm/web@2.0.0")
	assert.Contains(t, r.stdout, "DB_HOST")
}

// 本地模式关着，deploy.local.yaml 也迟早会被用上：一样要与 brickkit.yaml 一致。
func TestLintChecksLocalFileWhileSwitchOff(t *testing.T) {
	dir := lintConfigProject(t, "DB_HOST: db.local\n", map[string]string{
		"deploy.local.yaml": "target: docker\ncomponents: []\n",
	})
	r := runIn(t, dir, "lint")
	assert.Equal(t, clierr.ExitError, r.code)
	assert.Contains(t, r.stdout, "deploy.local.yaml")
	assert.Contains(t, r.stdout, "erp/api")

	writeTree(t, dir, map[string]string{"deploy.local.yaml": "target: docker\ncomponents:\n  - id: erp/api\n    mode: debug\n"})
	r = runIn(t, dir, "lint")
	assert.Equal(t, clierr.ExitOK, r.code, "本地文件里的 mode: debug 是合法的：%s", r.stdout+r.stderr)
}

// --strict 才查 ${VAR} 与 file:// 是否取得到：平常它们多半在 CI / 部署机上才有。
func TestLintStrictEnvAndFileReferences(t *testing.T) {
	config := "DB_HOST: db.local\nTOKEN: ${BRICKKIT_LINT_TEST_UNSET_VAR}\nCERT: file://.secrets/cert.pem\n"
	dir := lintConfigProject(t, config, nil)

	r := runIn(t, dir, "lint")
	assert.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)
	assert.NotContains(t, r.stdout, "BRICKKIT_LINT_TEST_UNSET_VAR")

	r = runIn(t, dir, "lint", "--strict")
	assert.Equal(t, clierr.ExitError, r.code)
	assert.Contains(t, r.stdout, "BRICKKIT_LINT_TEST_UNSET_VAR")
	assert.Contains(t, r.stdout, ".secrets/cert.pem")

	writeTree(t, dir, map[string]string{
		".env":              "BRICKKIT_LINT_TEST_UNSET_VAR=abc\n",
		".secrets/cert.pem": "pem",
	})
	r = runIn(t, dir, "lint", "--strict")
	assert.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)
}

// 外壳成员的值要 JSON 编码进外壳：非法 UTF-8 编不进去，lint 就拦下。
func TestLintMemberInvalidUTF8(t *testing.T) {
	shell := "apiVersion: brickkit/v1\nkind: Component\nmetadata:\n  id: erp/shell\n  name: x\n  version: 1.0.0\n  description: x\n" +
		"deployment:\n  type: container\n  image: registry.example.com/s:1.0.0\n  port: 8000\nhealthCheck:\n  type: http\n  path: /healthz\n" +
		"shell:\n  members:\n    - erp/api@1.0.0\n"
	dir := lintConfigProject(t, "DB_HOST: file://bad.bin\n", map[string]string{
		"brickkit.yaml":                       "project: shop\nsources:\n  - name: local-dev\n    type: local\n    path: ./components\ncomponents:\n  - id: erp/api\n    version: 1.0.0\n  - id: erp/shell\n    version: 1.0.0\n    kind: shell\n",
		"deploy.yaml":                         "target: docker\ncomponents:\n  - id: erp/shell\n    members:\n      - id: erp/api\n",
		"components/erp/shell/component.yaml": shell,
		"bad.bin":                             "\xff\xfe",
	})
	r := runIn(t, dir, "lint")
	assert.Equal(t, clierr.ExitError, r.code)
	assert.Contains(t, r.stdout, "UTF-8")

	writeTree(t, dir, map[string]string{"bad.bin": "fine"})
	r = runIn(t, dir, "lint")
	assert.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)
}

func TestLintDuplicateKeysStillReported(t *testing.T) {
	dir := lintConfigProject(t, "DB_HOST: a\nDB_HOST: b\n", nil)
	r := runIn(t, dir, "lint")
	assert.Equal(t, clierr.ExitError, r.code)
	assert.Contains(t, r.stdout, "DB_HOST")
}

// 写了 configSchema 里没有的键：up 会警告"不会生效"，lint 一样说。
func TestLintUnknownConfigKeyWarns(t *testing.T) {
	dir := lintConfigProject(t, "DB_HOST: a\nDB_HOTS: b\n", nil)
	r := runIn(t, dir, "lint")
	assert.Equal(t, clierr.ExitOK, r.code)
	assert.Contains(t, r.stdout, "DB_HOTS")
	r = runIn(t, dir, "lint", "--strict")
	assert.Equal(t, clierr.ExitError, r.code)
}

// 本地模式开着时读的是 deploy.local.yaml；团队的 deploy.yaml 也不能因此没人查。
func TestLintChecksTeamFileWhileLocalModeOn(t *testing.T) {
	dir := lintConfigProject(t, "DB_HOST: db.local\n", map[string]string{
		"deploy.local.yaml":    "target: docker\ncomponents:\n  - id: erp/api\n",
		"deploy.yaml":          "target: docker\ncomponents: []\n",
		".brickkit/local-mode": "on\n",
	})
	r := runIn(t, dir, "lint")
	assert.Equal(t, clierr.ExitError, r.code)
	assert.Contains(t, r.stdout, "deploy.yaml does not match the components in brickkit.yaml")

	r = runIn(t, dir, "lint", "-f", "deploy.local.yaml")
	assert.NotContains(t, r.stdout, "deploy.yaml does not match", "-f 只查那一份")
}

// 本地源提供的组件，up 从不读缓存里的旧 Manifest：lint 也必须按本地目录里的现状查。
func TestLintPrefersLocalSourceOverStaleCache(t *testing.T) {
	stale := strings.Replace(schemaComp("erp/api"), "  required: [DB_HOST]\n", "", 1)
	dir := lintConfigProject(t, "", map[string]string{
		".brickkit/manifests/erp/api/1.0.0/component.yaml": stale,
	})
	r := runIn(t, dir, "lint")
	assert.Equal(t, clierr.ExitError, r.code, "本地 component.yaml 里 DB_HOST 是必填：%s", r.stdout)
	assert.Contains(t, r.stdout, "DB_HOST")
}

// 缓存里的 Manifest 读不懂：说清是解析失败，不说"还没取回来"。
func TestLintNamesUnparsableCachedManifest(t *testing.T) {
	dir := lintConfigProject(t, "", map[string]string{
		"brickkit.yaml": "project: shop\ncomponents:\n  - id: crm/web\n    version: 2.0.0\n",
		"deploy.yaml":   "target: docker\ncomponents:\n  - id: crm/web\n",
		".brickkit/manifests/crm/web/2.0.0/component.yaml": "metadata: [broken\n",
	})
	r := runIn(t, dir, "lint")
	assert.Contains(t, r.stdout, "crm/web@2.0.0")
	assert.NotContains(t, r.stdout, "no manifest on disk yet")
	assert.Contains(t, r.stdout, "could not be read")
}

// 组件仓库兼作工作台：有了 brickkit.yaml 之后，lint 照样检查它要发布的 component.yaml。
func TestLintWorkbenchStillChecksOwnManifest(t *testing.T) {
	dir := t.TempDir()
	writeTree(t, dir, map[string]string{
		"component.yaml": schemaComp("erp/api") + "tpyo: 1\n",
		"brickkit.yaml":  "project: erp-api\ncomponents: []\n",
		"deploy.yaml":    "target: docker\ncomponents: []\n",
	})
	r := runIn(t, dir, "lint")
	assert.Equal(t, clierr.ExitError, r.code)
	assert.Contains(t, r.stdout, "tpyo")
}

// brickkit.yaml 的 kind: shell 由 lint 核对与 component.yaml 一致——离线就能查，
// 与配置检查同一条路：只看盘上有 Manifest 的组件。
func TestLintChecksShellKindAgainstManifest(t *testing.T) {
	dir := lintConfigProject(t, "DB_HOST: db.local\n", map[string]string{
		"brickkit.yaml": "project: shop\nsources:\n  - name: local-dev\n    type: local\n    path: ./components\ncomponents:\n  - id: erp/api\n    version: 1.0.0\n    kind: shell\n",
	})
	r := runIn(t, dir, "lint")
	assert.Equal(t, clierr.ExitError, r.code, r.stdout+r.stderr)
	assert.Contains(t, r.stdout+r.stderr, "erp/api@1.0.0")
	assert.Contains(t, r.stdout+r.stderr, "kind: shell")
}
