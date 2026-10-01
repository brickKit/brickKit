package skills

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/brickkit/brickkit/internal/i18n"
)

const assemble = ".claude/skills/brickkit-assemble/SKILL.md"

func newInstaller(t *testing.T) Installer {
	t.Helper()
	return Installer{Root: t.TempDir(), Version: "0.1.0"}
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
	for _, a := range Assets(i18n.EN) {
		if a.Target == target {
			return a
		}
	}
	t.Fatalf("资产清单里没有 %s", target)
	return Asset{}
}

func writeAt(t *testing.T, in Installer, target string, content []byte) string {
	t.Helper()
	p := filepath.Join(in.Root, filepath.FromSlash(target))
	require.NoError(t, os.MkdirAll(filepath.Dir(p), dirPerm))
	require.NoError(t, os.WriteFile(p, content, filePerm))
	return p
}

func TestFreshProjectIsAllMissingThenWritten(t *testing.T) {
	in := newInstaller(t)
	for _, s := range mustStatus(t, in) {
		assert.Equal(t, StateMissing, s.State, s.Target)
	}

	res, err := in.Apply()
	require.NoError(t, err)
	assert.Len(t, res.Written, len(Assets(i18n.EN)))
	assert.Empty(t, res.Skipped)

	for _, a := range Assets(i18n.EN) {
		data, err := os.ReadFile(filepath.Join(in.Root, a.Target))
		require.NoError(t, err, "没写出来：%s", a.Target)
		_, v, _, ok := ReadMarker(data)
		assert.True(t, ok, "%s 没带记录", a.Target)
		assert.Equal(t, "0.1.0", v)
	}
	assert.NoFileExists(t, filepath.Join(in.Root, ".brickkit", "skills.lock"), "不再写 lock")
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

// 用户手改过的文件绝不覆盖——这是整个设计里最要紧的一条。改了正文、留着记录行：已手改。
func TestModifiedFileIsNeverOverwritten(t *testing.T) {
	in := newInstaller(t)
	_, err := in.Apply()
	require.NoError(t, err)

	p := filepath.Join(in.Root, filepath.FromSlash(assemble))
	data, err := os.ReadFile(p)
	require.NoError(t, err)
	mine := append([]byte("我加了一行\n"), data...)
	require.NoError(t, os.WriteFile(p, mine, filePerm))

	assert.Equal(t, StateModified, stateOf(t, in, assemble).State)
	res, err := in.Apply()
	require.NoError(t, err)
	assert.NotContains(t, res.Written, assemble)
	after, err := os.ReadFile(p)
	require.NoError(t, err)
	assert.Equal(t, string(mine), string(after), "手改的内容被覆盖了")
	assert.Equal(t, StateModified, stateOf(t, in, assemble).State, "跳过之后它还是「已手改」，不是「未托管」")
}

// 不带记录的既有文件也不碰：可能是用户自己写的同名文件。
func TestUntrackedFileIsNeverOverwritten(t *testing.T) {
	in := newInstaller(t)
	mine := []byte("# 我自己写的导读\n")
	p := writeAt(t, in, assemble, mine)

	assert.Equal(t, StateUntracked, stateOf(t, in, assemble).State)
	res, err := in.Apply()
	require.NoError(t, err)
	assert.NotContains(t, res.Written, assemble)
	after, err := os.ReadFile(p)
	require.NoError(t, err)
	assert.Equal(t, string(mine), string(after))
}

// 旧版本写的、没改过 → 待更新，说得出是从哪个版本升上来的。
func TestOutdatedFileIsUpdated(t *testing.T) {
	in := newInstaller(t)
	p := writeAt(t, in, assemble, Mark([]byte("旧版本的技能\n"), "0.0.1"))

	st := stateOf(t, in, assemble)
	assert.Equal(t, StateOutdated, st.State)
	assert.Equal(t, "0.0.1", st.FromVersion, "要能说出是从哪个版本升上来的")

	res, err := in.Apply()
	require.NoError(t, err)
	assert.Contains(t, res.Written, assemble)
	after, err := os.ReadFile(p)
	require.NoError(t, err)
	body, v, _, ok := ReadMarker(after)
	require.True(t, ok)
	assert.Equal(t, "0.1.0", v)
	want, _ := assetNamed(t, assemble).Content()
	assert.Equal(t, normalize(want), body)
}

// 内容恰好就是当前资产、只是没带记录（手工拷来的）→ 补上记录，否则它永远升不上去。
func TestCurrentContentWithoutMarkerGetsMarked(t *testing.T) {
	in := newInstaller(t)
	want, err := assetNamed(t, assemble).Content()
	require.NoError(t, err)
	p := writeAt(t, in, assemble, want)

	assert.Equal(t, StateOutdated, stateOf(t, in, assemble).State)
	_, err = in.Apply()
	require.NoError(t, err)
	after, _ := os.ReadFile(p)
	_, _, _, ok := ReadMarker(after)
	assert.True(t, ok)
	assert.Equal(t, StateCurrent, stateOf(t, in, assemble).State)
}

// 缺失的文件被重新写出来（用户删了某个 skill 目录）。
func TestMissingFileIsRestored(t *testing.T) {
	in := newInstaller(t)
	_, err := in.Apply()
	require.NoError(t, err)
	require.NoError(t, os.Remove(filepath.Join(in.Root, filepath.FromSlash(assemble))))
	assert.Equal(t, StateMissing, stateOf(t, in, assemble).State)

	res, err := in.Apply()
	require.NoError(t, err)
	assert.Contains(t, res.Written, assemble)
}

// 旧版 lock 坏了要响亮报错，不能当成"没有 lock"继续：那会把旧版 CLI 写的文件全判成「未托管」。
func TestCorruptLegacyLockFailsLoudlyOnBothPaths(t *testing.T) {
	in := newInstaller(t)
	in.LegacyLockPath = filepath.Join(in.Root, ".brickkit", "skills.lock")
	require.NoError(t, os.MkdirAll(filepath.Dir(in.LegacyLockPath), dirPerm))
	require.NoError(t, os.WriteFile(in.LegacyLockPath, []byte("{ 坏了"), filePerm))

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
	require.NoError(t, os.MkdirAll(filepath.Join(in.Root, filepath.FromSlash(assemble)), dirPerm))

	_, err := in.Status()
	require.Error(t, err)
	assert.Contains(t, err.Error(), assemble)
}

// 要建的目录被一个普通文件占了，MkdirAll 会失败。
func TestWriteFailsWhenParentPathIsAFile(t *testing.T) {
	in := newInstaller(t)
	require.NoError(t, os.WriteFile(filepath.Join(in.Root, ".claude"), []byte("我是个文件，不是目录\n"), filePerm))

	_, err := in.Apply()
	require.Error(t, err)
	assert.Contains(t, err.Error(), ".claude")
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

// 组件仓库里只装 brickkit-component 这一份：没有 brickkit.yaml 的地方，
// 拼装/部署/排障三个技能都讲不通。
func TestComponentScopeManagesOnlyTheComponentSkill(t *testing.T) {
	in := newInstaller(t)
	in.Scope = ScopeComponent

	list := mustStatus(t, in)
	require.Len(t, list, 1)
	assert.Equal(t, ".claude/skills/brickkit-component/SKILL.md", list[0].Target)
	assert.Equal(t, StateMissing, list[0].State)

	res, err := in.Apply()
	require.NoError(t, err)
	assert.Equal(t, []string{".claude/skills/brickkit-component/SKILL.md"}, res.Written)

	for _, absent := range []string{assemble, ".claude/skills/brickkit-deploy/SKILL.md", ".claude/skills/brickkit-troubleshoot/SKILL.md"} {
		_, err := os.Stat(filepath.Join(in.Root, filepath.FromSlash(absent)))
		assert.True(t, os.IsNotExist(err), "组件仓库里不该出现 %s", absent)
	}
}

// 不指定范围时行为不变：完整的一套（brickkit init 装进项目的那些）。
func TestDefaultScopeStaysTheFullProjectSet(t *testing.T) {
	in := newInstaller(t)
	assert.Len(t, mustStatus(t, in), len(Assets(i18n.EN)))
	assert.Greater(t, len(Assets(i18n.EN)), 1)
}

// 组件范围的清单是一份按落点写死的名单——改名或删掉那份资产时，这里要立刻红，
// 而不是让组件仓库里静默少装一份。
func TestComponentScopeTargetsAllExistAmongAssets(t *testing.T) {
	all := map[string]bool{}
	for _, a := range Assets(i18n.EN) {
		all[a.Target] = true
	}
	require.NotEmpty(t, componentTargets)
	for _, target := range componentTargets {
		assert.True(t, all[target], "组件范围里列了 %s，但内嵌资产里没有它", target)
	}
}

// 组件范围下同样不覆盖手改过的文件（复用同一套状态机）。整份换掉、连记录行一起删了：
// 与使用者自己写的同名文件分不开，判「未托管」，同样不碰。
func TestComponentScopeNeverOverwritesHandEdits(t *testing.T) {
	in := newInstaller(t)
	in.Scope = ScopeComponent
	_, err := in.Apply()
	require.NoError(t, err)

	p := filepath.Join(in.Root, ".claude", "skills", "brickkit-component", "SKILL.md")
	mine := []byte("我改过了\n")
	require.NoError(t, os.WriteFile(p, mine, 0o644))

	res, err := in.Apply()
	require.NoError(t, err)
	assert.Empty(t, res.Written)
	require.Len(t, res.Skipped, 1)
	assert.Equal(t, StateUntracked, res.Skipped[0].State)

	after, err := os.ReadFile(p)
	require.NoError(t, err)
	assert.Equal(t, string(mine), string(after))
}
