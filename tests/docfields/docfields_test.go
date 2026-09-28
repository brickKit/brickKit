// 本包守着**文档里画的字段骨架**与**结构体真认的字段**一致。
//
// # 为什么需要它
//
// Manifest 删掉 `observability` 与 `compatibility` 之后，规范改了，
// 而根目录 AI-CONTEXT.md 的 Manifest 骨架里那两个字段一直留着。
// 那份文件开头写着「写给 AI 助手的，读完这一份就够了」——于是 AI 照着它
// 教用户写 component.yaml，用户第一条命令就撞上：
//
//	❌ 错误：component.yaml 校验失败
//	   observability：未知字段（第 15 行）
//
// 已有的五个文档守卫一个都抓不到它：它们查的是**小节引用、断链、命令、参数、
// 目录树、预期输出**，而 `observability:` 在它们眼里只是一段普通文本。
// 缺口不在"漏了一份文件"（check-cli-docs.py 确实扫 AI-CONTEXT.md 并通过了），
// 而在守卫的**形状**——没有一个盯着 YAML 字段名。
//
// # 真相来源是结构体本身，不是又抄一份清单
//
// 正向检查直接调 `yamlcheck.Walk`——**CLI 拒绝未知字段用的就是它**。
// 两边不可能给出不同的答案，因为它们是同一段代码。
//
// 形状上照着 clierr.TestEveryErrorCodeIsDocumented：那条守的是"新增了错误码
// 却忘了写进错误码文档"，这条守的是"改了字段却忘了改骨架"。都是那种
// **不会让任何东西失败**、只会让照着文档做的人撞墙的缺失。
package docfields_test

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"

	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/deployfile"
	"github.com/brickkit/brickkit/internal/manifest"
	"github.com/brickkit/brickkit/internal/projfile"
	"github.com/brickkit/brickkit/internal/yamlcheck"
)

// repoRoot 是仓库根目录（本包在 tests/docfields/ 下）。
const repoRoot = "../.."

// docFile 是一份要检查的文档。
type docFile struct {
	name string
	body string
}

// 完整性检查（"每个字段都出现过"）随 design/ 归档一并移除——它的判据依赖一份
// 详尽的参考文档，而 docs() 只扫几份刻意压缩的一页纸导读（AGENTS.md/
// AGENTS.zh.md、README.md/README.zh.md），要求它们详尽是不合理的。等价的检查
// 现在落在真正详尽的那份文档上，见 reference_test.go。

// docs 收集根目录的 AGENTS.md/AGENTS.zh.md 与 README.md/README.zh.md。
//
// AGENTS.zh.md、README.zh.md 各自与英文版内容对等（不是翻译附属），YAML
// 骨架逐字相同，一并扫描能防止两份文件里的骨架悄悄改出分叉。
//
// design/ 已归档为历史记录，不再参与"文档跟不跟得上 CLI"的验证——继续验证
// 一份承诺不再更新的文档没有意义。教程也不在其中：那里的 YAML 多是
// "改这一行"的片段，本来就不会被认成三种文件之一（见 candidates）。
func docs(t *testing.T) []docFile {
	t.Helper()

	var out []docFile
	paths := []string{
		filepath.Join(repoRoot, "AGENTS.md"),
		filepath.Join(repoRoot, "AGENTS.zh.md"),
		filepath.Join(repoRoot, "README.md"),
		filepath.Join(repoRoot, "README.zh.md"),
	}

	for _, path := range paths {
		body, err := os.ReadFile(path)
		require.NoError(t, err)
		out = append(out, docFile{name: filepath.Base(path), body: string(body)})
	}
	require.NotEmpty(t, out)
	return out
}

// yamlBlock 是文档里一段 ```yaml 代码块。
type yamlBlock struct {
	doc  string
	line int // 代码块起始行号，报错时指路用
	body string
}

// yamlBlocksOf 抽出一份文档里的所有 ```yaml 块。
func yamlBlocksOf(d docFile) []yamlBlock {
	var out []yamlBlock
	lines := strings.Split(d.body, "\n")

	for i := 0; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) != "```yaml" {
			continue
		}
		start := i + 1
		var body []string
		for i++; i < len(lines) && !strings.HasPrefix(strings.TrimSpace(lines[i]), "```"); i++ {
			body = append(body, lines[i])
		}
		out = append(out, yamlBlock{doc: d.name, line: start, body: strings.Join(body, "\n")})
	}
	return out
}

// skeletonTypes 是文档里的 YAML 块可能描述的三种文件，与各自的文件名。
var skeletonTypes = []struct {
	typ  reflect.Type
	file string
}{
	{reflect.TypeOf(manifest.Manifest{}), "component.yaml"},
	{reflect.TypeOf(projfile.File{}), "brickkit.yaml"},
	{reflect.TypeOf(deployfile.File{}), "deploy.yaml"},
}

