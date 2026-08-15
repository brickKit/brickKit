package security_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/security"
)

// 一份最小但真实的 Manifest（002 §3）。签名的对象就是它。
const sampleManifest = `apiVersion: brickkit.io/v1
kind: Component
metadata:
  id: people/basic
  name: 人员基础信息
  version: 1.2.0
deployment:
  image: registry.brickkit.io/people-basic:1.2.0
  port: 8080
`

const keyRef = "keys/people-basic-release.pub"

// newRing 构造一个只信任 k 的钥匙串。
func newRing(t *testing.T, k *testKey) *security.KeyRing {
	t.Helper()

	ring := security.NewKeyRing()
	require.NoError(t, ring.Add(keyRef, k.pubPEM))
	return ring
}

// validSignature 返回 k 对 sampleManifest 的规范化载荷所做的合法签名。
func validSignature(t *testing.T, k *testKey) security.Signature {
	t.Helper()

	payload, err := security.CanonicalPayload([]byte(sampleManifest))
	require.NoError(t, err)

	return security.Signature{
		Algorithm:    security.AlgorithmCosign,
		PublicKeyRef: keyRef,
		Value:        k.sign(t, payload),
		SignedAt:     time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC),
		SignedBy:     "release-bot@brickkit.io",
	}
}

// codeOf 取出 CLI 错误码；不是 CLI 错误就直接让测试失败。
func codeOf(t *testing.T, err error) clierr.Code {
	t.Helper()

	require.Error(t, err)
	var cerr *clierr.Error
	require.ErrorAs(t, err, &cerr, "签名失败必须是带错误码的 CLI 错误，不能是裸 error")
	return cerr.Code
}

// ---------------------------------------------------------------
// 20.2 签名校验通过
// ---------------------------------------------------------------

func TestVerifyAcceptsValidSignature(t *testing.T) {
	k := newTestKey(t)
	payload, err := security.CanonicalPayload([]byte(sampleManifest))
	require.NoError(t, err)

	assert.NoError(t, security.Verify(payload, validSignature(t, k), newRing(t, k)))
}

// ---------------------------------------------------------------
// 20.3 篡改后校验失败
// ---------------------------------------------------------------

func TestVerifyRejectsTamperedPayload(t *testing.T) {
	k := newTestKey(t)
	sig := validSignature(t, k)

	// 把镜像换成攻击者自己的：这正是签名要挡住的事
	tampered := `apiVersion: brickkit.io/v1
kind: Component
metadata:
  id: people/basic
  name: 人员基础信息
  version: 1.2.0
deployment:
  image: evil.example.com/people-basic:1.2.0
  port: 8080
`
	payload, err := security.CanonicalPayload([]byte(tampered))
	require.NoError(t, err)

	err = security.Verify(payload, sig, newRing(t, k))
	assert.Equal(t, clierr.CodeSignatureInvalid, codeOf(t, err))
}

// TestVerifyRejectsSignatureFromAnotherKey 覆盖"签名本身合法，但不是可信发布者签的"。
//
// 这条比篡改更要紧：攻击者手里一定有自己的密钥对，能造出格式完全正确的签名。
// 唯一挡住他的是"这个公钥不在项目的信任列表里"。
func TestVerifyRejectsSignatureFromAnotherKey(t *testing.T) {
	trusted := newTestKey(t)
	attacker := newTestKey(t)

	payload, err := security.CanonicalPayload([]byte(sampleManifest))
	require.NoError(t, err)

	sig := validSignature(t, attacker) // 攻击者用自己的私钥签，但冒用可信公钥的 ref
	err = security.Verify(payload, sig, newRing(t, trusted))
	assert.Equal(t, clierr.CodeSignatureInvalid, codeOf(t, err))
}

// ---------------------------------------------------------------
// 信任来源：公钥必须由项目自己声明
// ---------------------------------------------------------------

