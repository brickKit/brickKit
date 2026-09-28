package runcmd

import (
	"go/ast"
	"go/parser"
	"go/token"
	"sort"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Reasons() 是手写的清单，CLI 靠它给每个原因配一句翻译好的话。新加一个 Reason
// 常量却忘了登记，那个原因就会绕过翻译守卫，以原始的英文标识出现在用户面前。
// 所以这里从源码里把 Reason 类型的常量全找出来，要求清单一个不多、一个不少。
func TestReasonsListsEveryReasonConstant(t *testing.T) {
	files, err := parser.ParseDir(token.NewFileSet(), ".", nil, 0)
	require.NoError(t, err)

	var declared []string
	for _, pkg := range files {
		for name, file := range pkg.Files {
			if len(name) > 8 && name[len(name)-8:] == "_test.go" {
				continue
			}
			for _, decl := range file.Decls {
				gen, ok := decl.(*ast.GenDecl)
				if !ok || gen.Tok != token.CONST {
					continue
				}
				for _, spec := range gen.Specs {
					vs := spec.(*ast.ValueSpec)
					if id, ok := vs.Type.(*ast.Ident); !ok || id.Name != "Reason" {
						continue
					}
					for _, n := range vs.Values {
						if lit, ok := n.(*ast.BasicLit); ok {
							declared = append(declared, lit.Value[1:len(lit.Value)-1])
						}
					}
				}
			}
		}
	}
	require.NotEmpty(t, declared, "一个 Reason 常量都没找到——源码解析坏了")

	var listed []string
	for _, r := range Reasons() {
		listed = append(listed, string(r))
	}
	sort.Strings(declared)
	sort.Strings(listed)
	assert.Equal(t, declared, listed, "Reasons() 必须正好列出全部 Reason 常量")
}
