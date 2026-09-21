package config

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"

	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/i18n"
)

// 003 §3.1 合法名称：全部小写、字母数字中划线。
func TestValidateProjectNameAccepts(t *testing.T) {
	for _, name := range []string{
		"a", "123", "my-project", "project123", "my-erp-dev", "a1-b2-c3",
		strings.Repeat("a", MaxProjectNameLen),
	} {
		t.Run(name, func(t *testing.T) {
			assert.NoError(t, ValidateProjectName(name))
		})
	}
}

func TestValidateProjectNameRejects(t *testing.T) {
	cases := []struct {
		name       string
		input      string
		wantCode   clierr.Code
		wantReason string
	}{
		{"含空格", "my project", clierr.CodeConfigInvalid, "contains spaces"},
		{"含制表符", "my\tproject", clierr.CodeConfigInvalid, "contains spaces"},
		{"含大写", "MyProject", clierr.CodeConfigInvalid, "all lowercase"},
		{"含下划线", "my_project", clierr.CodeConfigInvalid, "contains illegal characters"},
		{"含点", "my.project", clierr.CodeConfigInvalid, "contains illegal characters"},
		{"含斜杠", "my/project", clierr.CodeConfigInvalid, "contains illegal characters"},
		{"含中文", "我的项目", clierr.CodeConfigInvalid, "contains illegal characters"},
		{"中划线开头", "-abc", clierr.CodeConfigInvalid, "start or end with a hyphen"},
		{"中划线结尾", "abc-", clierr.CodeConfigInvalid, "start or end with a hyphen"},
		{"超长", strings.Repeat("a", MaxProjectNameLen+1), clierr.CodeConfigInvalid, "length"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := ValidateProjectName(c.input)
			require.Error(t, err)

			e := clierr.As(err)
			assert.Equal(t, c.wantCode, e.Code)
			assert.Contains(t, e.Format(), "invalid project name")
			assert.Contains(t, e.Format(), c.wantReason)
			assert.Contains(t, e.Format(), ProjectNameRule(), "错误信息应带命名规则")
		})
	}
}

// 空名称属于用法错误（退出码 2），文案与开发计划 3.1 一致。
func TestValidateProjectNameEmpty(t *testing.T) {
	for _, input := range []string{"", "   "} {
		err := ValidateProjectName(input)
		require.Error(t, err)

		e := clierr.As(err)
		assert.Equal(t, clierr.CodeInvalidArgument, e.Code)
		assert.Equal(t, clierr.ExitUsage, e.ExitCode())
		assert.Contains(t, e.Format(), "❌ Please specify a project name: brickkit init <project-name>")
	}
}

func TestSuggestProjectName(t *testing.T) {
	cases := map[string]string{
		"MyProject":                        "myproject",
		"my project":                       "my-project",
		"my_project":                       "my-project",
		"My Project Name":                  "my-project-name",
		"-abc-":                            "abc",
		"my..project":                      "my-project",
		"my/project":                       "my-project",
		"我的项目":                             "",
		"我的-项目":                            "",
		strings.Repeat("a", 60):            strings.Repeat("a", MaxProjectNameLen),
		strings.Repeat("a", 54) + "-extra": strings.Repeat("a", MaxProjectNameLen),
	}
	for in, want := range cases {
		t.Run(in, func(t *testing.T) {
			assert.Equal(t, want, SuggestProjectName(in))
		})
	}
}

// 建议名称本身必须是合法名称，否则提示会误导用户。
func TestSuggestProjectNameAlwaysValidOrEmpty(t *testing.T) {
	for _, in := range []string{"MyProject", "my project", "___", "我的项目", strings.Repeat("A", 80)} {
		s := SuggestProjectName(in)
		if s == "" {
			continue
		}
		assert.NoError(t, ValidateProjectName(s), "建议名称 %q 必须合法", s)
	}
}

