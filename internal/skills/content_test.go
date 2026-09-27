package skills

import (
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/brickkit/brickkit/internal/i18n"
)

// 技能资产是写给项目里的 AI 助手的：讲的必须是现在的三层文件模型（提案 §4），
// 已经删掉的概念留在里面，AI 就会照着去写一个 CLI 根本不认的文件。
var removedConcepts = []*regexp.Regexp{
	regexp.MustCompile(`servedBy`),
	regexp.MustCompile(`override\.yaml`),
	regexp.MustCompile(`brickkit override`),
	regexp.MustCompile(`publish --path`),
	regexp.MustCompile(`(?m)^\s*resources:\s*$`),
	regexp.MustCompile(`dependencies\.resources`),
	regexp.MustCompile(`--config `),
	regexp.MustCompile(`--ignore-served-by`),
	regexp.MustCompile(`deploy\.target`),
	regexp.MustCompile(`envPrefix`),
}

func TestSkillAssetsHaveNoRemovedConcepts(t *testing.T) {
	forEachLang(t, func(t *testing.T, lang i18n.Lang) {
		for _, a := range Assets(lang) {
			b, err := a.Content()
			require.NoError(t, err)
			for _, re := range removedConcepts {
				assert.False(t, re.Match(b), "%s 还在讲已删除的概念 %q", a.Source, re.String())
			}
		}
	})
}

// 项目范围的资产（导读与三个项目技能）都要讲到三层文件与本地模式：
// 那是 AI 在项目里最先要知道的事。组件技能讲的是 component.yaml，不在此列。
func TestProjectSkillAssetsNameTheThreeLayers(t *testing.T) {
	forEachLang(t, func(t *testing.T, lang i18n.Lang) {
		for _, a := range Assets(lang) {
			if strings.Contains(a.Target, "brickkit-component") {
				continue
			}
			b, err := a.Content()
			require.NoError(t, err)
			for _, want := range []string{"brickkit.yaml", "deploy.yaml", "config/", "deploy.local.yaml"} {
				assert.Contains(t, string(b), want, "%s 没讲到 %s", a.Source, want)
			}
		}
	})
}
