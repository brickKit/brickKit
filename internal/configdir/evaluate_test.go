package configdir_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/configdir"
)

func TestEvaluateKinds(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "k.pem"), []byte("-----BEGIN-----\nabc\n"), 0o600))
	lookup := func(n string) (string, bool) { return map[string]string{"U": "app"}[n], n == "U" }

	got, err := configdir.Evaluate(configdir.Value{Kind: configdir.KindFileRef, Path: "k.pem"}, root, lookup)
	require.NoError(t, err)
	assert.Equal(t, "-----BEGIN-----\nabc\n", got, "file:// 内容逐字节保留，不 trim")

	got, err = configdir.Evaluate(configdir.Value{Kind: configdir.KindEnvTemplate, Text: "pg://${U}@${NOPE}"}, root, lookup)
	require.NoError(t, err)
	assert.Equal(t, "pg://app@${NOPE}", got, "找不到的引用原样保留")

	got, err = configdir.Evaluate(configdir.Value{Kind: configdir.KindLiteral, Text: "a$b"}, root, lookup)
	require.NoError(t, err)
	assert.Equal(t, "a$b", got)

	abs := filepath.Join(root, "k.pem")
	got, err = configdir.Evaluate(configdir.Value{Kind: configdir.KindFileRef, Path: abs}, "/elsewhere", lookup)
	require.NoError(t, err)
	assert.Equal(t, "-----BEGIN-----\nabc\n", got, "绝对路径原样使用")

	_, err = configdir.Evaluate(configdir.Value{Kind: configdir.KindSecretRef, SecretName: "s", SecretKey: "k"}, root, lookup)
	assert.Equal(t, clierr.CodeInternal, clierr.As(err).Code, "existingSecret 不能被求值，调用方必须先分流")
}

func TestEvaluateFileRefMissing(t *testing.T) {
	_, err := configdir.Evaluate(configdir.Value{Kind: configdir.KindFileRef, Path: "nope.pem"}, t.TempDir(), nil)
	require.Error(t, err)
	assert.Contains(t, clierr.As(err).Error(), "nope.pem")
}

// ${VAR:-默认值} 是引用：解析成模板而不是字面量，求值时变量取不到就用默认值。
// 外壳成员的 JSON、mode: local 的进程环境、mode: debug 的 env 文件都经过这里。
func TestEvaluateEnvDefault(t *testing.T) {
	v, err := configdir.ParseValue("pg-${PGH:-dev}")
	require.NoError(t, err)
	require.Equal(t, configdir.KindEnvTemplate, v.Kind)

	got, err := configdir.Evaluate(v, t.TempDir(), func(string) (string, bool) { return "", false })
	require.NoError(t, err)
	assert.Equal(t, "pg-dev", got)
}

// 不合语法的 ${…} 在解析时就失败：不能悄悄当成字面量交给容器。
func TestParseValueRejectsMalformedReference(t *testing.T) {
	_, err := configdir.ParseValue("pg-${PGH:-${DEF}}")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "${PGH:-${DEF}")
}
