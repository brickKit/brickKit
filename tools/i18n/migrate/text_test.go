package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestIsCJK(t *testing.T) {
	assert.True(t, isCJK("已添加"))
	assert.True(t, isCJK("、"), "全角标点也是要跟着语言变的用户可见文字")
	assert.True(t, isCJK("a：b"))
	assert.False(t, isCJK("Added %s"))
	assert.False(t, isCJK("⏎ → ✅"), "换行标记与 emoji 不算")
}

func TestPositional(t *testing.T) {
	cases := []struct {
		in    string
		want  string
		count int
		ok    bool
	}{
		{"没有动词", "没有动词", 0, true},
		{"%s 有 %d 个", "%[1]s 有 %[2]d 个", 2, true},
		{"进度 100%%", "进度 100%%", 0, true},
		{"%-36s 活跃", "%-36[1]s 活跃", 1, true},
		{"%5.1f%%", "%5.1[1]f%%", 1, true},
		{"已经是 %[1]s", "已经是 %[1]s", 0, false},
		{"宽度 %*d", "宽度 %*d", 0, false},
	}
	for _, c := range cases {
		got, count, ok := positional(c.in)
		assert.Equal(t, c.ok, ok, c.in)
		if c.ok {
			assert.Equal(t, c.want, got, c.in)
			assert.Equal(t, c.count, count, c.in)
		}
	}
}

func TestJSONStringKeepsAngleBracketsReadable(t *testing.T) {
	assert.Equal(t, `"docker logs <service-name> & more\n"`, jsonString("docker logs <service-name> & more\n"))
	assert.Equal(t, `"中文 \"引号\""`, jsonString(`中文 "引号"`))
}

func TestMakeKeyUsesFirstWordsAndDeduplicates(t *testing.T) {
	reg := &keyRegistry{names: map[string]bool{}, keys: map[string]bool{}}

	name, key := reg.makeKey("cli", "up_upgrade", "Added %[1]s to the config file today", "")
	assert.Equal(t, "CliUpUpgradeAddedToTheConfigFile", name, "前五个单词，占位符不算单词")
	assert.Equal(t, "cli.up.upgrade.added_to_the_config_file", key)

	name2, key2 := reg.makeKey("cli", "up_upgrade", "Added %[1]s to the config file today", "")
	assert.Equal(t, "CliUpUpgradeAddedToTheConfigFile2", name2, "重名加序号")
	assert.Equal(t, "cli.up.upgrade.added_to_the_config_file_2", key2)

	name3, key3 := reg.makeKey("cli", "down", "long text …", "Long")
	assert.Equal(t, "CliDownLong", name3, "命令的 Long / Short / Example 直接用字段名")
	assert.Equal(t, "cli.down.long", key3)

	name4, _ := reg.makeKey("skills", "install", "Failed to read", "")
	assert.Equal(t, "SkillsInstallFailedToRead", name4, "其他包用自己的前缀")

	name5, _ := reg.makeKey("cli", "x", "%[1]s", "")
	assert.Equal(t, "CliXMsg", name5, "一个单词都取不出来时用 Msg")
}
