package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/brickkit/brickkit/internal/msgid/msgidgen"
)

var (
	wordRe        = regexp.MustCompile(`[A-Za-z][A-Za-z0-9]*`)
	placeholderRe = regexp.MustCompile(`%(?:\[\d+\])?[-+# 0]*\d*(?:\.\d+)?[a-zA-Z]`)
	// yamlEntryRe 是 locales/*.yaml 里的一条：key: "文案"（迁移工具只写、也只读双引号这一种写法）
	yamlEntryRe = regexp.MustCompile(`(?m)^([a-z0-9_.]+):\s*("(?:[^"\\]|\\.)*")\s*$`)
)

// keyRegistry 记着已经用掉的常量名与 key 值，新生成的名字不许与它们冲突。
type keyRegistry struct {
	names map[string]bool // Go 常量名
	keys  map[string]bool // key 字符串值
}

// loadKeyRegistry 读源语言目录 locales/en.yaml，收齐现有的 key 与它们生成的常量名。
func loadKeyRegistry(root string) (*keyRegistry, error) {
	reg := &keyRegistry{names: map[string]bool{}, keys: map[string]bool{}}
	keys, err := msgidgen.SourceKeys(filepath.Join(root, "internal/i18n/locales/en.yaml"))
	if err != nil {
		return nil, err
	}
	for _, k := range keys {
		reg.names[msgidgen.GoName(k)] = true
		reg.keys[k] = true
	}
	return reg, nil
}

// makeKey 由英文文案生成 key 与常量名：取前五个单词，前面拼上包名与文件名，
// 重名时在末尾加序号。命令的 Short / Long / Example 字段直接用字段名，比取词更好认。
// 常量名一律是 msgidgen.GoName(key)——make generate-msgid 生成的正是这个名字。
func (r *keyRegistry) makeKey(pkg, stem, en, field string) (name, key string) {
	words := wordRe.FindAllString(placeholderRe.ReplaceAllString(en, " "), -1)
	if len(words) > 5 {
		words = words[:5]
	}
	if len(words) == 0 {
		words = []string{"Msg"}
	}
	snake := make([]string, len(words))
	for i, w := range words {
		snake[i] = strings.ToLower(w)
	}
	keyBase := pkg + "." + strings.ReplaceAll(stem, "_", ".") + "." + strings.Join(snake, "_")
	if field == "Short" || field == "Long" || field == "Example" {
		keyBase = pkg + "." + strings.ReplaceAll(stem, "_", ".") + "." + strings.ToLower(field)
	}

	key = keyBase
	name = msgidgen.GoName(key)
	for n := 2; r.names[name] || r.keys[key]; n++ {
		key = keyBase + "_" + strconv.Itoa(n)
		name = msgidgen.GoName(key)
	}
	r.names[name], r.keys[key] = true, true
	return name, key
}

// catalogIndex 是"中文文案 → 已有的 (常量名, 英文文案)"的反向索引，
// 用来自动复用已有 key：中文与英文都相同的文案不需要再建一份。
type catalogIndex map[string][]catalogItem

type catalogItem struct{ name, en string }

func loadCatalogIndex(root string) (catalogIndex, error) {
	zh, err := loadCatalog(root, "zh")
	if err != nil {
		return nil, err
	}
	en, err := loadCatalog(root, "en")
	if err != nil {
		return nil, err
	}
	idx := catalogIndex{}
	for name, z := range zh {
		idx[z] = append(idx[z], catalogItem{name, en[name]})
	}
	return idx, nil
}

// loadCatalog 读一份 locales/<lang>.yaml，返回"常量名 → 文案"。
func loadCatalog(root, lang string) (map[string]string, error) {
	body, err := os.ReadFile(filepath.Join(root, "internal/i18n/locales", lang+".yaml"))
	if err != nil {
		return nil, err
	}
	out := map[string]string{}
	for _, m := range yamlEntryRe.FindAllStringSubmatch(string(body), -1) {
		var v string
		if err := json.Unmarshal([]byte(m[2]), &v); err != nil {
			return nil, fmt.Errorf("%s: %w", m[1], err)
		}
		out[msgidgen.GoName(m[1])] = v
	}
	return out, nil
}

// find 返回 zh 对应、且英文也相同的已有常量名；没有返回空串。
func (idx catalogIndex) find(zh, en string) string {
	for _, c := range idx[zh] {
		if c.en == en {
			return c.name
		}
	}
	return ""
}
