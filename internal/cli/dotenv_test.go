// 本文件测试 parseDotEnv（项目根目录 .env 的解析）。
//
// brickKit 反馈：local-debug.*.env 序列化多行值和特殊字符会截断或解析错误——
// v0.4.4 复核指出，从前这里逐行按 `=` 切、只 Trim 掉值两端各一个引号字符，
// 一个跨多个物理行的双引号/单引号值会在第一行就被切断。这个文件同时被
// `docker compose` 读，所以规则不是凭印象定的，是拿真实 `docker compose config`
// 跑出来对照过的——TestParseDotEnvMatchesDockerCompose 就是那份对照本身，
// 没装 docker 时跳过，但下面每一条独立单测覆盖的都是被那次对照验证过的行为，
// 平时跑 go test 不需要 docker 也能测到。
package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func TestParseDotEnvSkipsBlankLinesAndComments(t *testing.T) {
	got := parseDotEnv("\n# 整行注释\n   # 前面有空白的注释\nKEY=value\n")
	assert.Equal(t, map[string]string{"KEY": "value"}, got)
}

func TestParseDotEnvSupportsExportPrefix(t *testing.T) {
	got := parseDotEnv("export KEY=value\n")
	assert.Equal(t, "value", got["KEY"])
}

func TestParseDotEnvTrimsSpaceAroundEquals(t *testing.T) {
	got := parseDotEnv("SPACED_KEY = trimmed\n")
	assert.Equal(t, "trimmed", got["SPACED_KEY"])
}

func TestParseDotEnvIgnoresLineWithoutEquals(t *testing.T) {
	got := parseDotEnv("NOEQ_LINE\nKEY=value\n")
	_, exists := got["NOEQ_LINE"]
	assert.False(t, exists)
	assert.Equal(t, "value", got["KEY"])
}

// 核心回归用例：多行双引号值必须完整读出来，这正是原始反馈的复现场景。
func TestParseDotEnvDoubleQuotedValueSpansMultipleLines(t *testing.T) {
	got := parseDotEnv("PEM=\"-----BEGIN PRIVATE KEY-----\nMIIBVQ\n-----END PRIVATE KEY-----\"\n")
	assert.Equal(t, "-----BEGIN PRIVATE KEY-----\nMIIBVQ\n-----END PRIVATE KEY-----", got["PEM"])
}

// 单引号同样要能跨行，且不处理任何转义（原样保留）。
func TestParseDotEnvSingleQuotedValueSpansMultipleLinesLiterally(t *testing.T) {
	got := parseDotEnv("CATALOG='crm.opportunity.edit|编辑商机\n第二行'\n")
	assert.Equal(t, "crm.opportunity.edit|编辑商机\n第二行", got["CATALOG"])
}

func TestParseDotEnvDoubleQuotedEscapes(t *testing.T) {
	got := parseDotEnv(`A="a\nb"` + "\n" +
		`B="a\tb"` + "\n" +
		`C="a\\b"` + "\n" +
		`D="has \"escaped\" quote"` + "\n")
	assert.Equal(t, "a\nb", got["A"])
	assert.Equal(t, "a\tb", got["B"])
	assert.Equal(t, `a\b`, got["C"])
	assert.Equal(t, `has "escaped" quote`, got["D"])
}

func TestParseDotEnvSingleQuotedValueKeepsBackslashLiteral(t *testing.T) {
	got := parseDotEnv(`E='single with \n literal backslash n'` + "\n")
	assert.Equal(t, `single with \n literal backslash n`, got["E"])
}

// 空白紧跟 # 才算行内注释；# 前面不是空白就是值的一部分。
func TestParseDotEnvUnquotedInlineComment(t *testing.T) {
	got := parseDotEnv("B=plain # not a comment for unquoted? test\nC=val#hash\n")
	assert.Equal(t, "plain", got["B"])
	assert.Equal(t, "val#hash", got["C"])
}

func TestParseDotEnvUnquotedValueTrimsSurroundingWhitespace(t *testing.T) {
	got := parseDotEnv("F=   spaced   \n")
	assert.Equal(t, "spaced", got["F"])
}

func TestParseDotEnvQuotedValueKeepsSurroundingWhitespace(t *testing.T) {
	got := parseDotEnv(`G="  keeps spaces  "` + "\n")
	assert.Equal(t, "  keeps spaces  ", got["G"])
}

// 闭引号之后同一行剩下的内容原样丢弃，不拼接进值里，也不报错。
func TestParseDotEnvDiscardsTrailingJunkAfterClosingQuote(t *testing.T) {
	got := parseDotEnv(`D="quoted"junk` + "\n")
	assert.Equal(t, "quoted", got["D"])
}

// TestParseDotEnvMatchesDockerCompose 是这份解析器的正确性依据本身：
// 拿同一份 .env 内容，一边喂给 parseDotEnv，一边真的跑 `docker compose config`
// 让 docker compose 自己解释，两边算出来的值必须逐字符相同。没装 docker 就跳过。
func TestParseDotEnvMatchesDockerCompose(t *testing.T) {
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("未安装 docker，跳过与真实 docker compose 的对照")
	}

	dotenv := "export EXPORTED=via-export\n" +
		"PEM=\"-----BEGIN PRIVATE KEY-----\nMIIBVQIBADANBgkqhkiG9w0BAQEFAASCAT8w\n-----END PRIVATE KEY-----\"\n" +
		"CATALOG='crm.opportunity.edit|编辑商机|action|crm/opportunity'\n" +
		"TABBED=\"a\\tb\"\n" +
		"BACKSLASH=\"a\\\\b\"\n" +
		"TRAILING=\"quoted\"junk\n" +
		"SPACED_KEY = trimmed\n" +
		"B=plain # not a comment for unquoted? test\n" +
		"C=val#hash\n"

	got := parseDotEnv(dotenv)

	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".env"), []byte(dotenv), 0o600))

	names := []string{"EXPORTED", "PEM", "CATALOG", "TABBED", "BACKSLASH", "TRAILING", "SPACED_KEY", "B", "C"}
	var compose strings.Builder
	compose.WriteString("services:\n  test:\n    image: busybox\n    environment:\n")
	for _, name := range names {
		compose.WriteString("      - " + name + "=${" + name + "}\n")
	}
	require.NoError(t, os.WriteFile(filepath.Join(dir, "compose.yaml"), []byte(compose.String()), 0o600))

	cmd := exec.Command("docker", "compose", "-f", filepath.Join(dir, "compose.yaml"), "config")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "docker compose config 失败：\n%s", out)

	var doc struct {
		Services struct {
			Test struct {
				Environment map[string]string `yaml:"environment"`
			} `yaml:"test"`
		} `yaml:"services"`
	}
	require.NoError(t, yaml.Unmarshal(out, &doc))

	for _, name := range names {
		assert.Equal(t, doc.Services.Test.Environment[name], got[name],
			"parseDotEnv 对 %s 的解释应该和真实 docker compose 一致", name)
	}
}
