package compose_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/compose"
	"github.com/brickkit/brickkit/internal/manifest"
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
