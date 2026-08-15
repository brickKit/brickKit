// 本文件覆盖 20.2 / 20.4 / 20.5 走完整 brickkit add 的行为。
//
// internal/source 已经测过判定逻辑本身，这里测的是**接线**：配置里的
// installer.requireSignature 与 installer.publicKeys 真的被读出来、
// 真的传到了取 Manifest 的那条路径上。接线断了，那边的规则再对也没用。
package cli

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/security"
)

const addKeyRef = "keys/people-basic-release.pub"

// signerFixture 是一把测试用的发布者密钥。
type signerFixture struct {
	priv   *ecdsa.PrivateKey
	pubPEM []byte
}

func newSigner(t *testing.T) *signerFixture {
	t.Helper()

	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	der, err := x509.MarshalPKIXPublicKey(&priv.PublicKey)
	require.NoError(t, err)
	return &signerFixture{priv: priv, pubPEM: pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der})}
}

// sign 对一份 component.yaml 文本的规范化载荷签名。
func (s *signerFixture) sign(t *testing.T, manifestYAML string) map[string]any {
	t.Helper()

	payload, err := security.CanonicalPayload([]byte(manifestYAML))
	require.NoError(t, err)
	digest := sha256.Sum256(payload)
	der, err := ecdsa.SignASN1(rand.Reader, s.priv, digest[:])
	require.NoError(t, err)

	return map[string]any{
		"algorithm":    security.AlgorithmCosign,
		"publicKeyRef": addKeyRef,
		"value":        base64.StdEncoding.EncodeToString(der),
		"signedBy":     "release-bot@brickkit.io",
	}
}

// writeTrustedKey 把公钥写进项目，并返回该写进 installer.publicKeys 的片段。
func (s *signerFixture) writeTrustedKey(t *testing.T, projectDir string) string {
	t.Helper()

	path := filepath.Join(projectDir, filepath.FromSlash(addKeyRef))
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, s.pubPEM, 0o644))
	return "  publicKeys:\n    " + addKeyRef + ": " + addKeyRef + "\n"
}

// signedManifestMarket 建一个把 manifestYAML 当作 Manifest 返回的市场，
// 可选地在信封里带上签名。
//
// 直接收 YAML 文本而不是 comp：篡改场景要的正是"签名照原件签、发出去的却是
// 改过的内容"，只有能自由改文本才造得出来。
func signedManifestMarket(t *testing.T, manifestYAML string, sig map[string]any) *fakeMarket {
	t.Helper()

	var doc map[string]any
	payload, err := security.CanonicalPayload([]byte(manifestYAML))
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(payload, &doc))

	data := map[string]any{"manifest": doc, "sourceType": "git"}
	if sig != nil {
		data["signature"] = sig
	}
	body, err := json.Marshal(map[string]any{"success": true, "data": data})
	require.NoError(t, err)

	m := newFakeMarket(t)
	m.overrides["/manifest"] = marketResponse{status: http.StatusOK, body: string(body)}
	return m
}

// installerConfig 写一份带 installer 段的项目配置。
func installerConfig(t *testing.T, f *projectFixture, requireSignature bool, publicKeys string) {
	t.Helper()

	enforce := "false"
	if requireSignature {
		enforce = "true"
	}
	f.writeConfig(t, "components: []\nresources: []\ninstaller:\n  requireSignature: "+
		enforce+"\n"+publicKeys)
}

// ============================================================
// 20.2 有效签名 → 装得上，并且明确告诉使用者验过了
// ============================================================

func TestAddVerifiesSignature(t *testing.T) {
	signer := newSigner(t)
	c := comp{ID: "people/basic", Version: "1.2.0"}
	m := signedManifestMarket(t, c.yamlText(), signer.sign(t, c.yamlText()))
	f := newMarketProject(t, m, "")
	keys := signer.writeTrustedKey(t, f.Dir)
	installerConfig(t, f, true, keys)

	r := runIn(t, f.Dir, "add", c.ref())
	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)

	assert.Contains(t, r.stdout, "已校验", "验过了就要说出来（008 §8）")
	assert.Contains(t, r.stdout, "release-bot@brickkit.io")
}

// ============================================================
// 20.4 requireSignature: true → 未签名组件阻断
// ============================================================

