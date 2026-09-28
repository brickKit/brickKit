package deployfile

import (
	"path/filepath"
	"reflect"

	"gopkg.in/yaml.v3"

	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/envref"
	"github.com/brickkit/brickkit/internal/i18n"
	"github.com/brickkit/brickkit/internal/msgid"
	"github.com/brickkit/brickkit/internal/yamlcheck"
	"github.com/brickkit/brickkit/internal/yamlfile"
)

// ParseFile 读取并解析一份部署文件。文件不存在时原样返回 fs.ErrNotExist——
// 缺的是 deploy.yaml、deploy.local.yaml 还是 -f 给的文件，提示完全不同，由装载器决定。
func ParseFile(path string, role Role) (*File, []*clierr.Error, error) {
	data, err := yamlfile.Read(path)
	if err != nil {
		return nil, nil, err
	}
	return Parse(data, path, role)
}

// Parse 解析并校验部署文件，返回不阻断的警告。
func Parse(data []byte, source string, role Role) (*File, []*clierr.Error, error) {
	doc, err := yamlfile.Document(data, source, false)
	if err != nil {
		return nil, nil, err
	}
	envref.ExpandNode(doc, func(path []string) bool { return len(path) > 0 && path[0] == "vars" })

	shape := newProblems(source)
	checkShapes(doc, shape)
	yamlcheck.Walk(doc, reflect.TypeOf(File{}), shape)
	if shape.Len() > 0 {
		return nil, nil, shape.Err()
	}

	var f File
	decode := newProblems(source)
	if !yamlfile.Decode(doc, &f, decode) {
		return nil, nil, decode.Err()
	}
	f.Source = source
	warnings, err := f.Validate(role)
	if err != nil {
		return nil, nil, err
	}
	return &f, warnings, nil
}

func checkShapes(doc *yaml.Node, p *clierr.ProblemSet) {
	yamlfile.RequireMapping(yamlfile.Lookup(doc, "k8s"), "k8s", p)
	yamlfile.RequireMapping(yamlfile.Lookup(doc, "vars"), "vars", p)
	components := yamlfile.Lookup(doc, "components")
	yamlfile.RequireSequence(components, "components", p)
	if components == nil || components.Kind != yaml.SequenceNode {
		return
	}
	for i, item := range components.Content {
		checkEntryShape(item, yamlfile.Indexed("components", i), p)
	}
}

// checkEntryShape 检查一个顶层条目的形状；外壳条目的 members 下面是成员条目，逐个检查。
func checkEntryShape(item *yaml.Node, field string, p *clierr.ProblemSet) {
	if item.Kind != yaml.MappingNode {
		p.Add(field, i18n.T(msgid.DeployfileComponentMustBeMapping))
		return
	}
	checkFieldShapes(item, field, p)
	members := yamlfile.Lookup(item, "members")
	yamlfile.RequireSequence(members, field+".members", p)
	if members == nil || members.Kind != yaml.SequenceNode {
		return
	}
	for j, member := range members.Content {
		checkMemberShape(member, yamlfile.Indexed(field+".members", j), p)
	}
}

// checkMemberShape 检查外壳下面一个成员条目的形状：字段与顶层条目相同，只是不能再往下嵌。
func checkMemberShape(item *yaml.Node, field string, p *clierr.ProblemSet) {
	if item.Kind != yaml.MappingNode {
		p.Add(field, i18n.T(msgid.DeployfileMemberMustBeMapping))
		return
	}
	checkFieldShapes(item, field, p)
	// 成员下面又写 members：说清"只嵌一层"，并把这个键摘掉，免得未知字段检查再报一遍
	// （只动内存里的文档：出错时它不会被写回任何地方）
	for k := 0; k+1 < len(item.Content); k += 2 {
		if item.Content[k].Value == "members" {
			p.Add(field+".members", i18n.T(msgid.DeployfileMemberNested))
			item.Content = append(item.Content[:k], item.Content[k+2:]...)
			return
		}
	}
}

// checkFieldShapes 检查顶层条目与成员条目共有字段的形状。
func checkFieldShapes(item *yaml.Node, field string, p *clierr.ProblemSet) {
	labels := yamlfile.Lookup(item, "labels")
	yamlfile.RequireMapping(labels, field+".labels", p)
	if labels != nil && labels.Kind == yaml.MappingNode {
		yamlcheck.CheckStringValues(labels, field+".labels", p.Add)
	}
	yamlfile.RequireMapping(yamlfile.Lookup(item, "resources"), field+".resources", p)
}

func newProblems(source string) *clierr.ProblemSet {
	return clierr.NewProblemSet(clierr.CodeConfigInvalid, i18n.T(msgid.ProblemValidationFailed, filepath.Base(source))).
		WithSource(i18n.T(msgid.LabelFile), source).WithHint(i18n.T(msgid.DeployfileHintFieldReference))
}
