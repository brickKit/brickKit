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
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
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

// docs 收集 design/ 下的设计书、根目录的 AI-CONTEXT.md 与 README.md。
//
// 试用指南不在其中：那里的 YAML 多是"改这一行"的片段，而片段既没有
// kind: Component 也没有顶层 project:，本来就不会被分类到（见 classify）。
func docs(t *testing.T) []docFile {
	t.Helper()

	var out []docFile
	paths, err := filepath.Glob(filepath.Join(repoRoot, "design", "*.md"))
	require.NoError(t, err)
	paths = append(paths, filepath.Join(repoRoot, "AI-CONTEXT.md"),
		filepath.Join(repoRoot, "README.md"))

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
	for _, line := range strings.Split(body, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "kind: Component" {
			return reflect.TypeOf(manifest.Manifest{})
		}
		// 顶层 project:（不缩进）——brickkit.yaml 的必填第一字段
		if strings.HasPrefix(line, "project:") {
			return reflect.TypeOf(config.Config{})
		}
	}
	return nil
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

// ============================================================
// 反向：结构体有的字段，文档里一个字都没提
// ============================================================

// 每个字段都得在设计书里出现过。
//
// 守的是"加了字段却忘了写文档"——那种缺失不会让任何东西失败，
// 只会让使用者永远不知道有这个字段。`installer.publicKeys` 就是这么漏的：
// 008 §8 讲了整套签名机制，而它是**唯一**让验签真正生效的字段
// （没配公钥时 SignaturePolicy 直接放行），配置骨架里却一次都没出现。
//
// 判据宽松——只要求字段名在设计书里出现过，不要求出现在哪一节、
// 更不要求出现在骨架里。宽松是有意的：这条测试要抓的是"完全没提"，
// 不是"没写进某张表"。收紧了它会开始误伤，然后被人加例外，然后就没用了。
func TestEveryFieldIsMentionedInDesignDocs(t *testing.T) {
	var all strings.Builder
	for _, d := range docs(t) {
		all.WriteString(d.body)
	}
	text := all.String()

	fields := map[string]string{} // 字段名 → 它的完整路径（报错时指路）
	collectFields(reflect.TypeOf(config.Config{}), "brickkit.yaml", fields)
	collectFields(reflect.TypeOf(manifest.Manifest{}), "component.yaml", fields)
	require.NotEmpty(t, fields, "一个字段都没提取到——反射走岔了，结论不可信")

	var missing []string
	for name, path := range fields {
		if !strings.Contains(text, name) {
			missing = append(missing, path)
		}
	}
	sort.Strings(missing)

	assert.Empty(t, missing,
		"这些字段在设计书里一次都没出现过——使用者无从知道它们存在：\n   %s",
		strings.Join(missing, "\n   "))
	t.Logf("检查了 %d 个字段名", len(fields))
}

// collectFields 递归收集结构体的全部 yaml 字段名。
//
// map 的值不往下走：那里的键是使用者自己定的（component.config、
// ingressAnnotations），不是平台的字段。
func collectFields(typ reflect.Type, path string, out map[string]string) {
	for typ != nil && typ.Kind() == reflect.Pointer {
		typ = typ.Elem()
	}
	if typ == nil {
		return
	}

	switch typ.Kind() {
	case reflect.Slice, reflect.Array:
		collectFields(typ.Elem(), path+"[]", out)
		return
	case reflect.Struct:
	default:
		return
	}

	for i := 0; i < typ.NumField(); i++ {
		field := typ.Field(i)
		name, _, _ := strings.Cut(field.Tag.Get("yaml"), ",")
		if name == "" || name == "-" {
			continue // 未导出、或刻意不进 YAML（如 PasswordFromEnv）
		}
		child := path + "." + name
		if _, seen := out[name]; !seen {
			out[name] = child
		}
		collectFields(field.Type, child, out)
	}
}
