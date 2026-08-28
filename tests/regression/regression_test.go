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
// 读清单与"问 go test 那个测试还在不在"这两件事，与 tests/checklist 完全相同，
// 因此共用 internal 之外的那个小包，而不是在这里再抄一份。
package regression

import (
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/brickkit/brickkit/tests/checklist"
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

// loadManifest 读清单。列的含义见 清单.tsv 的表头。
func loadManifest(t *testing.T) []item {
	t.Helper()

	rows, err := checklist.Load(manifestName, 6)
	require.NoError(t, err, "读不到回归清单")
	require.NotEmpty(t, rows, "清单是空的——那不是「全部通过」，是根本没读到东西")

	items := make([]item, 0, len(rows))
	for _, r := range rows {
		items = append(items, item{
			id: r.Cell(0), desc: r.Cell(1), step: r.Cell(2),
			module: r.Cell(3), pkg: r.Cell(4), test: r.Cell(5), line: r.Line,
		})
	}
	return items
}

// planItems 是开发计划 Step 37 列出的回归项数（R1–R25）。
//
// 它是**下限**，不是总数：见 TestManifestCoversEveryChecklistItem。
const planItems = 25

// regressionID 是清单编号的形状：R 加一个十进制数。
var regressionID = regexp.MustCompile(`^R[1-9][0-9]*$`)

// 清单必须**至少**覆盖 R1–R25，并允许在其之上继续添加。
//
// # 只守一个方向
//
// 计划里那 25 条承诺一条都不能失去证据——这个方向必须守死，
// 否则「跑完回归就安全了」就成了一句没有依据的话。
//
// 反方向（"不许有计划之外的条目"）**已经取消**。开发计划在 41 个 Step 全部
// 完成后冻结成了历史记录（见它的头部说明），而项目还在继续改。继续守"不许多"
// 等于规定此后新增的每一条用户承诺都不准进这份清单——而这份清单恰恰是
// 「承诺 → 证明它的测试」的唯一落点。那会把一个防止清单变成谎话的守卫，
// 变成一个阻止清单继续记录真话的守卫。
//
// 新增条目从 R26 起顺延编号，不需要回头改开发计划。
func TestManifestCoversEveryChecklistItem(t *testing.T) {
	items := loadManifest(t)

	seen := map[string]int{}
	for _, it := range items {
		seen[it.id]++
		assert.NotEmpty(t, it.desc, "%s 没有说明", it.id)
		assert.NotEmpty(t, it.test, "%s 没有指向任何测试", it.id)
		assert.Regexp(t, regressionID, it.id,
			"第 %d 行的编号 %q 不是 R<数字>：编号是清单与执行目标之间的键，"+
				"错一个字那一行就再也不会被跑到，而清单看上去仍然是满的", it.line, it.id)
	}

	for i := 1; i <= planItems; i++ {
		id := "R" + strconv.Itoa(i)
		assert.Equal(t, 1, seen[id],
			"Step 37：清单里 %s 出现了 %d 次（应当正好 1 次）", id, seen[id])
		delete(seen, id)
	}
	// 剩下的是计划冻结之后新增的承诺：不限数量，但同样不许重号
	for id, n := range seen {
		assert.Equal(t, 1, n, "清单里 %s 出现了 %d 次（应当正好 1 次）", id, n)
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

		found, err := checklist.Listed(root, k.module, k.pkg, names)
		require.NoError(t, err)

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
