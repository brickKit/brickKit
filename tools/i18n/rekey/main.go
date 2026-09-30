// rekey：给消息 ID 改名——同时改代码里的 msgid.X 与每份目录里的 key，文案一个字节不动。
//
//	go run ./tools/i18n/rekey -drift            列出名字可能与文案对不上的 key（审阅用，不做决定）
//	go run ./tools/i18n/rekey -apply renames.tsv 按 "旧key<TAB>新key" 改名；任何一条通不过校验就什么都不写
package main

import (
	"bufio"
	"flag"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/brickkit/brickkit/internal/msgid/msgidgen"
)

type rename struct{ Old, New string }

var keyLineRe = regexp.MustCompile(`^([a-z0-9_.]+):`)

// apply 先整批校验，再改写：旧 key 都存在、新 key 都不存在、改名后 Go 名字不撞。
func apply(root string, renames []rename) error {
	enPath := filepath.Join(root, "internal/i18n/locales/en.yaml")
	keys, err := msgidgen.SourceKeys(enPath)
	if err != nil {
		return err
	}
	exists := map[string]bool{}
	for _, k := range keys {
		exists[k] = true
	}
	byOld := map[string]string{}
	for _, r := range renames {
		if !exists[r.Old] {
			return fmt.Errorf("%s: no such key", r.Old)
		}
		if exists[r.New] || exists[r.New+msgidgen.PluralOneSuffix] {
			return fmt.Errorf("%s: key already exists", r.New)
		}
		if _, dup := byOld[r.Old]; dup {
			return fmt.Errorf("%s: renamed twice", r.Old)
		}
		byOld[r.Old] = r.New
		// 单数形式跟着改：TN 按 key + ".one" 找它
		if exists[r.Old+msgidgen.PluralOneSuffix] {
			byOld[r.Old+msgidgen.PluralOneSuffix] = r.New + msgidgen.PluralOneSuffix
		}
	}
	after := make([]string, len(keys))
	for i, k := range keys {
		if n, ok := byOld[k]; ok {
			k = n
		}
		after[i] = k
	}
	if _, err := msgidgen.Render(after); err != nil {
		return err // 改名后两个 key 推出同一个 Go 名字
	}
	names := map[string]string{} // 旧 Go 名 → 新 Go 名（单数形式没有常量）
	for o, n := range byOld {
		if !strings.HasSuffix(o, msgidgen.PluralOneSuffix) {
			names[msgidgen.GoName(o)] = msgidgen.GoName(n)
		}
	}

	pending := map[string][]byte{} // 全部算完再写，写之前任何一步失败都不留半成品
	locales, _ := filepath.Glob(filepath.Join(root, "internal/i18n/locales", "*.yaml"))
	for _, f := range locales {
		out, err := renameKeys(f, byOld)
		if err != nil {
			return err
		}
		pending[f] = out
	}
	for _, dir := range []string{"internal", "cmd", "tests", "tools", "market-server"} {
		err := filepath.WalkDir(filepath.Join(root, dir), func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				if os.IsNotExist(err) {
					return filepath.SkipDir
				}
				return err
			}
			if d.IsDir() || !strings.HasSuffix(path, ".go") {
				return nil
			}
			out, changed, err := renameSelectors(path, names)
			if err != nil {
				return err
			}
			if changed {
				pending[path] = out
			}
			return nil
		})
		if err != nil {
			return err
		}
	}
	for path, out := range pending {
		if err := os.WriteFile(path, out, 0o644); err != nil {
			return err
		}
	}
	return nil
}

// renameKeys 只改行首的 key，冒号之后原样保留。
func renameKeys(path string, byOld map[string]string) ([]byte, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	lines := strings.SplitAfter(string(body), "\n")
	for i, line := range lines {
		if m := keyLineRe.FindStringSubmatch(line); m != nil {
			if n, ok := byOld[m[1]]; ok {
				lines[i] = n + line[len(m[1]):]
			}
		}
	}
	return []byte(strings.Join(lines, "")), nil
}

