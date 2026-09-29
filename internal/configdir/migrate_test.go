package configdir_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"

	"github.com/brickkit/brickkit/internal/configdir"
	"github.com/brickkit/brickkit/internal/manifest"
)

func schema(props map[string]manifest.ConfigProperty, required ...string) *manifest.ConfigSchema {
	return &manifest.ConfigSchema{Type: "object", Properties: props, Required: required}
}

func str(def any) manifest.ConfigProperty {
	return manifest.ConfigProperty{Type: "string", Default: def}
}

func migrate(t *testing.T, old string, oldS, newS *manifest.ConfigSchema, choose func(configdir.Conflict) configdir.Choice) (string, configdir.MigrateReport) {
	t.Helper()
	if choose == nil {
		choose = func(configdir.Conflict) configdir.Choice { return configdir.ChooseDuplicate }
	}
	out, report, err := configdir.Migrate(configdir.MigrateInput{
		ID: "erp/api", FromVersion: "1.0.0", ToVersion: "2.0.0",
		Old: []byte(old), OldSchema: oldS, NewSchema: newS, Choose: choose,
	})
	require.NoError(t, err)
	return string(out), report
}

// 提案 §12.2：使用者写过、新版本还有的键，原样抄过去——$var:、${}、引号一个字符都不动。
func TestMigrateCopiesWrittenKeys(t *testing.T) {
	s := schema(map[string]manifest.ConfigProperty{
		"DB_HOST": str(nil), "DB_PASSWORD": {Type: "string", Secret: true}, "GREETING": str(nil),
	}, "DB_HOST")
	out, report := migrate(t, "DB_HOST: $var:DB_HOST\nDB_PASSWORD: ${PROD_DB_PASSWORD}\nGREETING: \"a: b\"\n", s, s, nil)
	assert.Contains(t, out, "DB_HOST: $var:DB_HOST")
	assert.Contains(t, out, "DB_PASSWORD: ${PROD_DB_PASSWORD}")
	assert.Contains(t, out, `GREETING: "a: b"`)
	assert.ElementsMatch(t, []string{"DB_HOST", "DB_PASSWORD", "GREETING"}, report.Copied)
	assert.True(t, strings.HasPrefix(out, "# Component: erp/api@2.0.0\n"))
}

// 新版本新增的键：写骨架行（必填无默认写 KEY: ""，其余注释掉跟随默认）。
func TestMigrateNewKeysFromSkeleton(t *testing.T) {
	out, report := migrate(t, "DB_HOST: pg\n",
		schema(map[string]manifest.ConfigProperty{"DB_HOST": str(nil)}),
		schema(map[string]manifest.ConfigProperty{"DB_HOST": str(nil), "TOKEN": str(nil), "LOG_LEVEL": str("info")}, "TOKEN"), nil)
	assert.Contains(t, out, `TOKEN: ""`)
	assert.Contains(t, out, "# LOG_LEVEL: info")
	assert.ElementsMatch(t, []string{"TOKEN", "LOG_LEVEL"}, report.Added)
}

// 旧有新无：不写进新文件，报告里列出来（值留在归档里）。
func TestMigrateDroppedKeysReported(t *testing.T) {
	out, report := migrate(t, "OLD_KEY: x\nDB_HOST: pg\n",
		schema(map[string]manifest.ConfigProperty{"OLD_KEY": str(nil), "DB_HOST": str(nil)}),
		schema(map[string]manifest.ConfigProperty{"DB_HOST": str(nil)}), nil)
	assert.NotContains(t, out, "OLD_KEY")
	assert.Equal(t, []string{"OLD_KEY"}, report.Dropped)
}

// 附录 A4：使用者没写的键（骨架里是注释）跟随新默认值——不会有假冲突。
func TestMigrateUnwrittenKeyFollowsNewDefault(t *testing.T) {
	out, report := migrate(t, "# LOG_LEVEL: info  # string (default)\n",
		schema(map[string]manifest.ConfigProperty{"LOG_LEVEL": str("info")}),
		schema(map[string]manifest.ConfigProperty{"LOG_LEVEL": str("warn")}), nil)
	assert.Contains(t, out, "# LOG_LEVEL: warn")
	assert.Empty(t, report.Conflicts)
}

// 提案 §12.3 第一行：值等于旧默认值——能确认使用者没改过它，跟随新默认值。
func TestMigrateUserValueEqualsOldDefaultFollowsNew(t *testing.T) {
	out, report := migrate(t, "LOG_LEVEL: info\n",
		schema(map[string]manifest.ConfigProperty{"LOG_LEVEL": str("info")}),
		schema(map[string]manifest.ConfigProperty{"LOG_LEVEL": str("warn")}), nil)
	assert.Contains(t, out, "# LOG_LEVEL: warn")
	assert.Equal(t, []string{"LOG_LEVEL"}, report.Followed)
	assert.Empty(t, report.Conflicts)
}

