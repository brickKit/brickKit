package deployfile

import (
	"bytes"
	"regexp"
)

var (
	focusLine  = regexp.MustCompile(`(?m)^focus:.*(\n|$)`)
	targetLine = regexp.MustCompile(`(?m)^target:.*\n`)
	commentRun = regexp.MustCompile(`\A(?:#.*\n|\n)*`)
)

// SetFocus 把个人部署文件的 focus: 设成 id（id 为空时删掉这一行），只动这一行：
// 注释、对齐用的空格、其余字段一个字节都不变。按行编辑而不是经过 YAML
// 重新编码——重新编码会改掉 `target: docker          # …` 这种对齐。
// 写在 target: 下面；没有 target: 行时写在文件头注释之后。
func SetFocus(data []byte, id string) ([]byte, error) {
	line := []byte("focus: " + id + "\n")
	if loc := focusLine.FindIndex(data); loc != nil {
		if id == "" {
			return append(append([]byte{}, data[:loc[0]]...), data[loc[1]:]...), nil
		}
		return append(append(append([]byte{}, data[:loc[0]]...), line...), data[loc[1]:]...), nil
	}
	if id == "" {
		return data, nil
	}
	at := len(commentRun.Find(data))
	if loc := targetLine.FindIndex(data); loc != nil {
		at = loc[1]
	}
	return bytes.Join([][]byte{data[:at], line, data[at:]}, nil), nil
}
