package msgidgen

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGoNameFollowsGoInitialisms(t *testing.T) {
	assert.Equal(t, "ManifestIDTooLong", GoName("manifest.id_too_long"))
	assert.Equal(t, "EngineHintKubectlJSON", GoName("engine.hint_kubectl_json"))
	assert.Equal(t, "CliUpErrorFailedToWriteThe2", GoName("cli.up.error_failed_to_write_the_2"))
	assert.Equal(t, "ManifestNotValidYAML", GoName("manifest.not_valid_yaml"))
}

// 两个 key 推出同一个 Go 名字时，生成必须失败，而不是写出一份编译不过（或悄悄少一条）的文件。
func TestGoNamesAreUnique(t *testing.T) {
	_, err := Render([]string{"a.b_c", "a_b.c"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "ABC")
}

// 单数形式（.one）不生成常量：代码里写的是 msgid.X + msgid.PluralOneSuffix。
func TestRenderSkipsPluralOneKeys(t *testing.T) {
	out, err := Render([]string{"count.files", "count.files.one"})
	require.NoError(t, err)
	assert.Contains(t, string(out), `CountFiles ID = "count.files"`)
	assert.NotContains(t, string(out), "count.files.one")
}

// 签入的 messages_gen.go 必须就是 en.yaml 生成的那一份：忘了 make generate-msgid 就在 lint 里拦下。
func TestGeneratedMsgidIsCurrent(t *testing.T) {
	keys, err := SourceKeys("../../i18n/locales/en.yaml")
	require.NoError(t, err)
	want, err := Render(keys)
	require.NoError(t, err)
	got, err := os.ReadFile("../messages_gen.go")
	require.NoError(t, err)
	// 不用 assert.Equal：它会把上千行的整份文件打进失败信息，把真正要看的那句提示淹掉
	if string(want) != string(got) {
		t.Fatal("internal/msgid/messages_gen.go does not match internal/i18n/locales/en.yaml; run: make generate-msgid")
	}
}
