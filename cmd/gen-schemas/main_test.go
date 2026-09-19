package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/brickkit/brickkit/internal/schemagen"
)

// 工具写出来的字节必须与 schemagen.Files() 给的一模一样（防漂移测试比的就是这份），
// 目录不存在时要自己建，重跑不改任何字节。
func TestRunWritesEveryFileAndIsIdempotent(t *testing.T) {
	want, err := schemagen.Files()
	require.NoError(t, err)

	dir := filepath.Join(t.TempDir(), "nested", "schemas")
	for round := 1; round <= 2; round++ {
		require.NoError(t, run(dir), "第 %d 次", round)

		entries, err := os.ReadDir(dir)
		require.NoError(t, err)
		assert.Len(t, entries, len(want), "只写 Files() 给出的那几份")
		for name, content := range want {
			got, err := os.ReadFile(filepath.Join(dir, name))
			require.NoError(t, err)
			assert.Equal(t, string(content), string(got), "%s（第 %d 次）", name, round)
		}
	}
}

func TestRunReportsAnUnwritableTarget(t *testing.T) {
	blocker := filepath.Join(t.TempDir(), "file")
	require.NoError(t, os.WriteFile(blocker, []byte("x"), 0o644))

	// 目标目录的父级是一个普通文件：建不出目录，要报错而不是悄悄什么都没写
	assert.Error(t, run(filepath.Join(blocker, "schemas")))
}
