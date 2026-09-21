// 本文件是 Step 12 在命令层的业务行为测试：`brickkit up --dry-run`
// 只生成部署文件、不启动任何东西（004 §3.5）。
//
// Step 12 的交付物是"生成"，`--dry-run` 正好就是这条路径；真正的启动属 Step 15。
package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/brickkit/brickkit/internal/clierr"
)

// generatedCompose 读出生成的 docker-compose.yaml。
func generatedCompose(t *testing.T, dir string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, ".brickkit", "generated", "docker-compose.yaml"))
	require.NoError(t, err, "应生成 .brickkit/generated/docker-compose.yaml")
	return string(data)
}

// composeProject 建一个带资源绑定的两组件项目。
func composeProject(t *testing.T) *projectFixture {
	t.Helper()

	comps := []comp{
		{ID: "erp/backend", Version: "1.0.0", Requires: []string{"people/basic@1.0.0"}},
		{ID: "people/basic", Version: "1.0.0"},
	}
	f := addedProject(t, comps, "erp/backend@1.0.0")
	f.writeConfig(t, `components:
  - id: people/basic
    version: 1.0.0
  - id: erp/backend
    version: 1.0.0
    expose: true
    exposePort: 18080

resources:
  - kind: database
    engine: postgresql
    id: postgres-main
    host: postgres
    port: 5432
    username: brickkit
    password: ${POSTGRES_PASSWORD}
    bindings:
      - componentId: people/basic
        database: brickkit_people
`)
	return f
}

// ============================================================
// 生成
// ============================================================

func TestUpDryRunGeneratesComposeFile(t *testing.T) {
	f := composeProject(t)

	r := runIn(t, f.Dir, "up", "--dry-run")

	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)
	text := generatedCompose(t, f.Dir)

	assert.Contains(t, text, "Generated automatically by the BrickKit CLI")
	assert.Contains(t, text, "people-basic-1-0-0:")
	assert.Contains(t, text, "erp-backend-1-0-0:")
	assert.Contains(t, text, "PEOPLE_BASIC_ENDPOINT=http://people-basic-1-0-0:8080")
	assert.Contains(t, r.stdout, "📄 Generated")
	assert.Contains(t, r.stdout, ".brickkit/generated/docker-compose.yaml")
}

// --dry-run 不能启动任何东西：它的全部意义就是"先看看会生成什么"。
func TestUpDryRunDoesNotStartAnything(t *testing.T) {
	f := composeProject(t)

	r := runIn(t, f.Dir, "up", "--dry-run")

	require.Equal(t, clierr.ExitOK, r.code)
	assert.NotContains(t, r.stdout, "🐳 Starting (")
	assert.Contains(t, r.stdout, "starts no component")
}

// 生成前先展示级联结果与启动顺序：使用者要能看出"这次会跑哪些、为什么"。
func TestUpDryRunShowsStatesAndOrder(t *testing.T) {
	f := composeProject(t)

	r := runIn(t, f.Dir, "up", "--dry-run")

	assert.Contains(t, r.stdout, "📋 Component state calculation:")
	assert.Contains(t, r.stdout, "📋 Start order")
	assert.Contains(t, r.stdout, "1. people-basic-1-0-0")
}

// 006 §9.5：CLI 不建库，但必须告诉使用者要建哪些库、怎么建。
func TestUpDryRunReportsRequiredDatabases(t *testing.T) {
	f := composeProject(t)

	r := runIn(t, f.Dir, "up", "--dry-run")

	require.Equal(t, clierr.ExitOK, r.code, r.stderr)
	assert.Contains(t, r.stdout, "brickkit_people")
	assert.Contains(t, r.stdout, "CREATE DATABASE")
	assert.Contains(t, r.stdout, "people/basic", "要说清是哪个组件用这个库")
}

// 重复执行覆盖同一个文件，且内容一致（生成是确定性的）。
//
// 文件头的生成时间戳精确到秒，两次真跑 time.Now() 之间如果恰好跨过秒的
// 边界，内容就会只在这一行上不一致——这不是生成不确定，是测试自己在跟
// 墙钟赛跑。固定 Options.Now（compose.Options.Now 已经支持注入，见
// internal/compose/compose.go）让两次调用拿到同一个时间戳，测的才是
// "生成本身是不是确定的"，不是"两次调用有没有跨过同一秒"。
func TestUpDryRunIsRepeatable(t *testing.T) {
	f := composeProject(t)
	fixedNow := func() time.Time { return time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC) }
	pinClock := func(o *Options) { o.Now = fixedNow }

	require.Equal(t, clierr.ExitOK, runWith(t, pinClock, f.Dir, "up", "--dry-run").code)
	first := generatedCompose(t, f.Dir)
	require.Equal(t, clierr.ExitOK, runWith(t, pinClock, f.Dir, "up", "--dry-run").code)

	assert.Equal(t, strings.Count(first, "services:"), 1)
	assert.Equal(t, first, generatedCompose(t, f.Dir))
}

