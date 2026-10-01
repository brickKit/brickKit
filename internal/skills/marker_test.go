package skills

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/brickkit/brickkit/internal/i18n"
)

func TestMarkRoundTrip(t *testing.T) {
	body := []byte("# skill\n\ntext\n")
	marked := Mark(body, "0.5.0")
	got, v, s, ok := ReadMarker(marked)
	require.True(t, ok)
	assert.Equal(t, normalize(body), got)
	assert.Equal(t, "0.5.0", v)
	assert.Equal(t, Sum(normalize(body)), s)
	assert.True(t, strings.HasSuffix(string(marked), "text\n\n<!-- brickkit:skill version=0.5.0 sum="+s+" -->\n"), string(marked))
}

func TestMarkerSumIgnoresTrailingNewlines(t *testing.T) {
	marked := Mark([]byte("# skill\n"), "0.5.0")
	for _, edited := range [][]byte{
		marked[:len(marked)-1],                       // 编辑器去掉了最后的换行
		append(append([]byte(nil), marked...), '\n'), // 或者多补了一个
	} {
		body, _, s, ok := ReadMarker(edited)
		require.True(t, ok)
		assert.Equal(t, s, Sum(body))
	}
}

func TestMarkerWithCRLF(t *testing.T) {
	marked := []byte("# skill\r\n\r\n<!-- brickkit:skill version=1.0.0 sum=" + Sum([]byte("# skill")) + " -->\r\n")
	body, v, s, ok := ReadMarker(marked)
	require.True(t, ok)
	assert.Equal(t, "1.0.0", v)
	assert.Equal(t, s, Sum(body))
}

func TestFreshCloneUpdatesWithoutLock(t *testing.T) {
	in := Installer{Root: t.TempDir(), Version: "1.0.0"}
	_, err := in.Apply()
	require.NoError(t, err)
	// 同事新克隆：.brickkit/ 不在，但文件自己带着记录
	in2 := Installer{Root: in.Root, Version: "2.0.0"}
	for _, a := range AssetsFor(ScopeProject, i18n.EN) {
		st, err := in2.stateOf(a)
		require.NoError(t, err)
		assert.Contains(t, []State{StateCurrent, StateOutdated}, st.State, a.Target)
	}
}

func TestLegacyLockMigratesOnceThenGoesAway(t *testing.T) {
	root := t.TempDir()
	a := AssetsFor(ScopeProject, i18n.EN)[0]
	old := []byte("old installed text\n")
	other := AssetsFor(ScopeProject, i18n.EN)[1]
	edited := []byte("edited by hand\n")
	in := Installer{Root: root, Version: "1.0.0", LegacyLockPath: filepath.Join(root, ".brickkit", "skills.lock")}
	writeAt(t, in, a.Target, old)
	writeAt(t, in, other.Target, edited)
	l := &Lock{Lang: "en"}
	l.Set(LockEntry{Path: a.Target, Version: "0.1.0", Sum: Sum(old)})
	l.Set(LockEntry{Path: other.Target, Version: "0.1.0", Sum: Sum([]byte("what we wrote\n"))})
	require.NoError(t, l.Save(in.LegacyLockPath))

	assert.Equal(t, StateOutdated, stateOf(t, in, a.Target).State)
	assert.Equal(t, "0.1.0", stateOf(t, in, a.Target).FromVersion)
	assert.Equal(t, StateModified, stateOf(t, in, other.Target).State)

	res, err := in.Apply()
	require.NoError(t, err)
	assert.Contains(t, res.Written, a.Target)
	assert.NotContains(t, res.Written, other.Target)
	assert.NoFileExists(t, in.LegacyLockPath)
	data, _ := os.ReadFile(filepath.Join(root, other.Target))
	assert.Equal(t, string(edited), string(data))
}

func TestOldAgentsMatcherReadsTheLockUpFront(t *testing.T) {
	root := t.TempDir()
	in := Installer{Root: root, LegacyLockPath: filepath.Join(root, ".brickkit", "skills.lock")}
	mine := []byte("# whatever an old CLI wrote\n")
	assert.False(t, in.IsOldAgents(mine))
	l := &Lock{}
	l.Set(LockEntry{Path: "AGENTS.md", Version: "0.1.0", Sum: Sum(mine)})
	require.NoError(t, l.Save(in.LegacyLockPath))
	match := in.OldAgentsMatcher()
	require.NoError(t, os.Remove(in.LegacyLockPath)) // Apply 删掉了它
	assert.True(t, match(mine))
}

func TestLangFromAgentsBlock(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "AGENTS.md"),
		[]byte("# P\n<!-- brickkit:managed:begin lang=zh -->\n<!-- brickkit:managed:end -->\n"), 0o644))
	in := Installer{Root: root, Version: "1.0.0"}
	lang, err := in.ResolvedLang()
	require.NoError(t, err)
	assert.Equal(t, i18n.ZH, lang)
}

func TestNoAgentsAsset(t *testing.T) {
	for _, l := range []i18n.Lang{i18n.EN, i18n.ZH} {
		for _, a := range AssetsFor(ScopeProject, l) {
			assert.NotEqual(t, "AGENTS.md", a.Target)
		}
	}
}

// Windows 上 Git 默认 core.autocrlf=true：检出时每一行都成了 CRLF。这不算手改。
func TestCRLFCheckoutIsNotAnEdit(t *testing.T) {
	in := Installer{Root: t.TempDir(), Version: "1.0.0"}
	_, err := in.Apply()
	require.NoError(t, err)
	for _, a := range AssetsFor(ScopeProject, i18n.EN) {
		p := filepath.Join(in.Root, a.Target)
		data, err := os.ReadFile(p)
		require.NoError(t, err)
		require.NoError(t, os.WriteFile(p, []byte(strings.ReplaceAll(string(data), "\n", "\r\n")), 0o644))
		st, err := in.stateOf(a)
		require.NoError(t, err)
		assert.Equal(t, StateCurrent, st.State, a.Target)
	}
}

// 老项目的同事新克隆（没有记录行，也没有旧版 skills.lock）：内容是某个旧版 CLI 装的、没被改过的，照样认得出、升得上去。
func TestOldShippedFileWithoutLockIsOutdated(t *testing.T) {
	old, err := os.ReadFile(filepath.Join("testdata", "legacy", "assemble-SKILL.md"))
	require.NoError(t, err)
	in := newInstaller(t)
	writeAt(t, in, assemble, []byte(strings.ReplaceAll(string(old), "\n", "\r\n"))) // Windows 检出也认得
	assert.Equal(t, StateOutdated, stateOf(t, in, assemble).State)
	res, err := in.Apply()
	require.NoError(t, err)
	assert.Contains(t, res.Written, assemble)
}

// 旧版 CLI 装的 AGENTS.md（当年是技能资产）：没有锁也认得出，交给 agentsmd 整份换成新骨架。
func TestOldShippedAgentsIsRecognisedWithoutLock(t *testing.T) {
	old, err := os.ReadFile(filepath.Join("testdata", "legacy", "AGENTS.md"))
	require.NoError(t, err)
	in := newInstaller(t)
	assert.True(t, in.IsOldAgents(old))
	assert.True(t, in.IsOldAgents([]byte(strings.ReplaceAll(string(old), "\n", "\r\n"))))
	assert.False(t, in.IsOldAgents(append(old, []byte("my line\n")...)), "an edited copy is the author's now")
}
