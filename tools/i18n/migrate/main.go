// 命令 migrate 把 Go 源码里用户可见的中文字符串字面量迁进消息目录。
//
// 它是"半自动"的：机械部分（找出字面量、判断所处的调用、改写成 i18n.T(...)、
// 生成 msgid 常量与两份目录条目）交给工具，英文措辞由人写。子项目 2（全量迁移）
// 就是这样把 internal/cli 的约 970 行中文迁完的；以后新增一个包、或者哪天要迁
// 别的东西，还可以用。完整流程见 tools/i18n/README.md。
//
//	migrate extract -out entries.json -listing listing.txt file.go...
//	migrate apply   -entries entries.json -trans trans.txt [-root .] [-pkg cli]
//
// extract 只读不写源码；apply 只在全部校验通过之后才改文件。
package main

import (
	"flag"
	"fmt"
	"io"
	"os"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stderr))
}

func run(args []string, log io.Writer) int {
	if len(args) == 0 {
		_, _ = fmt.Fprintln(log, "用法：migrate extract|apply …（见 tools/i18n/README.md）")
		return 2
	}
	var err error
	switch args[0] {
	case "extract":
		err = runExtract(args[1:], log)
	case "apply":
		err = runApply(args[1:], log)
	default:
		_, _ = fmt.Fprintf(log, "未知子命令 %q（可用：extract、apply）\n", args[0])
		return 2
	}
	if err != nil {
		_, _ = fmt.Fprintln(log, "❌", err)
		return 1
	}
	return 0
}

func runExtract(args []string, log io.Writer) error {
	fs := flag.NewFlagSet("extract", flag.ContinueOnError)
	fs.SetOutput(log)
	out := fs.String("out", "entries.json", "抽取结果（JSON，给 apply 读）")
	listing := fs.String("listing", "listing.txt", "给人看的清单（一行一条）")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() == 0 {
		return fmt.Errorf("extract 需要至少一个 .go 文件")
	}
	entries, err := extractFiles(fs.Args())
	if err != nil {
		return err
	}
	if err := writeEntries(*out, entries); err != nil {
		return err
	}
	if err := writeListing(*listing, entries); err != nil {
		return err
	}
	_, _ = fmt.Fprintf(log, "entries: %d total\n", len(entries))
	return nil
}

func runApply(args []string, log io.Writer) error {
	fs := flag.NewFlagSet("apply", flag.ContinueOnError)
	fs.SetOutput(log)
	o := applyOptions{log: log}
	fs.StringVar(&o.entriesPath, "entries", "entries.json", "extract 写出的 JSON")
	fs.StringVar(&o.transPath, "trans", "trans.txt", "译文文件：每行 编号<TAB>英文，@常量名 表示复用已有 key")
	fs.StringVar(&o.root, "root", ".", "仓库根")
	fs.StringVar(&o.pkg, "pkg", "cli", "被迁移的包名（决定 key 前缀与 msgid 文件名）")
	if err := fs.Parse(args); err != nil {
		return err
	}
	report, err := applyTranslations(o)
	if err != nil {
		return err
	}
	_, _ = fmt.Fprintf(log, "applied %d entries in %d files\n", report.applied, report.files)
	return nil
}
