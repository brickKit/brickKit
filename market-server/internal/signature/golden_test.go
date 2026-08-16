package signature_test

// 本文件把市场侧的规范化输出钉死在与 CLI 同一份黄金样本上（P30）。
//
// market-server 是独立 module，引用不了主 module 的 internal/security，
// 只能自己实现一份规范化与验签。而"两份实现"必然带来漂移——
// 漂移的表现极其难查：**签名在 CLI 侧验得过、市场侧验不过**，
// 而两边的代码单独看都是对的。
//
// 唯一的防线是：两侧断言同一份样本的**逐字节输出**。
// 样本由主 module 的 internal/security.CanonicalPayload 生成，是参照系。

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/brickkit/market-server/internal/signature"
)

type goldenSample struct {
	ManifestJSON    json.RawMessage `json:"manifestJSON"`
	CanonicalBase64 string          `json:"canonicalBase64"`
	PublicKeyPEM    string          `json:"publicKeyPEM"`
	SignatureBase64 string          `json:"signatureBase64"`
}

func loadGolden(t *testing.T) goldenSample {
	t.Helper()

	raw, err := os.ReadFile("../../../testdata/signature-golden.json")
	require.NoError(t, err, "读不到黄金样本——它是两侧实现的唯一参照系")

	var g goldenSample
	require.NoError(t, json.Unmarshal(raw, &g))
	return g
}

// 市场侧的规范化必须与 CLI 侧逐字节一致。
func TestCanonicalMatchesGolden(t *testing.T) {
	g := loadGolden(t)

	got, err := signature.Canonical(g.ManifestJSON)
	require.NoError(t, err)

	want, err := base64.StdEncoding.DecodeString(g.CanonicalBase64)
	require.NoError(t, err)

	assert.Equal(t, string(want), string(got),
		"P30：市场侧的规范化与 CLI 侧分叉了。"+
			"后果是发布者签的名在市场验得过、消费方验不过（或反过来），"+
			"而两边代码单独看都对——这条测试是唯一能提前发现它的地方")
}

// 用样本里的真签名与真公钥验一遍：证明市场侧的验签实现是对的。
func TestVerifyGoldenSignature(t *testing.T) {
	g := loadGolden(t)

	assert.NoError(t,
		signature.Verify(g.ManifestJSON, g.SignatureBase64, []byte(g.PublicKeyPEM)),
		"P30：CLI 签出来的名，市场必须验得过")
}

// 换一份 Manifest 就必须验不过——否则"验签"什么都没证明。
func TestVerifyRejectsTamperedManifest(t *testing.T) {
	g := loadGolden(t)

	tampered := []byte(string(g.ManifestJSON[:len(g.ManifestJSON)-1]) + `,"injected":"evil"}`)

	assert.Error(t,
		signature.Verify(tampered, g.SignatureBase64, []byte(g.PublicKeyPEM)),
		"P30：改了内容还能验过的话，这套机制毫无意义")
}

// 换一把公钥也必须验不过。
func TestVerifyRejectsWrongKey(t *testing.T) {
	g := loadGolden(t)
	other := `-----BEGIN PUBLIC KEY-----
MFkwEwYHKoZIzj0CAQYIKoZIzj0DAQcDQgAEXhH8h0Ry0kFhK1yZ0RPFHBnkYxHR
1kZ8Qw0hQZ8Zx0kYxHR1kZ8Qw0hQZ8Zx0kYxHR1kZ8Qw0hQZ8Zx0kYxHR1kZ8Qw==
-----END PUBLIC KEY-----
`
	assert.Error(t, signature.Verify(g.ManifestJSON, g.SignatureBase64, []byte(other)),
		"P30：换把钥匙就该验不过")
}
