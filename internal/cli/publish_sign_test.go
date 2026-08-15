// 本文件覆盖开发计划 20.1（签名生成正常）在 CLI 侧的行为：
// brickkit publish --sign。
//
// 这里用的是**真 cosign 二进制**。理由与 internal/security 的跨工具测试一样：
// 造一个假签名器只能证明"我们把字段填对了"，证明不了发布出去的东西真能被验。
package cli

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/security"
)

// requireCosign 找到 cosign，找不到就跳过。
func requireCosign(t *testing.T) string {
	t.Helper()

	if path, err := exec.LookPath("cosign"); err == nil {
		return path
	}
	if home, err := os.UserHomeDir(); err == nil {
		candidate := filepath.Join(home, "go", "bin", "cosign")
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
	}
	t.Skip("未安装 cosign，跳过签名发布测试")
	return ""
}

// generateKeyPair 在 dir 下用真 cosign 生成密钥对，返回私钥与公钥路径。
func generateKeyPair(t *testing.T, dir, name string) (keyPath, pubPath string) {
	t.Helper()

	bin := requireCosign(t)
	require.NoError(t, os.MkdirAll(dir, 0o755))

	cmd := exec.Command(bin, "generate-key-pair")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "COSIGN_PASSWORD=")
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "cosign generate-key-pair 失败：%s", out)

	keyPath = filepath.Join(dir, name+".key")
	pubPath = filepath.Join(dir, name+".pub")
	require.NoError(t, os.Rename(filepath.Join(dir, "cosign.key"), keyPath))
	require.NoError(t, os.Rename(filepath.Join(dir, "cosign.pub"), pubPath))
	return keyPath, pubPath
}

// publishedSignature 从假市场收到的建版本请求里取出签名与 Manifest。
func publishedSignature(t *testing.T, m *fakeMarket) (security.Signature, json.RawMessage) {
	t.Helper()

	call := m.find(t, "POST", "/components/people/basic/versions")
	var body struct {
		Manifest  json.RawMessage    `json:"manifest"`
		Signature security.Signature `json:"signature"`
	}
	require.NoError(t, json.Unmarshal(call.Body, &body))
	return body.Signature, body.Manifest
}

// ============================================================
// 20.1 签名生成正常
// ============================================================

// TestPublishSignProducesVerifiableSignature 是 20.1 的主干。
//
// 断言的不是"请求里有 signature 字段"，而是**这个签名对上传的那份 Manifest
// 真的验得过**——用的还是使用者侧将来会用的同一套校验代码。
// 只检查字段存在的话，签错内容、签了个空串都能过。
func TestPublishSignProducesVerifiableSignature(t *testing.T) {
	t.Setenv("COSIGN_PASSWORD", "")
	m := newFakeMarket(t)
	f := newMarketProject(t, m, "")
	loginTo(t, f, m)
	root := writeComponentDir(t, f.Dir, comp{ID: "people/basic", Version: "1.2.0"})

	keyPath, pubPath := generateKeyPair(t, filepath.Join(f.Dir, "keys"), "people-basic-release")

	r := runIn(t, f.Dir, "publish", "--path", root, "--sign", "--key", keyPath)
	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)

	sig, manifest := publishedSignature(t, m)
	assert.Equal(t, security.AlgorithmCosign, sig.Algorithm)
	assert.NotEmpty(t, sig.Value)
	assert.False(t, sig.SignedAt.IsZero(), "signedAt 要填（008 §8.3）")

	// 用使用者侧的校验代码验一遍：这才是签名有没有用的唯一判据
	pubPEM, err := os.ReadFile(pubPath)
	require.NoError(t, err)
	ring := security.NewKeyRing()
	require.NoError(t, ring.Add(sig.PublicKeyRef, pubPEM))

	assert.NoError(t, security.VerifyManifest(manifest, sig, ring, "people/basic", "1.2.0"),
		"发布出去的签名必须能被 add 侧验过")
}

// TestPublishSignDerivesPublicKeyRefFromKey 说明 ref 的默认值是怎么来的。
//
// ref 是发布者与使用者之间的契约（使用者照它在 installer.publicKeys 配同名条目），
// 所以既不能不填，也不该由 CLI 随手编一个。按 .key → .pub 推导，
// 恰好得到 008 §8.3 示例里的 keys/people-basic-release.pub，可预期也可解释。
func TestPublishSignDerivesPublicKeyRefFromKey(t *testing.T) {
	t.Setenv("COSIGN_PASSWORD", "")
	m := newFakeMarket(t)
	f := newMarketProject(t, m, "")
	loginTo(t, f, m)
	root := writeComponentDir(t, f.Dir, comp{ID: "people/basic", Version: "1.2.0"})

	generateKeyPair(t, filepath.Join(f.Dir, "keys"), "people-basic-release")

	r := runIn(t, f.Dir, "publish", "--path", root, "--sign",
		"--key", "keys/people-basic-release.key")
	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)

	sig, _ := publishedSignature(t, m)
	assert.Equal(t, "keys/people-basic-release.pub", sig.PublicKeyRef)
}