func TestAddBlocksUnsignedWhenRequired(t *testing.T) {
	signer := newSigner(t)
	c := comp{ID: "people/basic", Version: "1.2.0"}
	m := signedManifestMarket(t, c.yamlText(), nil) // 市场没有签名
	f := newMarketProject(t, m, "")
	keys := signer.writeTrustedKey(t, f.Dir)
	installerConfig(t, f, true, keys)

	r := runIn(t, f.Dir, "add", c.ref())

	assert.Equal(t, clierr.ExitError, r.code)
	assert.Contains(t, r.stderr, "未签名")
	assert.Contains(t, r.stderr, "requireSignature")

	// 阻断就得是彻底的：配置不能被改了一半
	raw, err := os.ReadFile(filepath.Join(f.Dir, "brickkit.yaml"))
	require.NoError(t, err)
	assert.NotContains(t, string(raw), "people/basic",
		"签名校验没过，组件不该被写进 brickkit.yaml")
}

// ============================================================
// 20.5 requireSignature: false → 未签名组件可安装
// ============================================================

func TestAddAllowsUnsignedWhenNotRequired(t *testing.T) {
	c := comp{ID: "people/basic", Version: "1.2.0"}
	m := signedManifestMarket(t, c.yamlText(), nil)
	f := newMarketProject(t, m, "")
	installerConfig(t, f, false, "")

	r := runIn(t, f.Dir, "add", c.ref())
	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)

	raw, err := os.ReadFile(filepath.Join(f.Dir, "brickkit.yaml"))
	require.NoError(t, err)
	assert.Contains(t, string(raw), "people/basic")
	assert.NotContains(t, r.stdout, "已校验", "没验过就不能说验过了")
}

// ============================================================
// 篡改：签名对不上就是装不上
// ============================================================

func TestAddRejectsTamperedManifest(t *testing.T) {
	signer := newSigner(t)
	original := comp{ID: "people/basic", Version: "1.2.0"}
	sig := signer.sign(t, original.yamlText())

	// 市场发出去的是改过镜像的版本，签名却是照原件签的
	tampered := strings.Replace(original.yamlText(),
		"registry.example.com", "evil.example.com", 1)
	m := signedManifestMarket(t, tampered, sig)

	f := newMarketProject(t, m, "")
	keys := signer.writeTrustedKey(t, f.Dir)
	installerConfig(t, f, true, keys)

	r := runIn(t, f.Dir, "add", original.ref())

	assert.Equal(t, clierr.ExitError, r.code)
	assert.Contains(t, r.stderr, "签名校验不通过")
	assert.Contains(t, r.stderr, "可能已被篡改")
	assert.Equal(t, 1, strings.Count(r.stderr, "联系组件发布者重新签名"),
		"同一句建议不该出现两次——看着像程序出了毛病")
}

// ============================================================
// 配置本身出错要说人话
// ============================================================

// TestAddReportsMissingPublicKeyFile：公钥文件没跟着项目提交是最常见的事故。
func TestAddReportsMissingPublicKeyFile(t *testing.T) {
	c := comp{ID: "people/basic", Version: "1.2.0"}
	m := signedManifestMarket(t, c.yamlText(), nil)
	f := newMarketProject(t, m, "")
	installerConfig(t, f, true, "  publicKeys:\n    "+addKeyRef+": "+addKeyRef+"\n")

	r := runIn(t, f.Dir, "add", c.ref())

	assert.Equal(t, clierr.ExitError, r.code)
	assert.Contains(t, r.stderr, addKeyRef)
	assert.Contains(t, r.stderr, "installer.publicKeys")
}

// TestAddWarnsWhenRequiringSignatureWithoutKeys 是最容易被忽略的落差。
//
// requireSignature 默认为 true，而没配 publicKeys 时校验根本不会发生。
// 这时既不能把人挡在门外（那会让所有现存项目当场装不了东西，却换不来任何
// 安全收益），也不能一声不吭——使用者以为自己开着强制校验。
func TestAddWarnsWhenRequiringSignatureWithoutKeys(t *testing.T) {
	c := comp{ID: "people/basic", Version: "1.2.0"}
	m := signedManifestMarket(t, c.yamlText(), nil)
	f := newMarketProject(t, m, "")
	installerConfig(t, f, true, "") // 要求强制，却一把公钥都没配

	r := runIn(t, f.Dir, "add", c.ref())

	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)
	assert.Contains(t, r.stdout+r.stderr, "requireSignature")
	assert.Contains(t, r.stdout+r.stderr, "publicKeys")
}
