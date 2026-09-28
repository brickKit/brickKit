package main

// 市场借用主模块的是**规则**（Manifest 怎么算合法、哪些配置项名字被保留），不是 CLI 本身。
// 规则放错了包，市场二进制就会把安装源、依赖解析、项目装载这一整套 CLI 代码链接进来——
// 它们在市场里一行都不会执行，却跟着市场一起编译、一起出漏洞公告。

import (
	"os/exec"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// cliOnly 是只属于 CLI 的包：市场不该依赖其中任何一个。
var cliOnly = []string{
	"internal/cli", "internal/source", "internal/resolver", "internal/project", "internal/cascade",
	"internal/inject", "internal/engine", "internal/compose", "internal/k8s", "internal/workspace",
	"internal/market", "internal/security",
}

func TestServerLinksOnlyTheRules(t *testing.T) {
	out, err := exec.Command("go", "list", "-deps", ".").Output()
	require.NoError(t, err)

	const main = "github.com/brickkit/brickkit/"
	var linked []string
	for _, pkg := range strings.Fields(string(out)) {
		for _, banned := range cliOnly {
			if pkg == main+banned {
				linked = append(linked, pkg)
			}
		}
	}
	assert.Empty(t, linked, "市场服务端链接了只属于 CLI 的包——规则该搬到它们不依赖 CLI 的地方")
	assert.Contains(t, string(out), main+"internal/manifest", "自检：市场确实用着 CLI 的 Manifest 规则")
}
