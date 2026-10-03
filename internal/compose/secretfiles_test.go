package compose_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/compose"
	"github.com/brickkit/brickkit/internal/manifest"
	"github.com/brickkit/brickkit/internal/project/projecttest"
)

func fileItem() manifest.ConfigProperty {
	return manifest.ConfigProperty{Type: "string", Secret: true, Mount: manifest.MountFile}
}

func secretFilesOf(result *compose.Result) map[string]string {
	out := map[string]string{}
	for _, f := range result.SecretFiles {
		out[f.Service+"/"+f.Key] = string(f.Content)
	}
	return out
}

// mount: file 的配置项：值写成文件（逐字节，不转义），环境变量里是文件在容器里的路径；
// compose.yaml 与 env 文件里都没有值。组件的目录只读挂进主容器与迁移容器——迁移读到的要和主服务一样。
func TestMountFileDeliversThePathAndMountsTheDirectory(t *testing.T) {
	b := newBuilder(t)
	m := withMigration(withSchema(simple("erp/sales", "1.0.0", 8080), map[string]manifest.ConfigProperty{
		"JWT_PRIVATE_KEY_FILE": fileItem(),
		"API_TOKEN_FILE":       fileItem(),
		"DB_PASSWORD":          {Type: "string", Secret: true},
	}))
	b.component(m, projecttest.Entry{Config: map[string]any{
		"JWT_PRIVATE_KEY_FILE": "file://secrets/key.pem",
		"API_TOKEN_FILE":       "tok-${TOKEN_FROM_ENV}",
		"DB_PASSWORD":          "pw",
	}})
	b.spec.Files["secrets/key.pem"] = pem
	b.env = map[string]string{"TOKEN_FROM_ENV": "t$1"}

	result := b.generate()
	text := string(result.YAML)
	assert.Contains(t, text, "JWT_PRIVATE_KEY_FILE=/run/brickkit/secrets/erp-sales-1-0-0/JWT_PRIVATE_KEY_FILE")
	assert.Contains(t, text, "API_TOKEN_FILE=/run/brickkit/secrets/erp-sales-1-0-0/API_TOKEN_FILE")
	assert.NotContains(t, text, "BEGIN KEY")
	assert.NotContains(t, text, "tok-")

	assert.Equal(t, map[string]string{
		"erp-sales-1-0-0/JWT_PRIVATE_KEY_FILE": pem,
		"erp-sales-1-0-0/API_TOKEN_FILE":       "tok-t$1",
	}, secretFilesOf(result), "${VAR} 在生成时就展开：文件里没有人替它做变量替换")

	require.Len(t, result.EnvFiles, 1)
	assert.Equal(t, `DB_PASSWORD="pw"`+"\n", string(result.EnvFiles[0].Content), "没写 mount 的密钥照旧在 env 文件里")

	doc := b.parsed()
	volume := map[string]any{
		"type": "bind", "read_only": true,
		"source": "./.brickkit/generated/secrets/erp-sales-1-0-0",
		"target": "/run/brickkit/secrets/erp-sales-1-0-0",
	}
	for _, service := range []string{"erp-sales-1-0-0", "erp-sales-1-0-0-migration"} {
		assert.Equal(t, []any{volume}, serviceOf(t, doc, service)["volumes"], service)
	}
}

// 没有以文件交付的项时，什么都不多：没有 volumes，没有要写的文件。
func TestNoMountFileMeansNoVolumes(t *testing.T) {
	b := newBuilder(t)
	b.component(withSchema(simple("erp/sales", "1.0.0", 8080), map[string]manifest.ConfigProperty{
		"DB_PASSWORD": {Type: "string", Secret: true},
	}), projecttest.Entry{Config: map[string]any{"DB_PASSWORD": "pw"}})

	result := b.generate()
	assert.Empty(t, result.SecretFiles)
	assert.NotContains(t, serviceOf(t, b.parsed(), "erp-sales-1-0-0"), "volumes")
}

// 在宿主机上跑的进程（mode: local / debug）拿到的是文件在宿主机上的绝对路径——它没有 /run/brickkit。
func TestMountFileOnHostProcessGetsTheHostPath(t *testing.T) {
	b := newBuilder(t)
	b.component(withSchema(simple("erp/sales", "1.0.0", 8080), map[string]manifest.ConfigProperty{
		"JWT_PRIVATE_KEY_FILE": fileItem(),
	}), projecttest.Entry{Mode: "local", Config: map[string]any{"JWT_PRIVATE_KEY_FILE": "file://secrets/key.pem"}})
	b.spec.Files["secrets/key.pem"] = pem

	result := b.generate()
	require.Len(t, result.LocalEnvFiles, 1)
	var path string
	for _, v := range result.LocalEnvFiles[0].Vars {
		if v.Name == "JWT_PRIVATE_KEY_FILE" {
			path = v.Value.Text
		}
	}
	assert.True(t, filepath.IsAbs(path), path)
	assert.True(t, strings.HasSuffix(filepath.ToSlash(path),
		"/.brickkit/generated/secrets/erp-sales-1-0-0/JWT_PRIVATE_KEY_FILE"), path)
	assert.Equal(t, map[string]string{"erp-sales-1-0-0/JWT_PRIVATE_KEY_FILE": pem}, secretFilesOf(result))
	assert.NotContains(t, string(result.LocalEnvFiles[0].Content), "BEGIN KEY", "调试用的 env 文件里也只有路径")
}