// candidates 列出这段 YAML 可能是哪几种文件：顶层键全都属于它的那些。
//
// 判据是"顶层键是不是全都属于某一边"，而不是"有没有那个招牌字段"。
//
// 从前只认整份骨架（有 kind: Component 或顶层 project:），于是 221 段 YAML
// 里只查了 24 段——而剩下那 197 段恰恰是**人们真正照抄的东西**：
// components: 46 段、apiVersion: 20 段、sources: 13 段、
// dependencies: 11 段、deployment: 9 段……真出过的两个字段 bug
// （教了一个不存在的字段、字段参考漏了 sources[].ref）都在那 197 段的势力范围里。
// 片段本身就是合法的部分文档，直接拿去 Walk 即可。
//
// 顶层出现了三边都不认的键——那多半根本不是这三种文件之一
// （K8s 清单、docker-compose、config/ 下的配置、示意用的伪 YAML），不查。
func candidates(body string) []int {
	// K8s 清单长得很像 component.yaml（apiVersion / kind / metadata 三个键都一样），
	// 而它的 metadata.labels 在 Manifest 里不存在。先按 kind 把它们剔出去。
	for _, line := range strings.Split(body, "\n") {
		if trimmed := strings.TrimSpace(line); strings.HasPrefix(trimmed, "kind:") {
			if strings.TrimSpace(strings.TrimPrefix(trimmed, "kind:")) != "Component" {
				return nil
			}
		}
	}

	keys := topLevelKeys(body)
	if len(keys) == 0 {
		return nil
	}
	var out []int
	for i, st := range skeletonTypes {
		known := yamlcheck.KnownFields(st.typ)
		all := true
		for _, k := range keys {
			if _, ok := known[k]; !ok {
				all = false
				break
			}
		}
		if all {
			out = append(out, i)
		}
	}
	return out
}

// skeletonProblems 检查一段 YAML：checked 表示它被认成了三种文件之一。
//
// 同一段 YAML 可能同时像两种文件（brickkit.yaml 与 deploy.yaml 都有 components:）。
// 只要其中**一种**能完整接受它，它就是对的；都不接受时，按问题最少的那种报——
// 那多半就是作者想写的文件。
func skeletonProblems(body string) (checked bool, file string, problems []clierr.Problem) {
	idx := candidates(body)
	if len(idx) == 0 {
		return false, "", nil
	}
	var root yaml.Node
	if err := yaml.Unmarshal([]byte(body), &root); err != nil {
		return true, skeletonTypes[idx[0]].file, []clierr.Problem{{Field: "(YAML)", Reason: err.Error()}}
	}
	if len(root.Content) == 0 {
		return false, "", nil
	}
	for n, i := range idx {
		p := clierr.NewProblemSet(clierr.CodeConfigInvalid, "未知字段")
		yamlcheck.Walk(root.Content[0], skeletonTypes[i].typ, p)
		items := p.Items()
		if len(items) == 0 {
			return true, skeletonTypes[i].file, nil
		}
		if n == 0 || len(items) < len(problems) {
			file, problems = skeletonTypes[i].file, items
		}
	}
	return true, file, problems
}

// topLevelKeys 取出不缩进的那些键名。
func topLevelKeys(body string) []string {
	var out []string
	for _, line := range strings.Split(body, "\n") {
		if line == "" || line[0] == ' ' || line[0] == '\t' || line[0] == '#' || line[0] == '-' {
			continue
		}
		if i := strings.Index(line, ":"); i > 0 {
			out = append(out, strings.TrimSpace(line[:i]))
		}
	}
	return out
}

// ============================================================
// 正向：文档写了结构体不认识的字段
// ============================================================

// 文档里画的骨架，必须能被 CLI 原样接受。
//
// 用的是 yamlcheck.Walk——CLI 解析 component.yaml / brickkit.yaml 时
// 拒绝未知字段用的**同一段代码**。所以这条测试与运行时不可能给出不同答案。
func TestDocSkeletonsUseOnlyKnownFields(t *testing.T) {
	checked := 0

	for _, d := range docs(t) {
		for _, block := range yamlBlocksOf(d) {
			ok, file, problems := skeletonProblems(block.body)
			if !ok {
				continue
			}
			checked++
			for _, problem := range problems {
				t.Errorf("%s 第 %d 行：%s —— %s\n"+
					"   照着这份骨架写出来的 %s 会被 CLI 当场拒绝",
					block.doc, block.line, problem.Field, problem.Reason, file)
			}
		}
	}

	// 自检：一个骨架都没认出来时，上面的"全过"没有任何意义
	require.GreaterOrEqual(t, checked, 4,
		"只认出 %d 个骨架——candidates 的判据坏了，这条测试的结论不可信", checked)
	t.Logf("检查了 %d 段骨架", checked)
}
