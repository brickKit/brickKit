package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// applyOptions 是 apply 子命令的参数。
type applyOptions struct {
	entriesPath string // extract 写出的 JSON
	transPath   string // 译文文件：每行 `编号<TAB>英文`，`@常量名` 表示复用已有 key
	root        string // 仓库根
	pkg         string // 被迁移的包名，决定 key 前缀与 msgid 文件名（cli / skills / …）
	log         io.Writer
}

// applyReport 是 apply 的结果统计。
type applyReport struct {
	applied int // 改写了多少条文案
	files   int // 改了多少个源文件
}

// readTranslations 读译文文件。`⏎` 表示换行；空行与 # 开头的行忽略。
func readTranslations(path string) (map[int]string, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	out := map[int]string{}
	for _, line := range strings.Split(string(raw), "\n") {
		if strings.TrimSpace(line) == "" || strings.HasPrefix(line, "#") {
			continue
		}
		id, text, ok := strings.Cut(line, "\t")
		if !ok {
			continue
		}
		n, err := strconv.Atoi(strings.TrimSpace(id))
		if err != nil {
			continue
		}
		out[n] = strings.ReplaceAll(text, "⏎", "\n")
	}
	return out, nil
}

func loadEntries(path string) ([]Entry, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var entries []Entry
	if err := json.Unmarshal(b, &entries); err != nil {
		return nil, err
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].ID < entries[j].ID })
	return entries, nil
}

// newItem 是一条新建的目录条目。
type newItem struct{ name, key, en, zh string }

// applyTranslations 按译文改写源文件、生成 msgid 常量、追加两份目录。
//
// 先在内存里把所有文件的改写都算完并校验（区间不重叠），确认没问题才动第一个文件——
// 半路失败不会留下"改了一半"的文件。
func applyTranslations(o applyOptions) (applyReport, error) {
	entries, err := loadEntries(o.entriesPath)
	if err != nil {
		return applyReport{}, err
	}
	trans, err := readTranslations(o.transPath)
	if err != nil {
		return applyReport{}, err
	}
	reg, err := loadKeyRegistry(o.root)
	if err != nil {
		return applyReport{}, err
	}
	idx, err := loadCatalogIndex(o.root)
	if err != nil {
		return applyReport{}, err
	}

	byFile := map[string][]*Entry{}
	var fileOrder []string
	for i := range entries {
		e := &entries[i]
		if _, ok := trans[e.ID]; !ok || e.Manual != "" {
			continue
		}
		if _, seen := byFile[e.File]; !seen {
			fileOrder = append(fileOrder, e.File)
		}
		byFile[e.File] = append(byFile[e.File], e)
	}
	sort.Strings(fileOrder) // 固定处理顺序：撞名时的序号才可复现

	made := map[string]string{} // zh\x00en → 本次新建的常量名
	perStem := map[string][]newItem{}
	perFile := map[string][]Edit{}
	report := applyReport{}

	for _, file := range fileOrder {
		src, err := os.ReadFile(file)
		if err != nil {
			return report, err
		}
		stem := strings.TrimSuffix(filepath.Base(file), ".go")
		edits, n, err := planFile(byFile[file], src, trans, func(e *Entry, en string) (string, error) {
			name, err := resolveName(o, reg, idx, made, perStem, stem, e, en)
			return name, err
		})
		if err != nil {
			return report, fmt.Errorf("%s: %w", file, err)
		}
		perFile[file] = edits
		report.applied += n
	}

	// 先全部校验，再统一写
	for file, edits := range perFile {
		sort.Slice(edits, func(i, j int) bool { return edits[i].Start > edits[j].Start })
		for i := 1; i < len(edits); i++ {
			if edits[i].End > edits[i-1].Start {
				return report, fmt.Errorf("%s: 改写区间重叠", file)
			}
		}
	}
	for _, file := range fileOrder {
		if err := rewriteFile(file, perFile[file]); err != nil {
			return report, err
		}
	}
	report.files = len(fileOrder)

	if err := writeMsgids(o, perStem); err != nil {
		return report, err
	}
	return report, nil
}

// resolveName 决定一条译文用哪个常量：`@名字` 显式复用；中文英文都相同的已有条目自动复用；
// 否则新建（并登记进 perStem，稍后写进 msgid 文件与目录）。
func resolveName(o applyOptions, reg *keyRegistry, idx catalogIndex, made map[string]string,
	perStem map[string][]newItem, stem string, e *Entry, en string) (string, error) {
	if strings.HasPrefix(en, "@") {
		name := strings.TrimSpace(strings.TrimPrefix(en, "@"))
		if !reg.names[name] {
			return "", fmt.Errorf("#%d: 找不到 msgid 常量 %s", e.ID, name)
		}
		return name, nil
	}
	if name := idx.find(e.ZH, en); name != "" {
		_, _ = fmt.Fprintf(o.log, "reuse %s for #%d\n", name, e.ID)
		return name, nil
	}
	if name := made[e.ZH+"\x00"+en]; name != "" {
		_, _ = fmt.Fprintf(o.log, "reuse %s for #%d\n", name, e.ID)
		return name, nil
	}
	name, key := reg.makeKey(o.pkg, stem, en, e.Field)
	perStem[stem] = append(perStem[stem], newItem{name, key, en, e.ZH})
	made[e.ZH+"\x00"+en] = name
	return name, nil
}

