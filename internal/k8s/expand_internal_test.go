package k8s

// ${VAR} 展开的代码层测试：这段逻辑决定了密码到底是真值还是一串字面量，
// 边界情况必须逐个钉死。

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/brickkit/brickkit/internal/clierr"
)

func lookupFrom(env map[string]string) func(string) (string, bool) {
	return func(name string) (string, bool) {
		value, ok := env[name]
		return value, ok
	}
}

func TestExpandPlainValue(t *testing.T) {
	e := newExpander(lookupFrom(nil))

	assert.Equal(t, "postgres.infra.svc", e.value("postgres.infra.svc"))
	assert.NoError(t, e.check())
}

func TestExpandWholeValue(t *testing.T) {
	e := newExpander(lookupFrom(map[string]string{"PG_PASSWORD": "s3cr3t"}))

	assert.Equal(t, "s3cr3t", e.value("${PG_PASSWORD}"))
	assert.NoError(t, e.check())
}

// 值里可以只有一部分是引用：`pg-${ENV}.infra.svc`。
func TestExpandEmbedded(t *testing.T) {
	e := newExpander(lookupFrom(map[string]string{"ENV": "staging"}))

	assert.Equal(t, "pg-staging.infra.svc", e.value("pg-${ENV}.infra.svc"))
}

func TestExpandMultiple(t *testing.T) {
	e := newExpander(lookupFrom(map[string]string{"A": "1", "B": "2"}))

	assert.Equal(t, "1-2", e.value("${A}-${B}"))
}

// ${VAR:-默认值}：与 shell / docker compose 的写法一致。
func TestExpandDefaultValue(t *testing.T) {
	e := newExpander(lookupFrom(nil))

	assert.Equal(t, "dev", e.value("${PG_PASSWORD:-dev}"))
	assert.NoError(t, e.check(), "有默认值就不算缺失")
}

func TestExpandDefaultValueNotUsedWhenSet(t *testing.T) {
	e := newExpander(lookupFrom(map[string]string{"PG_PASSWORD": "real"}))

	assert.Equal(t, "real", e.value("${PG_PASSWORD:-dev}"))
}

// 定义成空串算"定义过"：使用者可能就是想要空密码。
func TestExpandEmptyValueCounts(t *testing.T) {
	e := newExpander(lookupFrom(map[string]string{"PG_PASSWORD": ""}))

	assert.Equal(t, "", e.value("${PG_PASSWORD}"))
	assert.NoError(t, e.check())
}

// 没有右括号的不是引用，原样保留（比如某些密码里真的带 `${`）。
func TestExpandUnclosedBraceIsLiteral(t *testing.T) {
	e := newExpander(lookupFrom(nil))

	assert.Equal(t, "abc${def", e.value("abc${def"))
	assert.NoError(t, e.check())
}

// 缺失的变量一次全报，不是撞到一个就停。
func TestExpandMissingVarsReportedTogether(t *testing.T) {
	e := newExpander(lookupFrom(nil))

	e.value("${B_PASSWORD}")
	e.value("${A_PASSWORD}")
	err := e.check()

	require.Error(t, err)
	assert.Equal(t, clierr.CodeConfigInvalid, clierr.As(err).Code)
	assert.Contains(t, err.Error(), "A_PASSWORD")
	assert.Contains(t, err.Error(), "B_PASSWORD")
	assert.Less(t, indexOf(err.Error(), "A_PASSWORD"), indexOf(err.Error(), "B_PASSWORD"),
		"按变量名排序，输出才稳定")
}

// 同一个变量缺失多次只报一遍。
func TestExpandMissingVarReportedOnce(t *testing.T) {
	e := newExpander(lookupFrom(nil))

	e.value("${PG_PASSWORD}")
	e.value("${PG_PASSWORD}")

	assert.Equal(t, 1, len(e.missing))
}

func TestExpandWithoutLookup(t *testing.T) {
	e := newExpander(nil)

	assert.Equal(t, "${PG_PASSWORD}", e.value("${PG_PASSWORD}"), "展开不了时原样保留")
	assert.Error(t, e.check(), "但这次生成必须被阻断")
}

func indexOf(text, sub string) int {
	for i := 0; i+len(sub) <= len(text); i++ {
		if text[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

// ============================================================
// K8s 资源名
// ============================================================

func TestSanitizeName(t *testing.T) {
	assert.Equal(t, "people-db", sanitizeName("people-db"))
	assert.Equal(t, "people-db", sanitizeName("People_DB"))
	assert.Equal(t, "postgres-main", sanitizeName("postgres.main"))
	assert.Equal(t, "a-b", sanitizeName("-a/b-"), "首尾的中划线不合法，要去掉")
}

func TestSecretName(t *testing.T) {
	assert.Equal(t, "people-db-secret", secretName("people-db"))
	assert.Equal(t, "postgres-main-secret", secretName("postgres_main"))
}

func TestContainerName(t *testing.T) {
	assert.Equal(t, "people-basic", containerName("people/basic"))
	assert.Equal(t, "infra-api-docs", containerName("infra/api-docs"))
}

// 命名空间与引擎侧的 compose 项目名必须同名：
// 同一个项目换个部署目标不该换个名字。
func TestNamespaceMatchesProjectName(t *testing.T) {
	assert.Equal(t, "brickkit-my-erp", Namespace("my-erp"))
	assert.Equal(t, "brickkit", Namespace(""))
}
