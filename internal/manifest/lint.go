package manifest

import (
	"reflect"

	"gopkg.in/yaml.v3"

	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/i18n"
	"github.com/brickkit/brickkit/internal/msgid"
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

		// 标题不会渲染（这里只取 Items），所以不进目录
		scoped := clierr.NewProblemSet(clierr.CodeManifestInvalid, "unknown fields")
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

	warning := clierr.Warn(clierr.CodeManifestInvalid, i18n.T(msgid.ManifestConfigKeysIgnored)).
		WithDetail(i18n.T(msgid.ManifestLabelOrigin), source)
	for _, problem := range found {
		warning = warning.WithDetail(problem.Field, problem.Reason)
	}
	return []*clierr.Error{warning.
		WithDetail(i18n.T(msgid.LabelImpact), i18n.T(msgid.ManifestConfigKeysIgnoredImpact)).
		WithTip(i18n.T(msgid.ManifestConfigKeysIgnoredTip))}
}
