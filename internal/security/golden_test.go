package security_test

// 本文件把**规范化的输出**钉死在一份黄金样本上（P30）。
//
// # 为什么需要它
//
// 市场要真验签，就必须和 CLI 用同一套规范化逻辑。但 market-server 是独立
// module，引用不了 `internal/`——只能各写一份。而"两份实现"必然带来漂移，
// 漂移的表现极其难查：**签名在一边验得过、另一边验不过**，
// 而两边的代码单独看都是对的。
//
// 所以两侧各有一条测试，断言同一份黄金样本：
//
//	主 module（本文件）        CanonicalPayload(manifest) == golden.canonical
//	market-server            canonical(manifest)        == golden.canonical
//
// 样本刻意塞满了容易分叉的东西：乱序键、嵌套、数组、整数、浮点、布尔、
// unicode、转义、空 map、空数组、null、深层嵌套。任何一侧的解析或编码
// 行为变了，它这条测试就先红——而不是等到某个发布者的签名莫名其妙验不过。
//
// 重新生成样本：见 testdata/signature-golden.json 里的 _comment。

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/brickkit/brickkit/internal/security"
)

// goldenSample 是两侧共用的那份样本。
type goldenSample struct {
	ManifestJSON    json.RawMessage `json:"manifestJSON"`
	CanonicalBase64 string          `json:"canonicalBase64"`
	PublicKeyPEM    string          `json:"publicKeyPEM"`
	SignatureBase64 string          `json:"signatureBase64"`
}

func loadGolden(t *testing.T) goldenSample {
	t.Helper()

	raw, err := os.ReadFile("../../testdata/signature-golden.json")
	require.NoError(t, err, "读不到黄金样本——它是两侧实现的唯一参照系")

	var g goldenSample
	require.NoError(t, json.Unmarshal(raw, &g))
	return g
}

// 规范化的输出必须逐字节等于样本。
func TestCanonicalPayloadMatchesGolden(t *testing.T) {
	g := loadGolden(t)

	got, err := security.CanonicalPayload(g.ManifestJSON)
	require.NoError(t, err)

	want, err := base64.StdEncoding.DecodeString(g.CanonicalBase64)
	require.NoError(t, err)

	assert.Equal(t, string(want), string(got),
		"P30：规范化结果变了。要么是这里改坏了，要么样本该重新生成——"+
			"但**在确认 market-server 侧同步之前不要重新生成**，"+
			"否则漂移会被样本掩盖掉")
}

// 样本里的签名必须验得过——证明样本自身是自洽的。
func TestGoldenSignatureVerifies(t *testing.T) {
	g := loadGolden(t)

	ring := security.NewKeyRing()
	require.NoError(t, ring.Add("golden", []byte(g.PublicKeyPEM)))

	sig := security.Signature{
		Algorithm:    security.AlgorithmCosign,
		PublicKeyRef: "golden",
		Value:        g.SignatureBase64,
	}
	payload, err := security.CanonicalPayload(g.ManifestJSON)
	require.NoError(t, err)

	assert.NoError(t, security.Verify(payload, sig, ring),
		"P30：样本自身必须自洽，否则它证明不了任何事")
}
