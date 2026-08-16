// Package regression 守着回归测试清单（开发计划 Step 37）。
//
// # 这里为什么不是 25 条新测试
//
// R1–R25 全部**早就有测试覆盖**——项目里有 1599 个测试函数，
// 每条命令都有几十条。再写 25 条只是把已有的东西抄一遍。
//
// 清单真正缺的不是覆盖，是**连接**：计划里那 25 项是 25 个要人手打勾的方框，
// 谁也说不出「R7 up 基本功能」到底由哪个测试保证。于是有两个坏结果：
//
//	改了 up.go 之后，不知道该重点看哪几条测试有没有红
//	某条测试被改名或删掉，清单还挂着 ☑ —— **清单变成了谎话，而且没人会发现**
//
// 所以这个包做的事只有一件：**让清单无法悄悄失效**。
// 它读 清单.tsv，逐条确认那个测试**今天仍然存在**。
// 名字对不上就当场失败，并指出是哪一条承诺失去了证据。
//
// 执行交给 `make test-regression`（同样读 清单.tsv）。
// 一份数据、两个消费者，改清单只改一个地方。
//
// # 为什么用 `go test -list` 而不是搜源码
//
// 搜 `func TestXxx(` 也能确认名字在，但确认不了它**能被跑到**：
// 文件带了构建标签、包编译不过、测试名写在注释里——这些情况下
// 源码里有那串字符，`go test` 却一条也跑不到。
// `-list` 走的是真正的编译与注册流程，它说有才是真的有。
package regression

import (
	"bufio"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// item 是清单里的一行。
type item struct {
	id     string // R1
	desc   string // brickkit init 基本功能
	step   string // 3
	module string // "." 或 "market-server"
	pkg    string // ./internal/cli
	test   string // TestInitCreatesProjectStructure
	line   int
}

const manifestName = "清单.tsv"

// repoRoot 是仓库根目录（本包位于 tests/regression/）。
func repoRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	require.NoError(t, err)
	return root
}

// loadManifest 读清单。
func loadManifest(t *testing.T) []item {
	t.Helper()

	f, err := os.Open(manifestName)
	require.NoError(t, err, "读不到回归清单")
	defer f.Close()

	var items []item
	scanner := bufio.NewScanner(f)
	for n := 1; scanner.Scan(); n++ {
		line := scanner.Text()
		if strings.TrimSpace(line) == "" || strings.HasPrefix(line, "#") {
			continue
		}
		cols := strings.Split(line, "\t")
		require.Len(t, cols, 6,
			"%s 第 %d 行应有 6 列（编号/说明/Step/module/包/测试名），实际 %d 列：%q",
			manifestName, n, len(cols), line)

		items = append(items, item{
			id: cols[0], desc: cols[1], step: cols[2],
			module: cols[3], pkg: cols[4], test: cols[5], line: n,
		})
	}
	require.NoError(t, scanner.Err())
	return items
}

// 清单必须正好覆盖 R1–R25，不重不漏。
//
// 计划里是 25 项。少一条说明有承诺没了证据，多一条说明清单和计划已经对不上——
// 两种情况都会让「跑完回归就安全了」变成一句没有依据的话。
func TestManifestCoversEveryChecklistItem(t *testing.T) {
	items := loadManifest(t)

	seen := map[string]int{}
	for _, it := range items {
		seen[it.id]++
		assert.NotEmpty(t, it.desc, "%s 没有说明", it.id)
		assert.NotEmpty(t, it.test, "%s 没有指向任何测试", it.id)
	}

	for i := 1; i <= 25; i++ {
		id := "R" + strconv.Itoa(i)
		assert.Equal(t, 1, seen[id],
			"Step 37：清单里 %s 出现了 %d 次（应当正好 1 次）", id, seen[id])
		delete(seen, id)
	}
	for id, n := range seen {
		t.Errorf("Step 37：清单里有计划之外的条目 %s（%d 次）", id, n)
	}
}

// 清单点名的每个测试都必须**真的存在且能被跑到**。
//
// 这条是整个包存在的理由：它是清单与现实之间唯一的连接。
// 没有它，改名一个测试就能让某条承诺悄无声息地失去证据。
func TestEveryListedTestStillExists(t *testing.T) {
	items := loadManifest(t)
	root := repoRoot(t)

	// 按 module+包 分组，一个包只问一次
	type key struct{ module, pkg string }
	groups := map[key][]item{}
	var order []key
	for _, it := range items {
		k := key{it.module, it.pkg}
		if _, ok := groups[k]; !ok {
			order = append(order, k)
		}
		groups[k] = append(groups[k], it)
	}

	for _, k := range order {
		group := groups[k]

		names := make([]string, 0, len(group))
		for _, it := range group {
			names = append(names, it.test)
		}
		pattern := "^(" + strings.Join(names, "|") + ")$"

		cmd := exec.Command("go", "test", "-list", pattern, k.pkg)
		cmd.Dir = filepath.Join(root, k.module)
		out, err := cmd.CombinedOutput()
		require.NoError(t, err, "在 %s 里列举 %s 的测试失败：\n%s", k.module, k.pkg, out)

		found := map[string]bool{}
		for _, line := range strings.Split(string(out), "\n") {
			found[strings.TrimSpace(line)] = true
		}

		where := filepath.Join(k.module, strings.TrimPrefix(k.pkg, "./"))
		for _, it := range group {
			assert.True(t, found[it.test],
				"Step 37：%s（%s，对应 Step %s）指向的测试 %s 在 %s 里已经不存在了。\n"+
					"    清单少一条证据就等于多一句没人验证的话——\n"+
					"    请把它改成现在真正覆盖这条承诺的测试，而不是从清单里删掉这一项。",
				it.id, it.desc, it.step, it.test, where)
		}
	}
}

// 守卫本身会不会坏？
//
// 会——如果 `go test -list` 的输出格式变了、或者包路径拼错了，
// 上面那条会「一个都没找到」，看起来和「测试全被删了」一模一样。
// 所以这里拿一个**故意不存在**的名字验证：查不到的东西必须查不到。
// 如果连它都「找到了」，说明匹配逻辑坏了，上面那条的绿灯不可信。
func TestGuardDetectsMissingTest(t *testing.T) {
	root := repoRoot(t)

	cmd := exec.Command("go", "test", "-list",
		"^TestThisNameDeliberatelyDoesNotExist$", "./internal/cli")
	cmd.Dir = root
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "%s", out)

	assert.NotContains(t, string(out), "TestThisNameDeliberatelyDoesNotExist",
		"Step 37：一个不存在的测试被「找到」了，说明存在性检查根本没在工作——"+
			"那么 TestEveryListedTestStillExists 的绿灯也不可信")
}
