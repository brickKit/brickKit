package main

import (
	"bytes"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// sample 是一份带各种调用形态的夹具：Printf（头尾换行）、拼接链、Newf/WithDetailf、
// Sprintf、参数里嵌套另一处中文、带 %w 的 Errorf，以及两处工具不敢自动改的（const、比较）。
const sample = `package cli

import (
	"fmt"

	"github.com/brickkit/brickkit/internal/clierr"
)

const projectRule = "只能包含小写字母"

func demo(opts *Options, id string, err error) error {
	opts.Printf("\n✅ 已添加 %s（%d 个）\n", id, 3)
	e := clierr.New(clierr.CodeX, "错误：找不到"+id).
		WithDetail("路径", id).
		WithDetailf("详情", "%s：%d", id, 2)
	_ = fmt.Sprintf("共 %d 项", 4)
	opts.Printf("   看日志：%s\n", logs("<服务名>"))
	if id == "组件" {
		return e
	}
	return fmt.Errorf("读取 %s 失败：%w", id, err)
}
`

// fakeRepo 造一个只有 msgid / i18n 骨架的迷你仓库，并把 sample 放进 internal/cli/sample.go。
func fakeRepo(t *testing.T) (root, sampleFile string) {
	t.Helper()
	root = t.TempDir()
	write := func(rel, body string) {
		p := filepath.Join(root, rel)
		require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o755))
		require.NoError(t, os.WriteFile(p, []byte(body), 0o644))
	}
	write("internal/msgid/msgid.go", "package msgid\n\nconst (\n\tLabelPath = \"label.path\"\n)\n")
	write("internal/i18n/catalog_en.go", "package i18n\n\nvar en = map[string]string{\n\tmsgid.LabelPath: \"Path\",\n}\n")
	write("internal/i18n/catalog_zh.go", "package i18n\n\nvar zh = map[string]string{\n\tmsgid.LabelPath: \"路径\",\n}\n")
	write("internal/cli/sample.go", sample)
	return root, filepath.Join(root, "internal/cli/sample.go")
}

func read(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	require.NoError(t, err)
	return string(b)
}

func TestExtractClassifiesCallSites(t *testing.T) {
	_, file := fakeRepo(t)
	entries, err := extractFiles([]string{file})
	require.NoError(t, err)
	require.Len(t, entries, 11)

	byZH := map[string]Entry{}
	for _, e := range entries {
		byZH[e.ZH] = e
	}
	assert.Equal(t, "const", byZH["只能包含小写字母"].Manual, "const 里不能调 i18n.T，要人来处理")
	assert.Equal(t, "compare", byZH["组件"].Manual, "拿中文字面量做比较的，工具不敢改")

	var printf Entry
	for _, e := range entries {
		if strings.HasPrefix(e.ZH, "✅") {
			printf = e
		}
	}
	assert.Equal(t, "format:Printf", printf.Ctx)
	assert.Equal(t, "✅ 已添加 %[1]s（%[2]d 个）", printf.ZH, "头尾换行留在调用点，动词改成位置动词")
	assert.Equal(t, []string{"id", "3"}, printf.Args)
}

