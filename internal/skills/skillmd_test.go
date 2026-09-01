package skills

import (
	"path"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

// specFields 是 Agent Skills 规范允许的六个 frontmatter 字段。
// 只用这六个是为了跨工具可移植：规范外的键在别的分发路径上会报
// Unexpected key(s) in SKILL.md frontmatter。
var specFields = map[string]bool{
	"name": true, "description": true, "allowed-tools": true,
	"metadata": true, "license": true, "compatibility": true,
}

// descriptionMax 是硬上限。skill listing 在 1536 字符处截断（description 与
// when_to_use 合计），留出余量按 1024 判。
const descriptionMax = 1024

func skillAssets() []Asset {
	var out []Asset
	for _, a := range Assets() {
		if path.Base(a.Target) == "SKILL.md" {
			out = append(out, a)
		}
	}
	return out
}

func TestSkillCount(t *testing.T) {
	assert.Len(t, skillAssets(), 4, "四个技能，多一个少一个都要先改设计")
}

func TestSkillFrontmatter(t *testing.T) {
	for _, a := range skillAssets() {
		t.Run(a.Target, func(t *testing.T) {
			b, err := a.Content()
			require.NoError(t, err)
			text := string(b)

			// frontmatter 只在开头的 --- 位于文件第一行时才被解析。
			// 差一个空行，整个文件连 --- 都被当成正文，而且不报错。
			require.True(t, strings.HasPrefix(text, "---\n"),
				"frontmatter 必须从第一行开始")

			front, rest, ok := strings.Cut(text[4:], "\n---\n")
			require.True(t, ok, "frontmatter 没有闭合的 ---")

			var fm map[string]any
			require.NoError(t, yaml.Unmarshal([]byte(front), &fm))

			for k := range fm {
				assert.True(t, specFields[k],
					"frontmatter 用了规范外的字段：%s", k)
			}

			name, _ := fm["name"].(string)
			desc, _ := fm["description"].(string)

			// name 在项目级 skill 里只是显示标签，调用名来自目录名。
			// 两者不一致会让 /brickkit-x 和列表里显示的名字对不上。
			assert.Equal(t, path.Base(path.Dir(a.Target)), name,
				"name 必须与目录名一致")

			require.NotEmpty(t, desc, "description 是加载开关，不能空")
			assert.LessOrEqual(t, len(desc), descriptionMax,
				"description 超出硬上限 %d", descriptionMax)

			assert.NotEmpty(t, strings.TrimSpace(rest), "正文不能空")
		})
	}
}

// 技能正文必须真的写了那些「AI 会猜错」的事实。
// 这是内容层唯一能机械校验的部分：漏掉一条，技能就退化成了目录。
func TestSkillCoversFactsThatGetGuessedWrong(t *testing.T) {
	required := map[string][]string{
		".claude/skills/brickkit-assemble/SKILL.md": {
			"跟着上层走", "enabled",
		},
		".claude/skills/brickkit-component/SKILL.md": {
			"COMPONENT_ID", "_ENDPOINT", "DATABASE_", "REDIS_", "MQ_",
			"STORAGE_", "SEARCH_", "SMTP_",
			"startPeriodSeconds", "brickkit/v1",
		},
		".claude/skills/brickkit-deploy/SKILL.md": {
			"docker", "k8s",
		},
		".claude/skills/brickkit-troubleshoot/SKILL.md": {
			"DEPENDENCY_MISSING", "RESOURCE_UNBOUND",
		},
	}
	byTarget := map[string]Asset{}
	for _, a := range skillAssets() {
		byTarget[a.Target] = a
	}
	for target, facts := range required {
		a, ok := byTarget[target]
		require.True(t, ok, "找不到技能：%s", target)
		b, err := a.Content()
		require.NoError(t, err)
		for _, f := range facts {
			assert.Contains(t, string(b), f,
				"%s 漏了必须覆盖的事实：%s", target, f)
		}
	}
}