// 004 §3.2：骨架含 project / deploy / sources / components / resources 五个字段。
func TestSkeleton(t *testing.T) {
	raw := Skeleton("my-project", DefaultConfigFile)

	var doc map[string]any
	require.NoError(t, yaml.Unmarshal(raw, &doc))
	assert.Equal(t, "my-project", doc["project"])
	assert.Equal(t, map[string]any{"target": "docker"}, doc["deploy"])
	assert.Contains(t, doc, "components")
	assert.Contains(t, doc, "resources")
	assert.Empty(t, doc["components"])
	assert.Empty(t, doc["resources"])

	assert.True(t, strings.HasPrefix(string(raw), "# brickkit.yaml - BrickKit project config\n"))
	assert.True(t, strings.HasSuffix(string(raw), "\n"), "文件应以换行结尾")
}

// 骨架自带一个指向 ./components 的本地安装源。
//
// init 本来就会创建 components/（Layout.ManagedDirs），却不把它登记成安装源——
// 于是每个新项目都得先手工补一段 sources 才能做任何事。
func TestSkeletonShipsDefaultLocalSource(t *testing.T) {
	raw := Skeleton("my-project", DefaultConfigFile)

	cfg, err := ParseConfig(raw, "brickkit.yaml")
	require.NoError(t, err, "骨架本身必须是合法配置")

	require.Len(t, cfg.Sources, 1, "只自带本地源：CLI 猜不到你的市场地址")
	src := cfg.Sources[0]
	assert.Equal(t, "local-dev", src.ID)
	assert.Equal(t, SourceTypeLocal, src.Type)
	assert.Equal(t, "./"+DirComponents, src.Path)
	assert.True(t, src.IsEnabled())
}

// 市场源以注释形式给出：CLI 猜不到地址，但把字段形状摆在那儿比让人翻文档强。
func TestSkeletonMentionsMarketSourceAsComment(t *testing.T) {
	raw := string(Skeleton("my-project", DefaultConfigFile))

	assert.Contains(t, raw, "# - id: brickkit-market")
	assert.Contains(t, raw, "#   type: market")
}

// 骨架里的本地源路径必须与 init 真正创建的目录一致，否则一上来就是死的。
func TestSkeletonLocalSourcePointsAtManagedDir(t *testing.T) {
	l := NewLayout("/projects/erp", "")
	cfg, err := ParseConfig(Skeleton("erp", DefaultConfigFile), "brickkit.yaml")
	require.NoError(t, err)

	assert.Contains(t, l.ManagedDirs(), l.ComponentsDir(),
		"init 应当创建 components/")
	require.Len(t, cfg.Sources, 1)
	assert.Equal(t, "./"+DirComponents, cfg.Sources[0].Path)
}

// 文件头注释使用实际文件名，便于多环境配置自解释。
func TestSkeletonUsesFileNameInHeader(t *testing.T) {
	assert.Contains(t, string(Skeleton("p", "brickkit.prod.yaml")), "# brickkit.prod.yaml - BrickKit project config")
	assert.Contains(t, string(Skeleton("p", "")), "# brickkit.yaml - BrickKit project config")
}

// brickkit.yaml 骨架里的说明注释随语言变；结构（键、值）不随语言变，
// 两种语言生成的文件解析出来必须是同一份配置。
func TestSkeletonFollowsLanguageButNotStructure(t *testing.T) {
	prev := i18n.Current()
	t.Cleanup(func() { i18n.SetCurrent(prev) })

	i18n.SetCurrent(i18n.ZH)
	zh := Skeleton("demo", DefaultConfigFile)
	i18n.SetCurrent(i18n.EN)
	en := Skeleton("demo", DefaultConfigFile)

	assert.Contains(t, string(zh), "# 安装源：按声明顺序依次尝试，前一个找不到就试下一个\nsources:")
	assert.Contains(t, string(en), "# Install sources: tried in declaration order")
	assert.NotRegexp(t, "[一-鿿]", string(en), "英文骨架里不该夹中文")

	var zhDoc, enDoc map[string]any
	require.NoError(t, yaml.Unmarshal(zh, &zhDoc))
	require.NoError(t, yaml.Unmarshal(en, &enDoc))
	assert.Equal(t, zhDoc, enDoc)
}
