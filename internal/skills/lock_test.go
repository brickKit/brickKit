package skills

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadLockMissingFileIsEmptyNotError(t *testing.T) {
	// 没有 lock 是常态（老项目、刚 clone），不是错误。
	l, err := LoadLock(filepath.Join(t.TempDir(), "skills.lock"))
	require.NoError(t, err)
	assert.Empty(t, l.Entries)
}

func TestLockRoundTrip(t *testing.T) {
	p := filepath.Join(t.TempDir(), "skills.lock")
	l := &Lock{}
	l.Set(LockEntry{Path: "AGENTS.md", Version: "0.1.0", Sum: Sum([]byte("a"))})
	require.NoError(t, l.Save(p))

	got, err := LoadLock(p)
	require.NoError(t, err)
	e, ok := got.Get("AGENTS.md")
	require.True(t, ok)
	assert.Equal(t, "0.1.0", e.Version)
	assert.Equal(t, Sum([]byte("a")), e.Sum)
}

func TestLockSetReplacesInsteadOfAppending(t *testing.T) {
	l := &Lock{}
	l.Set(LockEntry{Path: "AGENTS.md", Version: "0.1.0", Sum: "sha256:aa"})
	l.Set(LockEntry{Path: "AGENTS.md", Version: "0.2.0", Sum: "sha256:bb"})
	require.Len(t, l.Entries, 1, "同一路径不能留两条记录")
	e, _ := l.Get("AGENTS.md")
	assert.Equal(t, "0.2.0", e.Version)
}

// lock 是要提交进 Git 的：顺序不稳定会让每次 update 都产生假 diff。
func TestLockSaveIsSortedAndStable(t *testing.T) {
	p := filepath.Join(t.TempDir(), "skills.lock")
	l := &Lock{}
	l.Set(LockEntry{Path: "z.md", Version: "1", Sum: "sha256:z"})
	l.Set(LockEntry{Path: "a.md", Version: "1", Sum: "sha256:a"})
	require.NoError(t, l.Save(p))
	first, err := os.ReadFile(p)
	require.NoError(t, err)

	assert.Less(t, strings.Index(string(first), "a.md"),
		strings.Index(string(first), "z.md"), "条目必须按路径排序")

	// 反序再存一次，字节应当完全一致。
	l2 := &Lock{}
	l2.Set(LockEntry{Path: "a.md", Version: "1", Sum: "sha256:a"})
	l2.Set(LockEntry{Path: "z.md", Version: "1", Sum: "sha256:z"})
	p2 := filepath.Join(t.TempDir(), "skills.lock")
	require.NoError(t, l2.Save(p2))
	second, err := os.ReadFile(p2)
	require.NoError(t, err)
	assert.Equal(t, string(first), string(second))
}

// lock 所在目录不存在时要自己建出来（init 之外的场景可能还没有 .brickkit/）。
func TestLockSaveCreatesParentDir(t *testing.T) {
	p := filepath.Join(t.TempDir(), ".brickkit", "skills.lock")
	l := &Lock{}
	l.Set(LockEntry{Path: "AGENTS.md", Version: "1", Sum: "sha256:a"})
	require.NoError(t, l.Save(p))
	_, err := os.Stat(p)
	assert.NoError(t, err)
}

func TestLoadLockCorruptFileReportsClearly(t *testing.T) {
	p := filepath.Join(t.TempDir(), "skills.lock")
	require.NoError(t, os.WriteFile(p, []byte("{ 这不是 json"), 0o644))
	_, err := LoadLock(p)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "skills.lock")
}