// planFile 算出一个文件的全部改写。
//
// 嵌套：一个 Printf 的参数里可能还嵌着另一处中文（`Printf("…%s", logsCommand(x, "<服务名>"))`）。
// 所以先处理范围小的（内层），外层取参数源码时把内层已经算好的改写套进去，
// 内层的改写随之被"消费"，不再单独出现在结果里。
func planFile(list []*Entry, src []byte, trans map[int]string,
	nameOf func(e *Entry, en string) (string, error)) ([]Edit, int, error) {
	span := func(e *Entry) int {
		lo, hi := 1<<30, 0
		for _, ed := range e.Edits {
			lo, hi = min(lo, ed.Start), max(hi, ed.End)
		}
		return hi - lo
	}
	sort.SliceStable(list, func(i, j int) bool { return span(list[i]) < span(list[j]) })

	type done struct {
		ed       Edit
		consumed bool
	}
	var all []*done
	render := func(r [2]int) string {
		var inner []*done
		for _, d := range all {
			if !d.consumed && d.ed.Start >= r[0] && d.ed.End <= r[1] {
				inner = append(inner, d)
			}
		}
		sort.Slice(inner, func(i, j int) bool { return inner[i].ed.Start > inner[j].ed.Start })
		buf := append([]byte{}, src[r[0]:r[1]]...)
		for _, d := range inner {
			a, b := d.ed.Start-r[0], d.ed.End-r[0]
			buf = append(buf[:a:a], append([]byte(d.ed.Text), buf[b:]...)...)
			d.consumed = true
		}
		return string(buf)
	}

	applied := 0
	for _, e := range list {
		name, err := nameOf(e, trans[e.ID])
		if err != nil {
			return nil, 0, err
		}
		call := "i18n.T(msgid." + name
		for _, r := range e.ArgRanges {
			call += ", " + render(r)
		}
		call += ")"
		last := ""
		if e.LastRange != [2]int{} {
			last = render(e.LastRange)
		}
		for _, ed := range e.Edits {
			t := strings.ReplaceAll(ed.Text, "{{T}}", call)
			t = strings.ReplaceAll(t, "{{LAST}}", last)
			all = append(all, &done{ed: Edit{Start: ed.Start, End: ed.End, Text: t}})
		}
		applied++
	}

	var edits []Edit
	for _, d := range all {
		if !d.consumed {
			edits = append(edits, d.ed)
		}
	}
	return edits, applied, nil
}

var (
	internalImportRe = regexp.MustCompile(`\t"github.com/brickkit/brickkit/internal/[a-z0-9]+"\n`)
	importOpenRe     = regexp.MustCompile(`import \(\n`)
)

// rewriteFile 应用改写并补上 i18n / msgid 两个 import（缺哪个补哪个）。
func rewriteFile(file string, edits []Edit) error {
	src, err := os.ReadFile(file)
	if err != nil {
		return err
	}
	for _, ed := range edits { // edits 已按起点从后往前排序
		src = append(src[:ed.Start:ed.Start], append([]byte(ed.Text), src[ed.End:]...)...)
	}
	s := string(src)
	for _, imp := range []string{
		`"github.com/brickkit/brickkit/internal/i18n"`,
		`"github.com/brickkit/brickkit/internal/msgid"`,
	} {
		if strings.Contains(s, imp) {
			continue
		}
		if m := internalImportRe.FindStringIndex(s); m != nil {
			s = s[:m[1]] + "\t" + imp + "\n" + s[m[1]:]
		} else if m := importOpenRe.FindStringIndex(s); m != nil {
			s = s[:m[1]] + "\t" + imp + "\n" + s[m[1]:]
		} else {
			return fmt.Errorf("%s: 找不到可以插入 import 的位置", file)
		}
	}
	return os.WriteFile(file, []byte(s), 0o644)
}

// writeMsgids 把新建的常量写进 internal/msgid/，并把两份译文追加到 internal/i18n/ 的目录里。
func writeMsgids(o applyOptions, perStem map[string][]newItem) error {
	stems := make([]string, 0, len(perStem))
	for s := range perStem {
		stems = append(stems, s)
	}
	sort.Strings(stems)

	var enBody, zhBody strings.Builder
	for _, stem := range stems {
		var block strings.Builder
		fmt.Fprintf(&block, "// internal/%s/%s.go\nconst (\n", o.pkg, stem)
		for _, it := range perStem[stem] {
			fmt.Fprintf(&block, "\t%s = %s\n", it.name, jsonString(it.key))
			fmt.Fprintf(&enBody, "\tmsgid.%s: %s,\n", it.name, jsonString(it.en))
			fmt.Fprintf(&zhBody, "\tmsgid.%s: %s,\n", it.name, jsonString(it.zh))
		}
		block.WriteString(")\n")

		name := "cli_" + stem + ".go"
		if o.pkg != "cli" {
			name = o.pkg + ".go"
		}
		path := filepath.Join(o.root, "internal/msgid", name)
		content := "package msgid\n\n" + block.String()
		if old, err := os.ReadFile(path); err == nil {
			content = string(old) + "\n" + block.String()
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			return err
		}
	}
	if err := appendCatalog(filepath.Join(o.root, "internal/i18n/catalog_en.go"), enBody.String()); err != nil {
		return err
	}
	return appendCatalog(filepath.Join(o.root, "internal/i18n/catalog_zh.go"), zhBody.String())
}

// appendCatalog 把条目追加到目录 map 字面量的结尾（最后一个 `}` 之前）。
func appendCatalog(path, body string) error {
	if body == "" {
		return nil
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	s := strings.TrimRight(string(b), "\n ")
	i := strings.LastIndex(s, "}")
	if i < 0 {
		return fmt.Errorf("%s: 不像目录文件（找不到结尾的 })", path)
	}
	return os.WriteFile(path, []byte(s[:i]+body+s[i:]+"\n"), 0o644)
}
