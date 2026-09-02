package yamlcheck

// 本文件守着**透传 labels 的值必须是字符串**这一件事（002 §4.7、003 §4.11）。
//
// # 为什么值得一条专门的检查
//
// 这个字段最常见的写法直接来自 Traefik 自己的文档：
//
//	labels:
//	  traefik.enable: "true"
//
// 而人照着敲的时候最容易丢的就是那对引号。丢了以后 YAML 把它读成布尔，
// 于是 `map[string]string` 解码失败，用户拿到的是 yaml 库那句
// `cannot unmarshal !!bool `true` into string`——它说的是 Go 的类型，
// 不是"你少了一对引号"。
//
// Docker 的 labels 与 K8s 的 annotations 两边都只接受字符串，所以这里没有
// "帮他转成字符串"这个选项可选（转了就得决定 `1.0` 该变成 "1" 还是 "1.0"，
// 而那是平台开始解释键值——正是这个字段声明不做的事）。
// 能做的是**把话说对**。

import (
	"strconv"

	"gopkg.in/yaml.v3"
)

// CheckStringValues 检查一个映射节点的每个值都是字符串写法。
//
// path 是这个映射在 YAML 里的位置（如 `deployment.labels`），
// add 收问题。节点不是映射时什么都不做——"必须是映射"由调用方的形状检查报，
// 它那里才有统一的措辞。
func CheckStringValues(node *yaml.Node, path string, add func(field, message string)) {
	if node == nil || node.Kind != yaml.MappingNode {
		return
	}
	for i := 0; i+1 < len(node.Content); i += 2 {
		key, value := node.Content[i], node.Content[i+1]
		if value.Kind != yaml.ScalarNode {
			add(path+"."+key.Value, "值必须是字符串（第 "+strconv.Itoa(value.Line)+" 行）")
			continue
		}
		if value.Tag == "!!null" {
			add(path+"."+key.Value, "值不能为空（第 "+strconv.Itoa(value.Line)+" 行）")
			continue
		}
		// !!str 之外的标量（!!bool / !!int / !!float）都是"少了引号"
		if value.Tag != "" && value.Tag != "!!str" {
			add(path+"."+key.Value,
				"值必须是字符串，加上引号写成 \""+value.Value+"\"（第 "+
					strconv.Itoa(value.Line)+" 行）")
		}
	}
}
