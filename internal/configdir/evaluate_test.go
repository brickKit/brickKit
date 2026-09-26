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
