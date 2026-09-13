// 本包守着**文档里画的字段骨架**与**结构体真认的字段**一致。
//
// # 为什么需要它
//
// 002 §2.3 删掉 `observability` 与 `compatibility` 之后，规范书改了，
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
// 却忘了写进 004 §10.2.1"，这条守的是"改了字段却忘了改骨架"。都是那种
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
	"github.com/brickkit/brickkit/internal/config"
	"github.com/brickkit/brickkit/internal/manifest"
	"github.com/brickkit/brickkit/internal/yamlcheck"
)

// repoRoot 是仓库根目录（本包在 tests/docfields/ 下）。
const repoRoot = "../.."

// docFile 是一份要检查的文档。
type docFile struct {
	name string
	body string
}

// 完整性检查（"每个字段都出现过"、"每张标注过的表都完整"）随 design/ 归档一并
// 移除——它们的判据依赖一份详尽的参考文档，而 design/ 归档后不再维护，
// docs()现在只扫两份刻意压缩的一页纸导读（AGENTS.md、README.md），要求
// 它们详尽是不合理的。docs/en/architecture 长出comprehensive 内容之后，
// 应该在那里重新引入等价的详尽性检查，而不是勉强让这两个刻意收窄的文件满足
// 一条为详尽参考文档设计的判据。

// docs 收集根目录的 AGENTS.md 与 README.md。
//
// design/ 已归档为历史记录，不再参与"文档跟不跟得上 CLI"的验证——继续验证
// 一份承诺不再更新的文档没有意义。试用指南也不在其中：那里的 YAML 多是
// "改这一行"的片段，本来就不会被分类到（见 classify）。
func docs(t *testing.T) []docFile {
	t.Helper()

	var out []docFile
	paths := []string{
		filepath.Join(repoRoot, "AGENTS.md"),
		filepath.Join(repoRoot, "README.md"),
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

// classify 判断这段 YAML 是不是一份完整的 component.yaml / brickkit.yaml 骨架。
//
// 判据刻意窄：只认**完整骨架**（有 kind: Component，或有顶层 project:）。
// 片段（"给这个组件加一行 expose: true"）不检查——它们没有上下文，
// 拿全结构体去比会把一堆正常写法判成错的。
//
// 生成物（docker-compose.yaml、K8s 清单）因此天然被排除：
// 它们既没有 kind: Component，也没有顶层 project:。
func classify(body string) reflect.Type {
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

	// 判据是"顶层键是不是全都属于某一边"，而不是"有没有那个招牌字段"。
	//
	// 从前只认整份骨架（有 kind: Component 或顶层 project:），于是 221 段 YAML
	// 里只查了 24 段——而剩下那 197 段恰恰是**人们真正照抄的东西**：
	// components: 46 段、apiVersion: 20 段、resources: 16 段、sources: 13 段、
	// dependencies: 11 段、deployment: 9 段……真出过的两个字段 bug
	// （006 §3.2 教了一个不存在的 resources[].database、附录 D.1 漏了
	// sources[].ref）都在那 197 段的势力范围里。
	//
	// 片段本身就是合法的部分文档：`resources:` 开头的那段就是一份只写了
	// resources 的 brickkit.yaml，直接拿去 Walk 即可。
	cfg, man := reflect.TypeOf(config.Config{}), reflect.TypeOf(manifest.Manifest{})
	inCfg, inMan := true, true
	for _, k := range keys {
		if !hasYAMLField(cfg, k) {
			inCfg = false
		}
		if !hasYAMLField(man, k) {
			inMan = false
		}
	}
	switch {
	case inMan && !inCfg:
		return man
	case inCfg:
		// 两边都认（如 resources / version）时归 brickkit.yaml：那边的顶层
		// 字段集合更大，误判成它只会让检查更宽，不会冤枉正确的文档
		return cfg
	default:
		// 顶层出现了两边都不认的键——那多半根本不是这两份文件之一
		// （K8s 清单、docker-compose、示意用的伪 YAML）
		return nil
	}
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

// hasYAMLField 判断结构体有没有这个 yaml 字段名。
func hasYAMLField(typ reflect.Type, name string) bool {
	for i := 0; i < typ.NumField(); i++ {
		if tag := strings.Split(typ.Field(i).Tag.Get("yaml"), ",")[0]; tag == name {
			return true
		}
	}
	return false
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
			typ := classify(block.body)
			if typ == nil {
				continue
			}
			checked++

			var root yaml.Node
			if err := yaml.Unmarshal([]byte(block.body), &root); err != nil {
				t.Errorf("%s 第 %d 行的骨架不是合法 YAML：%v\n"+
					"   骨架是给人照抄的，抄不动就没有意义", block.doc, block.line, err)
				continue
			}
			if len(root.Content) == 0 {
				continue
			}

			p := clierr.NewProblemSet(clierr.CodeConfigInvalid, "未知字段")
			yamlcheck.Walk(root.Content[0], typ, p)

			for _, problem := range p.Items() {
				t.Errorf("%s 第 %d 行：%s —— %s\n"+
					"   照着这份骨架写出来的 %s 会被 CLI 当场拒绝",
					block.doc, block.line, problem.Field, problem.Reason, fileNameOf(typ))
			}
		}
	}

	// 自检：一个骨架都没认出来时，上面的"全过"没有任何意义
	require.GreaterOrEqual(t, checked, 4,
		"只认出 %d 个骨架——classify 的判据坏了，这条测试的结论不可信", checked)
	t.Logf("检查了 %d 段完整骨架", checked)
}

func fileNameOf(typ reflect.Type) string {
	if typ == reflect.TypeOf(manifest.Manifest{}) {
		return "component.yaml"
	}
	return "brickkit.yaml"
}