func TestApplyRewritesSourceAndCatalogs(t *testing.T) {
	root, file := fakeRepo(t)
	entries, err := extractFiles([]string{file})
	require.NoError(t, err)

	// 把 MANUAL 的排除在外后，按出现顺序给自动条目编号对应的译文
	dir := t.TempDir()
	require.NoError(t, writeEntries(filepath.Join(dir, "e.json"), entries))
	require.NoError(t, writeListing(filepath.Join(dir, "l.txt"), entries))
	listing := read(t, filepath.Join(dir, "l.txt"))
	assert.Contains(t, listing, "MANUAL", "工具不敢自动改的排在清单最后")

	// 清单里自动条目的编号不一定是 1..9（const 排在最前面），按 ZH 对号入座
	trans := map[string]string{
		"✅ 已添加 %[1]s（%[2]d 个）": "✅ Added %[1]s (%[2]d)",
		"错误：找不到%[1]s":          "Error: %[1]s not found",
		"路径":                   "@LabelPath",
		"详情":                   "Details",
		"%[1]s：%[2]d":          "%[1]s: %[2]d",
		"共 %[1]d 项":            "%[1]d items",
		"   看日志：%[1]s":         "   View the logs: %[1]s",
		"<服务名>":                "<service-name>",
		"读取 %[1]s 失败：":         "Failed to read %[1]s: ",
	}
	var lines []string
	for _, e := range entries {
		if e.Manual != "" {
			continue
		}
		en, ok := trans[e.ZH]
		require.True(t, ok, "夹具里有一条没准备译文：%q", e.ZH)
		lines = append(lines, strconv.Itoa(e.ID)+"\t"+en)
	}
	require.NoError(t, os.WriteFile(filepath.Join(dir, "t.txt"), []byte(strings.Join(lines, "\n")+"\n"), 0o644))

	var log bytes.Buffer
	report, err := applyTranslations(applyOptions{
		entriesPath: filepath.Join(dir, "e.json"), transPath: filepath.Join(dir, "t.txt"),
		root: root, pkg: "cli", log: &log,
	})
	require.NoError(t, err)
	assert.Equal(t, 9, report.applied)
	assert.Equal(t, 1, report.files)

	got := read(t, file)
	// 改写后仍然是合法的 Go 语法
	_, perr := parser.ParseFile(token.NewFileSet(), file, got, 0)
	require.NoError(t, perr, got)

	assert.Contains(t, got, `opts.Printf("\n%s\n", i18n.T(msgid.CliSampleAdded, id, 3))`,
		"Printf：头尾换行留在格式串里，文案整体换成 i18n.T")
	assert.Contains(t, got, `WithDetail(i18n.T(msgid.LabelPath), id)`, "@名字 复用已有共享 key")
	assert.Contains(t, got, `WithDetail(i18n.T(msgid.CliSampleDetails), i18n.T(msgid.CliSample`,
		"WithDetailf 变成 WithDetail，格式串与参数并进 i18n.T")
	assert.Contains(t, got, `_ = i18n.T(msgid.CliSampleItems, 4)`, "Sprintf 整个调用被换掉")
	assert.Contains(t, got, `opts.Printf("%s\n", i18n.T(msgid.CliSampleViewTheLogs, logs(i18n.T(msgid.CliSampleServiceName))))`,
		"嵌套：外层的参数里带着内层已经改好的文案")
	assert.Contains(t, got, `fmt.Errorf("%s%w", i18n.T(msgid.CliSampleFailedToRead, id), err)`,
		"结尾的 %w 留在调用点包装原错误")
	assert.Contains(t, got, `"只能包含小写字母"`, "MANUAL 的原样留着")
	assert.Contains(t, got, `"github.com/brickkit/brickkit/internal/i18n"`)
	assert.Contains(t, got, `"github.com/brickkit/brickkit/internal/msgid"`)

	// msgid 常量与两份目录
	msgidFile := read(t, filepath.Join(root, "internal/msgid/cli_sample.go"))
	assert.Contains(t, msgidFile, "package msgid")
	assert.Contains(t, msgidFile, `CliSampleAdded = "cli.sample.added"`)
	en := read(t, filepath.Join(root, "internal/i18n/catalog_en.go"))
	zh := read(t, filepath.Join(root, "internal/i18n/catalog_zh.go"))
	assert.Contains(t, en, `msgid.CliSampleAdded: "✅ Added %[1]s (%[2]d)",`)
	assert.Contains(t, zh, `msgid.CliSampleAdded: "✅ 已添加 %[1]s（%[2]d 个）",`)
	assert.Contains(t, en, `msgid.CliSampleServiceName: "<service-name>",`, "尖括号不转义")
	assert.Equal(t, 1, strings.Count(en, "msgid.LabelPath"), "复用已有 key，不重复建")

	// 第二遍：源码里只剩 MANUAL 的两处中文
	again, err := extractFiles([]string{file})
	require.NoError(t, err)
	require.Len(t, again, 2)
	for _, e := range again {
		assert.NotEmpty(t, e.Manual)
	}
}

func TestApplyLeavesFilesUntouchedOnError(t *testing.T) {
	root, file := fakeRepo(t)
	entries, err := extractFiles([]string{file})
	require.NoError(t, err)
	dir := t.TempDir()
	require.NoError(t, writeEntries(filepath.Join(dir, "e.json"), entries))

	var auto Entry
	for _, e := range entries {
		if e.Manual == "" {
			auto = e
			break
		}
	}
	require.NoError(t, os.WriteFile(filepath.Join(dir, "t.txt"), []byte(strconv.Itoa(auto.ID)+"\t@NoSuchConstant\n"), 0o644))

	before := read(t, file)
	_, err = applyTranslations(applyOptions{
		entriesPath: filepath.Join(dir, "e.json"), transPath: filepath.Join(dir, "t.txt"),
		root: root, pkg: "cli", log: &bytes.Buffer{},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "NoSuchConstant")
	assert.Equal(t, before, read(t, file), "出错时不改任何文件")
	assert.NoFileExists(t, filepath.Join(root, "internal/msgid/cli_sample.go"))
}

func TestReadTranslationsIgnoresCommentsAndRestoresNewlines(t *testing.T) {
	p := filepath.Join(t.TempDir(), "t.txt")
	require.NoError(t, os.WriteFile(p, []byte("# c\n\n1\tline one⏎line two\nbad line\n2\t@LabelPath\n"), 0o644))
	got, err := readTranslations(p)
	require.NoError(t, err)
	assert.Equal(t, map[int]string{1: "line one\nline two", 2: "@LabelPath"}, got)
}

func TestRunReportsUsageErrors(t *testing.T) {
	var log bytes.Buffer
	assert.Equal(t, 2, run(nil, &log))
	assert.Equal(t, 2, run([]string{"nosuch"}, &log))
	assert.Equal(t, 1, run([]string{"extract"}, &log), "extract 不给文件是错误")
	assert.Equal(t, 1, run([]string{"apply", "-entries", "/nonexistent.json"}, &log))
}
