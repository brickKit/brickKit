// 本包守着 Step 32–35 的验收清单（边界 / 错误处理 / 兼容性 / 安全）。
//
// # 这里为什么不是 75 条新测试
//
// 那 75 项**早就有测试覆盖**——它们在写各自的解析器、错误路径、
// 生成器时就顺手写了，只是散落在被测代码旁边。再抄一遍没有意义。
//
// 清单缺的从来不是覆盖，是**连接**：计划里那 75 项是 75 个要人手打勾的方框，
// 谁也说不出「35.11 不声明 expose 不生成 Ingress」到底由哪个测试保证。
// 于是有两个坏结果：
//
//	改了生成器之后，不知道该重点看哪几条测试有没有红
//	某条测试被改名或删掉，清单还挂着 ☑ —— **清单变成了谎话，而且没人会发现**
//
// # 以及一个更难看的历史
//
// 这四个 Step 的目录（tests/boundary、tests/error、tests/compat、tests/security）
// 从 Step 1 建起来之后**一直是空的**，只有一个 .gitkeep。
// 而 `make test-all` 里那四行照跑不误，每次打印「暂无测试文件，跳过」。
// 看起来像"这几类还没写"，实际是写完了、只是不在那儿。
//
// 一个永远不会失败的检查等于没有检查；一个永远跳过、却还在成绩单上
// 占一行的检查比那更坏——它让人以为那一格是空的，而不是错位的。
package checklist_test

import (
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/brickkit/brickkit/tests/checklist"
)

const manifestName = "清单.tsv"

// 计划里每个 Step 的验收项数量（开发计划 Step 32–35）。
//
// total 是**下限**，不是总数：见 TestManifestCoversEveryPlanItem。
var plan = []struct {
	category string
	step     string
	total    int
}{
	{"boundary", "32", 25},
	{"error", "33", 20},
	{"compat", "34", 12},
	{"security", "35", 18},
}

// 列序号，与 清单.tsv 的表头一致。
const (
	colCategory = iota
	colID
	colDesc
	colKind
	colModule
	colPkg
	colTest
	numCols
)

func load(t *testing.T) []checklist.Row {
	t.Helper()
	rows, err := checklist.Load(manifestName, numCols)
	require.NoError(t, err, "读不到验收清单")
	require.NotEmpty(t, rows, "清单是空的——那不是「全部通过」，是根本没读到东西")
	return rows
}

func repoRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	require.NoError(t, err)
	return root
}

// 清单必须覆盖 Step 32–35 的每一项，一项都不能少；允许在其之上继续添加。
//
// # 只守一个方向
//
// 计划里那 75 项一条都不能失去证据——这个方向必须守死，否则
// "跑完这四类就安全了"就成了一句没有依据的话。
//
// 反方向（"不许有计划之外的条目"）**已经取消**。开发计划在 41 个 Step 全部
// 完成后冻结成了历史记录（见它的头部说明），而项目还在继续改：今天补一条
// 边界用例、明天补一条安全用例，它们都该落在这份清单上，因为这里是
// 「验收项 → 证明它的证据」的唯一落点。继续守"不许多"，等于规定此后新增的
// 每一项都不准被记录——那是把防止清单变成谎话的守卫，变成阻止它继续记录真话。
//
// 新增条目在对应 Step 的编号上顺延即可（如 security.19），
// 类别与编号必须对得上仍由 TestCategoryMatchesStepNumber 守着。
func TestManifestCoversEveryPlanItem(t *testing.T) {
	rows := load(t)

	seen := map[string]int{}
	for _, r := range rows {
		id := r.Cell(colID)
		seen[id]++
		assert.NotEmpty(t, r.Cell(colDesc), "第 %d 行（%s）没有说明", r.Line, id)
		assert.NotEmpty(t, r.Cell(colTest), "第 %d 行（%s）没有指向任何证据", r.Line, id)
	}

	knownStep := map[string]bool{}
	for _, p := range plan {
		knownStep[p.step] = true
		for i := 1; i <= p.total; i++ {
			id := p.step + "." + strconv.Itoa(i)
			assert.NotZero(t, seen[id],
				"Step %s：清单里找不到 %s——这一项在计划里是 ✅，"+
					"却没有任何东西证明它今天还成立", p.step, id)
			delete(seen, id)
		}
	}
	// 剩下的是计划冻结之后新增的验收项：不限数量，但必须仍属于这四个 Step，
	// 否则 make test-boundary 那几个目标筛不到它，那一行从此不会被执行
	for id := range seen {
		step, _, ok := strings.Cut(id, ".")
		assert.True(t, ok && knownStep[step],
			"清单里的 %s 不属于 Step 32–35 中的任何一个——"+
				"那样它不会被任何 make 目标跑到，而清单看上去仍然是满的", id)
	}
}

