// Package yamlcomment 生成 YAML / .env / .gitignore 这类文件里的多行注释块。
//
// 平台写出的文件（brickkit.yaml 骨架、component.yaml 骨架、docker-compose.yaml、
// local-debug.*.env）开头都带说明注释，而说明文字要跟着语言变——各语言要几行由
// 消息目录自己决定，所以这里按行拆开加前缀，而不是让调用方写死行数。排版只在这一处定义。
package yamlcomment

import (
	"bytes"
	"strings"
)

const rule = "# ============================================================\n"

// Block 把 text（可以多行）变成注释：每行前面加 indent 和 "# "，末尾换行。
func Block(indent, text string) string {
	var b strings.Builder
	for _, line := range strings.Split(text, "\n") {
		b.WriteString(indent + "# " + line + "\n")
	}
	return b.String()
}

// Banner 把 text 包成头注释：上下各一条分隔线，末尾留一个空行。
func Banner(text string) []byte {
	var b bytes.Buffer
	b.WriteString(rule)
	b.WriteString(Block("", text))
	b.WriteString(rule)
	b.WriteString("\n")
	return b.Bytes()
}
