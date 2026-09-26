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
		checkEntryShape(item, yamlfile.Indexed("components", i), msgid.DeployfileComponentMustBeMapping, p)
	}
}

// checkEntryShape 检查一个条目的形状；外壳条目的 members 下面同样是完整条目，递归一层。
func checkEntryShape(item *yaml.Node, field, notMapping string, p *clierr.ProblemSet) {
	if item.Kind != yaml.MappingNode {
		p.Add(field, i18n.T(notMapping))
		return
	}
	labels := yamlfile.Lookup(item, "labels")
	yamlfile.RequireMapping(labels, field+".labels", p)
	if labels != nil && labels.Kind == yaml.MappingNode {
		yamlcheck.CheckStringValues(labels, field+".labels", p.Add)
	}
	yamlfile.RequireMapping(yamlfile.Lookup(item, "resources"), field+".resources", p)
	members := yamlfile.Lookup(item, "members")
	yamlfile.RequireSequence(members, field+".members", p)
	if members == nil || members.Kind != yaml.SequenceNode {
		return
	}
	for j, member := range members.Content {
		checkEntryShape(member, yamlfile.Indexed(field+".members", j), msgid.DeployfileMemberMustBeMapping, p)
	}
}

func newProblems(source string) *clierr.ProblemSet {
	return clierr.NewProblemSet(clierr.CodeConfigInvalid, i18n.T(msgid.ProblemValidationFailed, filepath.Base(source))).
		WithSource(i18n.T(msgid.LabelFile), source)
}
