// cmd/gen-msgid：从 internal/i18n/locales/en.yaml 生成 internal/msgid/messages_gen.go。
// 新增一条消息只写目录：en.yaml 加一行、其余语言各加一行，再跑 make generate-msgid。
//
// -check-names：一次性的迁移检查，列出现有手写常量里名字与 GoName(key) 不一致的（Task 5 用完即可保留，无害）。
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"regexp"

	"github.com/brickkit/brickkit/internal/msgid/msgidgen"
)

func main() {
	checkNames := flag.Bool("check-names", false, "list hand-written constants whose name differs from GoName(key)")
	flag.Parse()
	if *checkNames {
		os.Exit(listMismatches())
	}
	keys, err := msgidgen.SourceKeys("internal/i18n/locales/en.yaml")
	if err == nil {
		var out []byte
		if out, err = msgidgen.Render(keys); err == nil {
			err = os.WriteFile("internal/msgid/messages_gen.go", out, 0o644)
		}
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "gen-msgid:", err)
		os.Exit(1)
	}
}

var constRe = regexp.MustCompile(`(?m)^\s*(\w+)\s*=\s*"([^"]+)"`)

func listMismatches() int {
	files, _ := filepath.Glob("internal/msgid/*.go")
	bad := 0
	for _, f := range files {
		if filepath.Base(f) == "messages_gen.go" {
			continue
		}
		body, _ := os.ReadFile(f)
		for _, m := range constRe.FindAllStringSubmatch(string(body), -1) {
			if m[1] == "PluralOneSuffix" {
				continue
			}
			if want := msgidgen.GoName(m[2]); want != m[1] {
				fmt.Printf("%s\t%s\t%s\n", m[1], m[2], want)
				bad++
			}
		}
	}
	if bad > 0 {
		return 1
	}
	return 0
}
