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
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
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

// ============================================================
// 「完整字段参考」必须真的完整
// ============================================================

// referenceSkeleton 是自称"完整字段参考"的那两处。
//
// 它们与别处不同：附录 B.1 与 D.1 是**开发时查阅**的那一份（000 的阅读路径里
// 就是这么引导的——"附录 B：Manifest 完整字段参考（开发时查阅）"）。
// 一个字段没写进去，读者的结论就是"平台没有这个能力"。
type referenceSkeleton struct {
	heading string
	typ     reflect.Type
	what    string
}

var referenceSkeletons = []referenceSkeleton{
	{"### B.1 完整字段结构", reflect.TypeOf(manifest.Manifest{}), "component.yaml"},
	{"### D.1 完整字段结构", reflect.TypeOf(config.Config{}), "brickkit.yaml"},
}

// 附录 B.1 / D.1 里必须列全每一个字段。
//
// # 与上面那条宽松检查的分工
//
// TestEveryFieldIsMentionedInDesignDocs 只问"在设计书里出现过没有"，而且刻意
// 保持宽松——收紧了会开始误伤，然后被人加例外，然后就没用了。
//
// 这条不一样：它只盯**两处**，而那两处自己许下了"完整"这个承诺。守它不是收紧
// 一条宽松的规则，是让一句自我声明能被验证。
//
// 真漏过：`sources[].ref`（git 安装源指定分支 / tag / commit）在 003 §6.3 写得
// 清清楚楚，附录 D.1 里一个字都没有——而 D.1 恰恰是"配置时查阅"的那一份。
// 上面那条检查看见 003 提过就放行了。
func TestReferenceSkeletonsListEveryField(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join(repoRoot, "design", "附录合集.md"))
	require.NoError(t, err, "读不到附录合集——这条守卫失去了对象")
	body := string(raw)

	for _, ref := range referenceSkeletons {
		section := sectionAfter(t, body, ref.heading)

		fields := map[string]string{}
		collectFields(ref.typ, ref.what, fields)
		require.NotEmpty(t, fields, "%s：一个字段都没提取到，结论不可信", ref.heading)

		var missing []string
		for name, path := range fields {
			// 认字段名出现在 `名字:`（含 `- 名字:` 这种列表项、以及注释掉的那种）
			// 或表格 `| 名字 |` 里，就算列了
			if !regexp.MustCompile(`(?m)(^[\s#-]*` + regexp.QuoteMeta(name) + `\s*:|\|\s*` +
				regexp.QuoteMeta(name) + `\s*\|)`).MatchString(section) {
				missing = append(missing, path)
			}
		}
		sort.Strings(missing)

		assert.Empty(t, missing,
			"%s 自称「完整字段结构」，但这些字段没有列出来——"+
				"而它正是使用者开发/配置时查阅的那一份：\n   %s",
			ref.heading, strings.Join(missing, "\n   "))
	}
}

