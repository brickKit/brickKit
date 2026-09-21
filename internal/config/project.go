package config

import (
	"fmt"
	"regexp"
	"strings"
	"unicode"

	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/i18n"
	"github.com/brickkit/brickkit/internal/msgid"
	"github.com/brickkit/brickkit/internal/yamlcomment"
)

// 项目名称规则（003 §3.1）：
//   - 全部小写
//   - 只能包含字母、数字、中划线
//   - 不得包含空格
//
// 额外约束：首尾必须是字母或数字。项目名称会用于 K8s namespace
// （brickkit-<项目名>）与 Docker Network（brickkit-<项目名>-net），
// 两者都要求符合 RFC 1123 的 DNS 标签规则。
var projectNameRe = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]*[a-z0-9])?$`)

// MaxProjectNameLen 是项目名称长度上限。
// K8s namespace 最长 63 字符，减去 CLI 添加的 "brickkit-" 前缀（9 字符）。
const MaxProjectNameLen = 54

// ProjectNameRule 是给用户看的命名规则说明。
func ProjectNameRule() string { return i18n.T(msgid.ConfigProjectNameRule) }

// ValidateProjectName 校验项目名称。不合法时返回可直接展示的 *clierr.Error。
func ValidateProjectName(name string) error {
	if strings.TrimSpace(name) == "" {
		return clierr.New(clierr.CodeInvalidArgument, i18n.T(msgid.ConfigProjectNameMissing)).
			WithExit(clierr.ExitUsage)
	}

	reason := projectNameProblem(name)
	if reason == "" {
		return nil
	}

	err := clierr.New(clierr.CodeConfigInvalid, i18n.T(msgid.ConfigProjectNameInvalid)).
		WithDetail(i18n.T(msgid.ConfigLabelProjectName), name).
		WithDetail(i18n.T(msgid.LabelReason), reason).
		WithDetail(i18n.T(msgid.ConfigLabelNamingRule), ProjectNameRule())
	if suggestion := SuggestProjectName(name); suggestion != "" && suggestion != name {
		_ = err.WithHint(i18n.T(msgid.ConfigHintUseName, suggestion))
	}
	return err
}

// projectNameProblem 返回不合法的具体原因；名称合法时返回空字符串。
func projectNameProblem(name string) string {
	if projectNameRe.MatchString(name) {
		if len(name) > MaxProjectNameLen {
			return i18n.T(msgid.ConfigProjectNameTooLong, len(name), MaxProjectNameLen)
		}
		return ""
	}

	switch {
	case strings.ContainsAny(name, " \t"):
		return i18n.T(msgid.ConfigProjectNameHasSpace)
	case strings.HasPrefix(name, "-"), strings.HasSuffix(name, "-"):
		return i18n.T(msgid.ConfigProjectNameEdgeHyphen)
	case hasUpper(name):
		return i18n.T(msgid.ConfigProjectNameHasUpper)
	default:
		return i18n.T(msgid.ConfigProjectNameIllegalChar)
	}
}

func hasUpper(s string) bool {
	for _, r := range s {
		if unicode.IsUpper(r) {
			return true
		}
	}
	return false
}

// SuggestProjectName 把用户输入规范化为一个合法名称，用于错误提示中的"建议"。
// 无法规范化（例如全部是非 ASCII 字符）时返回空字符串。
func SuggestProjectName(name string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(name) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '-', r == '_', r == ' ', r == '.', r == '/':
			b.WriteRune('-')
		}
	}

	// 合并连续中划线并去掉首尾中划线。
	parts := strings.FieldsFunc(b.String(), func(r rune) bool { return r == '-' })
	suggestion := strings.Join(parts, "-")
	if len(suggestion) > MaxProjectNameLen {
		suggestion = strings.TrimRight(suggestion[:MaxProjectNameLen], "-")
	}
	if !projectNameRe.MatchString(suggestion) {
		return ""
	}
	return suggestion
}

// Skeleton 生成 brickkit.yaml 骨架（004 §3.2）。
// fileName 只用于文件头注释，便于多环境配置（如 brickkit.prod.yaml）自解释。
func Skeleton(project, fileName string) []byte {
	if fileName == "" {
		fileName = DefaultConfigFile
	}
	return []byte(fmt.Sprintf(`%s%s
deploy:
  target: docker          # docker | k8s

%ssources:
  - id: local-dev
    type: local
    path: ./%s      # %s
%s  # - id: brickkit-market
  #   type: market
  #   url: https://market.example.com/api/v1

components: []
resources: []
`,
		yamlcomment.Block("", i18n.T(msgid.ConfigSkeletonHeader, fileName)), "project: "+project+"\n",
		yamlcomment.Block("", i18n.T(msgid.ConfigSkeletonSources)),
		DirComponents, i18n.T(msgid.ConfigSkeletonLocalDirNote),
		yamlcomment.Block("  ", i18n.T(msgid.ConfigSkeletonMarketNote))))
}
