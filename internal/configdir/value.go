package configdir

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/brickkit/brickkit/internal/envref"
	"github.com/brickkit/brickkit/internal/i18n"
	"github.com/brickkit/brickkit/internal/msgid"
)

// 引用写法的前缀（提案 §7.2.3、§7.4）。
const (
	VarRefPrefix  = "$var:"
	FileRefPrefix = "file://"
)

// Kind 是一个配置值"是什么"：字面量，还是某种引用。
//
// 解析时只认形状、不求值——什么时候求值取决于部署目标（附录 A6/A7），那是 P2 渲染器的事。
type Kind int

const (
	// KindLiteral：写死的值（数字、布尔、列表/映射按 JSON 编码成一行）。
	KindLiteral Kind = iota
	// KindVarRef：$var:NAME，从 deploy 文件的 vars: 或 config/vars.yaml 取。
	KindVarRef
	// KindEnvTemplate：含 ${VAR} 的字符串，从进程环境 / .env 求值。
	KindEnvTemplate
	// KindFileRef：file://path，读本地文件内容（路径相对项目根）。
	KindFileRef
	// KindSecretRef：{ existingSecret, key }，引用集群里已有的 K8s Secret。
	KindSecretRef
)

// Value 是一个配置值。
type Value struct {
	Kind Kind
	// Text：KindLiteral 的值，或 KindEnvTemplate 的原文。
	Text string
	// Name：KindVarRef 引用的变量名。
	Name string
	// Path：KindFileRef 的文件路径。
	Path string
	// SecretName / SecretKey：KindSecretRef 引用的 Secret 与其中的 key。
	SecretName string
	SecretKey  string
}

// IsUnset 表示"没给值"：YAML 的 null 或空串。没给值的键绝不注入空串；
// 必填键没给值就是缺失（骨架里的 KEY: "" 正是这种）。
func (v Value) IsUnset() bool { return v.Kind == KindLiteral && v.Text == "" }

// String 把值还原成使用者写的样子，用于提示。
func (v Value) String() string {
	switch v.Kind {
	case KindVarRef:
		return VarRefPrefix + v.Name
	case KindFileRef:
		return FileRefPrefix + v.Path
	case KindSecretRef:
		return fmt.Sprintf("{existingSecret: %s, key: %s}", v.SecretName, v.SecretKey)
	default:
		return v.Text
	}
}

var nameRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// IsValidName 报告 s 是否是合法的变量名（配置键、公共变量名都用这条规则）。
func IsValidName(s string) bool { return nameRe.MatchString(s) }

// ParseValue 把 YAML 解码出来的原始值归类成 Value。
func ParseValue(raw any) (Value, error) {
	switch v := raw.(type) {
	case nil:
		return literal(""), nil
	case string:
		return parseString(v)
	case bool:
		return literal(strconv.FormatBool(v)), nil
	case int:
		return literal(strconv.Itoa(v)), nil
	case int64:
		return literal(strconv.FormatInt(v, 10)), nil
	case uint64:
		return literal(strconv.FormatUint(v, 10)), nil
	case float64:
		return literal(formatFloat(v)), nil
	case map[string]any:
		if name, key, ok := secretRef(v); ok {
			return Value{Kind: KindSecretRef, SecretName: name, SecretKey: key}, nil
		}
		return jsonLiteral(v)
	default:
		return jsonLiteral(v)
	}
}

func literal(text string) Value { return Value{Kind: KindLiteral, Text: text} }

func parseString(s string) (Value, error) {
	switch {
	case strings.HasPrefix(s, VarRefPrefix):
		name := strings.TrimPrefix(s, VarRefPrefix)
		if !IsValidName(name) {
			return Value{}, errors.New(i18n.T(msgid.ConfigdirVarRefBadName, s))
		}
		return Value{Kind: KindVarRef, Name: name}, nil
	case strings.HasPrefix(s, FileRefPrefix):
		path := strings.TrimPrefix(s, FileRefPrefix)
		if path == "" {
			return Value{}, errors.New(i18n.T(msgid.ConfigdirFileRefEmpty))
		}
		return Value{Kind: KindFileRef, Path: path}, nil
	case envref.Has(s):
		return Value{Kind: KindEnvTemplate, Text: s}, nil
	default:
		return literal(s), nil
	}
}

func secretRef(m map[string]any) (name, key string, ok bool) {
	if len(m) != 2 {
		return "", "", false
	}
	name, nameOK := m["existingSecret"].(string)
	key, keyOK := m["key"].(string)
	if !nameOK || !keyOK || name == "" || key == "" {
		return "", "", false
	}
	return name, key, true
}

// jsonLiteral 把列表 / 映射编码成一行 JSON：环境变量只能是字符串，
// JSON 是组件最容易原样解析回来的写法。
func jsonLiteral(v any) (Value, error) {
	data, err := json.Marshal(v)
	if err != nil {
		return Value{}, errors.New(i18n.T(msgid.ConfigdirValueUnencodable, err.Error()))
	}
	return literal(string(data)), nil
}

// formatFloat 让 YAML 里被解成 float64 的整数不带小数点：20 而不是 20.000000。
func formatFloat(v float64) string {
	if v == float64(int64(v)) {
		return strconv.FormatInt(int64(v), 10)
	}
	return strconv.FormatFloat(v, 'g', -1, 64)
}
