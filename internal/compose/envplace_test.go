package compose_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/compose"
	"github.com/brickkit/brickkit/internal/i18n"
	"github.com/brickkit/brickkit/internal/manifest"
	"github.com/brickkit/brickkit/internal/msgid"
	"github.com/brickkit/brickkit/internal/project/projecttest"
)

func withSchema(m *manifest.Manifest, props map[string]manifest.ConfigProperty) *manifest.Manifest {
	m.ConfigSchema = &manifest.ConfigSchema{Properties: props}
	return m
}

// 直接写在配置里的字面量含 $：必须转义成 $$，否则 compose 会把它当变量插值，
// 容器里拿到的密码就变了样。
func TestComposeLiteralDollarEscaped(t *testing.T) {
	b := newBuilder(t)
	b.component(withSchema(simple("people/basic", "1.0.0", 8080), map[string]manifest.ConfigProperty{
		"PASSWORD": {Type: "string"},
	}), projecttest.Entry{Config: map[string]any{"PASSWORD": "pa$$word"}})

	result := b.generate()
	assert.Contains(t, string(result.YAML), "PASSWORD=pa$$$$word")
	assert.Empty(t, result.EnvFiles)
}

// ${VAR} 模板原样留给 compose 启动时展开，但模板里其余的 $ 是字面量：brickkit 只把
// ${NAME} 当引用（K8s 也只展开它），compose 却会把 $5、$HOME、$$ 都当成它自己的语法——
// 不转义的话同一个值在 Docker 与 K8s 下到达容器时就不一样了，$weird 甚至会被悄悄吞成空串。
// 行内与 env 文件两处都要转义。
func TestComposeTemplateEscapesOtherDollars(t *testing.T) {
	b := newBuilder(t)
	b.component(withSchema(simple("people/basic", "1.0.0", 8080), map[string]manifest.ConfigProperty{
		"GREETING": {Type: "string"},
		"DSN":      {Type: "string", Secret: true},
	}), projecttest.Entry{Config: map[string]any{
		"GREETING": "cost $5 at ${HOOK} $$HOME",
		"DSN":      "pg://u:${DB_PASS}@h/$weird",
	}})
	b.env = map[string]string{"HOOK": "h", "DB_PASS": "p"}

	result := b.generate()
	assert.Contains(t, string(result.YAML), "GREETING=cost $$5 at ${HOOK} $$$$HOME")
	require.Len(t, result.EnvFiles, 1)
	assert.Equal(t, `DSN="pg://u:${DB_PASS}@h/$$weird"`+"\n", string(result.EnvFiles[0].Content))
}

const pem = "-----BEGIN KEY-----\nab$c\"d\\e\n-----END KEY-----\n"

// 密钥与 file:// 内容绝不进 compose.yaml：写进 0600 的 env 文件，主容器与迁移容器都引用它。
func TestComposeSecretsGoToEnvFile(t *testing.T) {
	b := newBuilder(t)
	m := withMigration(withSchema(simple("people/basic", "1.0.0", 8080), map[string]manifest.ConfigProperty{
		"TOKEN": {Type: "string", Secret: true},
		"KEY":   {Type: "string"},
		"PLAIN": {Type: "string"},
	}))
	b.component(m, projecttest.Entry{Config: map[string]any{
		"TOKEN": "${TOKEN_FROM_ENV}", "KEY": "file://secrets/key.pem", "PLAIN": "visible",
	}})
	b.spec.Files["secrets/key.pem"] = pem
	b.env = map[string]string{"TOKEN_FROM_ENV": "t"}

	result := b.generate()
	text := string(result.YAML)
	assert.NotContains(t, text, "TOKEN=")
	assert.NotContains(t, text, "BEGIN KEY")
	assert.Contains(t, text, "PLAIN=visible")

	require.Len(t, result.EnvFiles, 1)
	file := result.EnvFiles[0]
	assert.Equal(t, ".brickkit/generated/env/people-basic-1-0-0.env", file.Path)
	assert.Equal(t,
		`KEY="-----BEGIN KEY-----\nab$$c\"d\\e\n-----END KEY-----\n"`+"\n"+
			`TOKEN="${TOKEN_FROM_ENV}"`+"\n",
		string(file.Content), "文件内容逐字节转义；写成 ${VAR} 的密钥留给 compose 启动时插值")

	doc := b.parsed()
	for _, service := range []string{"people-basic-1-0-0", "people-basic-1-0-0-migration"} {
		svc := serviceOf(t, doc, service)
		assert.Equal(t, []any{map[string]any{"path": file.Path}}, svc["env_file"], service)
	}
}

