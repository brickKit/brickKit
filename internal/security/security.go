// Package security 实现组件签名的生成与校验（008 §8、010 §6）。
//
// # 签名的对象是 Manifest，不是镜像
//
// 开发计划 20.1/20.3 的措辞是"对镜像签名"，但 008 §8.4 与 007 §7.4 描述的流程
// 是 **CLI 在 add 时校验 Manifest 的签名**。两者是不同的东西：镜像签名存在
// registry 里，由集群的准入控制器校验，CLI 根本碰不到。本包实现的是设计书那一条
// ——签组件版本的 Manifest，因为那才是 CLI 真正下载、真正能验的东西。
//
// **镜像那一半不靠镜像签名解决，而是靠钉 digest**（008 §8.3.2）：
// `deployment.image` 本来就在 Manifest 里、已被签名覆盖，唯一的缺口是
// "tag 可变"——`brickkit publish` 默认把 tag 解析成 digest **再**签名，
// 缺口就此关上，不需要任何新的密码学机制。
//
// # 验签不需要装 cosign
//
// cosign sign-blob 产出的就是标准的 ECDSA P-256 over SHA-256 签名（ASN.1 DER
// 再 base64），公钥是 PKIX PEM。Go 标准库直接能验，本包因此**零依赖**。
// 这不是为了少一个依赖而已：如果验签也要装 cosign，那"生产环境强制签名"
// （008 §8.5）就意味着每台机器、每个 CI runner 都得装 cosign——大多数团队会
// 因此直接把校验关掉，安全措施变成摆设。
//
//	发布者   brickkit publish --sign  → 需要 cosign（一次性，通常在 CI 里）
//	使用者   brickkit add             → 不需要，标准库就够
//
// 互操作性由 cosign_interop_test.go 用真 cosign 证明，不靠推断。
package security

import (
	"crypto/ecdsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"strings"
	"time"

	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/i18n"
	"github.com/brickkit/brickkit/internal/msgid"
)

// AlgorithmCosign 是目前唯一支持的签名算法标识（008 §8.3）。
const AlgorithmCosign = "cosign"

// Signature 是一条组件版本签名（008 §8.3、007 §7.3）。
//
// PublicKeyRef 只是一个**名字**，不是密钥材料。真正的公钥必须由使用者在
// brickkit.yaml 的 installer.publicKeys 里声明——见 KeyRing 的说明。
type Signature struct {
	Algorithm    string    `json:"algorithm" yaml:"algorithm"`
	PublicKeyRef string    `json:"publicKeyRef" yaml:"publicKeyRef"`
	Value        string    `json:"value" yaml:"value"`
	SignedAt     time.Time `json:"signedAt,omitempty" yaml:"signedAt,omitempty"`
	SignedBy     string    `json:"signedBy,omitempty" yaml:"signedBy,omitempty"`
}

// Empty 表示这个版本没有签名。
//
// 它与"签名无效"是两回事：没签名要不要放行由 installer.requireSignature 决定
// （008 §8.5），签名无效则任何情况下都不能放行。
func (s Signature) Empty() bool {
	return strings.TrimSpace(s.Algorithm) == "" && strings.TrimSpace(s.Value) == ""
}

// Verify 用 ring 中的公钥校验 payload 的签名。
//
// payload 必须是 CanonicalPayload 的输出——直接拿原始字节来验会因为
// JSON/YAML 序列化差异而失败，见 canonical.go 的说明。
func Verify(payload []byte, sig Signature, ring *KeyRing) error {
	if sig.Empty() {
		return invalid(i18n.T(msgid.SecurityUnsigned)).
			WithHint(i18n.T(msgid.SecurityHintRepublishSigned),
				i18n.T(msgid.SecurityHintAllowUnsigned))
	}

	// 不认识的算法只能拒绝。"不认识就放过"等于让攻击者自己挑一个我们不校验的算法。
	if sig.Algorithm != AlgorithmCosign {
		return invalid(i18n.T(msgid.SecurityAlgorithmUnsupported)).
			WithDetail(i18n.T(msgid.SecurityLabelAlgorithm), sig.Algorithm).
			WithDetail(i18n.T(msgid.SecurityLabelSupported), AlgorithmCosign).
			WithHint(i18n.T(msgid.SecurityHintUpgradeOrCosign))
	}

	key, err := lookupKey(sig.PublicKeyRef, ring)
	if err != nil {
		return err
	}

	der, err := base64.StdEncoding.DecodeString(strings.TrimSpace(sig.Value))
	if err != nil {
		return invalid(i18n.T(msgid.SecuritySignatureMalformed)).
			WithDetail(i18n.T(msgid.SecurityLabelPublicKey), sig.PublicKeyRef).
			WithDetail(i18n.T(msgid.LabelReason), i18n.T(msgid.SecurityNotBase64Detail)).
			WithHint(i18n.T(msgid.SecurityHintContactPublisher)).
			WithCause(err)
	}

	digest := sha256.Sum256(payload)
	if !ecdsa.VerifyASN1(key, digest[:], der) {
		return invalid(i18n.T(msgid.SecuritySignatureInvalid)).
			WithDetail(i18n.T(msgid.SecurityLabelPublicKey), sig.PublicKeyRef).
			WithDetail(i18n.T(msgid.LabelReason), i18n.T(msgid.SecurityMismatchDetail)).
			WithHint(i18n.T(msgid.SecurityHintContactPublisher),
				i18n.T(msgid.SecurityHintCheckRefIsPublisher))
	}
	return nil
}

