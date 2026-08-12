package clierr

import "fmt"

// Problem 是一条校验问题：哪个字段、为什么不合法。
type Problem struct {
	// Field 是字段路径，如 metadata.id、components[0].version。
	Field string
	// Reason 是不合法的原因（面向使用者的中文说明）。
	Reason string
}

// ProblemSet 收集一次校验中的全部问题，最终渲染成一个 *Error。
//
// 设计意图：校验不"遇错即返回"，而是一次报出所有问题，
// 使用者改一轮就能改完（004 §10 的错误可读性要求）。
//
// 典型用法：
//
//	p := clierr.NewProblemSet(clierr.CodeManifestInvalid, "错误：component.yaml 校验失败").
//		WithSource("文件", source).
//		WithHint("参考 002 §2.2")
//	p.Missing("metadata.id")
//	return p.Err()   // 没有问题时返回 nil
type ProblemSet struct {
	code    Code
	message string
	source  *Detail
	hints   []string
	items   []Problem
}

// NewProblemSet 创建一个问题收集器。
func NewProblemSet(code Code, message string) *ProblemSet {
	return &ProblemSet{code: code, message: message}
}

// WithSource 设置渲染在所有问题之前的来源明细行（如 "文件：brickkit.yaml"）。
func (s *ProblemSet) WithSource(key, value string) *ProblemSet {
	s.source = &Detail{Key: key, Value: value}
	return s
}

// WithHint 追加建议。
func (s *ProblemSet) WithHint(hints ...string) *ProblemSet {
	s.hints = append(s.hints, hints...)
	return s
}

// Add 追加一条问题。
func (s *ProblemSet) Add(field, reason string) {
	s.items = append(s.items, Problem{Field: field, Reason: reason})
}

// Addf 追加一条问题（原因支持格式化）。
func (s *ProblemSet) Addf(field, format string, args ...any) {
	s.Add(field, fmt.Sprintf(format, args...))
}

// Missing 追加一条"必填字段缺失"，统一措辞。
func (s *ProblemSet) Missing(field string) {
	s.Add(field, "缺失（必填字段）")
}

// Len 返回已收集的问题数量。
func (s *ProblemSet) Len() int { return len(s.items) }

// Items 返回已收集的问题（只读）。
func (s *ProblemSet) Items() []Problem { return s.items }

// Err 把收集到的问题渲染成 *Error；没有问题时返回 nil。
func (s *ProblemSet) Err() error {
	if len(s.items) == 0 {
		return nil
	}
	e := New(s.code, s.message)
	if s.source != nil {
		e.WithDetail(s.source.Key, s.source.Value)
	}
	for _, item := range s.items {
		e.WithDetail(item.Field, item.Reason)
	}
	return e.WithHint(s.hints...)
}