func TestComposeFileRefMissing(t *testing.T) {
	b := newBuilder(t)
	b.component(withSchema(simple("people/basic", "1.0.0", 8080), map[string]manifest.ConfigProperty{
		"KEY": {Type: "string"},
	}), projecttest.Entry{Config: map[string]any{"KEY": "file://nope.pem"}})

	_, err := b.build(compose.Options{})
	require.Error(t, err)
	rendered := clierr.As(err).Format()
	assert.Contains(t, rendered, "nope.pem")
	assert.Contains(t, rendered, "people/basic@1.0.0")
	assert.Contains(t, rendered, "KEY")
}

// existingSecret 在 Docker 下没有对应概念：跳过（表现成"没配"），不写空串。
func TestComposeExistingSecretSkipped(t *testing.T) {
	b := newBuilder(t)
	b.component(withSchema(simple("people/basic", "1.0.0", 8080), map[string]manifest.ConfigProperty{
		"DB_PASSWORD": {Type: "string", Secret: true},
	}), projecttest.Entry{Config: map[string]any{
		"DB_PASSWORD": map[string]any{"existingSecret": "db", "key": "password"},
	}})

	result := b.generate()
	assert.NotContains(t, string(result.YAML), "DB_PASSWORD")
	assert.Empty(t, result.EnvFiles)
}

// 裸进程（mode: debug / local）不做变量替换：file:// 在生成时就读成真值。
func TestLocalEnvEvaluatesFileRef(t *testing.T) {
	b := newBuilder(t)
	b.component(withSchema(simple("people/basic", "1.0.0", 8080), map[string]manifest.ConfigProperty{
		"KEY": {Type: "string"},
		"REF": {Type: "string", Secret: true},
	}), projecttest.Entry{Mode: "local", Config: map[string]any{
		"KEY": "file://secrets/key.pem",
		"REF": map[string]any{"existingSecret": "db", "key": "password"},
	}})
	b.spec.Files["secrets/key.pem"] = pem

	result := b.generate()
	require.Len(t, result.LocalEnvFiles, 1)
	vars := map[string]string{}
	for _, v := range result.LocalEnvFiles[0].Vars {
		vars[v.Name] = v.Value.Text
	}
	assert.Equal(t, pem, vars["KEY"])
	assert.NotContains(t, vars, "REF", "existingSecret 在宿主机上没有对应物")
}

// ${VAR} 在进程环境与 .env 里都找不到时，docker compose 会把它换成空字符串，只在它自己的输出里
// 警告一句——而 up 成功时那段输出没人看得到。组件拿到 postgres://app@:5432/shop 这样的残缺值，
// 却不会有任何报错。K8s 下同一份配置在生成时就失败；Docker 下也在生成时失败，两个目标说法一致。
// 带默认值的 ${VAR:-x} 不算未定义。
func TestComposeUndefinedReferenceFails(t *testing.T) {
	b := newBuilder(t)
	b.component(withSchema(simple("people/basic", "1.0.0", 8080), map[string]manifest.ConfigProperty{
		"DB_URL":   {Type: "string"},
		"TOKEN":    {Type: "string", Secret: true},
		"LOG_HOOK": {Type: "string"},
	}), projecttest.Entry{Config: map[string]any{
		"DB_URL":   "postgres://app@${PG_HOST}:5432/shop",
		"TOKEN":    "${API_TOKEN}",
		"LOG_HOOK": "${LOG_HOOK_URL:-http://localhost}",
	}})

	_, err := b.build(compose.Options{Lookup: func(string) (string, bool) { return "", false }})
	require.Error(t, err)
	e := clierr.As(err)
	assert.Equal(t, clierr.CodeConfigInvalid, e.Code)
	assert.Equal(t, i18n.T(msgid.ComposeEnvVarsUndefined), e.Message)
	rendered := e.Format()
	assert.Contains(t, rendered, "API_TOKEN")
	assert.Contains(t, rendered, "PG_HOST")
	assert.NotContains(t, rendered, "LOG_HOOK_URL")

	defined := map[string]bool{"PG_HOST": true, "API_TOKEN": true}
	_, err = b.build(compose.Options{Lookup: func(name string) (string, bool) { return "x", defined[name] }})
	require.NoError(t, err)
}
