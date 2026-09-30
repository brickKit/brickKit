// Package yamlcomment 生成 YAML / .env / .gitignore 这类文件里的多行注释块。
//
// 平台写出的文件（brickkit.yaml 骨架、component.yaml 骨架、compose.yaml、
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
		if line == "" {
			b.WriteString(indent + "#\n") // 空行不留行尾空格
			continue
		}
		b.WriteString(indent + "# " + line + "\n")
	}
	return b.String()
}

// Section 是一行分节标题：# === title ===。"===" 由这里加、不进译文——读文件的一方
// （配置迁移）靠这个形状认出生成的标题，不靠标题的文字，译文怎么改都认得出来。
func Section(indent, title string) string {
	return indent + "# === " + title + " ===\n"
}

// IsSection 报告 line 是不是 Section 写出的分节标题，只看形状，不看文字。
func IsSection(line string) bool {
	t := strings.TrimSpace(line)
	return strings.HasPrefix(t, "# === ") && strings.HasSuffix(t, " ===")
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
