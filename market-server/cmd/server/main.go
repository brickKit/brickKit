// Command server 是 BrickKit Market（组件市场后端）的入口。
//
// 市场只回答两个问题（001 §6）：有什么可以装？谁有权装？
// 它不安装组件、不运行组件、不管运行状态。
//
// 本文件在 Step 1 中仅作为骨架存在，完整实现见 Step 18。
package main

import (
	"fmt"
	"os"
)

func main() {
	fmt.Println("brickkit market-server (skeleton, Step 1)")
	os.Exit(0)
}