// sectionAfter 取出某个标题到下一个同级（或更高级）标题之间的内容。
//
// 必须**跳过围栏内的行**：YAML 骨架里满是 `# ===== 项目基本信息 =====` 这样的
// 注释，它们在行首、以 # 开头，正则一眼看去就是个标题——不跳的话这一节会被
// 切在第一行注释上，于是"字段没列出来"全体误报。
func sectionAfter(t *testing.T, body, heading string) string {
	t.Helper()
	i := strings.Index(body, heading)
	require.NotEqual(t, -1, i, "附录里找不到标题 %q——这条守卫失去了对象", heading)

	level := strings.Count(strings.SplitN(heading, " ", 2)[0], "#")
	var out []string
	inFence := false
	for _, line := range strings.Split(body[i+len(heading):], "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "```") {
			inFence = !inFence
		} else if !inFence && strings.HasPrefix(line, "#") {
			if n := len(line) - len(strings.TrimLeft(line, "#")); n <= level {
				break
			}
		}
		out = append(out, line)
	}
	return strings.Join(out, "\n")
}

// ============================================================
// 字段表：写在表里的字段必须真的存在
// ============================================================

// fieldTableTypes 是 `<!-- 字段表: X -->` 注解认得的类型名。
//
// 用注解而不是靠标题猜：一张表描述的是哪个结构体，只有写它的人知道
// （003 §4.4 那张"local（本地调试）"讲的是 config.Component 的两个字段，
// 从标题里推不出来）。注解一行，判据就唯一了。
var fieldTableTypes = map[string]reflect.Type{
	"config.Config":       reflect.TypeOf(config.Config{}),
	"config.Deploy":       reflect.TypeOf(config.Deploy{}),
	"config.Component":    reflect.TypeOf(config.Component{}),
	"config.Resource":     reflect.TypeOf(config.Resource{}),
	"config.Binding":      reflect.TypeOf(config.Binding{}),
	"config.Source":       reflect.TypeOf(config.Source{}),
	"config.Installer":    reflect.TypeOf(config.Installer{}),
	"manifest.Metadata":   reflect.TypeOf(manifest.Metadata{}),
	"manifest.Artifact":   reflect.TypeOf(manifest.Artifact{}),
	"manifest.Deployment": reflect.TypeOf(manifest.Deployment{}),
}

var fieldTableMark = regexp.MustCompile(`<!-- 字段表: ([\w.]+) -->`)

// 字段表里列的每个字段名，都必须是那个结构体真有的字段。
//
// # 为什么骨架检查覆盖不到它
//
// 上面那条查的是 ```yaml 围栏里的 YAML；而**字段表是 markdown 表格**，在它眼里
// 只是一段普通文本。可字段表恰恰是"查阅"用的那种东西——读者不会去数骨架里的
// 缩进，他会看那张表。
//
// 真出过：006 §3.2「资源字段说明」里写着 `database`（"默认数据库名，可被
// bindings 覆盖"），而 `config.Resource` 根本没有这个字段——照着填会被 CLI
// 当场拒绝（`resources[0].database：未知字段`），而报错还让人去查附录 D.1，
// 那里是对的。两份文档打架，读者按错的那份做。
//
// # 只查正向
//
// 不要求"结构体的字段都在表里"：好几张表是**有意的子集**（003 §4.4 只讲
// local / localPort 两个字段）。完备性由附录 B.1 / D.1 那条守着——
// 那两处自己许下了"完整"这个承诺，别处没有。
func TestFieldTablesListOnlyRealFields(t *testing.T) {
	tables := 0
	var bad []string

	for _, d := range docs(t) {
		lines := strings.Split(d.body, "\n")
		for i, line := range lines {
			m := fieldTableMark.FindStringSubmatch(line)
			if m == nil {
				continue
			}
			typ, ok := fieldTableTypes[m[1]]
			require.True(t, ok,
				"%s 第 %d 行：注解写的类型 %q 不认识——"+
					"要么拼错了，要么该加进 fieldTableTypes", d.name, i+1, m[1])
			tables++

			for _, name := range tableFieldNames(lines[i:]) {
				if !hasYAMLField(typ, name) {
					bad = append(bad, fmt.Sprintf("%s：字段表（%s）里的 %q 不存在于该结构体",
						d.name, m[1], name))
				}
			}
		}
	}

	require.NotZero(t, tables,
		"一张标注过的字段表都没找到——注解格式变了？那样这条检查会安静地什么都不查")
	sort.Strings(bad)
	assert.Empty(t, bad, "字段表写了不存在的字段：\n   %s", strings.Join(bad, "\n   "))
	t.Logf("检查了 %d 张字段表", tables)
}

// tableFieldNames 取出注解之后那张表第一列的字段名。
//
// 归一化两件事：去掉反引号（表里常写 `podSecurity`），以及点号路径只取最后一段
// （`deploy.target` / `migration.command` 指的是 target / command）。
func tableFieldNames(lines []string) []string {
	var out []string
	started := false
	for _, line := range lines {
		if !strings.HasPrefix(line, "|") {
			if started {
				break
			}
			continue
		}
		started = true
		cell := strings.TrimSpace(strings.Trim(strings.SplitN(line, "|", 3)[1], " "))
		cell = strings.Trim(cell, "`")
		if cell == "" || cell == "字段" || strings.HasPrefix(cell, "-") {
			continue
		}
		if i := strings.LastIndex(cell, "."); i >= 0 {
			cell = cell[i+1:]
		}
		out = append(out, cell)
	}
	return out
}