// 提案 §12.3：使用者改过、开发者也改了默认值——写重复键（注释说明），大声失败。
func TestMigrateConflictDuplicate(t *testing.T) {
	out, report := migrate(t, "LOG_LEVEL: debug\n",
		schema(map[string]manifest.ConfigProperty{"LOG_LEVEL": str("info")}),
		schema(map[string]manifest.ConfigProperty{"LOG_LEVEL": str("warn")}), nil)
	require.Len(t, report.Conflicts, 1)
	assert.Equal(t, configdir.Conflict{Key: "LOG_LEVEL", UserYAML: "debug", OldDefault: "info", NewDefault: "warn"}, report.Conflicts[0])
	assert.Equal(t, 2, strings.Count(out, "LOG_LEVEL: "))

	// 迁移出来的文件被装载时就是一处冲突：up 会拒绝启动，直到使用者删掉一行
	_, err := configdir.ParseComponentFile([]byte(out), "config/erp-api.yaml")
	var conflict *configdir.ConflictError
	require.ErrorAs(t, err, &conflict)
}

func TestMigrateConflictChooseMineAndNew(t *testing.T) {
	oldS := schema(map[string]manifest.ConfigProperty{"LOG_LEVEL": str("info")})
	newS := schema(map[string]manifest.ConfigProperty{"LOG_LEVEL": str("warn")})
	out, report := migrate(t, "LOG_LEVEL: debug\n", oldS, newS, func(configdir.Conflict) configdir.Choice { return configdir.ChooseMine })
	assert.Contains(t, out, "LOG_LEVEL: debug")
	assert.Equal(t, 1, strings.Count(out, "LOG_LEVEL"))
	assert.Equal(t, configdir.ChooseMine, report.Resolved["LOG_LEVEL"])

	out, _ = migrate(t, "LOG_LEVEL: debug\n", oldS, newS, func(configdir.Conflict) configdir.Choice { return configdir.ChooseNew })
	assert.Contains(t, out, "# LOG_LEVEL: warn")
	assert.NotContains(t, out, "debug")
}

// 旧 configSchema 不知道（归档恢复、缓存里没有旧版本）：文件里的键都算使用者写的，照抄，不判冲突。
func TestMigrateUnknownOldSchemaTreatsKeysAsWritten(t *testing.T) {
	out, report := migrate(t, "LOG_LEVEL: debug\n", nil,
		schema(map[string]manifest.ConfigProperty{"LOG_LEVEL": str("warn")}), nil)
	assert.Contains(t, out, "LOG_LEVEL: debug")
	assert.Empty(t, report.Conflicts)
}

// 旧文件里还有没解决的冲突：先解决它，不在冲突上再叠一层冲突。
func TestMigrateRefusesUnresolvedConflictMarkers(t *testing.T) {
	old := configdir.ConflictBlock("LOG_LEVEL", "debug", "1.0.0", "warn", "0.9.0")
	_, _, err := configdir.Migrate(configdir.MigrateInput{ID: "erp/api", FromVersion: "1.0.0", ToVersion: "2.0.0",
		Old: []byte(old), NewSchema: schema(map[string]manifest.ConfigProperty{"LOG_LEVEL": str("warn")}),
		Choose: func(configdir.Conflict) configdir.Choice { return configdir.ChooseDuplicate }})
	var conflict *configdir.ConflictError
	require.ErrorAs(t, err, &conflict)
}

// 多行的值（块标量）整段抄过去。
func TestMigrateCopiesBlockScalars(t *testing.T) {
	s := schema(map[string]manifest.ConfigProperty{"CERT": str(nil), "NEXT": str(nil)})
	out, _ := migrate(t, "CERT: |\n  line one\n  line two\nNEXT: x\n", s, s, nil)
	assert.Contains(t, out, "CERT: |\n  line one\n  line two\n")
	parsed, err := configdir.ParseComponentFile([]byte(out), "x.yaml")
	require.NoError(t, err)
	v, _ := parsed.Lookup("CERT")
	assert.Equal(t, "line one\nline two\n", v.Text)
}

// 列表 / 映射类型的键冲突：两行重复键都写成单行（JSON 流式写法），文件照样是合法 YAML、
// 照样被认成一处冲突——而不是一份解析不了的文件。
func TestMigrateConflictOnListAndMapStaysValidYAML(t *testing.T) {
	oldS := schema(map[string]manifest.ConfigProperty{
		"HOSTS": {Type: "array", Default: []any{"a"}}, "M": {Type: "object", Default: map[string]any{"x": 1}}})
	newS := schema(map[string]manifest.ConfigProperty{
		"HOSTS": {Type: "array", Default: []any{"c"}}, "M": {Type: "object", Default: map[string]any{"y": 2}}})
	out, report := migrate(t, "HOSTS:\n  - b\nM:\n  z: 3\n", oldS, newS, nil)
	require.Len(t, report.Conflicts, 2)
	_, err := configdir.ParseComponentFile([]byte(out), "config/erp-api.yaml")
	var conflict *configdir.ConflictError
	require.ErrorAs(t, err, &conflict, "解析出来是冲突，不是语法错误：\n%s", out)
}

