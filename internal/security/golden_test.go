package security_test

// 本文件把**规范化的输出**钉死在一份黄金样本上。
//
// # 规范化是一种线格式
//
// 签名不是对原始字节做的，而是对"解析成数据结构、再按固定规则编码回来"的
// **规范化载荷**做的（见 canonical.go 的说明）。这意味着那套规则一旦改变，
// **此前发布的每一个签名都会一起失效**——而且失效得非常安静：
//
//	发布者：什么都没做，签名还在市场里
//	使用者：升级了 CLI，然后每个组件都验签失败
//	现场：  "签名校验不通过"，而没有任何东西被篡改过
//
// 会触发它的改动看着都很无害：换个 JSON 编码器、给 normalize 多加一个
// 类型分支、把 yaml.v3 换成别的解析器、甚至只是改了缩进参数。
// 这条测试是唯一能在合并前发现它的地方。
//
// 样本刻意塞满了容易分叉的东西：乱序键、深层嵌套、数组、整数、浮点、布尔、
// unicode、转义、空 map、空数组、null。
//
// ⚠️ **这条测试红了，默认不是它错了。** 先确认那个改动是不是真要
// 让所有历史签名失效；确实要改的话，那是一次破坏性变更，
// 需要连同版本策略一起考虑，而不是重新生成样本了事。
// 重新生成的办法见 testdata/signature-golden.json 里的 _comment。

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/brickkit/brickkit/internal/security"
)

type goldenSample struct {
	ManifestJSON    json.RawMessage `json:"manifestJSON"`
	CanonicalBase64 string          `json:"canonicalBase64"`
	PublicKeyPEM    string          `json:"publicKeyPEM"`
	SignatureBase64 string          `json:"signatureBase64"`
}

func loadGolden(t *testing.T) goldenSample {
	t.Helper()

	raw, err := os.ReadFile("../../testdata/signature-golden.json")
	require.NoError(t, err)

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
		"规范化结果变了——**此前发布的每一个签名都会失效**。"+
			"确认这是有意的破坏性变更之前，不要重新生成样本")
}

// 一年前签的名，今天必须还验得过。
//
// 上一条测的是"编码没变"，这条测的是"整条链路没变"：
// 规范化 + 摘要 + 公钥解析 + ECDSA 校验，任何一环变了它都会红。
func TestGoldenSignatureStillVerifies(t *testing.T) {
	g := loadGolden(t)

	ring := security.NewKeyRing()
	require.NoError(t, ring.Add("golden", []byte(g.PublicKeyPEM)))

	payload, err := security.CanonicalPayload(g.ManifestJSON)
	require.NoError(t, err)

	assert.NoError(t, security.Verify(payload, security.Signature{
		Algorithm:    security.AlgorithmCosign,
		PublicKeyRef: "golden",
		Value:        g.SignatureBase64,
	}, ring), "历史签名必须一直验得过，否则升级 CLI 就等于把所有组件废掉")
}

// 内容动一个字节就必须验不过——否则上面两条什么都没证明。
func TestGoldenRejectsTamperedManifest(t *testing.T) {
	g := loadGolden(t)

	tampered := append([]byte(nil), g.ManifestJSON[:len(g.ManifestJSON)-1]...)
	tampered = append(tampered, []byte(`,"injected":"evil"}`)...)

	ring := security.NewKeyRing()
	require.NoError(t, ring.Add("golden", []byte(g.PublicKeyPEM)))

	payload, err := security.CanonicalPayload(tampered)
	require.NoError(t, err)

	assert.Error(t, security.Verify(payload, security.Signature{
		Algorithm:    security.AlgorithmCosign,
		PublicKeyRef: "golden",
		Value:        g.SignatureBase64,
	}, ring), "改了内容还能验过的话，这套机制毫无意义")
}