// TestVerifyRejectsUnknownKeyRef 是整套签名机制成立的前提。
//
// 如果公钥能跟着签名一起从市场取，那等于市场自己给自己发证：市场被攻破时，
// 攻击者把组件和公钥一起换掉，验签照样通过，签名就成了摆设（008 §14.1
// "市场被攻破 → 签名校验"）。所以公钥只能来自 brickkit.yaml。
func TestVerifyRejectsUnknownKeyRef(t *testing.T) {
	k := newTestKey(t)
	payload, err := security.CanonicalPayload([]byte(sampleManifest))
	require.NoError(t, err)

	sig := validSignature(t, k)
	sig.PublicKeyRef = "keys/somebody-else.pub"

	err = security.Verify(payload, sig, newRing(t, k))
	require.Equal(t, clierr.CodeSignatureInvalid, codeOf(t, err))

	var cerr *clierr.Error
	require.ErrorAs(t, err, &cerr)
	assert.Contains(t, cerr.Format(), "installer",
		"错误必须告诉使用者去哪儿声明这个公钥，否则他只会以为是组件坏了")
}

func TestVerifyRejectsEmptyKeyRing(t *testing.T) {
	k := newTestKey(t)
	payload, err := security.CanonicalPayload([]byte(sampleManifest))
	require.NoError(t, err)

	err = security.Verify(payload, validSignature(t, k), security.NewKeyRing())
	assert.Equal(t, clierr.CodeSignatureInvalid, codeOf(t, err))
}

// ---------------------------------------------------------------
// 签名结构本身的校验
// ---------------------------------------------------------------

func TestVerifyRejectsUnknownAlgorithm(t *testing.T) {
	k := newTestKey(t)
	payload, err := security.CanonicalPayload([]byte(sampleManifest))
	require.NoError(t, err)

	sig := validSignature(t, k)
	sig.Algorithm = "rsa-pkcs1" // 我们不认识 → 只能拒绝，绝不能"不认识就放过"

	err = security.Verify(payload, sig, newRing(t, k))
	assert.Equal(t, clierr.CodeSignatureInvalid, codeOf(t, err))
}

func TestVerifyRejectsMalformedValue(t *testing.T) {
	k := newTestKey(t)
	payload, err := security.CanonicalPayload([]byte(sampleManifest))
	require.NoError(t, err)

	for name, value := range map[string]string{
		"空值":               "",
		"不是 base64":        "这不是签名",
		"base64 但不是 ASN.1": "aGVsbG8gd29ybGQ=",
	} {
		t.Run(name, func(t *testing.T) {
			sig := validSignature(t, k)
			sig.Value = value
			err := security.Verify(payload, sig, newRing(t, k))
			assert.Equal(t, clierr.CodeSignatureInvalid, codeOf(t, err))
		})
	}
}

// ---------------------------------------------------------------
// 规范化：签名要经得起 JSON / YAML 来回转换
// ---------------------------------------------------------------

// TestCanonicalPayloadSurvivesFormatRoundTrip 是这套方案能不能落地的关键。
//
// 发布时 CLI 把 component.yaml 转成 JSON 上传，市场存 JSON，CLI 取回来又转回
// YAML 去解析——中间字节序早就变了。如果签名是对"某一种写法的字节"做的，
// 它在这条链路上必然失效。所以签名的对象是**规范化载荷**：不论拿到的是 YAML
// 还是 JSON、键的顺序如何，只要语义相同，规范化结果就必须逐字节相同。
func TestCanonicalPayloadSurvivesFormatRoundTrip(t *testing.T) {
	fromYAML, err := security.CanonicalPayload([]byte(sampleManifest))
	require.NoError(t, err)

	// 同一份 Manifest 的 JSON 写法，键的顺序还故意打乱了
	asJSON := `{"deployment":{"port":8080,"image":"registry.brickkit.io/people-basic:1.2.0"},
	"kind":"Component","apiVersion":"brickkit.io/v1",
	"metadata":{"version":"1.2.0","id":"people/basic","name":"人员基础信息"}}`
	fromJSON, err := security.CanonicalPayload([]byte(asJSON))
	require.NoError(t, err)

	assert.Equal(t, string(fromYAML), string(fromJSON))
}

