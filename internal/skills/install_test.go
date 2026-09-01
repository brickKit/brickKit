package skills

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newInstaller(t *testing.T) Installer {
	t.Helper()
	root := t.TempDir()
	return Installer{
		Root:     root,
		LockPath: filepath.Join(root, ".brickkit", "skills.lock"),
		Version:  "0.1.0",
	}
}

func mustStatus(t *testing.T, in Installer) []FileStatus {
	t.Helper()
	list, err := in.Status()
	require.NoError(t, err)
	require.NotEmpty(t, list)
	return list
}

func stateOf(t *testing.T, in Installer, target string) FileStatus {
	t.Helper()
	for _, s := range mustStatus(t, in) {
		if s.Target == target {
			return s
		}
	}
	t.Fatalf("状态列表里没有 %s", target)
	return FileStatus{}
}

func assetNamed(t *testing.T, target string) Asset {
	t.Helper()
	for _, a := range Assets() {
		if a.Target == target {
			return a
		}
	}
	t.Fatalf("资产清单里没有 %s", target)
	return Asset{}
}

func TestFreshProjectIsAllMissingThenWritten(t *testing.T) {
	in := newInstaller(t)
	for _, s := range mustStatus(t, in) {
		assert.Equal(t, StateMissing, s.State, s.Target)
	}

	res, err := in.Apply()
	require.NoError(t, err)
	assert.Len(t, res.Written, len(Assets()))
	assert.Empty(t, res.Skipped)

	for _, a := range Assets() {
		_, err := os.Stat(filepath.Join(in.Root, a.Target))
		assert.NoError(t, err, "没写出来：%s", a.Target)
	}
}

func TestApplyIsIdempotent(t *testing.T) {
	in := newInstaller(t)
	_, err := in.Apply()
	require.NoError(t, err)

	res, err := in.Apply()
	require.NoError(t, err)
	assert.Empty(t, res.Written, "第二次不该有任何写入")
	assert.Empty(t, res.Skipped)
	for _, s := range mustStatus(t, in) {
		assert.Equal(t, StateCurrent, s.State, s.Target)
	}
}

// 用户手改过的文件绝不覆盖——这是整个设计里最要紧的一条。
func TestModifiedFileIsNeverOverwritten(t *testing.T) {
	in := newInstaller(t)
	_, err := in.Apply()
	require.NoError(t, err)

	p := filepath.Join(in.Root, "AGENTS.md")
	mine := []byte("这是我自己写的，别动\n")
	require.NoError(t, os.WriteFile(p, mine, filePerm))

	assert.Equal(t, StateModified, stateOf(t, in, "AGENTS.md").State)

	res, err := in.Apply()
	require.NoError(t, err)
	assert.NotContains(t, res.Written, "AGENTS.md")

	after, err := os.ReadFile(p)
	require.NoError(t, err)
	assert.Equal(t, string(mine), string(after), "手改的内容被覆盖了")
}

// lock 里没有记录的既有文件也不碰：可能是用户自己写的同名文件。
func TestUntrackedFileIsNeverOverwritten(t *testing.T) {
	in := newInstaller(t)
	p := filepath.Join(in.Root, "AGENTS.md")
	mine := []byte("# 我自己写的导读\n")
	require.NoError(t, os.WriteFile(p, mine, filePerm))

	assert.Equal(t, StateUntracked, stateOf(t, in, "AGENTS.md").State)

	res, err := in.Apply()
	require.NoError(t, err)
	assert.NotContains(t, res.Written, "AGENTS.md")

	after, err := os.ReadFile(p)
	require.NoError(t, err)
	assert.Equal(t, string(mine), string(after))
}

// lock 记的指纹与磁盘一致、但与当前资产不一致 → 待更新，可以覆盖。
func TestOutdatedFileIsUpdated(t *testing.T) {
	in := newInstaller(t)
	p := filepath.Join(in.Root, "AGENTS.md")
	old := []byte("旧版本的导读\n")
	require.NoError(t, os.WriteFile(p, old, filePerm))

	l := &Lock{}
	l.Set(LockEntry{Path: "AGENTS.md", Version: "0.0.1", Sum: Sum(old)})
	require.NoError(t, l.Save(in.LockPath))

	st := stateOf(t, in, "AGENTS.md")
	assert.Equal(t, StateOutdated, st.State)
	assert.Equal(t, "0.0.1", st.FromVersion, "要能说出是从哪个版本升上来的")

	res, err := in.Apply()
	require.NoError(t, err)
	assert.Contains(t, res.Written, "AGENTS.md")

	after, err := os.ReadFile(p)
	require.NoError(t, err)
	assert.NotEqual(t, string(old), string(after))
}

// lock 丢了之后：没动过的文件判「最新」并补登记（内容本就逐字节相同，
// 补登记不覆盖任何东西，却把升级通道修回来了）；改过的判「未托管」，一个字不动。
//
// 反过来做——一律判「未托管」——的后果是：用户误删一次 lock，这个项目的技能
// 就永远升不上去，哪怕他一个字都没改过。那是坑，不是保守。
func TestLostLockRecoversUnmodifiedAndSparesTheRest(t *testing.T) {
	in := newInstaller(t)
	_, err := in.Apply()
	require.NoError(t, err)

	mine := []byte("我改过这一份\n")
	require.NoError(t, os.WriteFile(filepath.Join(in.Root, "AGENTS.md"), mine, filePerm))
	require.NoError(t, os.Remove(in.LockPath))

	for _, s := range mustStatus(t, in) {
		if s.Target == "AGENTS.md" {
			assert.Equal(t, StateUntracked, s.State, "改过的：未托管")
			continue
		}
		assert.Equal(t, StateCurrent, s.State, "没动过的：最新")
	}

	res, err := in.Apply()
	require.NoError(t, err)
	assert.Empty(t, res.Written, "没有任何文件需要重写")
	require.Len(t, res.Skipped, 1)
	assert.Equal(t, "AGENTS.md", res.Skipped[0].Target)

	after, err := os.ReadFile(filepath.Join(in.Root, "AGENTS.md"))
	require.NoError(t, err)
	assert.Equal(t, string(mine), string(after), "改过的那份被覆盖了")

	// lock 已经重建，且不含那份未托管的。
	l, err := LoadLock(in.LockPath)
	require.NoError(t, err)
	assert.NotEmpty(t, l.Entries)
	_, ok := l.Get("AGENTS.md")
	assert.False(t, ok, "未托管的不该被登记进 lock")
}

