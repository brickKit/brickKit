package cli

// 组件文档从头到尾走一遍，照着 be-assembly-standard 那种项目的用法：init 一个项目、new 两个组件、
// 填好其中一个的文档（带一份中文译本）、add --local，再看 AGENTS.md 的组件表与 lint 的文档检查。

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/i18n"
)

var todoComment = regexp.MustCompile(`<!-- TODO: [^>]*-->`)

// fillTodos 把骨架里的每条 TODO 提示换成一句正文——模拟作者把文档写完。
func fillTodos(t *testing.T, path string) string {
	t.Helper()
	body := todoComment.ReplaceAllString(readFile(t, path), "Written.")
	require.NoError(t, os.WriteFile(path, []byte(body), 0o644))
	return body
}

func zhSixSections(id string) string {
	return "# " + id + "\n\n## 组件定位\n销售订单。\n\n## 部署前准备\n除下面的配置外无需准备。\n\n## 依赖说明\n- mdm/customer：客户是谁。\n\n" +
		"## 配置指南\n无。\n\n## 契约索引\n- `api/service.proto`\n\n## 外壳声明\n不是外壳。\n"
}

func TestComponentDocsEndToEnd(t *testing.T) {
	root := t.TempDir()
	require.Equal(t, clierr.ExitOK, runIn(t, root, "init", "shop").code)
	shop := filepath.Join(root, "shop")
	require.Equal(t, clierr.ExitOK, runIn(t, shop, "new", "mdm/customer").code)
	require.Equal(t, clierr.ExitOK, runIn(t, shop, "new", "erp/sales", "--contract", "proto").code)

	sales := filepath.Join(shop, "components", "erp", "sales")
	appendTo(t, filepath.Join(sales, "component.yaml"), "\ndependencies:\n  components:\n    - mdm/customer@0.1.0\n")
	doc := fillTodos(t, filepath.Join(sales, "BRICKKIT.md"))
	doc = strings.Replace(doc, "## Dependencies\n\nWritten.", "## Dependencies\n\n- mdm/customer: who the customer is.", 1)
	require.NoError(t, os.WriteFile(filepath.Join(sales, "BRICKKIT.md"), []byte(doc), 0o644))
	fillTodos(t, filepath.Join(sales, "AGENTS.md"))
	fillTodos(t, filepath.Join(sales, "README.md"))
	require.NoError(t, os.WriteFile(filepath.Join(sales, "BRICKKIT.zh.md"), []byte(zhSixSections("erp/sales")), 0o644))

	r := runWith(t, func(o *Options) { o.Engine = newFakeEngine() }, shop, "add", "--local", "--yes")
	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)

	agents := readFile(t, filepath.Join(shop, "AGENTS.md"))
	assert.Regexp(t, `\| erp/sales \| 0\.1\.0 \| [^|]+ \| BRICKKIT\.md \+zh \| — \|`, agents)
	assert.Regexp(t, `\| mdm/customer \| 0\.1\.0 \| [^|]+ \| BRICKKIT\.md \| — \|`, agents)

	lint := runIn(t, shop, "lint")
	require.Equal(t, clierr.ExitOK, lint.code, "documentation problems are warnings: "+lint.stdout)
	assert.NotContains(t, lint.stdout, "does not mention")
	assert.Contains(t, lint.stdout, filepath.Join("components", "mdm", "customer", "BRICKKIT.md"), "the TODOs left in mdm/customer are reported")
	assert.NotContains(t, lint.stdout, "File: "+filepath.Join("components", "erp", "sales", "BRICKKIT.md"), "erp/sales is complete")

	// 文档里删掉对依赖的说明：lint 点名缺了哪条依赖
	require.NoError(t, os.WriteFile(filepath.Join(sales, "BRICKKIT.md"),
		[]byte(strings.Replace(doc, "- mdm/customer: who the customer is.", "None.", 1)), 0o644))
	lint = runWithLogs(t, shop, "lint")
	assert.Contains(t, lint.stdout, "component.yaml depends on mdm/customer")
	assert.Contains(t, lint.stderr, "DOC_OUT_OF_STEP")

	// 中文的同事再跑一次 add：维护区仍按它记下的语言（en）写，不跟着他的 CLI 语言变
	prev := i18n.Current()
	t.Cleanup(func() { i18n.SetCurrent(prev) }) // 命令会把当前语言设成 zh：别漏给后面的测试
	t.Setenv("BRICKKIT_LANG", "zh")
	r = runWith(t, func(o *Options) { o.Engine = newFakeEngine() }, shop, "add", "--local", "--yes")
	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)
	agents = readFile(t, filepath.Join(shop, "AGENTS.md"))
	assert.Contains(t, agents, "<!-- brickkit:managed:begin lang=en -->")
	assert.Equal(t, 1, strings.Count(agents, "## Components"), "still the English template, rendered once")
}