// renameSelectors 把 msgid.<旧名> 换成 msgid.<新名>，按 AST 位置改，不做文本搜索。
func renameSelectors(path string, names map[string]string) ([]byte, bool, error) {
	src, err := os.ReadFile(path)
	if err != nil {
		return nil, false, err
	}
	if !strings.Contains(string(src), "msgid.") {
		return src, false, nil // 不引用 msgid 的文件（包括 testdata 里故意写坏的）不用解析
	}
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, src, parser.ParseComments)
	if err != nil {
		return nil, false, err
	}
	type edit struct {
		off  int
		old  string
		name string
	}
	var edits []edit
	ast.Inspect(f, func(n ast.Node) bool {
		sel, ok := n.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		if id, ok := sel.X.(*ast.Ident); ok && id.Name == "msgid" {
			if nn, ok := names[sel.Sel.Name]; ok {
				edits = append(edits, edit{fset.Position(sel.Sel.Pos()).Offset, sel.Sel.Name, nn})
			}
		}
		return true
	})
	if len(edits) == 0 {
		return src, false, nil
	}
	sort.Slice(edits, func(i, j int) bool { return edits[i].off > edits[j].off })
	out := string(src)
	for _, e := range edits {
		out = out[:e.off] + e.name + out[e.off+len(e.old):]
	}
	formatted, err := format.Source([]byte(out))
	return formatted, true, err
}

// roleWords 是 key 里表示"这条消息是什么角色"的词，不要求出现在文案里。
var roleWords = wordSet("short long use example label hint reason detail title tip note header col flag count one error warn prefix suffix")

// stopWords 是英文虚词，不算。
var stopWords = wordSet("the a an to of in is and or for on it be not by with at as this that s t")

func wordSet(s string) map[string]bool {
	out := map[string]bool{}
	for _, w := range strings.Fields(s) {
		out[w] = true
	}
	return out
}

var (
	entryRe  = regexp.MustCompile(`(?m)^([a-z0-9_.]+):\s*("(?:[^"\\]|\\.)*")\s*$`)
	numberRe = regexp.MustCompile(`^[0-9]+$`)
	textWord = regexp.MustCompile(`[a-z0-9]+`)
)

// drift 列出名字可能与文案对不上的 key：key 里（第一段之后）有意义的词，不到一半出现在英文文案里。
// 只是审阅的线索，不做决定。
func drift(root string, w *bufio.Writer) error {
	body, err := os.ReadFile(filepath.Join(root, "internal/i18n/locales/en.yaml"))
	if err != nil {
		return err
	}
	for _, m := range entryRe.FindAllStringSubmatch(string(body), -1) {
		key := m[1]
		if strings.HasSuffix(key, msgidgen.PluralOneSuffix) {
			continue
		}
		text := strings.ToLower(strings.Trim(m[2], `"`))
		have := map[string]bool{}
		for _, t := range textWord.FindAllString(text, -1) {
			have[t] = true
		}
		parts := strings.SplitN(key, ".", 2)
		if len(parts) < 2 {
			continue
		}
		var words []string
		for _, p := range strings.FieldsFunc(parts[1], func(r rune) bool { return r == '.' || r == '_' }) {
			if roleWords[p] || stopWords[p] || numberRe.MatchString(p) {
				continue
			}
			words = append(words, p)
		}
		if len(words) == 0 {
			continue
		}
		hits := 0
		for _, word := range words {
			if have[word] {
				hits++
			}
		}
		if hits*2 < len(words) {
			if _, err := fmt.Fprintf(w, "%s\t%s\t%s\n", msgidgen.GoName(key), key, m[2]); err != nil {
				return err
			}
		}
	}
	return w.Flush()
}

// readRenames 读 "旧key<TAB>新key"，跳过空行与 # 注释。
func readRenames(path string) ([]rename, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var out []rename
	for i, line := range strings.Split(string(body), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		f := strings.Split(line, "\t")
		if len(f) != 2 {
			return nil, fmt.Errorf("%s:%d: want old.key<TAB>new.key", path, i+1)
		}
		out = append(out, rename{Old: strings.TrimSpace(f[0]), New: strings.TrimSpace(f[1])})
	}
	return out, nil
}

func main() {
	driftFlag := flag.Bool("drift", false, "list keys whose name may no longer match their English text")
	applyPath := flag.String("apply", "", "rename keys listed as old.key<TAB>new.key in this file")
	flag.Parse()
	var err error
	switch {
	case *driftFlag:
		err = drift(".", bufio.NewWriter(os.Stdout))
	case *applyPath != "":
		var rs []rename
		if rs, err = readRenames(*applyPath); err == nil {
			err = apply(".", rs)
		}
	default:
		flag.Usage()
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "rekey:", err)
		os.Exit(1)
	}
}
