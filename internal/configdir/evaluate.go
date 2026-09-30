package configdir

import (
	"os"
	"path/filepath"

	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/envref"
	"github.com/brickkit/brickkit/internal/i18n"
	"github.com/brickkit/brickkit/internal/msgid"
)

// Evaluate 把一个值求成最终的字符串（何时调用由渲染器决定）。
//
//	字面量       原样
//	${VAR}       用 lookup 展开（找不到的引用原样保留，生成物里一眼能看出漏配了哪个）
//	file://path  读文件内容，逐字节保留；相对路径按 root 解析。读不到就大声失败
//	existingSecret / $var:  不能求值——前者只能被引用，后者在 Resolve 里已经解开了
func Evaluate(v Value, root string, lookup func(string) (string, bool)) (string, error) {
	switch v.Kind {
	case KindLiteral:
		return v.Text, nil
	case KindEnvTemplate:
		if lookup == nil {
			return v.Text, nil
		}
		return envref.Expand(v.Text, lookup), nil
	case KindFileRef:
		path := v.Path
		if !filepath.IsAbs(path) {
			path = filepath.Join(root, path)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return "", clierr.New(clierr.CodeConfigInvalid, i18n.T(msgid.ConfigdirFileRefMissing, v.String())).
				WithDetail(i18n.T(msgid.LabelPath), path).
				WithDetail(i18n.T(msgid.LabelReason), err.Error()).
				WithHint(i18n.T(msgid.ConfigdirHintFileRef)).
				WithCause(err)
		}
		return string(data), nil
	default:
		return "", clierr.New(clierr.CodeInternal, i18n.T(msgid.ConfigdirCannotEvaluate, v.String())).WithHint(i18n.T(msgid.HintInternalBug))
	}
}
