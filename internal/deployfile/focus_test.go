package deployfile

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const personal = "# my notes about this file\n" +
	"target: docker          # docker | podman | k8s\n" +
	"\n" +
	"components:\n" +
	"  # focus: this comment mentions the word and must stay\n" +
	"  - id: erp/api\n" +
	"    localPort: 8081\n" +
	"  - id: erp/db"

// 写焦点只动 focus: 这一行：注释、对齐用的空格、没有结尾换行，一个字节都不变。
func TestSetFocusKeepsEveryOtherByte(t *testing.T) {
	out, err := SetFocus([]byte(personal), "erp/api")
	require.NoError(t, err)
	want := "# my notes about this file\n" +
		"target: docker          # docker | podman | k8s\n" +
		"focus: erp/api\n" +
		personal[len("# my notes about this file\ntarget: docker          # docker | podman | k8s\n"):]
	assert.Equal(t, want, string(out))

	again, err := SetFocus(out, "erp/db")
	require.NoError(t, err)
	assert.Equal(t, strings.Replace(want, "focus: erp/api", "focus: erp/db", 1), string(again))

	cleared, err := SetFocus(again, "")
	require.NoError(t, err)
	assert.Equal(t, personal, string(cleared))
}

// 没有 target: 行时（手改过的文件），焦点写在文件头注释之后的第一行。
func TestSetFocusWithoutATargetLine(t *testing.T) {
	out, err := SetFocus([]byte("# header\ncomponents:\n  - id: erp/api\n"), "erp/api")
	require.NoError(t, err)
	assert.Equal(t, "# header\nfocus: erp/api\ncomponents:\n  - id: erp/api\n", string(out))
}

// 焦点对准的默认版本读出来是 mode: local；debug / disable 按写的来；别的组件不受影响。
func TestEntryAtAppliesTheFocus(t *testing.T) {
	f := &File{Target: TargetDocker, Focus: "erp/api", Components: []Component{
		{Entry: Entry{ID: "erp/api"}}, {Entry: Entry{ID: "erp/db"}},
	}}
	e, ok := f.Entry("erp/api", "1.0.0", true)
	require.True(t, ok)
	assert.Equal(t, ModeLocal, e.Mode)
	e, _ = f.Entry("erp/db", "1.0.0", true)
	assert.Equal(t, "", e.Mode)

	f.Components[0].Mode = ModeDebug
	e, _ = f.Entry("erp/api", "1.0.0", true)
	assert.Equal(t, ModeDebug, e.Mode)

	// 兼容版本（带 requiredBy 的）有自己的条目：焦点不碰它——只有默认版本从源码跑
	f.Components = append(f.Components, Component{Entry: Entry{ID: "erp/api@0.9.0"}})
	e, ok = f.Entry("erp/api", "0.9.0", false)
	require.True(t, ok)
	assert.Equal(t, "", e.Mode, "只有默认版本从源码跑")
}

// 焦点是个人的事：团队文件里写了就拒绝；k8s 上拒绝（集群够不着你的机器）。
func TestFocusValidation(t *testing.T) {
	f := &File{Target: TargetDocker, Focus: "erp/api", Components: []Component{{Entry: Entry{ID: "erp/api"}}}}
	_, err := f.Validate(RoleTeam)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "focus")

	_, err = f.Validate(RoleLocal)
	require.NoError(t, err)

	f.Target = TargetK8s
	_, err = f.Validate(RoleLocal)
	require.Error(t, err)

	f.Target, f.Focus = TargetDocker, "not a component id"
	_, err = f.Validate(RoleLocal)
	require.Error(t, err)
}

// 焦点组件本身写了 localPort（没写 mode）：它按 local 跑，localPort 合法。
func TestFocusEntryMayCarryALocalPort(t *testing.T) {
	f := &File{Target: TargetDocker, Focus: "erp/api", Components: []Component{{Entry: Entry{ID: "erp/api", LocalPort: 8081}}}}
	_, err := f.Validate(RoleLocal)
	require.NoError(t, err)
}

// 兼容版本的条目（erp/api@0.9.0）不从源码跑：它写 localPort 照样是错，焦点不替它开这个口子。
func TestFocusDoesNotLicenseALocalPortOnAnotherVersion(t *testing.T) {
	f := &File{Target: TargetDocker, Focus: "erp/api", Components: []Component{
		{Entry: Entry{ID: "erp/api"}},
		{Entry: Entry{ID: "erp/api@0.9.0", LocalPort: 8082}},
	}}
	_, err := f.Validate(RoleLocal)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "localPort")
}