// 被外壳承载的成员：文件挂进外壳的容器，路径与成员自己跑时一样；成员的 JSON 里放的是路径，不是值。
// 成员的迁移容器也挂同一个目录。外壳自己没有以文件交付的项时，只挂成员的。
func TestMountFileOfAHostedMemberIsMountedIntoTheShell(t *testing.T) {
	member := withMigration(withSchema(simple("mdm/customer", "1.0.7", 8080), map[string]manifest.ConfigProperty{
		"SIGNING_KEY_FILE": fileItem(),
	}))
	entry := servedByEntry("infra/shell-go-core", "1.0.0")
	entry.Config = map[string]any{"SIGNING_KEY_FILE": "file://secrets/key.pem"}
	b := newBuilder(t)
	b.component(simple("infra/shell-go-core", "1.0.0", 9000), projecttest.Entry{})
	b.component(member, entry)
	b.spec.Files["secrets/key.pem"] = pem

	result := b.generate()
	assert.Equal(t, map[string]string{"mdm-customer-1-0-7/SIGNING_KEY_FILE": pem}, secretFilesOf(result))

	var shellFile string
	for _, f := range result.EnvFiles {
		if f.Service == "infra-shell-go-core-1-0-0" {
			shellFile = string(f.Content)
		}
	}
	assert.Contains(t, shellFile, `\"SIGNING_KEY_FILE\":\"/run/brickkit/secrets/mdm-customer-1-0-7/SIGNING_KEY_FILE\"`)
	assert.NotContains(t, shellFile, "BEGIN KEY")

	doc := b.parsed()
	volume := map[string]any{
		"type": "bind", "read_only": true,
		"source": "./.brickkit/generated/secrets/mdm-customer-1-0-7",
		"target": "/run/brickkit/secrets/mdm-customer-1-0-7",
	}
	assert.Equal(t, []any{volume}, serviceOf(t, doc, "infra-shell-go-core-1-0-0")["volumes"])
	assert.Equal(t, []any{volume}, serviceOf(t, doc, "mdm-customer-1-0-7-migration")["volumes"])
}

// 文件里的 ${VAR} 取不到：和别处一样在生成时失败——写一个带着字面 ${VAR} 的"密钥"进去，组件要到用它时才出错。
func TestMountFileUndefinedReferenceFails(t *testing.T) {
	b := newBuilder(t)
	b.component(withSchema(simple("erp/sales", "1.0.0", 8080), map[string]manifest.ConfigProperty{
		"API_TOKEN_FILE": fileItem(),
	}), projecttest.Entry{Config: map[string]any{"API_TOKEN_FILE": "${API_TOKEN}"}})

	_, err := b.build(compose.Options{Lookup: func(string) (string, bool) { return "", false }})
	require.Error(t, err)
	assert.Equal(t, clierr.CodeConfigInvalid, clierr.As(err).Code)
	assert.Contains(t, clierr.As(err).Format(), "API_TOKEN")
}

// existingSecret 在 Docker 下没有对应物：写了 mount: file 也一样，这条变量不注入，也不生成文件。
func TestMountFileWithExistingSecretIsSkippedOnDocker(t *testing.T) {
	b := newBuilder(t)
	b.component(withSchema(simple("erp/sales", "1.0.0", 8080), map[string]manifest.ConfigProperty{
		"TLS_KEY_FILE": fileItem(),
	}), projecttest.Entry{Config: map[string]any{
		"TLS_KEY_FILE": map[string]any{"existingSecret": "app-tls", "key": "tls.key"},
	}})

	result := b.generate()
	assert.NotContains(t, string(result.YAML), "TLS_KEY_FILE")
	assert.Empty(t, result.SecretFiles)
}

// 外壳是裸进程时，成员的代码在宿主机上读文件：交给外壳的 JSON 里是宿主机路径。成员的迁移仍是容器，
// 它拿到的还是容器里的路径，目录照样挂进去。
func TestMountFileOfAMemberInABareShell(t *testing.T) {
	member := withMigration(withSchema(simple("erp/a", "1.0.0", 8081), map[string]manifest.ConfigProperty{
		"SIGNING_KEY_FILE": fileItem(),
	}))
	entry := servedByEntry("erp/shell", "1.0.0")
	entry.Config = map[string]any{"SIGNING_KEY_FILE": "file://secrets/key.pem"}
	b := newBuilder(t)
	b.component(simple("erp/shell", "1.0.0", 9000), projecttest.Entry{Mode: "local"})
	b.component(member, entry)
	b.spec.Files["secrets/key.pem"] = pem

	result := b.generate()
	entries := servedJSON(t, localEnv(t, result, "erp-shell-1-0-0"))
	require.Len(t, entries, 1)
	config, _ := entries[0]["config"].(map[string]any)
	path, _ := config["SIGNING_KEY_FILE"].(string)
	assert.True(t, filepath.IsAbs(path) && strings.HasSuffix(filepath.ToSlash(path),
		"/.brickkit/generated/secrets/erp-a-1-0-0/SIGNING_KEY_FILE"), path)

	migration := serviceOf(t, docOf(t, result), "erp-a-1-0-0-migration")
	assert.Equal(t, "/run/brickkit/secrets/erp-a-1-0-0/SIGNING_KEY_FILE", envOf(t, migration)["SIGNING_KEY_FILE"])
	assert.Len(t, migration["volumes"], 1)
}
