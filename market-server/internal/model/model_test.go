// 本文件是 Step 18-A 的代码层单测：依赖的三种写法、错误结构与版本状态判定。
package model

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 002 §3.2 的标量写法、映射写法，以及 007 §3.7 的 id + version 分列写法，都要认。
func TestComponentDepUnmarshalAllForms(t *testing.T) {
	cases := []struct {
		name  string
		input string
		want  ComponentDep
	}{
		{
			name:  "标量强依赖",
			input: `"department/tree@1.0.0"`,
			want:  ComponentDep{ID: "department/tree", Version: "1.0.0", Ref: "department/tree@1.0.0"},
		},
		{
			name:  "映射弱依赖（版本写在 id 里）",
			input: `{"id":"infra/redis-event-bus@1.0.0","optional":true}`,
			want: ComponentDep{
				ID: "infra/redis-event-bus", Version: "1.0.0", Optional: true,
				Ref: "infra/redis-event-bus@1.0.0",
			},
		},
		{
			name:  "映射（版本单独字段，007 §3.7）",
			input: `{"id":"department/tree","version":"1.0.0"}`,
			want:  ComponentDep{ID: "department/tree", Version: "1.0.0", Ref: "department/tree@1.0.0"},
		},
		{
			name:  "只有 id、没有版本",
			input: `{"id":"department/tree"}`,
			want:  ComponentDep{ID: "department/tree", Ref: "department/tree"},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var got ComponentDep
			require.NoError(t, json.Unmarshal([]byte(c.input), &got))
			assert.Equal(t, c.want, got)
		})
	}
}

func TestComponentDepUnmarshalRejectsGarbage(t *testing.T) {
	var d ComponentDep
	assert.Error(t, json.Unmarshal([]byte(`123`), &d))
}

// 对外输出统一成映射形式，查询接口的消费方不必再处理两种写法。
func TestComponentDepMarshal(t *testing.T) {
	out, err := json.Marshal(ComponentDep{ID: "people/basic", Version: "1.0.0", Optional: true})
	require.NoError(t, err)
	assert.JSONEq(t, `{"id":"people/basic","version":"1.0.0","optional":true}`, string(out))

	out, err = json.Marshal(ComponentDep{ID: "people/basic", Version: "1.0.0"})
	require.NoError(t, err)
	assert.JSONEq(t, `{"id":"people/basic","version":"1.0.0"}`, string(out))
}

func TestArtifactIsContainer(t *testing.T) {
	assert.True(t, Artifact{Type: ArtifactTypeContainer}.IsContainer())
	assert.False(t, Artifact{Type: ArtifactTypeAPIContract}.IsContainer())
}

func TestAPIError(t *testing.T) {
	e := Errorf(CodeManifestInvalid, "Manifest 校验失败").
		WithDetail("componentId", "people/basic").
		WithDetail("version", "1.0.0")

	assert.Equal(t, "MANIFEST_INVALID: Manifest 校验失败", e.Error())
	assert.Equal(t, "people/basic", e.Details["componentId"])
	assert.Equal(t, "1.0.0", e.Details["version"])

	// 序列化后仍是 007 §18 约定的形状
	out, err := json.Marshal(e)
	require.NoError(t, err)
	assert.Contains(t, string(out), `"code":"MANIFEST_INVALID"`)
	assert.Contains(t, string(out), `"details"`)
}

// 007 §6：blocked 不能安装；deleted 视同不存在；deprecated 可以安装但要提示风险。
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
