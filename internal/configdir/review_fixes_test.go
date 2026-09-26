package configdir_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"

	"github.com/brickkit/brickkit/internal/configdir"
	"github.com/brickkit/brickkit/internal/manifest"
)

// $var: 指向 null 等同于"没写"：回落到 schema 默认值，而不是直接丢掉或判缺失。
func TestResolveUnsetVarFallsBackToDefault(t *testing.T) {
	schema := &manifest.ConfigSchema{
		Properties: map[string]manifest.ConfigProperty{
			"LOG": {Type: "string", Default: "info"},
			"REQ": {Type: "string", Default: "fallback"},
		},
		Required: []string{"REQ"},
	}
	res, err := configdir.Resolve(configdir.Input{
		ComponentID: "a/b", Version: "1.0.0", Schema: schema,
		File: mustFile(t, "LOG: $var:EMPTY\nREQ: $var:EMPTY\n"),
		Vars: mustVars(t, "EMPTY:\n"),
	})
	require.NoError(t, err)
	assert.Empty(t, res.Missing)
	log, _ := res.Get("LOG")
	assert.Equal(t, "info", log.Value.Text)
	assert.Equal(t, configdir.OriginDefault, log.Origin)
	req, _ := res.Get("REQ")
	assert.Equal(t, "fallback", req.Value.Text)
}

// 组件作者写的 default 是字面量：看不到项目的 vars、也不该去读部署者机器上的文件。
func TestResolveDefaultsAreAlwaysLiteral(t *testing.T) {
	schema := &manifest.ConfigSchema{Properties: map[string]manifest.ConfigProperty{
		"A": {Type: "string", Default: "$var:NOPE"},
		"B": {Type: "string", Default: "file:///etc/ssl/ca.pem"},
		"C": {Type: "string", Default: "x${HOME}"},
	}}
	res, err := configdir.Resolve(configdir.Input{ComponentID: "a/b", Version: "1.0.0", Schema: schema})
	require.NoError(t, err)
	for key, want := range map[string]string{"A": "$var:NOPE", "B": "file:///etc/ssl/ca.pem", "C": "x${HOME}"} {
		got, ok := res.Get(key)
		require.True(t, ok, key)
		assert.Equal(t, configdir.KindLiteral, got.Value.Kind, key)
		assert.Equal(t, want, got.Value.Text, key)
	}
}

// 写错形状的 existingSecret 引用必须报错，绝不能被当成明文 JSON 注入。
func TestParseValueMalformedSecretRef(t *testing.T) {
	for _, raw := range []map[string]any{
		{"existingSecret": "db", "Key": "pw"},
		{"existingSecret": "db", "key": "pw", "extra": 1},
		{"existingSecret": "", "key": "pw"},
		{"existingSecret": "db"},
	} {
		_, err := configdir.ParseValue(raw)
		assert.Error(t, err, raw)
	}
	_, err := configdir.ParseComponentFile([]byte("DB_PASSWORD: {existingSecret: db, Key: pw}\n"), "config/a-b.yaml")
	assert.Error(t, err)
}

// 数字按使用者写下的原文注入：VER: 1.10 就是 "1.10"，不是 "1.1"。
func TestNumbersKeepWrittenText(t *testing.T) {
	f := mustFile(t, "VER: 1.10\nHEX: 0x1F\nN: 20\nON: true\n")
	m := f.Map()
	assert.Equal(t, "1.10", m["VER"].Text)
	assert.Equal(t, "0x1F", m["HEX"].Text)
	assert.Equal(t, "20", m["N"].Text)
	assert.Equal(t, "true", m["ON"].Text)

	vars := mustVars(t, "PORT: 05432\n")
	assert.Equal(t, "05432", vars["PORT"].Text)
}

// 部署文件的 vars: 与 vars.yaml 同一条规则：数字按原文。
func TestDeployVarsKeepWrittenText(t *testing.T) {
	var raw map[string]yaml.Node
	require.NoError(t, yaml.Unmarshal([]byte("VER: 1.10\nS: x\n"), &raw))
	m, err := configdir.ParseVarsMap(raw, "deploy.yaml")
	require.NoError(t, err)
	assert.Equal(t, "1.10", m["VER"].Text)
	assert.Equal(t, "x", m["S"].Text)
}

// 空串与 null 的分工：null（KEY: 或 ~）= 没给；必填键写 "" = 骨架留的空位（缺失，有默认值时回落）；
// 可选键写 "" = 明确要一个空串，照样注入（旧版 compose 的回归测试钉住过这条）。
func TestResolveEmptyStringVsNull(t *testing.T) {
	schema := &manifest.ConfigSchema{
		Properties: map[string]manifest.ConfigProperty{
			"REGION":   {Type: "string", Default: "cn-north"},
			"NULLED":   {Type: "string", Default: "fallback"},
			"REQ_DEF":  {Type: "string", Default: "req-default"},
			"REQ_NONE": {Type: "string"},
			"VIA_VAR":  {Type: "string", Default: "info"},
		},
		Required: []string{"REQ_DEF", "REQ_NONE"},
	}
	res, err := configdir.Resolve(configdir.Input{
		ComponentID: "a/b", Version: "1.0.0", Schema: schema,
		File: mustFile(t, "REGION: \"\"\nNULLED:\nREQ_DEF: \"\"\nREQ_NONE: \"\"\nVIA_VAR: $var:EMPTY\n"),
		Vars: mustVars(t, "EMPTY: \"\"\n"),
	})
	require.NoError(t, err)

	region, ok := res.Get("REGION")
	require.True(t, ok, "可选键写空串是明确的值，要注入")
	assert.Equal(t, "", region.Value.Text)
	assert.Equal(t, configdir.OriginFile, region.Origin)

	nulled, _ := res.Get("NULLED")
	assert.Equal(t, "fallback", nulled.Value.Text, "null 等于没写，回落默认值")

	reqDef, _ := res.Get("REQ_DEF")
	assert.Equal(t, "req-default", reqDef.Value.Text, "必填键的空串是空位，有默认值就用默认值")

	assert.Equal(t, []string{"REQ_NONE"}, res.Missing, "必填键的空串且无默认值 = 缺失")

	viaVar, _ := res.Get("VIA_VAR")
	assert.Equal(t, "", viaVar.Value.Text, "经 $var: 引到空串，与直接写空串同一条规则")
}
