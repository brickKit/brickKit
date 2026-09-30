// tocatalog 把 internal/i18n/catalog_<lang>.go 里的 Go map 原样转成 locales/<lang>.yaml：
// key 顺序照旧，文案一律写成双引号纯量。Go map 一行一条、没有注释，所以逐行转换。
// 一次性工具：Task 4 把运行时切到 YAML 之后删掉。
//
// 用法：go run ./tools/i18n/tocatalog en zh
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

var (
	entryRe = regexp.MustCompile(`^\s*msgid\.(\w+)( \+ msgid\.PluralOneSuffix)?:\s*("(?:[^"\\]|\\.)*"),?\s*$`)
	constRe = regexp.MustCompile(`(?m)^\s*(\w+)\s*=\s*"([^"]+)"`)
)

var headers = map[string]string{
	"en": "# BrickKit CLI messages — English. This is the source catalog: every key is declared here first,\n" +
		"# and every other locales/<lang>.yaml has exactly the same keys.\n" +
		"# Each text is written in double quotes (\"…\") or as a literal block (|); %[1]s, %[2]d… are the\n" +
		"# message's arguments, by position. After adding or removing a key here, run: make generate-msgid\n",
	"zh": "# BrickKit CLI 消息——中文。key 与 en.yaml 一一对应；en.yaml 里没有的 key 不要写。\n" +
		"# 文案写在双引号（\"…\"）里，或者写成字面块（|）；%[1]s、%[2]d… 是按位置排的参数，与英文用同一组。\n",
}

func main() {
	keys, err := constants()
	if err == nil {
		for _, lang := range os.Args[1:] {
			if err = convert(lang, keys); err != nil {
				break
			}
		}
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "tocatalog:", err)
		os.Exit(1)
	}
}

// constants 读 internal/msgid 下的常量：Go 名 → key。
func constants() (map[string]string, error) {
	files, err := filepath.Glob("internal/msgid/*.go")
	if err != nil {
		return nil, err
	}
	out := map[string]string{}
	for _, f := range files {
		body, err := os.ReadFile(f)
		if err != nil {
			return nil, err
		}
		for _, m := range constRe.FindAllStringSubmatch(string(body), -1) {
			out[m[1]] = m[2]
		}
	}
	return out, nil
}

func convert(lang string, keys map[string]string) error {
	body, err := os.ReadFile("internal/i18n/catalog_" + lang + ".go")
	if err != nil {
		return err
	}
	var b strings.Builder
	b.WriteString(headers[lang] + "\n")
	n := 0
	blank := false
	for _, line := range strings.Split(string(body), "\n") {
		m := entryRe.FindStringSubmatch(line)
		if m == nil {
			// Go map 里分组用的空行照样留下，译者读 YAML 时分组还在
			blank = blank || (n > 0 && strings.TrimSpace(line) == "")
			continue
		}
		if blank {
			b.WriteString("\n")
			blank = false
		}
		key, ok := keys[m[1]]
		if !ok {
			return fmt.Errorf("%s: msgid.%s is not declared in internal/msgid", lang, m[1])
		}
		if m[2] != "" {
			key += keys["PluralOneSuffix"]
		}
		text, err := strconv.Unquote(m[3])
		if err != nil {
			return fmt.Errorf("%s: %s: %w", lang, key, err)
		}
		quoted := strconv.Quote(text)
		// strconv.Quote 的转义里，YAML 双引号不认的只有 \x.. 与 \U........：遇到就停，别写出读不回来的文件
		if strings.Contains(quoted, `\x`) || strings.Contains(quoted, `\U`) {
			return fmt.Errorf("%s: %s: text needs an escape YAML can't read: %s", lang, key, quoted)
		}
		fmt.Fprintf(&b, "%s: %s\n", key, quoted)
		n++
	}
	if err := os.MkdirAll("internal/i18n/locales", 0o755); err != nil {
		return err
	}
	if err := os.WriteFile("internal/i18n/locales/"+lang+".yaml", []byte(b.String()), 0o644); err != nil {
		return err
	}
	fmt.Printf("%s: %d entries\n", lang, n)
	return nil
}
