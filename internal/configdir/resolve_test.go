package configdir_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/configdir"
	"github.com/brickkit/brickkit/internal/manifest"
)

func backendSchema() *manifest.ConfigSchema {
	return &manifest.ConfigSchema{
		Properties: map[string]manifest.ConfigProperty{
			"DB_HOST":     {Type: "string"},
			"DB_PASSWORD": {Type: "string", Secret: true},
			"DB_PORT":     {Type: "integer", Default: 5432},
			"LOG_LEVEL":   {Type: "string", Default: "info"},
			"OPTIONAL_X":  {Type: "string"},
		},
		Required: []string{"DB_HOST", "DB_PASSWORD"},
	}
}

func mustFile(t *testing.T, data string) *configdir.File {
	t.Helper()
	f, err := configdir.ParseComponentFile([]byte(data), "config/erp-backend.yaml")
	require.NoError(t, err)
	return f
}

func mustVars(t *testing.T, data string) map[string]configdir.Value {
	t.Helper()
	f, err := configdir.ParseVarsFile([]byte(data), "config/vars.yaml")
	require.NoError(t, err)
	return f.Map()
}

func TestResolveVarPrecedence(t *testing.T) {
	res, err := configdir.Resolve(configdir.Input{
		ComponentID: "erp/backend", Version: "2.0.0",
		Schema:     backendSchema(),
		File:       mustFile(t, "DB_HOST: $var:ERP_DB_HOST\nDB_PASSWORD: $var:DB_PASSWORD\nLOG_LEVEL: debug\nDB_HOTS: typo\n"),
		Vars:       mustVars(t, "ERP_DB_HOST: pg.internal\nDB_PASSWORD: ${SECRET_DB_PASSWORD}\n"),
		DeployVars: map[string]configdir.Value{"DB_PASSWORD": {Kind: configdir.KindLiteral, Text: "local_pwd"}},
	})
	require.NoError(t, err)
	assert.Empty(t, res.Missing)

	host, _ := res.Get("DB_HOST")
	assert.Equal(t, configdir.Resolved{Key: "DB_HOST", Value: configdir.Value{Kind: configdir.KindLiteral, Text: "pg.internal"},
		Origin: configdir.OriginVar, VarName: "ERP_DB_HOST"}, host)
	pwd, _ := res.Get("DB_PASSWORD")
	assert.Equal(t, "local_pwd", pwd.Value.Text, "deploy vars: override vars.yaml")
	assert.True(t, pwd.Secret)
	port, _ := res.Get("DB_PORT")
	assert.Equal(t, "5432", port.Value.Text)
	assert.Equal(t, configdir.OriginDefault, port.Origin)
	level, _ := res.Get("LOG_LEVEL")
	assert.Equal(t, configdir.OriginFile, level.Origin)
	_, present := res.Get("OPTIONAL_X")
	assert.False(t, present, "an optional key with no value and no default is not injected at all")

	require.Len(t, res.Warnings, 1, "DB_HOTS is not in configSchema")
	assert.Contains(t, res.Warnings[0].Hints[0], "DB_HOST")
}

func TestResolveEmptyRequiredIsMissing(t *testing.T) {
	res, err := configdir.Resolve(configdir.Input{
		ComponentID: "erp/backend", Version: "2.0.0", Schema: backendSchema(),
		File: mustFile(t, "DB_HOST: \"\"\nDB_PASSWORD: $var:EMPTY\n"),
		Vars: mustVars(t, "EMPTY: \"\"\n"),
	})
	require.NoError(t, err)
	assert.Equal(t, []string{"DB_HOST", "DB_PASSWORD"}, res.Missing)
}

func TestResolveUndefinedVar(t *testing.T) {
	_, err := configdir.Resolve(configdir.Input{
		ComponentID: "erp/backend", Version: "2.0.0", Schema: backendSchema(),
		File: mustFile(t, "DB_HOST: $var:NOPE\nDB_PASSWORD: x\n"),
	})
	require.Error(t, err)
	e := clierr.As(err)
	assert.Equal(t, clierr.CodeConfigInvalid, e.Code)
	assert.Contains(t, e.Error(), "$var:NOPE")
}

func TestResolveNoFileUsesDefaults(t *testing.T) {
	res, err := configdir.Resolve(configdir.Input{ComponentID: "erp/backend", Version: "2.0.0", Schema: backendSchema()})
	require.NoError(t, err)
	assert.Equal(t, []string{"DB_HOST", "DB_PASSWORD"}, res.Missing)
	assert.Len(t, res.Values, 2)
}

func TestResolveNoSchemaWarns(t *testing.T) {
	res, err := configdir.Resolve(configdir.Input{ComponentID: "a/b", Version: "1.0.0", File: mustFile(t, "X: 1\n")})
	require.NoError(t, err)
	assert.Empty(t, res.Values)
	require.Len(t, res.Warnings, 1)
}

func TestResolveSecretRefOnNonSecret(t *testing.T) {
	res, err := configdir.Resolve(configdir.Input{
		ComponentID: "erp/backend", Version: "2.0.0", Schema: backendSchema(),
		File: mustFile(t, "DB_HOST: {existingSecret: db, key: host}\nDB_PASSWORD: {existingSecret: db, key: password}\n"),
	})
	require.NoError(t, err)
	require.Len(t, res.Warnings, 1)
	_, hostPresent := res.Get("DB_HOST")
	assert.False(t, hostPresent)
	pwd, _ := res.Get("DB_PASSWORD")
	assert.Equal(t, configdir.KindSecretRef, pwd.Value.Kind)
}