func TestPublishSignHonorsExplicitPublicKeyRef(t *testing.T) {
	t.Setenv("COSIGN_PASSWORD", "")
	m := newFakeMarket(t)
	f := newMarketProject(t, m, "")
	loginTo(t, f, m)
	root := writeComponentDir(t, f.Dir, comp{ID: "people/basic", Version: "1.2.0"})
	keyPath, _ := generateKeyPair(t, filepath.Join(f.Dir, "keys"), "people-basic-release")

	r := runIn(t, f.Dir, "publish", "--path", root, "--sign", "--key", keyPath,
		"--public-key-ref", "keys/acme-2026.pub")
	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)

	sig, _ := publishedSignature(t, m)
	assert.Equal(t, "keys/acme-2026.pub", sig.PublicKeyRef)
}

// TestPublishSignTellsUsersWhatToConfigure：签完要告诉发布者接下来该说什么。
//
// 签名的另一半在使用者手里——他们得把这个 ref 和公钥配进 installer.publicKeys，
// 否则谁也验不了。发布者不知道要转告什么，这条链就永远接不上。
func TestPublishSignTellsUsersWhatToConfigure(t *testing.T) {
	t.Setenv("COSIGN_PASSWORD", "")
	m := newFakeMarket(t)
	f := newMarketProject(t, m, "")
	loginTo(t, f, m)
	root := writeComponentDir(t, f.Dir, comp{ID: "people/basic", Version: "1.2.0"})
	keyPath, _ := generateKeyPair(t, filepath.Join(f.Dir, "keys"), "people-basic-release")

	r := runIn(t, f.Dir, "publish", "--path", root, "--sign", "--key", keyPath)
	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)

	assert.Contains(t, r.stdout, "已签名")
	assert.Contains(t, r.stdout, "installer.publicKeys",
		"要告诉发布者：使用者得配这个才能验签")
	assert.Contains(t, r.stdout, "keys/people-basic-release.pub")
}

// ============================================================
// 失败要在建版本**之前**
// ============================================================

// TestPublishSignFailsBeforeCreatingVersion：签名失败不能留下半成品。
//
// 版本号一旦建出来就不可回收（市场侧 18.14：版本号不可重复，软删除也占位）。
// 先建 draft 再签名的话，签名一失败，这个版本号就永远废了——发布者只能改版本号
// 重发，而组件的版本号是有语义的，不该因为一次密钥路径写错就被烧掉一个。
func TestPublishSignFailsBeforeCreatingVersion(t *testing.T) {
	m := newFakeMarket(t)
	f := newMarketProject(t, m, "")
	loginTo(t, f, m)
	root := writeComponentDir(t, f.Dir, comp{ID: "people/basic", Version: "1.2.0"})

	r := runIn(t, f.Dir, "publish", "--path", root, "--sign",
		"--key", filepath.Join(f.Dir, "keys", "nonexistent.key"))

	assert.Equal(t, clierr.ExitError, r.code)
	assert.Contains(t, r.stderr, "nonexistent.key", "要指出是哪个私钥找不到")
	assert.NotContains(t, strings.Join(m.requests(), " "), "POST /components/people/basic/versions",
		"签名失败时不能已经把版本建出来——版本号不可回收")
}

// TestPublishWithoutSignStillWorks 是回归：不加 --sign 的发布一切照旧，不带签名。
func TestPublishWithoutSignStillWorks(t *testing.T) {
	m := newFakeMarket(t)
	f := newMarketProject(t, m, "")
	loginTo(t, f, m)
	root := writeComponentDir(t, f.Dir, comp{ID: "people/basic", Version: "1.2.0"})

	r := runIn(t, f.Dir, "publish", "--path", root)
	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)

	call := m.find(t, "POST", "/components/people/basic/versions")
	var body map[string]any
	require.NoError(t, json.Unmarshal(call.Body, &body))
	assert.NotContains(t, body, "signature", "没签名就不该出现 signature 字段")
}
