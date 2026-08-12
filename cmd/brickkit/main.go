// Command brickkit 是 BrickKit CLI 的入口。
//
// BrickKit CLI 只做六件事（001 §5.1）：管理项目配置、拉取组件与产物、
// 解析依赖与推测顺序、生成部署文件并执行迁移、调用底层引擎与发布、
// 管理组件源码工作区。
//
// 命令树与全局选项见 internal/cli。
package main

import (
	"os"

	"github.com/brickkit/brickkit/internal/cli"
)

func main() {
	os.Exit(cli.Execute())
}
