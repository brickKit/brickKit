package skills

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/brickkit/brickkit/internal/i18n"
)

// 技能资产是写给项目里的 AI 助手的：讲的必须是现在的三层文件模型，
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

// errorCodes 从 clierr.go 里取出全部错误码（真相来源是源码，不另抄一份清单）。
func errorCodes(t *testing.T) []string {
	t.Helper()
	body, err := os.ReadFile(filepath.Join("..", "clierr", "clierr.go"))
	require.NoError(t, err)
	var out []string
	for _, m := range regexp.MustCompile(`(?m)^\s*Code\w+\s+Code\s*=\s*"([A-Z_]+)"`).FindAllStringSubmatch(string(body), -1) {
		out = append(out, m[1])
	}
	require.Greater(t, len(out), 20, "只取到 %d 个错误码——解析坏了，结论不可信", len(out))
	return out
}

// codesNoCommandProduces 是排错技能不必讲的错误码：没有命令会产生它们。
// 码一旦发布就不删（见 clierr.go），所以保留码还在；TestExemptCodesAreReallyUnused 确认它们确实没人用。
var codesNoCommandProduces = map[string]string{
	"NOT_IMPLEMENTED":   "保留码",
	"VERSION_AMBIGUOUS": "保留码：多个版本要指明的情形现在报 INVALID_ARGUMENT",
	"RESOURCE_UNBOUND":  "保留码：\"基础资源\"的概念已经没有了",
}

// 每种语言的排错技能都讲到每一个会出现的错误码：AI 拿到一个 error_code，要能在技能里找到它。
// 两种语言各查各的，所以这条也守着两份技能的覆盖面一样。
func TestTroubleshootSkillCoversEveryErrorCode(t *testing.T) {
	codes := errorCodes(t)
	forEachLang(t, func(t *testing.T, lang i18n.Lang) {
		for _, a := range Assets(lang) {
			if !strings.Contains(a.Target, "brickkit-troubleshoot") {
				continue
			}
			b, err := a.Content()
			require.NoError(t, err)
			for _, code := range codes {
				if _, exempt := codesNoCommandProduces[code]; exempt {
					continue
				}
				if !strings.Contains(string(b), "`"+code+"`") {
					t.Errorf("%s 没讲到错误码 %s", a.Source, code)
				}
			}
		}
	})
}

// 技能里写出来的错误码都真实存在：写错一个，AI 就会拿着一个不存在的码去排错。
// 形如错误码的判据：全大写带下划线、末段与某个真实错误码的末段相同（_MISSING、_FAILED……），
// 这样 DB_HOST、ERP_API_ENDPOINT 这些环境变量名不会被误认。
func TestSkillAssetsNameOnlyRealErrorCodes(t *testing.T) {
	codes := errorCodes(t)
	real, tails := map[string]bool{}, map[string]bool{}
	for _, c := range codes {
		real[c] = true
		if i := strings.LastIndex(c, "_"); i >= 0 {
			tails[c[i:]] = true
		}
	}
	token := regexp.MustCompile("`([A-Z][A-Z]*(?:_[A-Z]+)+)`")
	forEachLang(t, func(t *testing.T, lang i18n.Lang) {
		for _, a := range Assets(lang) {
			b, err := a.Content()
			require.NoError(t, err)
			for _, m := range token.FindAllStringSubmatch(string(b), -1) {
				name := m[1]
				if tails[name[strings.LastIndex(name, "_"):]] && !real[name] {
					t.Errorf("%s 写了一个不存在的错误码 %s", a.Source, name)
				}
			}
		}
	})
}

// 豁免的错误码确实没有生产代码在用：哪天有命令开始产生它，排错技能就得讲它。
func TestExemptCodesAreReallyUnused(t *testing.T) {
	names := map[string]string{} // 常量名 → 码
	body, err := os.ReadFile(filepath.Join("..", "clierr", "clierr.go"))
	require.NoError(t, err)
	for _, m := range regexp.MustCompile(`(?m)^\s*(Code\w+)\s+Code\s*=\s*"([A-Z_]+)"`).FindAllStringSubmatch(string(body), -1) {
		names[m[1]] = m[2]
	}
	err = filepath.WalkDir("..", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") ||
			strings.HasPrefix(filepath.ToSlash(p), "../clierr/") {
			return err
		}
		src, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		for name, code := range names {
			if _, exempt := codesNoCommandProduces[code]; exempt && strings.Contains(string(src), "clierr."+name) {
				t.Errorf("%s 产生了 %s：它不再是没人用的码，把它从 codesNoCommandProduces 里拿掉，并在排错技能里讲它", p, code)
			}
		}
		return nil
	})
	require.NoError(t, err)
}