// 旧版本必填、没默认值、使用者还没填的占位 P: ""——不是使用者写的值：新版本给了默认值时跟随它，不算冲突。
func TestMigrateUnfilledRequiredPlaceholderIsNotWritten(t *testing.T) {
	out, report := migrate(t, "P: \"\"\n",
		schema(map[string]manifest.ConfigProperty{"P": str(nil)}, "P"),
		schema(map[string]manifest.ConfigProperty{"P": str("ready")}), nil)
	assert.Empty(t, report.Conflicts)
	assert.Contains(t, out, "# P: ready")
}

// 不知道旧 schema 时，没写的键不能都报成"新版本新增"。
func TestMigrateUnknownOldSchemaReportsNoAdded(t *testing.T) {
	_, report := migrate(t, "A: x\n", nil, schema(map[string]manifest.ConfigProperty{"A": str(nil), "B": str(nil)}), nil)
	assert.Empty(t, report.Added)
}

// 保留式块标量（|+、>+）末尾的空行是值的一部分：迁移后解析出来的值必须一字不差，
// 否则报告里"原样保留"就是假话。
func TestMigrateKeepsTrailingBlankLinesOfKeepBlocks(t *testing.T) {
	s := schema(map[string]manifest.ConfigProperty{"GREETING": str(nil), "DB_HOST": str(nil)})
	old := "GREETING: |+\n  你好\n  欢迎\n\n\n# 下一项\nDB_HOST: pg\n"
	out, report := migrate(t, old, s, s, nil)
	assert.ElementsMatch(t, []string{"GREETING", "DB_HOST"}, report.Copied)

	var before, after map[string]any
	require.NoError(t, yaml.Unmarshal([]byte(old), &before))
	require.NoError(t, yaml.Unmarshal([]byte(out), &after))
	assert.Equal(t, "你好\n欢迎\n\n\n", after["GREETING"])
	assert.Equal(t, before, after)
}

// 使用者写在键上方的注释跟着这个键走；写在文件开头的注释放回新文件头之后。
// 骨架自己生成的注释（文件头、分节标题）不会因此多出一份。
func TestMigrateCarriesUserComments(t *testing.T) {
	oldS := schema(map[string]manifest.ConfigProperty{"GREETING": str("Hello"), "DB_HOST": str(nil)})
	newS := schema(map[string]manifest.ConfigProperty{"GREETING": str("Hello"), "DB_HOST": str(nil), "TOKEN": str(nil)})
	old := string(configdir.Skeleton("erp/api", "1.0.0", oldS, nil))
	old = "# 这份配置由支付组维护\n" + old
	old = strings.Replace(old, "# DB_HOST:", "# 生产库，DBA 说不要改\nDB_HOST: pg.internal  #", 1)

	out, _ := migrate(t, old, oldS, newS, nil)
	assert.Contains(t, out, "# 这份配置由支付组维护\n")
	assert.Contains(t, out, "# 生产库，DBA 说不要改\nDB_HOST: pg.internal")
	assert.True(t, strings.HasPrefix(out, "# Component: erp/api@2.0.0\n"), "生成的文件头仍在最前面")
	assert.Equal(t, 1, strings.Count(out, "# Component: "), out)
	fresh := string(configdir.Skeleton("erp/api", "2.0.0", newS, nil))
	for _, line := range strings.Split(fresh, "\n") {
		if strings.HasPrefix(line, "# ===") {
			assert.Equal(t, 1, strings.Count(out, line), "分节标题只出现一次：%s\n%s", line, out)
		}
	}
}

// 骨架里注释掉的键行（# KEY: …）不是使用者的注释：它们挨着使用者取消注释的那个键，
// 当成注释带过去，新文件里同一行就出现两次。
func TestMigrateDoesNotCarryCommentedSkeletonLines(t *testing.T) {
	s := schema(map[string]manifest.ConfigProperty{"A_KEY": str("a"), "B_KEY": str("b"), "C_KEY": str("c")})
	old := string(configdir.Skeleton("erp/api", "1.0.0", s, nil))
	old = strings.Replace(old, "# B_KEY: b", "B_KEY: mine  #", 1)
	out, _ := migrate(t, old, s, s, nil)
	assert.Equal(t, 1, strings.Count(out, "# A_KEY: "), out)
	assert.Contains(t, out, "B_KEY: mine")
}
