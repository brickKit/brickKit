package configdir_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/brickkit/brickkit/internal/configdir"
	"github.com/brickkit/brickkit/internal/i18n"
	"github.com/brickkit/brickkit/internal/manifest"
	"github.com/brickkit/brickkit/internal/msgid"
)

func TestSkeleton(t *testing.T) {
	schema := &manifest.ConfigSchema{
		Properties: map[string]manifest.ConfigProperty{
			"DB_HOST":     {Type: "string", Description: "database\nhost"},
			"DB_PASSWORD": {Type: "string", Secret: true},
			"DB_PORT":     {Type: "integer", Default: 5432},
			"TAGS":        {Type: "array", Default: []any{"a"}},
			"OPTIONAL_X":  {Type: "string"},
			"KEY_FILE":    {Type: "string", Secret: true, Mount: manifest.MountFile},
		},
		Required: []string{"DB_HOST", "DB_PASSWORD", "DB_PORT"},
	}
	out := string(configdir.Skeleton("erp/backend", "1.0.0", schema, map[string]string{"DB_PASSWORD": "SHARED_PWD"}))
	def := i18n.T(msgid.ConfigdirSkeletonDefault)

	assert.Contains(t, out, "# Component: erp/backend@1.0.0\n")
	assert.Contains(t, out, "DB_HOST: \"\"  # string | database host\n")
	assert.Contains(t, out, "DB_PASSWORD: $var:SHARED_PWD  # string | secret\n")
	assert.Contains(t, out, "# DB_PORT: 5432  # integer ("+def+")\n", "required-with-default goes to the optional section, commented")
	assert.Contains(t, out, "# TAGS: [\"a\"]  # array ("+def+")\n")
	assert.Contains(t, out, "# OPTIONAL_X:  # string\n")
	assert.Contains(t, out, "# KEY_FILE:  # string | secret | "+i18n.T(msgid.ConfigdirSkeletonAsFile)+"\n",
		"值照常填在这里；说明里点明组件拿到的是文件")

	// 生成的骨架必须能直接被解析与解析出正确结果：只有真正必填的缺失。
	f, err := configdir.ParseComponentFile([]byte(out), "config/erp-backend.yaml")
	require.NoError(t, err)
	assert.Equal(t, []string{"DB_HOST", "DB_PASSWORD"}, f.Keys())
	res, err := configdir.Resolve(configdir.Input{
		ComponentID: "erp/backend", Version: "1.0.0", Schema: schema, File: f,
		Vars: map[string]configdir.Value{"SHARED_PWD": {Kind: configdir.KindLiteral, Text: "pwd"}},
	})
	require.NoError(t, err)
	assert.Equal(t, []string{"DB_HOST"}, res.Missing)
	tags, _ := res.Get("TAGS")
	assert.Equal(t, `["a"]`, tags.Value.Text)

	id, version, ok := configdir.Header([]byte(out))
	require.True(t, ok)
	assert.Equal(t, "erp/backend", id)
	assert.Equal(t, "1.0.0", version)

	assert.Nil(t, configdir.Skeleton("a/b", "1.0.0", nil, nil))
	assert.Nil(t, configdir.Skeleton("a/b", "1.0.0", &manifest.ConfigSchema{}, nil))
}
