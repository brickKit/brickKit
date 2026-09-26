package projfile

import (
	"errors"
	"io/fs"
	"reflect"

	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/envref"
	"github.com/brickkit/brickkit/internal/i18n"
	"github.com/brickkit/brickkit/internal/msgid"
	"github.com/brickkit/brickkit/internal/yamlcheck"
	"github.com/brickkit/brickkit/internal/yamlfile"
)

// ParseFile 读取并解析 brickkit.yaml。文件不存在按"这里不是 BrickKit 项目"报。
func ParseFile(path string) (*File, error) {
	data, err := yamlfile.Read(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, clierr.New(clierr.CodeProjectMissing, i18n.T(msgid.ProjectMissing)).
			WithDetail(i18n.T(msgid.LabelPath), path).
			WithHint(i18n.T(msgid.ProjectMissingHintInit)).
			WithCause(err)
	}
	if err != nil {
		return nil, err
	}
	return Parse(data, path)
}

// Parse 解析并校验 brickkit.yaml：YAML → ${VAR} 展开 → 形状与未知字段 → 解码 → 语义校验。
//
// 这一层没有密钥候选值（密钥都在 config/），所以 ${VAR} 一律在解析时展开。
func Parse(data []byte, source string) (*File, error) {
	if source == "" {
		source = FileName
	}
	doc, err := yamlfile.Document(data, source, false)
	if err != nil {
		return nil, err
	}
	envref.ExpandNode(doc, nil)

	shape := newProblems(source)
	yamlfile.RequireSequence(yamlfile.Lookup(doc, "sources"), "sources", shape)
	yamlfile.RequireSequence(yamlfile.Lookup(doc, "components"), "components", shape)
	yamlcheck.Walk(doc, reflect.TypeOf(File{}), shape)
	if shape.Len() > 0 {
		return nil, shape.Err()
	}

	var f File
	decode := newProblems(source)
	if !yamlfile.Decode(doc, &f, decode) {
		return nil, decode.Err()
	}
	f.Source = source
	if err := f.Validate(); err != nil {
		return nil, err
	}
	return &f, nil
}

func newProblems(source string) *clierr.ProblemSet {
	if source == "" {
		source = FileName
	}
	return clierr.NewProblemSet(clierr.CodeConfigInvalid, i18n.T(msgid.ProblemValidationFailed, FileName)).
		WithSource(i18n.T(msgid.LabelFile), source)
}