// lookupKey 在钥匙串里找公钥，并在找不到时把"该去哪儿配"讲清楚。
func lookupKey(ref string, ring *KeyRing) (*ecdsa.PublicKey, error) {
	if ring == nil || ring.Empty() {
		return nil, invalid(i18n.T(msgid.SecurityNoTrustedKeys)).
			WithDetail(i18n.T(msgid.SecurityLabelSignedByKey), ref).
			WithHint(
				i18n.T(msgid.SecurityHintDeclarePublisherKey),
				i18n.T(msgid.SecurityHintDisableVerification),
			).
			WithTip(i18n.T(msgid.SecurityTipKeyMustBeYours))
	}

	key, ok := ring.Get(ref)
	if !ok {
		return nil, invalid(i18n.T(msgid.SecurityKeyNotTrusted)).
			WithDetail(i18n.T(msgid.SecurityLabelSignedByKey), ref).
			WithDetail(i18n.T(msgid.SecurityLabelTrustedKeys), strings.Join(ring.Refs(), i18n.T(msgid.ListSeparator))).
			WithHint(
				i18n.T(msgid.SecurityHintFindPublisherKey),
				i18n.T(msgid.SecurityHintRefMustMatch),
			)
	}
	return key, nil
}

// VerifyManifest 校验一份 Manifest 的签名，并核对它就是要装的那个组件版本。
//
// 只验签名是不够的：签名能证明"这份 Manifest 是发布者签的"，但证明不了
// "它就是我要装的那个"。被攻破的市场可以把 people/basic@1.2.0 的响应换成同一
// 发布者签过的 people/basic@0.9.0（含已知漏洞）——签名完全有效，降级攻击成立。
// 组件 ID 与版本本身就在签名覆盖的内容里，核对一下就能堵住。
func VerifyManifest(raw []byte, sig Signature, ring *KeyRing, componentID, version string) error {
	payload, err := CanonicalPayload(raw)
	if err != nil {
		return err
	}

	if err := Verify(payload, sig, ring); err != nil {
		return withComponent(err, componentID, version)
	}

	var doc struct {
		Metadata struct {
			ID      string `json:"id"`
			Version string `json:"version"`
		} `json:"metadata"`
	}
	if err := json.Unmarshal(payload, &doc); err != nil {
		return withComponent(invalid(i18n.T(msgid.SecuritySignedManifestUnparseable)).WithCause(err),
			componentID, version)
	}

	if doc.Metadata.ID != componentID || doc.Metadata.Version != version {
		return withComponent(
			invalid(i18n.T(msgid.SecurityWrongVersionSigned)).
				WithDetailf(i18n.T(msgid.SecurityLabelSignatureCovers), "%s@%s", doc.Metadata.ID, doc.Metadata.Version).
				WithHint(i18n.T(msgid.SecurityHintMarketMismatch),
					i18n.T(msgid.SecurityHintTryOtherSource)),
			componentID, version)
	}
	return nil
}

// withComponent 给错误补上"是哪个组件"，并附上 008 §8.7 的处置建议。
func withComponent(err error, componentID, version string) error {
	cerr := clierr.As(err)
	if cerr == nil {
		return err
	}
	if componentID == "" {
		return cerr
	}

	// 组件放在第一条：使用者最先要知道的是"哪个组件装不上"
	details := append([]clierr.Detail{{Key: i18n.T(msgid.LabelComponent), Value: componentID + "@" + version}}, cerr.Details...)
	cerr.Details = details

	// 各处的错误已经带了自己的建议，其中好几条本来就是"联系发布者重新签名"。
	// 无条件再追加一遍，渲染出来就是同一句话出现两次——看着像程序出了毛病。
	contact := i18n.T(msgid.SecurityHintContactPublisher)
	for _, hint := range cerr.Hints {
		if hint == contact {
			return cerr
		}
	}
	return cerr.WithHint(contact)
}

func invalid(message string) *clierr.Error {
	return clierr.New(clierr.CodeSignatureInvalid, message)
}