// ============================================================
// 13.4 本地调试
// ============================================================

// localDebugProject：people/basic 在 IDE 里跑，强依赖容器里的 department/tree。
func localDebugProject(t *testing.T) *projectFixture {
	t.Helper()

	comps := []comp{
		{ID: "people/basic", Version: "1.0.0", Requires: []string{"department/tree@1.0.0"}},
		{ID: "department/tree", Version: "1.0.0"},
	}
	f := addedProject(t, comps, "people/basic@1.0.0")
	f.writeConfig(t, `components:
  - id: people/basic
    version: 1.0.0
    local: true
    localPort: 8081
  - id: department/tree
    version: 1.0.0
`)
	return f
}

// ============================================================
// servedBy 外壳合并部署
// ============================================================

// servedByProject：mdm/customer 被 infra/shell-go-core 收编，自己没有容器。
func servedByProject(t *testing.T) *projectFixture {
	t.Helper()

	comps := []comp{
		{ID: "infra/shell-go-core", Version: "1.0.0"},
		{ID: "mdm/customer", Version: "1.0.7", Port: 8081},
	}
	f := addedProject(t, comps, "infra/shell-go-core@1.0.0", "mdm/customer@1.0.7")
	f.writeConfig(t, `components:
  - id: infra/shell-go-core
    version: 1.0.0
  - id: mdm/customer
    version: 1.0.7
    servedBy: infra/shell-go-core@1.0.0
`)
	return f
}

// 13.4：env 文件按版本化服务名落到 .brickkit/generated/。
func TestUpDryRunWritesLocalDebugEnvFile(t *testing.T) {
	f := localDebugProject(t)

	r := runIn(t, f.Dir, "up", "--dry-run")

	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)
	path := filepath.Join(f.Dir, ".brickkit", "generated", "local-debug.people-basic-1-0-0.env")
	require.FileExists(t, path, "13.4")

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Contains(t, string(data), "COMPONENT_ID=people/basic", "13.7")
	assert.Contains(t, string(data), "DEPARTMENT_TREE_ENDPOINT=http://localhost:", "13.5")
}

// sourceLocalDebugEnvVar 用真实 bash `source` 这份文件，取出某个变量的值。
//
// brickKit 反馈：local-debug.*.env 序列化多行值和特殊字符会截断或解析错误——
// v0.4.4 复核指出，`readDotEnv` 从前按物理行 `Cut("=")`，一个跨多行的
// 双引号 `.env` 值会在第一行就被切断，而这条 bug 只有真的走"读 .env 文件"
// 这条路才会触发，不能直接构造内存字符串绕过去。所以这里跟 compose 包的
// `sourceEnvFile` 一样，让真实 shell 去读生成的文件，而不是自己写解析器。
func sourceLocalDebugEnvVar(t *testing.T, path, name string) string {
	t.Helper()
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("未安装 bash，跳过 env 文件的真实 source 校验")
	}
	cmd := exec.Command("bash", "-c",
		`set -a && source "$1" && set +a && printf '%s' "${!2}"`, "_", path, name)
	out, err := cmd.Output()
	require.NoError(t, err, "生成的 env 文件 source 失败")
	return string(out)
}