// 每一行的类别必须与编号对得上。
//
// 类别是 `make test-boundary` 这些目标用来筛行的键。错一个字，
// 那一整行就从此不再被任何目标执行，而清单看上去仍然是满的。
func TestCategoryMatchesStepNumber(t *testing.T) {
	byStep := map[string]string{}
	for _, p := range plan {
		byStep[p.step] = p.category
	}

	for _, r := range load(t) {
		step, _, ok := strings.Cut(r.Cell(colID), ".")
		require.True(t, ok, "第 %d 行的编号 %q 不是 <Step>.<项> 的形式", r.Line, r.Cell(colID))
		assert.Equal(t, byStep[step], r.Cell(colCategory),
			"第 %d 行：编号 %s 属于 Step %s，类别却写成了 %q",
			r.Line, r.Cell(colID), step, r.Cell(colCategory))
	}
}

// 清单点名的每个测试都必须**真的存在且能被跑到**。
//
// 这条是整个包存在的理由：它是清单与现实之间唯一的连接。
// 没有它，改名一个测试就能让某条承诺悄无声息地失去证据。
func TestEveryListedTestStillExists(t *testing.T) {
	root := repoRoot(t)

	type key struct{ module, pkg string }
	groups := map[key][]checklist.Row{}
	var order []key
	for _, r := range load(t) {
		if r.Cell(colKind) != "test" {
			continue
		}
		k := key{r.Cell(colModule), r.Cell(colPkg)}
		if _, ok := groups[k]; !ok {
			order = append(order, k)
		}
		groups[k] = append(groups[k], r)
	}
	require.NotEmpty(t, order, "一行 test 类型的证据都没有——多半是列序号错了")

	for _, k := range order {
		group := groups[k]
		names := make([]string, 0, len(group))
		for _, r := range group {
			names = append(names, r.Cell(colTest))
		}

		found, err := checklist.Listed(root, k.module, k.pkg, names)
		require.NoError(t, err)

		where := filepath.Join(k.module, strings.TrimPrefix(k.pkg, "./"))
		for _, r := range group {
			assert.True(t, found[r.Cell(colTest)],
				"%s（%s）指向的测试 %s 在 %s 里已经不存在了。\n"+
					"    清单少一条证据就等于多一句没人验证的话——\n"+
					"    请把它改成现在真正覆盖这条承诺的测试，而不是从清单里删掉这一项。",
				r.Cell(colID), r.Cell(colDesc), r.Cell(colTest), where)
		}
	}
}

// record 类型的证据指向的文件必须存在。
//
// 有极少数验收项不是单个测试，而是一次全量审计（例如 33.17 扫了 154 处错误构造）。
// 它们的证据是那份记录本身——记录被删或改名，这一项同样失去了依据。
func TestRecordEvidenceFilesExist(t *testing.T) {
	root := repoRoot(t)
	checked := 0

	for _, r := range load(t) {
		if r.Cell(colKind) != "record" {
			continue
		}
		path := filepath.Join(root, r.Cell(colTest))
		assert.FileExists(t, path,
			"%s（%s）的证据文件不存在了", r.Cell(colID), r.Cell(colDesc))
		checked++
	}
	assert.NotZero(t, checked, "一条 record 证据都没查到——清单或列序号可能错了")
}

// 守卫本身会不会坏？
//
// 会——如果 `go test -list` 的输出格式变了、或者包路径拼错了，
// 上面那条会「一个都没找到」，看起来和「测试全被删了」一模一样。
// 所以这里拿一个**故意不存在**的名字验证：查不到的东西必须查不到。
// 如果连它都「找到了」，说明匹配逻辑坏了，上面那条的绿灯不可信。
func TestGuardDetectsMissingTest(t *testing.T) {
	cmd := exec.Command("go", "test", "-list",
		"^TestThisNameDeliberatelyDoesNotExist$", "./internal/cli")
	cmd.Dir = repoRoot(t)
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "%s", out)

	assert.NotContains(t, string(out), "TestThisNameDeliberatelyDoesNotExist",
		"一个不存在的测试被「找到」了，说明存在性检查根本没在工作——"+
			"那么 TestEveryListedTestStillExists 的绿灯也不可信")
}
