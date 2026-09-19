// gen-schemas 把 component.yaml 与 brickkit.yaml 的 JSON Schema 写进 schemas/。
//
// 它不进 brickkit 二进制：schema 是仓库里的一份生成物，不是使用者机器上要有的东西。
// 用法：make generate-schemas（等价于 go run ./cmd/gen-schemas [目标目录]）。
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/brickkit/brickkit/internal/schemagen"
)

func main() {
	dir := "schemas"
	if len(os.Args) > 1 {
		dir = os.Args[1]
	}
	if err := run(dir); err != nil {
		fmt.Fprintln(os.Stderr, "❌ 生成 schema 失败：", err)
		os.Exit(1)
	}
}

func run(dir string) error {
	files, err := schemagen.Files()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, files[name], 0o644); err != nil {
			return err
		}
		fmt.Println("✅", path)
	}
	return nil
}
