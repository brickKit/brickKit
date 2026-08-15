// Package security 实现组件签名的生成与校验（008 §8、010 §6）。
//
// # 签名的对象是 Manifest，不是镜像
//
// 开发计划 20.1/20.3 的措辞是"对镜像签名"，但 008 §8.4 与 007 §7.4 描述的流程
// 是 **CLI 在 add 时校验 Manifest 的签名**。两者是不同的东西：镜像签名存在
// registry 里，由集群的准入控制器校验，CLI 根本碰不到。本包实现的是设计书那一条
// ——签组件版本的 Manifest，因为那才是 CLI 真正下载、真正能验的东西。
// 镜像签名另行登记（见《开发进度》延后清单）。
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
		return invalid("错误：组件版本没有签名").
			WithHint("发布者需要执行 brickkit publish --sign 重新发布",
				"确认无签名也要安装时，在 brickkit.yaml 设置 installer.requireSignature: false")
	}

	// 不认识的算法只能拒绝。"不认识就放过"等于让攻击者自己挑一个我们不校验的算法。
	if sig.Algorithm != AlgorithmCosign {
		return invalid("错误：不支持的签名算法").
			WithDetail("算法", sig.Algorithm).
			WithDetail("支持", AlgorithmCosign).
			WithHint("升级 brickkit CLI，或让发布者改用 cosign 签名")
	}

	key, err := lookupKey(sig.PublicKeyRef, ring)
	if err != nil {
		return err
	}

	der, err := base64.StdEncoding.DecodeString(strings.TrimSpace(sig.Value))
	if err != nil {
		return invalid("错误：签名格式不正确").
			WithDetail("公钥", sig.PublicKeyRef).
			WithDetail("原因", "签名值不是合法的 base64").
			WithHint("联系组件发布者重新签名").
			WithCause(err)
	}

	digest := sha256.Sum256(payload)
	if !ecdsa.VerifyASN1(key, digest[:], der) {
		return invalid("错误：签名校验不通过").
			WithDetail("公钥", sig.PublicKeyRef).
			WithDetail("原因", "内容与签名不匹配，可能已被篡改，或签名不是该公钥所签").
			WithHint("联系组件发布者重新签名",
				"确认 installer.publicKeys 中该 ref 指向的确实是发布者的公钥")
	}
	return nil
}

// lookupKey 在钥匙串里找公钥，并在找不到时把"该去哪儿配"讲清楚。
func lookupKey(ref string, ring *KeyRing) (*ecdsa.PublicKey, error) {
	if ring == nil || ring.Empty() {
		return nil, invalid("错误：项目没有声明任何可信公钥，无法校验签名").
			WithDetail("签名声明的公钥", ref).
			WithHint(
				"在 brickkit.yaml 的 installer.publicKeys 下声明发布者的公钥",
				"或设置 installer.requireSignature: false 关闭校验（仅限本地开发）",
			).
			WithTip("公钥必须由你自己配置，不能跟着签名一起从市场取——" +
				"否则市场被攻破时，攻击者把组件和公钥一起换掉，验签照样通过。")
	}

	key, ok := ring.Get(ref)
	if !ok {
		return nil, invalid("错误：签名使用的公钥不在项目的信任列表里").
			WithDetail("签名声明的公钥", ref).
			WithDetail("项目信任的公钥", strings.Join(ring.Refs(), "、")).
			WithHint(
				"确认这个组件的发布者是谁，拿到其公钥后加进 brickkit.yaml 的 installer.publicKeys",
				"ref 必须与签名里的完全一致（含 keys/ 前缀）",
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
		return withComponent(invalid("错误：签名覆盖的 Manifest 无法解析").WithCause(err),
			componentID, version)
	}

	if doc.Metadata.ID != componentID || doc.Metadata.Version != version {
		return withComponent(
			invalid("错误：签名有效，但签的不是这个组件版本").
				WithDetailf("签名覆盖的是", "%s@%s", doc.Metadata.ID, doc.Metadata.Version).
				WithHint("市场返回的内容与请求不一致，可能是市场配置错误或响应被替换",
					"换一个安装源重试，并联系市场管理员核查"),
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
	details := append([]clierr.Detail{{Key: "组件", Value: componentID + "@" + version}}, cerr.Details...)
	cerr.Details = details

	// 各处的错误已经带了自己的建议，其中好几条本来就是"联系发布者重新签名"。
	// 无条件再追加一遍，渲染出来就是同一句话出现两次——看着像程序出了毛病。
	const contact = "联系组件发布者重新签名"
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