// TestCanonicalPayloadIgnoresCommentsAndIndent 说明什么不算篡改：
// 注释、缩进、引号风格改了，签名仍然有效——它们不改变组件的任何行为。
func TestCanonicalPayloadIgnoresCommentsAndIndent(t *testing.T) {
	reformatted := `# 人员基础信息组件
apiVersion: "brickkit.io/v1"
kind: Component
metadata:
    id: "people/basic"
    name: 人员基础信息
    version: "1.2.0"
deployment:
    image: "registry.brickkit.io/people-basic:1.2.0"   # 发布镜像
    port: 8080
`
	a, err := security.CanonicalPayload([]byte(sampleManifest))
	require.NoError(t, err)
	b, err := security.CanonicalPayload([]byte(reformatted))
	require.NoError(t, err)

	assert.Equal(t, string(a), string(b))
}

// TestCanonicalPayloadRejectsDuplicateKeys 堵住规范化本身的攻击面。
//
// YAML 里写两次同一个键时，不同解析器取的可能是第一个也可能是最后一个。
// 只要存在这种分歧，攻击者就能构造出"发布者看到的是 A、CLI 校验的也是 A、
// 但运行时用的是 B"的文档。宁可直接拒绝。
func TestCanonicalPayloadRejectsDuplicateKeys(t *testing.T) {
	_, err := security.CanonicalPayload([]byte(`apiVersion: brickkit.io/v1
kind: Component
deployment:
  image: registry.brickkit.io/people-basic:1.2.0
  image: evil.example.com/people-basic:1.2.0
`))
	assert.Error(t, err)
}

func TestCanonicalPayloadRejectsNonMapping(t *testing.T) {
	for name, raw := range map[string]string{
		"空文档":  "",
		"只有注释": "# 什么都没有\n",
		"是列表":  "- a\n- b\n",
	} {
		t.Run(name, func(t *testing.T) {
			_, err := security.CanonicalPayload([]byte(raw))
			assert.Error(t, err)
		})
	}
}

// ---------------------------------------------------------------
// VerifyManifest：连同"这是谁的签名"一起校验
// ---------------------------------------------------------------

// TestVerifyManifestRejectsSubstitutedComponent 挡住版本替换攻击。
//
// 签名只能证明"这份 Manifest 是发布者签的"，不能证明"它就是我要装的那个组件"。
// 被攻破的市场可以把 people/basic@1.2.0 的响应换成同一发布者签过的
// people/basic@0.9.0（含已知漏洞）——签名完全有效。所以还要核对身份。
func TestVerifyManifestRejectsSubstitutedComponent(t *testing.T) {
	k := newTestKey(t)
	sig := validSignature(t, k)

	err := security.VerifyManifest([]byte(sampleManifest), sig, newRing(t, k),
		"people/basic", "9.9.9")
	assert.Equal(t, clierr.CodeSignatureInvalid, codeOf(t, err))

	err = security.VerifyManifest([]byte(sampleManifest), sig, newRing(t, k),
		"other/component", "1.2.0")
	assert.Equal(t, clierr.CodeSignatureInvalid, codeOf(t, err))
}

func TestVerifyManifestAcceptsMatchingComponent(t *testing.T) {
	k := newTestKey(t)

	assert.NoError(t, security.VerifyManifest([]byte(sampleManifest), validSignature(t, k),
		newRing(t, k), "people/basic", "1.2.0"))
}

// TestVerifyManifestErrorNamesComponent 对应 008 §8.7 的失败提示。
func TestVerifyManifestErrorNamesComponent(t *testing.T) {
	k := newTestKey(t)
	sig := validSignature(t, k)
	sig.Value = validSignature(t, newTestKey(t)).Value

	err := security.VerifyManifest([]byte(sampleManifest), sig, newRing(t, k),
		"people/basic", "1.2.0")
	require.Error(t, err)

	var cerr *clierr.Error
	require.ErrorAs(t, err, &cerr)
	rendered := cerr.Format()
	assert.Contains(t, rendered, "people/basic@1.2.0")
	assert.Contains(t, rendered, "发布者")
}
