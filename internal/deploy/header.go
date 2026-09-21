package deploy

import (
	"bytes"
	"strings"
	"time"

	"github.com/brickkit/brickkit/internal/i18n"
	"github.com/brickkit/brickkit/internal/msgid"
)

const bannerRule = "# ============================================================\n"

// CommentBanner 把 text（可以多行）包成注释块：上下各一条分隔线，每行以 "# " 开头，
// 末尾留一个空行。生成的 YAML / .env 文件开头全用它——排版只在这一处定，
// 文案（含要几行）由各语言的目录自己决定。
func CommentBanner(text string) []byte {
	var b bytes.Buffer
	b.WriteString(bannerRule)
	for _, line := range strings.Split(text, "\n") {
		b.WriteString("# " + line + "\n")
	}
	b.WriteString(bannerRule)
	b.WriteString("\n")
	return b.Bytes()
}

// FileHeader 是 docker-compose.yaml 和 K8s 清单共用的头注释。
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
	return CommentBanner(strings.Join(append(lines, extra...), "\n"))
}
