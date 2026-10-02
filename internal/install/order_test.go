package install_test

import (
	"sort"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/brickkit/brickkit/internal/install"
	"github.com/brickkit/brickkit/internal/yamlfile"
)

func ordered(keys ...yamlfile.EntryKey) []yamlfile.EntryKey {
	out := append([]yamlfile.EntryKey{}, keys...)
	sort.SliceStable(out, func(i, j int) bool { return install.EntryBefore(out[i], out[j]) })
	return out
}

// brickkit.yaml：按 ID；同一个 ID 默认版本（没有 requiredBy）在前，兼容版本按版本号从低到高
// ——10.0.0 排在 9.0.0 后面，不是按字符串比。
func TestEntryBeforeDeclLines(t *testing.T) {
	assert.Equal(t, []yamlfile.EntryKey{
		{ID: "erp/api", Version: "9.5.0"},
		{ID: "erp/api", Version: "9.0.0", RequiredBy: true},
		{ID: "erp/api", Version: "10.0.0", RequiredBy: true},
		{ID: "erp/api-gateway", Version: "1.0.0"},
		{ID: "mdm/customer", Version: "1.0.0"},
	}, ordered(
		yamlfile.EntryKey{ID: "mdm/customer", Version: "1.0.0"},
		yamlfile.EntryKey{ID: "erp/api", Version: "10.0.0", RequiredBy: true},
		yamlfile.EntryKey{ID: "erp/api-gateway", Version: "1.0.0"},
		yamlfile.EntryKey{ID: "erp/api", Version: "9.0.0", RequiredBy: true},
		yamlfile.EntryKey{ID: "erp/api", Version: "9.5.0"},
	))
}

// 部署文件：裸 ID 的条目（默认版本）在前，id@version 紧跟其后；"@" 不参与 ID 的比较，
// erp/api@1.0.0 不会被 erp/api-gateway 隔开。
func TestEntryBeforeDeployEntries(t *testing.T) {
	assert.Equal(t, []yamlfile.EntryKey{
		{ID: "erp/api"},
		{ID: "erp/api@1.0.0"},
		{ID: "erp/api@1.10.0"},
		{ID: "erp/api-gateway"},
		{ID: "mdm/customer@2.0.0"},
	}, ordered(
		yamlfile.EntryKey{ID: "mdm/customer@2.0.0"},
		yamlfile.EntryKey{ID: "erp/api-gateway"},
		yamlfile.EntryKey{ID: "erp/api@1.10.0"},
		yamlfile.EntryKey{ID: "erp/api@1.0.0"},
		yamlfile.EntryKey{ID: "erp/api"},
	))
}
