package cli

// 本文件覆盖清单 35.17：**config/ 里不该放明文密钥**。
//
// 泄漏路径不是生成物，而是 config/ 本身：config/<组件>.yaml 与 config/vars.yaml 是要跟着项目
// 提交进 Git 的。于是这样一行会把密钥带进版本历史，而且删不干净：
//
//	# config/demo-hello.yaml
//	API_TOKEN: sk-live-REALSECRET123456
//
// 判据是组件声明了 secret: true，或者名字长得像密钥（只看名字，不看值）；写成 ${VAR}、
// file://、existingSecret 的都是做对了。经 $var: 引到公共变量的明文同样算，来自个人
// deploy.local.yaml（不进 Git）的不算。
//
// （写了 configSchema 里没有的键会另出一条"不会生效"的警告。本文件的用例因此都让组件
// 真的声明了对应的配置项：两条警告混在一起会让"普通配置项不该被误判"的断言失去意义。）
//
// 是**警告不是错误**：config 里放什么由使用者决定，平台不该替他判断哪个值算密钥；
// 但看着像密钥的东西必须说一声，而且绝不把值本身打出来。

import (
	"os"
	"path/filepath"
	"strings"
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
	f.rewrite(t, body)
	return f
}

// 看着像密钥的 config 值要警告。
func TestConfigWithSecretWarns(t *testing.T) {
	f := configProject(t, `    config:
      apiToken: "sk-live-REALSECRET123456"
`, "apiToken")

	r := runWithEngine(t, newFakeEngine(), f.Dir, "up", "--dry-run")

	require.Equal(t, clierr.ExitOK, r.code, "是警告不是错误：%s", r.stderr)
	out := r.stdout + r.stderr
	assert.Contains(t, out, "API_TOKEN", "要点名是哪个配置项：%s", out)
	assert.NotContains(t, out, "sk-live-REALSECRET123456",
		"**绝不能把密钥本身打出来**——那等于又抄了一遍到终端和 CI 日志里")
}

// 警告要说清为什么，以及该怎么做。
func TestConfigSecretWarningExplainsWhy(t *testing.T) {
	f := configProject(t, `    config:
      dbPassword: "hunter2-plaintext"
`, "dbPassword")

	r := runWithEngine(t, newFakeEngine(), f.Dir, "up", "--dry-run")
	out := r.stdout + r.stderr

	assert.Contains(t, out, "${", "要给出 ${ENV_VAR} 这条出路：%s", out)
	assert.Contains(t, out, "Git",
		"要说清 brickkit.yaml 是建议提交进 Git 的——那才是使用者没想到的地方：%s", out)
}

// 用了 ${ENV_VAR} 就不该警告——那正是做对了的写法。
func TestConfigWithEnvVarDoesNotWarn(t *testing.T) {
	t.Setenv("MY_API_TOKEN", "sk-live-whatever")
	f := configProject(t, `    config:
      apiToken: ${MY_API_TOKEN}
`, "apiToken")

	r := runWithEngine(t, newFakeEngine(), f.Dir, "up", "--dry-run")

	require.Equal(t, clierr.ExitOK, r.code, r.stderr)
	assert.NotContains(t, r.stdout+r.stderr, "API_TOKEN",
		"用了环境变量引用就是做对了，不该再骂人")
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
			"%s 不是密钥，误报会让这个告警很快被无视", name)
	}
}

// 密钥确实躺在 config/ 的组件配置文件里——而那个目录是要提交进 Git 的。
//
// 这条是整个告警存在的理由：不是"值会泄漏到某个生成物"，
// 而是**它就在那份大家都会提交的配置里**。
func TestConfigSecretSitsInCommittedConfig(t *testing.T) {
	f := configProject(t, `    config:
      apiToken: "sk-live-REALSECRET123456"
`, "apiToken")

	body, err := os.ReadFile(filepath.Join(f.Layout.ConfigDir(), "demo-hello@1.0.0.yaml"))
	require.NoError(t, err)
	assert.Contains(t, string(body), "sk-live-REALSECRET123456",
		"密钥就在 config/ 里，而 config/ 是要提交进 Git 的")
}

// 组件亲口声明了 secret: true 的配置项，不必名字长得像密钥也要警告：
// 名字启发式宁可漏报，而组件作者的声明比它准得多。
func TestDeclaredSecretConfigWarnsRegardlessOfName(t *testing.T) {
	f := addedProject(t,
		[]comp{{ID: "demo/hello", Version: "1.0.0",
			ConfigSchema: []string{"webhookUrl:"}, SecretConfig: []string{"webhookUrl"}}},
		"demo/hello@1.0.0")
	body := configHeader + `
components:
  - id: demo/hello
    version: 1.0.0
    config:
      webhookUrl: "https://hooks.example.com/services/T000/B000/XXXX"
`
	f.rewrite(t, body)

	r := runWithEngine(t, newFakeEngine(), f.Dir, "up", "--dry-run")

	require.Equal(t, clierr.ExitOK, r.code, "是警告不是错误：%s", r.stderr)
	out := r.stdout + r.stderr
	assert.Contains(t, out, "WEBHOOK_URL", "要点名是哪个配置项：%s", out)
	assert.NotContains(t, out, "hooks.example.com", "绝不能把值本身打出来")
}

