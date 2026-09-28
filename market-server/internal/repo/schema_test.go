package repo

import (
	"regexp"
	"testing"

	"github.com/stretchr/testify/assert"
)

// schema.sql 每次启动都整份执行。里面一旦有删表、删列，升级之后就回不去了：
// 旧镜像（回滚、或滚动升级时还没换下的副本）照旧读那一列，查询直接失败。
// 不再用的列留着（可空），结构只加不减。
func TestSchemaHasNoDestructiveStatements(t *testing.T) {
	destructive := regexp.MustCompile(`(?i)\b(DROP|TRUNCATE|DELETE\s+FROM|RENAME)\b`)
	assert.Empty(t, destructive.FindAllString(schemaSQL, -1))
}
