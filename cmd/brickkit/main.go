// Command brickkit 是 BrickKit CLI 的入口。
//
// BrickKit CLI 只做六件事（001 §5.1）：管理项目配置、拉取组件与产物、
// 解析依赖与推测顺序、生成部署文件并执行迁移、调用底层引擎与发布、
// 管理组件源码工作区。
//
// 本文件在 Step 1 中仅作为骨架存在，Step 2 会替换为 cobra root 命令。
package main

import (
	"fmt"
	"os"

	"github.com/brickkit/brickkit/internal/version"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "❌", err)
		os.Exit(1)
	}
}

func run(_ []string) error {
	fmt.Printf("brickkit %s (skeleton, Step 1)\n", version.Version)
	fmt.Printf("支持的 Manifest 版本：%s\n", version.ManifestAPIVersion)
	fmt.Printf("支持的部署目标：%s\n", version.SupportedTargets())
	return nil
}