// 声明了 secret: true 又写成 ${VAR} 引用，就是做对了，不警告。
func TestDeclaredSecretConfigWithReferenceDoesNotWarn(t *testing.T) {
	f := addedProject(t,
		[]comp{{ID: "demo/hello", Version: "1.0.0",
			ConfigSchema: []string{"webhookUrl:"}, SecretConfig: []string{"webhookUrl"}}},
		"demo/hello@1.0.0")
	body := configHeader + `
components:
  - id: demo/hello
    version: 1.0.0
    config:
      webhookUrl: ${HELLO_WEBHOOK}
`
	f.rewrite(t, body)
	t.Setenv("HELLO_WEBHOOK", "https://hooks.example.com/x") // 部署机上有这个变量

	r := runWithEngine(t, newFakeEngine(), f.Dir, "up", "--dry-run")

	require.Equal(t, clierr.ExitOK, r.code, "%s%s", r.stdout, r.stderr)
	assert.NotContains(t, r.stdout+r.stderr, "plaintext secrets")
}

// existingSecret 形状是"做对了"，不该被明文密钥警告误伤——它不是字面密钥，是一个引用。
func TestExistingSecretShapeDoesNotTriggerPlaintextWarning(t *testing.T) {
	declared := secretFlowComp
	declared.SecretConfig = []string{"apiToken"}
	f := k8sProjectWith(t, declared, `    config:
      apiToken:
        existingSecret: acme-hello-vault-synced
        key: api-key
`, "")

	r := runWithEngine(t, newK8sEngine(), f.Dir, "up", "--dry-run")

	require.Equal(t, clierr.ExitOK, r.code, "%s%s", r.stdout, r.stderr)
	assert.NotContains(t, r.stdout+r.stderr, "plaintext secrets")
}

// 密钥明文经 $var: 来自部署文件的 vars:：团队的 deploy.yaml 进 Git，要警告；
// 个人的 deploy.local.yaml 被 .gitignore 挡在 Git 外面，本地口令写在那里正是它的用处，不警告。
func TestPlaintextSecretFromDeployVarsDependsOnWhichFile(t *testing.T) {
	setup := func(t *testing.T) *projectFixture {
		f := configProject(t, "    config:\n      dbPassword: $var:PG_PASSWORD\n", "dbPassword")
		return f
	}
	appendVars := func(t *testing.T, path string) {
		body, err := os.ReadFile(path)
		require.NoError(t, err)
		require.NoError(t, os.WriteFile(path, append(body, []byte("\nvars:\n  PG_PASSWORD: plain-pass\n")...), 0o644))
	}

	t.Run("team deploy.yaml", func(t *testing.T) {
		f := setup(t)
		appendVars(t, filepath.Join(f.Dir, "deploy.yaml"))
		r := runWithEngine(t, newFakeEngine(), f.Dir, "up", "--dry-run")
		require.Equal(t, clierr.ExitOK, r.code, "%s%s", r.stdout, r.stderr)
		assert.Contains(t, r.stdout+r.stderr, "plaintext secrets", "deploy.yaml 进 Git")
		assert.NotContains(t, r.stdout+r.stderr, "plain-pass")
	})
	t.Run("personal deploy.local.yaml", func(t *testing.T) {
		f := setup(t)
		r := runIn(t, f.Dir, "local", "on")
		require.Equal(t, clierr.ExitOK, r.code, "%s%s", r.stdout, r.stderr)
		appendVars(t, filepath.Join(f.Dir, "deploy.local.yaml"))
		r = runWithEngine(t, newFakeEngine(), f.Dir, "up", "--dry-run")
		require.Equal(t, clierr.ExitOK, r.code, "%s%s", r.stdout, r.stderr)
		assert.NotContains(t, r.stdout+r.stderr, "plaintext secrets", "deploy.local.yaml 不进 Git")
	})
	// 本地模式遮住了同名变量，但**已提交**的文件里仍然写着明文：泄漏的是那份文件，
	// 与这次跑的是哪一份部署文件无关。
	for _, committed := range []string{"config/vars.yaml", "deploy.yaml"} {
		t.Run("personal value shadows plaintext in "+committed, func(t *testing.T) {
			f := setup(t)
			path := filepath.Join(f.Dir, filepath.FromSlash(committed))
			if committed == "config/vars.yaml" {
				require.NoError(t, os.WriteFile(path, []byte("PG_PASSWORD: plain-pass\n"), 0o644))
			} else {
				appendVars(t, path)
			}
			r := runIn(t, f.Dir, "local", "on")
			require.Equal(t, clierr.ExitOK, r.code, "%s%s", r.stdout, r.stderr)
			local := filepath.Join(f.Dir, "deploy.local.yaml")
			body, err := os.ReadFile(local)
			require.NoError(t, err)
			if !strings.Contains(string(body), "vars:") {
				body = append(body, []byte("\nvars:\n")...)
			}
			body = []byte(strings.Replace(string(body), "  PG_PASSWORD: plain-pass\n", "", 1))
			body = []byte(strings.Replace(string(body), "vars:\n", "vars:\n  PG_PASSWORD: my-local-pass\n", 1))
			require.NoError(t, os.WriteFile(local, body, 0o644))

			r = runWithEngine(t, newFakeEngine(), f.Dir, "up", "--dry-run")
			require.Equal(t, clierr.ExitOK, r.code, "%s%s", r.stdout, r.stderr)
			assert.Contains(t, r.stdout+r.stderr, "plaintext secrets", "%s 进 Git，明文还在那里", committed)
			assert.Contains(t, r.stdout+r.stderr, "PG_PASSWORD")
			assert.NotContains(t, r.stdout+r.stderr, "plain-pass")
			assert.NotContains(t, r.stdout+r.stderr, "my-local-pass")
		})
	}
}
