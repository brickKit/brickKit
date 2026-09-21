package skills

import (
	"path"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"

	"github.com/brickkit/brickkit/internal/i18n"
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

func skillAssets(lang i18n.Lang) []Asset {
	var out []Asset
	for _, a := range Assets(lang) {
		if path.Base(a.Target) == "SKILL.md" {
			out = append(out, a)
		}
	}
	return out
}

func TestSkillCount(t *testing.T) {
	forEachLang(t, func(t *testing.T, lang i18n.Lang) {
		assert.Len(t, skillAssets(lang), 4, "四个技能，多一个少一个都要先改设计")
	})
}

func TestSkillFrontmatter(t *testing.T) {
	forEachLang(t, func(t *testing.T, lang i18n.Lang) {
		for _, a := range skillAssets(lang) {
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
	})
}

// 技能正文必须真的写了那些「AI 会猜错」的事实。这是内容层唯一能机械校验的
// 部分：漏掉一条，技能就退化成了目录。两种语言各有一份对应的事实清单——
// 语言无关的标识（错误码、环境变量前缀、apiVersion 字符串）两边共用，
// 措辞相关的短语（"follows the layer above it" / "跟着上层走"）各写各的。
func requiredFacts(lang i18n.Lang) map[string][]string {
	langNeutral := map[string][]string{
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
	perLang := map[i18n.Lang]map[string][]string{
		i18n.EN: {
			".claude/skills/brickkit-assemble/SKILL.md": {
				"follows the layer above it", "enabled",
			},
		},
		i18n.ZH: {
			".claude/skills/brickkit-assemble/SKILL.md": {
				"跟着上层走", "enabled",
			},
		},
	}
	out := map[string][]string{}
	for target, facts := range langNeutral {
		out[target] = append(out[target], facts...)
	}
	for target, facts := range perLang[lang] {
		out[target] = append(out[target], facts...)
	}
	return out
}

func TestSkillCoversFactsThatGetGuessedWrong(t *testing.T) {
	forEachLang(t, func(t *testing.T, lang i18n.Lang) {
		byTarget := map[string]Asset{}
		for _, a := range skillAssets(lang) {
			byTarget[a.Target] = a
		}
		for target, facts := range requiredFacts(lang) {
			a, ok := byTarget[target]
			require.True(t, ok, "找不到技能：%s", target)
			b, err := a.Content()
			require.NoError(t, err)
			for _, f := range facts {
				assert.Contains(t, string(b), f,
					"%s 漏了必须覆盖的事实：%s", target, f)
			}
		}
	})
}
