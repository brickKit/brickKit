// 本文件是 model 的代码层单测：错误结构与版本状态判定。
package model

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAPIError(t *testing.T) {
	e := Errorf(CodeManifestInvalid, "Manifest 校验失败").
		WithDetail("componentId", "people/basic").
		WithDetail("version", "1.0.0")

	assert.Equal(t, "MANIFEST_INVALID: Manifest 校验失败", e.Error())
	assert.Equal(t, "people/basic", e.Details["componentId"])
	assert.Equal(t, "1.0.0", e.Details["version"])

	// 序列化后仍是 API 约定的形状
	out, err := json.Marshal(e)
	require.NoError(t, err)
	assert.Contains(t, string(out), `"code":"MANIFEST_INVALID"`)
	assert.Contains(t, string(out), `"details"`)
}

// blocked 不能安装；deleted 视同不存在；deprecated 可以安装但要提示风险。
func TestVersionInstallable(t *testing.T) {
	cases := map[string]bool{
		VersionStable:     true,
		VersionDeprecated: true,
		VersionDraft:      false,
		VersionBlocked:    false,
		VersionDeleted:    false,
	}
	for status, want := range cases {
		assert.Equal(t, want, (&Version{Status: status}).Installable(), "状态 %s", status)
	}
}
