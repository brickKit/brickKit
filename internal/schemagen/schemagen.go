package schemagen

import (
	"reflect"

	"github.com/brickkit/brickkit/internal/config"
	"github.com/brickkit/brickkit/internal/manifest"
)

// 落盘的文件名（在仓库根目录的 schemas/ 下）。
//
// 这两个名字是使用者编辑器里 $schema 注释指向的公开地址的一部分，改名会让所有已经配好的
// 编辑器悄悄失效。
const (
	ComponentFile = "component.schema.json"
	ProjectFile   = "brickkit.schema.json"
)

// overrides 是"类型 → 手写 schema"的覆盖表：yaml.v3 不按字段反射来解码的类型。
//
// 生成器遇到这样的类型却不在表里会直接报错（见包注释），schemas_test.go 里的
// TestEveryUnmarshalerTypeIsCoveredByAnOverride 再从两个方向查一遍：该在表里的都在，
// 表里的都还真有自定义解码。
func overrides() map[reflect.Type]func() schema {
	return map[reflect.Type]func() schema{
		reflect.TypeOf(manifest.ComponentDep{}): componentDepSchema,
	}
}

// componentDepSchema：依赖项有两种写法（见 manifest.ComponentDep.UnmarshalYAML）——
// 字符串 department/tree@1.0.0，或映射 {id, optional}。版本必须精确：不接受 ^ ~ 范围
// （AGENTS §9.2），与 manifest.Validate 一致。
//
// 组件 ID 的细则（全小写、字符集、63 字符的长度上限，见 manifest.componentIDProblem）不放进来：
// 长度没法用一个 pattern 表达，只抄一半反而多出一处要同步的真相；而 schema 宁可松也不能比校验器
// 更严。这里只钉"<id>@<精确版本>"这个骨架：id 非空、不含 @ 与空格，版本是三段数字。
//
// 改 ComponentDep 的写法（多一种形式、多一个键）要同步改这里。schemas_test.go 会拿真实的
// manifest.Parse 与 yamlcheck.KnownFields 核对其中的版本 pattern、id 必填、optional 可缺省、
// 映射写法认的键集合；核对不到的是新增的写法本身，那要改 UnmarshalYAML 的人自己想起来这里。
func componentDepSchema() schema {
	const ref = "^[^@ ]+@[0-9]+[.][0-9]+[.][0-9]+$"
	return schema{"oneOf": []any{
		schema{"type": "string", "pattern": ref},
		schema{
			"type": "object",
			"properties": map[string]any{
				"id":       schema{"type": "string", "pattern": ref},
				"optional": schema{"type": "boolean"},
			},
			"required":             []string{"id"},
			"additionalProperties": false,
		},
	}}
}

// Component 返回 component.yaml 的 JSON Schema。
func Component() ([]byte, error) {
	return newGenerator(overrides()).document(reflect.TypeOf(manifest.Manifest{}), "BrickKit component.yaml")
}

// Project 返回 brickkit.yaml 的 JSON Schema。
func Project() ([]byte, error) {
	return newGenerator(overrides()).document(reflect.TypeOf(config.Config{}), "BrickKit brickkit.yaml")
}

// Files 返回要落盘的全部 schema：文件名 → 内容。落盘工具（cmd/gen-schemas）与防漂移测试共用它，
// 以后多一份 schema 只改这一处。
func Files() (map[string][]byte, error) {
	component, err := Component()
	if err != nil {
		return nil, err
	}
	project, err := Project()
	if err != nil {
		return nil, err
	}
	return map[string][]byte{ComponentFile: component, ProjectFile: project}, nil
}
