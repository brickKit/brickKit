// Package checklist 是"清单 → 测试"这条连接的共用机制。
//
// 项目里有两份这样的清单：
//
//	tests/regression/清单.tsv    开发计划 Step 37 的 25 条回归承诺
//	tests/checklist/清单.tsv     Step 32–35 的边界 / 错误 / 兼容 / 安全验收项
//
// 两份清单要做的事完全一样：**让"某某项已验证"这句话无法悄悄变成谎话**。
// 计划里的验收项本身只是一个个要人手打勾的方框，勾上之后没人说得出
// 到底是哪个测试保证了它；而测试一旦被改名或删掉，方框还勾着。
//
// 所以这里提供两件东西：读清单，以及**问 go test 那个测试今天还在不在**。
//
// # 为什么用 `go test -list` 而不是搜源码
//
// 搜 `func TestXxx(` 也能确认名字在，但确认不了它**能被跑到**：
// 文件带了构建标签、包编译不过、测试名写在注释里——这些情况下
// 源码里有那串字符，`go test` 却一条也跑不到。
// `-list` 走的是真正的编译与注册流程，它说有才是真的有。
package checklist

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Row 是清单里的一行（已按制表符切开）。Line 是行号，用来把错误指回原文件。
type Row struct {
	Cells []string
	Line  int
}

// Cell 取第 i 列；越界返回空串，让调用方自己决定怎么报错。
func (r Row) Cell(i int) string {
	if i < 0 || i >= len(r.Cells) {
		return ""
	}
	return strings.TrimSpace(r.Cells[i])
}

// Load 读一份 TSV 清单：跳过空行与 # 注释，并要求每行正好 cols 列。
//
// 列数写死是有意的：清单是人手维护的，少一个制表符就会让某一列
// 悄悄错位——错位之后每一列都还"有值"，只是全都指向了别处。
func Load(path string, cols int) ([]Row, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var rows []Row
	scanner := bufio.NewScanner(f)
	for n := 1; scanner.Scan(); n++ {
		line := scanner.Text()
		if strings.TrimSpace(line) == "" || strings.HasPrefix(line, "#") {
			continue
		}
		cells := strings.Split(line, "\t")
		if len(cells) != cols {
			return nil, fmt.Errorf("%s 第 %d 行应有 %d 列，实际 %d 列：%q",
				filepath.Base(path), n, cols, len(cells), line)
		}
		rows = append(rows, Row{Cells: cells, Line: n})
	}
	return rows, scanner.Err()
}

// Listed 问 go test：module/pkg 这个包里，names 里的测试哪些真的存在。
//
// root 是仓库根目录，module 是 "." 或 "market-server"（那是**另一个 Go module**，
// 根模块的 ./... 到不了它），pkg 形如 "./internal/cli"。
func Listed(root, module, pkg string, names []string) (map[string]bool, error) {
	pattern := "^(" + strings.Join(names, "|") + ")$"

	cmd := exec.Command("go", "test", "-list", pattern, pkg)
	cmd.Dir = filepath.Join(root, module)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("在 %s 里列举 %s 的测试失败：\n%s", module, pkg, out)
	}

	found := map[string]bool{}
	for _, line := range strings.Split(string(out), "\n") {
		if s := strings.TrimSpace(line); s != "" {
			found[s] = true
		}
	}
	return found, nil
}
