package main

import (
	"bytes"
	"encoding/json"
	"regexp"
	"strconv"
	"strings"
	"unicode"
)

// isCJK 判断字符串里有没有中文字符（汉字，加中日韩标点与全角形式）。
//
// 全角标点也算：`"、"`、`"："` 这类单独的分隔符同样是要跟着语言变的用户可见文字。
func isCJK(s string) bool {
	for _, r := range s {
		if unicode.Is(unicode.Han, r) || (r >= 0x3000 && r <= 0x303f) || (r >= 0xff00 && r <= 0xffef) {
			return true
		}
	}
	return false
}

// verbRe 匹配一个 fmt 动词：`%%`、已经带位置的 `%[1]s`，或者 `%-12s`、`%5.1f` 这类。
var verbRe = regexp.MustCompile(`%(?:%|\[\d+\]|[-+# 0]*(?:\d+|\*)?(?:\.(?:\d+|\*))?[a-zA-Z])`)

// positional 把顺序动词改成位置动词（`%s` → `%[1]s`），返回新文本与动词个数。
//
// 目录里的动词一律是位置动词，因为中英文语序经常不同。遇到 `*` 宽度或已经带 `[n]`
// 的动词时 ok 为 false——那种要人来处理。`%%` 原样保留、不计数。
func positional(s string) (text string, count int, ok bool) {
	ok = true
	out := verbRe.ReplaceAllStringFunc(s, func(m string) string {
		if m == "%%" {
			return m
		}
		if strings.ContainsAny(m, "*[") {
			ok = false
			return m
		}
		count++
		i := len(m) - 1
		return m[:i] + "[" + strconv.Itoa(count) + "]" + m[i:]
	})
	return out, count, ok
}

// jsonString 把 s 写成 Go 目录里用的双引号字面量。
//
// 用 json 编码（Go 字符串转义规则与之兼容），但关掉 HTML 转义——否则 `<`、`>`、`&`
// 会变成 < 之类，目录里的命令示例（`<服务名>`）就没法读了。
func jsonString(s string) string {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(s) // 编码字符串不会失败
	return strings.TrimRight(b.String(), "\n")
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}
