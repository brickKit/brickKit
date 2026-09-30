// cmd/gen-llms：生成 llms/<lang>/ 下的文档合集，并更新 llms.txt / llms.zh.txt 里的合集清单。
// 在仓库根目录运行；-check 只比较、不写，与生成结果不一致时以非零退出。
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/brickkit/brickkit/internal/llmsgen"
)

func main() {
	check := flag.Bool("check", false, "compare instead of writing; exit 1 when llms/ is stale")
	flag.Parse()
	outs, err := llmsgen.Generate(".", llmsgen.DefaultOptions())
	if err != nil {
		fmt.Fprintln(os.Stderr, "gen-llms:", err)
		os.Exit(1)
	}
	if *check {
		if problems := llmsgen.Check(".", outs); len(problems) > 0 {
			for _, p := range problems {
				fmt.Fprintln(os.Stderr, "gen-llms:", p)
			}
			fmt.Fprintln(os.Stderr, "gen-llms: run make generate-llms")
			os.Exit(1)
		}
		return
	}
	if err := llmsgen.Write(".", outs); err != nil {
		fmt.Fprintln(os.Stderr, "gen-llms:", err)
		os.Exit(1)
	}
}