// 内容恰好与资产一致、但 lock 里没记录 → 判「最新」并补登记，
// 否则它下个版本还是「未托管」，永远升不上去。
func TestCurrentWithoutLockEntryGetsRecorded(t *testing.T) {
	in := newInstaller(t)
	want, err := assetNamed(t, "AGENTS.md").Content()
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(in.Root, "AGENTS.md"), want, filePerm))

	assert.Equal(t, StateCurrent, stateOf(t, in, "AGENTS.md").State)

	_, err = in.Apply()
	require.NoError(t, err)

	l, err := LoadLock(in.LockPath)
	require.NoError(t, err)
	e, ok := l.Get("AGENTS.md")
	require.True(t, ok, "「最新」也要补登记进 lock")
	assert.Equal(t, "0.1.0", e.Version)
}

// 缺失的文件被重新写出来（用户删了某个 skill 目录）。
func TestMissingFileIsRestored(t *testing.T) {
	in := newInstaller(t)
	_, err := in.Apply()
	require.NoError(t, err)

	p := filepath.Join(in.Root, "AGENTS.md")
	require.NoError(t, os.Remove(p))
	assert.Equal(t, StateMissing, stateOf(t, in, "AGENTS.md").State)

	res, err := in.Apply()
	require.NoError(t, err)
	assert.Contains(t, res.Written, "AGENTS.md")
}

// 跳过的文件不能被从 lock 里抹掉：抹了它下次就从「已手改」变成「未托管」，
// 状态信息丢失，而两者对用户的提示是不一样的。
func TestSkippedFileKeepsItsLockEntry(t *testing.T) {
	in := newInstaller(t)
	_, err := in.Apply()
	require.NoError(t, err)

	require.NoError(t, os.WriteFile(filepath.Join(in.Root, "AGENTS.md"),
		[]byte("改过了\n"), filePerm))
	_, err = in.Apply()
	require.NoError(t, err)

	l, err := LoadLock(in.LockPath)
	require.NoError(t, err)
	_, ok := l.Get("AGENTS.md")
	assert.True(t, ok, "跳过的文件仍应留在 lock 里")
	assert.Equal(t, StateModified, stateOf(t, in, "AGENTS.md").State)
}

// lock 坏了要响亮报错，不能当成「没有 lock」继续往下走：
// 那会把一个可修的问题变成「所有文件突然都判未托管」的怪现象。
func TestCorruptLockFailsLoudlyOnBothPaths(t *testing.T) {
	in := newInstaller(t)
	require.NoError(t, os.MkdirAll(filepath.Dir(in.LockPath), dirPerm))
	require.NoError(t, os.WriteFile(in.LockPath, []byte("{ 坏了"), filePerm))

	_, err := in.Status()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "skills.lock")

	_, err = in.Apply()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "skills.lock")
}

// 落点被一个目录占了（读它会得到 EISDIR，不是 NotExist）。
// 这时必须报错，绝不能误判成「缺失」然后去写——那会失败得更难懂。
func TestTargetOccupiedByDirectoryIsAnError(t *testing.T) {
	in := newInstaller(t)
	require.NoError(t, os.Mkdir(filepath.Join(in.Root, "AGENTS.md"), dirPerm))

	_, err := in.Status()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "AGENTS.md")
}

// 要建的目录被一个普通文件占了，MkdirAll 会失败。
func TestWriteFailsWhenParentPathIsAFile(t *testing.T) {
	in := newInstaller(t)
	require.NoError(t, os.WriteFile(filepath.Join(in.Root, ".claude"),
		[]byte("我是个文件，不是目录\n"), filePerm))

	_, err := in.Apply()
	require.Error(t, err)
	assert.Contains(t, err.Error(), ".claude")
}

// lock 写不出去时要报错，而不是假装装好了——
// 下次运行会因为没有记录而把一切判成「未托管」，用户完全看不懂。
func TestApplyFailsWhenLockCannotBeSaved(t *testing.T) {
	in := newInstaller(t)
	// 用一个普通文件占住 .brickkit/，让 lock 的父目录建不出来。
	require.NoError(t, os.WriteFile(filepath.Join(in.Root, ".brickkit"),
		[]byte("占位\n"), filePerm))

	_, err := in.Apply()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "skills.lock")
}

// 目录建得出来但文件写不进去（只读目录）——这是真实场景：
// 项目目录被设成只读，或者跑在权限受限的 CI 里。
func TestWriteFailsOnReadOnlyDirectory(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root 无视文件权限，这个用例在 root 下无意义")
	}
	in := newInstaller(t)
	if err := os.Chmod(in.Root, 0o555); err != nil {
		t.Skipf("改不了目录权限：%v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(in.Root, dirPerm) })

	_, err := in.Apply()
	require.Error(t, err)
}
