package deploy

import (
	"strings"
	"time"

	"github.com/brickkit/brickkit/internal/i18n"
	"github.com/brickkit/brickkit/internal/msgid"
	"github.com/brickkit/brickkit/internal/yamlcomment"
)

// FileHeader 是 compose.yaml 和 K8s 清单共用的头注释。
//
// 这些文件会被人打开看、被 git 记录，所以要写清楚"这是谁生成的、别手改"。
// 前两行、生成时间、项目名两种目标完全一样，各自特有的行（deploy.target、
// 组件数、文件路径……）由调用方作为 extra 传进来——它们已经是翻译好的整行文字。
func FileHeader(project string, now time.Time, extra ...string) []byte {
	lines := []string{
		i18n.T(msgid.HeaderDoNotEdit),
		i18n.T(msgid.HeaderOverwritten),
		i18n.T(msgid.HeaderGeneratedAt, now.UTC().Format(time.RFC3339)),
		i18n.T(msgid.HeaderProject, project),
	}
	return yamlcomment.Banner(strings.Join(append(lines, extra...), "\n"))
}
