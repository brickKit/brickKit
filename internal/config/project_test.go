package config

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"

	"github.com/brickkit/brickkit/internal/clierr"
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
		{"含空格", "my project", clierr.CodeConfigInvalid, "包含空格"},
		{"含制表符", "my\tproject", clierr.CodeConfigInvalid, "包含空格"},
		{"含大写", "MyProject", clierr.CodeConfigInvalid, "全部小写"},
		{"含下划线", "my_project", clierr.CodeConfigInvalid, "包含非法字符"},
		{"含点", "my.project", clierr.CodeConfigInvalid, "包含非法字符"},
		{"含斜杠", "my/project", clierr.CodeConfigInvalid, "包含非法字符"},
		{"含中文", "我的项目", clierr.CodeConfigInvalid, "包含非法字符"},
		{"中划线开头", "-abc", clierr.CodeConfigInvalid, "中划线开头或结尾"},
		{"中划线结尾", "abc-", clierr.CodeConfigInvalid, "中划线开头或结尾"},
		{"超长", strings.Repeat("a", MaxProjectNameLen+1), clierr.CodeConfigInvalid, "长度"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := ValidateProjectName(c.input)
			require.Error(t, err)

			e := clierr.As(err)
			assert.Equal(t, c.wantCode, e.Code)
			assert.Contains(t, e.Format(), "项目名称不合法")
			assert.Contains(t, e.Format(), c.wantReason)
			assert.Contains(t, e.Format(), ProjectNameRule, "错误信息应带命名规则")
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
		assert.Contains(t, e.Format(), "❌ 请指定项目名称：brickkit init <项目名称>")
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

// 004 §3.2：骨架含 project / deploy / components / resources 四个字段。
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

	assert.True(t, strings.HasPrefix(string(raw), "# brickkit.yaml - BrickKit 项目配置\n"))
	assert.True(t, strings.HasSuffix(string(raw), "\n"), "文件应以换行结尾")
}

// 文件头注释使用实际文件名，便于多环境配置自解释。
func TestSkeletonUsesFileNameInHeader(t *testing.T) {
	assert.Contains(t, string(Skeleton("p", "brickkit.prod.yaml")), "# brickkit.prod.yaml - BrickKit 项目配置")
	assert.Contains(t, string(Skeleton("p", "")), "# brickkit.yaml - BrickKit 项目配置")
}
