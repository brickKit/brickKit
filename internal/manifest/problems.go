package manifest

import (
	"fmt"

	"github.com/brickkit/brickkit/internal/clierr"
)

// Problem 是一条校验问题：哪个字段、为什么不合法。
type Problem struct {
	// Field 是字段路径，如 metadata.id、deployment.extraPorts[0].name。
	Field string
	// Reason 是不合法的原因（面向使用者的中文说明）。
	Reason string
}

// problems 收集一次校验中的全部问题。
//
// 一次报出所有问题，而不是让使用者改一个跑一次（004 §10 的错误可读性要求）。
type problems []Problem

func (p *problems) add(field, reason string) {
	*p = append(*p, Problem{Field: field, Reason: reason})
}

func (p *problems) addf(field, format string, args ...any) {
	p.add(field, fmt.Sprintf(format, args...))
}

// missing 是"必填字段缺失"的统一措辞。
func (p *problems) missing(field string) {
	p.add(field, "缺失（必填字段）")
}

// err 把收集到的问题转换成 CLI 统一错误；没有问题时返回 nil。
func (p problems) err(source string) error {
	if len(p) == 0 {
		return nil
	}
	e := clierr.New(clierr.CodeManifestInvalid, "错误：component.yaml 校验失败").
		WithDetail("文件", source)
	for _, item := range p {
		e.WithDetail(item.Field, item.Reason)
	}
	return e.WithHint(
		"参考 002 组件规范 §2.2 的 Manifest 完整结构",
		"参考附录 B.1 的完整字段参考",
	)
}
