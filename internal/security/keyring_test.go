package security_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/security"
)

// TestLoadKeyRingResolvesRelativeToProject 说明相对路径以谁为基准。
//
// brickkit.yaml 里写的是 keys/people-basic-release.pub。若相对当前工作目录解析，
// 那么在 components/ 子目录里执行 brickkit 就会找不到公钥——同一份配置在不同
// 目录下行为不同，是最难查的一类问题。基准必须是项目根目录。
func TestLoadKeyRingResolvesRelativeToProject(t *testing.T) {
	k := newTestKey(t)
	projectRoot := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(projectRoot, "keys"), 0o755))
	require.NoError(t, os.WriteFile(
		filepath.Join(projectRoot, "keys", "people-basic-release.pub"), k.pubPEM, 0o644))

	ring, err := security.LoadKeyRing(map[string]string{keyRef: keyRef}, projectRoot)
	require.NoError(t, err)

	assert.False(t, ring.Empty())
	assert.Equal(t, []string{keyRef}, ring.Refs())

	payload, err := security.CanonicalPayload([]byte(sampleManifest))
	require.NoError(t, err)
	assert.NoError(t, security.Verify(payload, validSignature(t, k), ring))
}

func TestLoadKeyRingAcceptsAbsolutePath(t *testing.T) {
	k := newTestKey(t)
	path := k.writePub(t, "release.pub")

	ring, err := security.LoadKeyRing(map[string]string{keyRef: path}, t.TempDir())
	require.NoError(t, err)

	_, ok := ring.Get(keyRef)
	assert.True(t, ok)
}

// TestLoadKeyRingReportsMissingFile 覆盖最常见的一种：公钥没跟着项目提交。
func TestLoadKeyRingReportsMissingFile(t *testing.T) {
	projectRoot := t.TempDir()

	_, err := security.LoadKeyRing(map[string]string{keyRef: keyRef}, projectRoot)
	require.Error(t, err)

	var cerr *clierr.Error
	require.ErrorAs(t, err, &cerr)
	rendered := cerr.Format()
	assert.Contains(t, rendered, keyRef)
	assert.Contains(t, rendered, "installer.publicKeys", "要指出是哪条配置有问题")
	assert.Contains(t, rendered, filepath.Join(projectRoot, keyRef), "要给出找过的完整路径")
}

func TestLoadKeyRingRejectsGarbage(t *testing.T) {
	projectRoot := t.TempDir()
	path := filepath.Join(projectRoot, "broken.pub")
	require.NoError(t, os.WriteFile(path, []byte("这不是 PEM"), 0o644))

	_, err := security.LoadKeyRing(map[string]string{keyRef: "broken.pub"}, projectRoot)
	require.Error(t, err)

	var cerr *clierr.Error
	require.ErrorAs(t, err, &cerr)
	assert.Contains(t, cerr.Format(), "PEM")
}

// TestLoadKeyRingEmptyIsNotAnError 说明"没配公钥"本身不是配置错误。
//
// requireSignature 默认就是 true（附录 D.1），若把"没配公钥"直接判为配置非法，
// 那么每一个还没用上签名的现有项目连 brickkit status 都跑不起来了。
// 该拦的地方是安装时（那里能说清楚是哪个组件、该怎么办），不是解析配置时。
func TestLoadKeyRingEmptyIsNotAnError(t *testing.T) {
	ring, err := security.LoadKeyRing(nil, t.TempDir())
	require.NoError(t, err)
	assert.True(t, ring.Empty())
}
