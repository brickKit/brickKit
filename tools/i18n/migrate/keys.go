package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

var (
	wordRe        = regexp.MustCompile(`[A-Za-z][A-Za-z0-9]*`)
	placeholderRe = regexp.MustCompile(`%(?:\[\d+\])?[-+# 0]*\d*(?:\.\d+)?[a-zA-Z]`)
	constRe       = regexp.MustCompile(`(?m)^\s*([A-Z][A-Za-z0-9]*)\s*=\s*"([^"]*)"`)
	catalogRe     = regexp.MustCompile(`(?m)^\s*msgid\.(\w+):\s*("(?:[^"\\]|\\.)*"),?\s*$`)
)

// keyRegistry 记着已经用掉的常量名与 key 值，新生成的名字不许与它们冲突。
type keyRegistry struct {
	names map[string]bool // Go 常量名
	keys  map[string]bool // key 字符串值
}

// loadKeyRegistry 读 internal/msgid/*.go，收齐现有的常量名与 key 值。
func loadKeyRegistry(root string) (*keyRegistry, error) {
	reg := &keyRegistry{names: map[string]bool{}, keys: map[string]bool{}}
	files, err := filepath.Glob(filepath.Join(root, "internal/msgid/*.go"))
	if err != nil {
		return nil, err
	}
	for _, f := range files {
		body, err := os.ReadFile(f)
		if err != nil {
			return nil, err
		}
		for _, m := range constRe.FindAllStringSubmatch(string(body), -1) {
			reg.names[m[1]] = true
			reg.keys[m[2]] = true
		}
	}
	return reg, nil
}

// makeKey 由英文文案生成常量名与 key 值：取前五个单词，前面拼上包名与文件名，
// 重名时在末尾加序号。命令的 Short / Long / Example 字段直接用字段名，比取词更好认。
func (r *keyRegistry) makeKey(pkg, stem, en, field string) (name, key string) {
	words := wordRe.FindAllString(placeholderRe.ReplaceAllString(en, " "), -1)
	if len(words) > 5 {
		words = words[:5]
	}
	if len(words) == 0 {
		words = []string{"Msg"}
	}
	var pascal, snake []string
	for _, w := range words {
		w = strings.ToLower(w)
		pascal = append(pascal, strings.ToUpper(w[:1])+w[1:])
		snake = append(snake, w)
	}
	pkgTitle := strings.ToUpper(pkg[:1]) + pkg[1:]
	base := pkgTitle + title(stem) + strings.Join(pascal, "")
	keyBase := pkg + "." + strings.ReplaceAll(stem, "_", ".") + "." + strings.Join(snake, "_")
	if field == "Short" || field == "Long" || field == "Example" {
		base = pkgTitle + title(stem) + field
		keyBase = pkg + "." + strings.ReplaceAll(stem, "_", ".") + "." + strings.ToLower(field)
	}

	name, key = base, keyBase
	for n := 2; r.names[name] || r.keys[key]; n++ {
		name = base + strconv.Itoa(n)
		key = keyBase + "_" + strconv.Itoa(n)
	}
	r.names[name], r.keys[key] = true, true
	return name, key
}

// title 把 snake_case 的文件名变成 PascalCase：up_upgrade_diff → UpUpgradeDiff。
func title(stem string) string {
	var b strings.Builder
	for _, p := range strings.Split(stem, "_") {
		if p != "" {
			b.WriteString(strings.ToUpper(p[:1]) + p[1:])
		}
	}
	return b.String()
}

// catalogIndex 是"中文文案 → 已有的 (常量名, 英文文案)"的反向索引，
// 用来自动复用已有 key：中文与英文都相同的文案不需要再建一份。
type catalogIndex map[string][]catalogItem

type catalogItem struct{ name, en string }

func loadCatalogIndex(root string) (catalogIndex, error) {
	zh, err := loadCatalog(filepath.Join(root, "internal/i18n/catalog_zh.go"))
	if err != nil {
		return nil, err
	}
	en, err := loadCatalog(filepath.Join(root, "internal/i18n/catalog_en.go"))
	if err != nil {
		return nil, err
	}
	idx := catalogIndex{}
	for name, z := range zh {
		idx[z] = append(idx[z], catalogItem{name, en[name]})
	}
	return idx, nil
}

// loadCatalog 读一份目录文件，返回"常量名 → 文案"。
func loadCatalog(path string) (map[string]string, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	out := map[string]string{}
	for _, m := range catalogRe.FindAllStringSubmatch(string(body), -1) {
		var v string
		if err := json.Unmarshal([]byte(m[2]), &v); err != nil {
			return nil, err
		}
		out[m[1]] = v
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
