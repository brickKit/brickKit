package config

import (
	"fmt"
	"regexp"
	"strings"
	"unicode"

	"github.com/brickkit/brickkit/internal/clierr"
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
const ProjectNameRule = "只能包含小写字母、数字与中划线，且以字母或数字开头结尾"

// ValidateProjectName 校验项目名称。不合法时返回可直接展示的 *clierr.Error。
func ValidateProjectName(name string) error {
	if strings.TrimSpace(name) == "" {
		return clierr.New(clierr.CodeInvalidArgument, "请指定项目名称：brickkit init <项目名称>").
			WithExit(clierr.ExitUsage)
	}

	reason := projectNameProblem(name)
	if reason == "" {
		return nil
	}

	err := clierr.New(clierr.CodeConfigInvalid, "错误：项目名称不合法").
		WithDetail("项目名称", name).
		WithDetail("原因", reason).
		WithDetail("命名规则", ProjectNameRule)
	if suggestion := SuggestProjectName(name); suggestion != "" && suggestion != name {
		err.WithHint(fmt.Sprintf("改用 %s", suggestion))
	}
	return err
}

// projectNameProblem 返回不合法的具体原因；名称合法时返回空字符串。
func projectNameProblem(name string) string {
	if projectNameRe.MatchString(name) {
		if len(name) > MaxProjectNameLen {
			return fmt.Sprintf("长度 %d 超过上限 %d（K8s namespace 限制）", len(name), MaxProjectNameLen)
		}
		return ""
	}

	switch {
	case strings.ContainsAny(name, " \t"):
		return "包含空格"
	case strings.HasPrefix(name, "-"), strings.HasSuffix(name, "-"):
		return "不能以中划线开头或结尾"
	case hasUpper(name):
		return "包含大写字母（项目名称必须全部小写）"
	default:
		return "包含非法字符（仅允许小写字母、数字与中划线）"
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
	return []byte(fmt.Sprintf(`# %s - BrickKit 项目配置
# 由 brickkit init 生成，由用户编辑，由 CLI 读取
project: %s

deploy:
  target: docker          # docker | k8s

# 安装源：按声明顺序依次尝试，前一个找不到就试下一个（003 §6.5）
sources:
  - id: local-dev
    type: local
    path: ./%s      # brickkit init 已经建好这个目录
  # 组件市场按需取消注释并填上地址：
  # - id: brickkit-market
  #   type: market
  #   url: https://market.example.com/api/v1

components: []
resources: []
`, fileName, project, DirComponents))
}
