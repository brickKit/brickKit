package configdir_test

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"

	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/configdir"
	"github.com/brickkit/brickkit/internal/i18n"
	"github.com/brickkit/brickkit/internal/manifest"
	"github.com/brickkit/brickkit/internal/msgid"
)

func TestFileNaming(t *testing.T) {
	assert.Equal(t, "erp-backend.yaml", configdir.FileName("erp/backend", ""))
	assert.Equal(t, "erp-backend@1.0.0.yaml", configdir.FileName("erp/backend", "1.0.0"))

	cases := map[string]struct {
		base, version string
		ok            bool
	}{
		"erp-backend.yaml":       {"erp-backend", "", true},
		"erp-backend@1.0.0.yaml": {"erp-backend", "1.0.0", true},
		"vars.yaml":              {"", "", false},
		"x@latest.yaml":          {"", "", false},
		"notes.txt":              {"", "", false},
		".yaml":                  {"", "", false},
	}
	for name, want := range cases {
		base, version, ok := configdir.ParseFileName(name)
		assert.Equal(t, want.ok, ok, name)
		assert.Equal(t, want.base, base, name)
		assert.Equal(t, want.version, version, name)
	}
}

func TestParseValue(t *testing.T) {
	cases := []struct {
		raw  any
		kind configdir.Kind
		text string
	}{
		{nil, configdir.KindLiteral, ""},
		{"", configdir.KindLiteral, ""},
		{"abc", configdir.KindLiteral, "abc"},
		{5, configdir.KindLiteral, "5"},
		{2.5, configdir.KindLiteral, "2.5"},
		{float64(20), configdir.KindLiteral, "20"},
		{true, configdir.KindLiteral, "true"},
		{[]any{"a", "b"}, configdir.KindLiteral, `["a","b"]`},
		{map[string]any{"a": 1}, configdir.KindLiteral, `{"a":1}`},
		{"pg://${USER}@h", configdir.KindEnvTemplate, "pg://${USER}@h"},
	}
	for _, tc := range cases {
		v, err := configdir.ParseValue(tc.raw)
		require.NoError(t, err, tc.raw)
		assert.Equal(t, tc.kind, v.Kind, tc.raw)
		assert.Equal(t, tc.text, v.Text, tc.raw)
	}

	v, err := configdir.ParseValue("$var:DB_HOST")
	require.NoError(t, err)
	assert.Equal(t, configdir.Value{Kind: configdir.KindVarRef, Name: "DB_HOST"}, v)

	v, err = configdir.ParseValue("file://secrets/key.pem")
	require.NoError(t, err)
	assert.Equal(t, configdir.Value{Kind: configdir.KindFileRef, Path: "secrets/key.pem"}, v)

	v, err = configdir.ParseValue(map[string]any{"existingSecret": "db", "key": "password"})
	require.NoError(t, err)
	assert.Equal(t, configdir.Value{Kind: configdir.KindSecretRef, SecretName: "db", SecretKey: "password"}, v)

	_, err = configdir.ParseValue("$var:bad name")
	assert.Error(t, err)
	_, err = configdir.ParseValue("file://")
	assert.Error(t, err)

	assert.True(t, configdir.Value{}.IsUnset())
	assert.Equal(t, "$var:X", configdir.Value{Kind: configdir.KindVarRef, Name: "X"}.String())
}

func TestParseComponentFile(t *testing.T) {
	f, err := configdir.ParseComponentFile([]byte("# Component: erp/backend@1.0.0\nDB_HOST: $var:ERP_DB_HOST\nDB_PORT: 5432\nEMPTY: \"\"\n"), "config/erp-backend.yaml")
	require.NoError(t, err)
	assert.Equal(t, []string{"DB_HOST", "DB_PORT", "EMPTY"}, f.Keys())
	assert.Equal(t, 2, f.Entries[0].Line)
	v, ok := f.Lookup("DB_PORT")
	require.True(t, ok)
	assert.Equal(t, "5432", v.Text)
	assert.True(t, f.Map()["EMPTY"].IsUnset())
}

func TestParseEmptyFiles(t *testing.T) {
	f, err := configdir.ParseComponentFile([]byte("# nothing yet\n"), "config/a-b.yaml")
	require.NoError(t, err)
	assert.Empty(t, f.Entries)
}

func TestParseComponentFileBadKey(t *testing.T) {
	_, err := configdir.ParseComponentFile([]byte("db-host: x\n"), "config/a-b.yaml")
	require.Error(t, err)
	assert.Equal(t, clierr.CodeConfigInvalid, clierr.As(err).Code)
}

