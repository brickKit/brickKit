package cli

// 本文件覆盖开发计划 35.17：**`component.config` 里不该放密钥**。
//
// P5 那条明文密码告警只看 `resources[].password`，而 `config` 完全没人管——
// 于是这样的配置一声不吭地通过：
//
//	components:
//	  - id: demo/hello
//	    config:
//	      apiToken: "sk-live-REALSECRET123456"
//
// 后果值得警惕的地方在于**泄漏路径不是生成物，而是 `brickkit.yaml` 本身**：
// 那个文件是**明确建议提交进 Git 的**（003 §1.2「可版本控制：建议提交到 Git，
// 团队共享」）。写在 `resources[].password` 里的密码有 P5 告警兜着，
// 写在 `config` 里的却一声不吭。
//
// 顺带澄清一个我一开始搞错的点：未在 `configSchema` 里声明的 config 项
// **不会被注入**，所以它未必会进生成物。但它照样躺在提交进 Git 的
// `brickkit.yaml` 里——泄漏路径与生成物无关。
//
// （那种"写了却不生效"的情形现在会另出一条警告，见 inject.unknownConfigWarnings。
// 本文件的用例因此都让组件真的声明了对应的 configSchema 项：
// 两条警告混在一起会让下面"普通配置项不该被误判"的断言失去意义。）
//
// 与 P5 一样是**警告不是错误**：`config` 里放什么由使用者决定，
// 平台不该替他判断哪个值算密钥；但看着像密钥的东西必须说一声。

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/brickkit/brickkit/internal/clierr"
)

// configProject 造一个组件带 config 的项目。
//
// schemaKeys 是组件在 configSchema 里声明的配置项。**必须与 configLines 里
// 写的键对得上**：对不上的键会另外触发"这一项不会生效"的警告（B3），
// 那条警告会点名配置项，把本文件"普通配置项不该被误判"的断言搅浑——
// 而那是另一件事，不该在这里搭便车验证。
func configProject(t *testing.T, configLines string, schemaKeys ...string) *projectFixture {
	t.Helper()

	schema := make([]string, 0, len(schemaKeys))
	for _, key := range schemaKeys {
		schema = append(schema, key+":") // name:default，默认值留空
	}
	f := addedProject(t,
		[]comp{{ID: "demo/hello", Version: "1.0.0", ConfigSchema: schema}},
		"demo/hello@1.0.0")
	body := configHeader + `
components:
  - id: demo/hello
    version: 1.0.0
` + configLines
	require.NoError(t, os.WriteFile(f.Layout.ConfigPath(), []byte(body), 0o644))
	return f
}

// 看着像密钥的 config 值要警告。
func TestConfigWithSecretWarns(t *testing.T) {
	f := configProject(t, `    config:
      apiToken: "sk-live-REALSECRET123456"
`, "apiToken")

	r := runWithEngine(t, newFakeEngine(), f.Dir, "up", "--dry-run")

	require.Equal(t, clierr.ExitOK, r.code, "35.17：是警告不是错误：%s", r.stderr)
	out := r.stdout + r.stderr
	assert.Contains(t, out, "apiToken", "35.17：要点名是哪个配置项：%s", out)
	assert.NotContains(t, out, "sk-live-REALSECRET123456",
		"35.17：**绝不能把密钥本身打出来**——那等于又抄了一遍到终端和 CI 日志里")
}

// 警告要说清为什么，以及该怎么做。
func TestConfigSecretWarningExplainsWhy(t *testing.T) {
	f := configProject(t, `    config:
      dbPassword: "hunter2-plaintext"
`, "dbPassword")

	r := runWithEngine(t, newFakeEngine(), f.Dir, "up", "--dry-run")
	out := r.stdout + r.stderr

	assert.Contains(t, out, "${", "35.17：要给出 ${ENV_VAR} 这条出路：%s", out)
	assert.Contains(t, out, "Git",
		"35.17：要说清 brickkit.yaml 是建议提交进 Git 的——那才是使用者没想到的地方：%s", out)
}

// 用了 ${ENV_VAR} 就不该警告——那正是做对了的写法。
func TestConfigWithEnvVarDoesNotWarn(t *testing.T) {
	t.Setenv("MY_API_TOKEN", "sk-live-whatever")
	f := configProject(t, `    config:
      apiToken: ${MY_API_TOKEN}
`, "apiToken")

	r := runWithEngine(t, newFakeEngine(), f.Dir, "up", "--dry-run")

	require.Equal(t, clierr.ExitOK, r.code, r.stderr)
	assert.NotContains(t, r.stdout+r.stderr, "apiToken",
		"35.17：用了环境变量引用就是做对了，不该再骂人")
}

// 普通配置项不该被误判。
//
// 这条比"能不能报出来"更重要：一个见谁都喊的告警，两天之内就会被所有人无视，
// 那时它连真的密钥也保护不了。
func TestOrdinaryConfigDoesNotWarn(t *testing.T) {
	f := configProject(t, `    config:
      sessionTtlSeconds: 7200
      greeting: "你好"
      logLevel: "debug"
      retryCount: 3
      enabled: true
`, "sessionTtlSeconds", "greeting", "logLevel", "retryCount", "enabled")

	r := runWithEngine(t, newFakeEngine(), f.Dir, "up", "--dry-run")

	require.Equal(t, clierr.ExitOK, r.code, r.stderr)
	for _, name := range []string{"sessionTtlSeconds", "greeting", "logLevel", "retryCount"} {
		assert.NotContains(t, r.stdout+r.stderr, name,
			"35.17：%s 不是密钥，误报会让这个告警很快被无视", name)
	}
}

// 密钥确实躺在 brickkit.yaml 里——而那个文件是建议提交进 Git 的。
//
// 这条是整个告警存在的理由：不是"值会泄漏到某个生成物"，
// 而是**它就在那份大家都会提交的配置里**。
func TestConfigSecretSitsInCommittedConfig(t *testing.T) {
	f := configProject(t, `    config:
      apiToken: "sk-live-REALSECRET123456"
`, "apiToken")

	body, err := os.ReadFile(f.Layout.ConfigPath())
	require.NoError(t, err)
	assert.Contains(t, string(body), "sk-live-REALSECRET123456",
		"35.17：密钥就在 brickkit.yaml 里，而 003 §1.2 建议把它提交进 Git")
}