// v0.4.4 复核结果：这条从前是 TestLocalDebugEnvHandlesMultilineValue（compose 包）
// 用 config.Component{Config: map[string]any{...}} 直接塞一个内存里的完整字符串，
// 跳过了真实项目里几乎总会走的那条路——brickkit.yaml 里这类值写的是
// `"${VAR}"` 引用，真正的值要从项目根目录的 `.env` 文件里查（`readDotEnv`）。
// 这条测试走完整链路：真实 `.env` 文件 → `envLookup` → `expandValue` →
// `shellQuote`，而不是绕过第一步——正是这条链路里 `readDotEnv` 按物理行
// `Cut("=")` 截断多行值的地方，之前的回归测试没测到。
func TestUpDryRunLocalDebugEnvResolvesMultilineDotEnvValue(t *testing.T) {
	comps := []comp{
		{ID: "infra/iam-casdoor", Version: "1.0.0", ConfigSchema: []string{"appTokenSigningKeyPem:"}},
	}
	f := addedProject(t, comps, "infra/iam-casdoor@1.0.0")
	f.writeConfig(t, `components:
  - id: infra/iam-casdoor
    version: 1.0.0
    local: true
    localPort: 8081
    config:
      appTokenSigningKeyPem: "${APP_TOKEN_SIGNING_KEY_PEM}"
`)
	pem := "-----BEGIN PRIVATE KEY-----\n" +
		"MIIBVQIBADANBgkqhkiG9w0BAQEFAASCAT8wggE7AgEAAkEA\n" +
		"-----END PRIVATE KEY-----"
	dotenv := `APP_TOKEN_SIGNING_KEY_PEM="` + pem + "\"\n"
	require.NoError(t, os.WriteFile(filepath.Join(f.Dir, ".env"), []byte(dotenv), 0o600))

	r := runIn(t, f.Dir, "up", "--dry-run")
	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)

	path := filepath.Join(f.Dir, ".brickkit", "generated", "local-debug.infra-iam-casdoor-1-0-0.env")
	got := sourceLocalDebugEnvVar(t, path, "APP_TOKEN_SIGNING_KEY_PEM")
	assert.Equal(t, pem, got,
		"跨多行的 .env 值经过完整链路 source 之后应该保留三行，不能被 readDotEnv 截断成第一行")
}

// 使用者需要知道：这个组件不会被启动，得自己在 IDE 里跑，监听哪个端口。
func TestUpDryRunTellsHowToDebugLocally(t *testing.T) {
	f := localDebugProject(t)

	r := runIn(t, f.Dir, "up", "--dry-run")

	assert.Contains(t, r.stdout, "Local debugging")
	assert.Contains(t, r.stdout, "localhost:8081")
	assert.Contains(t, r.stdout, "local-debug.people-basic-1-0-0.env")
	assert.Contains(t, r.stdout, "envFile", "给出 IDE 里怎么用")
}

// 没有 local 组件时不该冒出本地调试的输出，也不该留下 env 文件。
func TestUpDryRunWithoutLocalComponentWritesNoEnvFile(t *testing.T) {
	f := composeProject(t)

	r := runIn(t, f.Dir, "up", "--dry-run")

	require.Equal(t, clierr.ExitOK, r.code, r.stderr)
	assert.NotContains(t, r.stdout, "Local debugging")

	entries, err := os.ReadDir(filepath.Join(f.Dir, ".brickkit", "generated"))
	require.NoError(t, err)
	for _, entry := range entries {
		assert.NotContains(t, entry.Name(), "local-debug")
	}
}

// ============================================================
// 错误路径
// ============================================================

// 端口冲突在生成阶段就要报出来（P4）。
func TestUpDryRunReportsExposePortConflict(t *testing.T) {
	comps := []comp{
		{ID: "portal/user-frontend", Version: "1.0.0"},
		{ID: "admin/console", Version: "1.0.0"},
	}
	f := addedProject(t, comps, "portal/user-frontend@1.0.0", "admin/console@1.0.0")
	f.writeConfig(t, `components:
  - id: portal/user-frontend
    version: 1.0.0
    expose: true
    exposePort: 8080
  - id: admin/console
    version: 1.0.0
    expose: true
    exposePort: 8080
`)

	r := runIn(t, f.Dir, "up", "--dry-run")

	assert.Equal(t, clierr.ExitError, r.code)
	assert.Contains(t, r.stderr, "8080")
	assert.Contains(t, r.stderr, "exposePort")
}

// 一个组件都不启动时不生成文件：一份空的 compose 只会让人困惑。
func TestUpDryRunWithNothingRunning(t *testing.T) {
	comps := []comp{{ID: "people/basic", Version: "1.0.0"}}
	f := addedProject(t, comps, "people/basic@1.0.0")
	f.writeConfig(t, `components:
  - id: people/basic
    version: 1.0.0
    enabled: false
`)

	r := runIn(t, f.Dir, "up", "--dry-run")

	require.Equal(t, clierr.ExitOK, r.code, r.stderr)
	assert.Contains(t, r.stdout, "No component will start this run")
	assert.NoFileExists(t, filepath.Join(f.Dir, ".brickkit", "generated", "docker-compose.yaml"))
}

// 空项目给出引导，而不是报错。
func TestUpDryRunOnEmptyProject(t *testing.T) {
	f := newProjectFixture(t)

	r := runIn(t, f.Dir, "up", "--dry-run")

	assert.Equal(t, clierr.ExitOK, r.code, r.stderr)
	assert.Contains(t, r.stdout, "The current project has no components")
}