func TestParseVarsRejectsChain(t *testing.T) {
	_, err := configdir.ParseVarsFile([]byte("A: $var:B\n"), "config/vars.yaml")
	require.Error(t, err)

	f, err := configdir.ParseVarsFile([]byte("A: ${SECRET}\nB: file://k.pem\n"), "config/vars.yaml")
	require.NoError(t, err)
	assert.Equal(t, configdir.KindEnvTemplate, f.Map()["A"].Kind)

	_, err = configdir.ParseVarsMap(nodes(t, "A: $var:B\n"), "deploy.yaml")
	require.Error(t, err)
	m, err := configdir.ParseVarsMap(nodes(t, "A: x\nN: 3\n"), "deploy.yaml")
	require.NoError(t, err)
	assert.Equal(t, "3", m["N"].Text)
}

func nodes(t *testing.T, data string) map[string]yaml.Node {
	t.Helper()
	var out map[string]yaml.Node
	require.NoError(t, yaml.Unmarshal([]byte(data), &out))
	return out
}

func TestParseComponentFileUnmarkedDuplicate(t *testing.T) {
	_, err := configdir.ParseComponentFile([]byte("A: 1\nB: 2\nA: 3\n"), "config/a-b.yaml")
	var conflict *configdir.ConflictError
	require.True(t, errors.As(err, &conflict))
	require.Len(t, conflict.Files, 1)
	key := conflict.Files[0].Keys[0]
	assert.Equal(t, "A", key.Key)
	assert.Equal(t, []configdir.ConflictLine{{Line: 1, Value: "1"}, {Line: 3, Value: "3"}}, key.Lines)
	assert.Equal(t, clierr.CodeConfigConflict, conflict.Render().Code)
}

func TestConflictBlockRoundTrip(t *testing.T) {
	block := configdir.ConflictBlock("LOG_LEVEL",
		configdir.ScalarYAML("debug"), "1.0.0", configdir.ScalarYAML("warn"), "2.0.0")
	_, err := configdir.ParseComponentFile([]byte("X: 1\n"+block), "config/erp-backend.yaml")

	var conflict *configdir.ConflictError
	require.True(t, errors.As(err, &conflict))
	lines := conflict.Files[0].Keys[0].Lines
	require.Len(t, lines, 2)
	assert.Equal(t, configdir.ConflictLine{Line: lines[0].Line, Value: "debug", Side: configdir.SideCurrent, Version: "1.0.0"}, lines[0])
	assert.Equal(t, configdir.ConflictLine{Line: lines[1].Line, Value: "warn", Side: configdir.SideProposed, Version: "2.0.0"}, lines[1])

	merged := &configdir.ConflictError{}
	merged.Merge(conflict)
	merged.Merge(conflict)
	assert.Len(t, merged.Files, 2)
}

func TestScalarYAML(t *testing.T) {
	assert.Equal(t, "debug", configdir.ScalarYAML("debug"))
	assert.Equal(t, `"5"`, configdir.ScalarYAML("5"))
	assert.Equal(t, "5", configdir.ScalarYAML(5))
	assert.Equal(t, `"a\nb"`, configdir.ScalarYAML("a\nb"))
}

func TestConflictHintMatchesMarker(t *testing.T) {
	_, err := configdir.ParseComponentFile([]byte("A: 1\nA: 2\n"), "config/a-b.yaml")
	var plain *configdir.ConflictError
	require.True(t, errors.As(err, &plain))
	assert.Equal(t, i18n.T(msgid.ConfigdirConflictHintEditPlain), plain.Render().Hints[0])

	block := configdir.ConflictBlock("A", "1", "1.0.0", "2", "2.0.0")
	_, err = configdir.ParseComponentFile([]byte(block), "config/a-b.yaml")
	var marked *configdir.ConflictError
	require.True(t, errors.As(err, &marked))
	assert.Equal(t, i18n.T(msgid.ConfigdirConflictHintEditMarked), marked.Render().Hints[0])
}

// manifest 不能 import configdir（方向反了），只好各持一份环境变量名规则：这里盯着两边不许分叉。
func TestEnvNameRuleMatchesManifest(t *testing.T) {
	for _, name := range []string{"DB_HOST", "_X", "a1", "defaultPageSize", "1BAD", "db-host", "", "A B", "é"} {
		assert.Equal(t, configdir.IsValidName(name), manifest.IsEnvName(name), name)
	}
}
