package manifest

import (
	"reflect"

	"gopkg.in/yaml.v3"

	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/yamlcheck"
)

// PropertyKeyWarnings 找出 configSchema.properties.<key> 声明里不认识的键。
//
// # 为什么单独成一个函数，而不是并进 Parse
//
// yamlcheck.Walk 不往 map 的值里下钻（map 的键是作者自己定的，那里写什么都合法），
// 所以属性声明里的笔误——defualt、descripton——被解析器静默丢掉：组件拿不到默认值，
// 而没有任何东西说一声。这与 Walk 存在的理由是同一类事故。
//
// 但把它做成 Parse 的硬错误会让已发布组件里带着多余键的 Manifest（照着 JSON Schema
// 习惯写的 format、examples……）在新版 CLI 上装不上，而消费方对别人的 Manifest
// 无能为力。所以它是**建议性检查**：只在作者自己听得到、也改得动的地方调用
// （publish、本地安装源扫描），Parse 对同一份文本的行为不变。
//
// 多处笔误合成一条警告，逐条列出。
func PropertyKeyWarnings(raw []byte, source string) []*clierr.Error {
	var doc yaml.Node
	if err := yaml.Unmarshal(raw, &doc); err != nil || len(doc.Content) == 0 {
		return nil
	}
	properties := lookup(doc.Content[0], "configSchema", "properties")
	if properties == nil || properties.Kind != yaml.MappingNode {
		return nil
	}

	var found []clierr.Problem
	for i := 0; i+1 < len(properties.Content); i += 2 {
		name := properties.Content[i].Value

		scoped := clierr.NewProblemSet(clierr.CodeManifestInvalid, "未知字段")
		yamlcheck.Walk(properties.Content[i+1], reflect.TypeOf(ConfigProperty{}), scoped)
		for _, problem := range scoped.Items() {
			found = append(found, clierr.Problem{
				Field:  "configSchema.properties." + name + "." + problem.Field,
				Reason: problem.Reason,
			})
		}
	}
	if len(found) == 0 {
		return nil
	}

	warning := clierr.Warn(clierr.CodeManifestInvalid, "警告：configSchema 里有配置项声明的键不会生效").
		WithDetail("来源", source)
	for _, problem := range found {
		warning = warning.WithDetail(problem.Field, problem.Reason)
	}
	return []*clierr.Error{warning.
		WithDetail("影响", "这些键会被解析器静默丢弃——比如 default 拼错，组件就拿不到默认值").
		WithTip("configSchema 是说明书，每个配置项只认固定的几个键（清单见 component.yaml 字段参考）；JSON Schema 里别的关键字（format、examples……）写了也没有任何效果")}
}
