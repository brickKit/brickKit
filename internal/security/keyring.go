package security

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/x509"
	"encoding/pem"
	"os"
	"path/filepath"
	"sort"

	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/i18n"
	"github.com/brickkit/brickkit/internal/msgid"
)

// KeyRing 是项目信任的公钥集合，来自 brickkit.yaml 的 installer.publicKeys。
//
// # 公钥为什么必须由使用者配置
//
// 008 §8.3 的签名信息里只有 publicKeyRef（一个名字），没有密钥材料——设计书
// 没有明说这个名字该由谁来解析，而这恰恰决定了整套机制有没有意义：
//
//	从市场取公钥   市场自己给自己发证。市场被攻破 → 攻击者把组件和公钥一起换掉
//	               → 验签通过 → 签名等于没有（008 §14.1 恰恰把"市场被攻破"的
//	               应对写成"签名校验"，这条路把它变成了空话）
//	从项目取公钥   信任锚点在使用者手里，跟着 brickkit.yaml 进 Git、有评审记录
//
// 所以本包只接受后者：ref 必须能在 KeyRing 里找到，找不到就是校验失败，
// 绝不回退到"那就从市场拿一个吧"。
type KeyRing struct {
	keys map[string]*ecdsa.PublicKey
}

// NewKeyRing 返回一个空钥匙串。
func NewKeyRing() *KeyRing {
	return &KeyRing{keys: map[string]*ecdsa.PublicKey{}}
}

// Add 以 ref 为名加入一个 PKIX PEM 公钥（cosign generate-key-pair 产出的 cosign.pub）。
func (r *KeyRing) Add(ref string, pemBytes []byte) error {
	key, err := parsePublicKey(ref, pemBytes)
	if err != nil {
		return err
	}
	if r.keys == nil {
		r.keys = map[string]*ecdsa.PublicKey{}
	}
	r.keys[ref] = key
	return nil
}

// Get 按 ref 取公钥。
func (r *KeyRing) Get(ref string) (*ecdsa.PublicKey, bool) {
	if r == nil {
		return nil, false
	}
	key, ok := r.keys[ref]
	return key, ok
}

// Empty 表示项目没有声明任何可信公钥。
func (r *KeyRing) Empty() bool { return r == nil || len(r.keys) == 0 }

// Refs 返回全部 ref（已排序），用于在错误里告诉使用者"项目现在信任的是哪些"。
func (r *KeyRing) Refs() []string {
	if r == nil {
		return nil
	}
	out := make([]string, 0, len(r.keys))
	for ref := range r.keys {
		out = append(out, ref)
	}
	sort.Strings(out)
	return out
}

// LoadKeyRing 按 ref → 公钥文件路径的映射加载钥匙串。
//
// 相对路径相对 baseDir（项目根目录）解析，这样 brickkit.yaml 里写的
// keys/people-basic-release.pub 无论从哪个子目录执行 brickkit 都指同一个文件。
func LoadKeyRing(entries map[string]string, baseDir string) (*KeyRing, error) {
	ring := NewKeyRing()

	// 排序后加载：多把钥匙同时配错时，报错顺序稳定，不会每次跑出来不一样
	refs := make([]string, 0, len(entries))
	for ref := range entries {
		refs = append(refs, ref)
	}
	sort.Strings(refs)

	for _, ref := range refs {
		path := entries[ref]
		if !filepath.IsAbs(path) {
			path = filepath.Join(baseDir, path)
		}

		pemBytes, err := os.ReadFile(path)
		if err != nil {
			return nil, clierr.New(clierr.CodeConfigInvalid, i18n.T(msgid.SecurityReadTrustedKeyFailed)).
				WithDetail(i18n.T(msgid.SecurityLabelKeyRef), ref).
				WithDetail(i18n.T(msgid.LabelPath), path).
				WithHint(i18n.T(msgid.SecurityHintCheckPublicKeysPath),
					i18n.T(msgid.SecurityHintPublicKeyInGit)).
				WithCause(err)
		}
		if err := ring.Add(ref, pemBytes); err != nil {
			return nil, err
		}
	}
	return ring, nil
}

// parsePublicKey 解析一份 PKIX PEM 公钥，并确认它是 cosign 那种 ECDSA P-256。
func parsePublicKey(ref string, pemBytes []byte) (*ecdsa.PublicKey, error) {
	block, _ := pem.Decode(pemBytes)
	if block == nil {
		return nil, badKey(ref, i18n.T(msgid.SecurityKeyNotPEM)).
			WithHint(i18n.T(msgid.SecurityHintPEMShape),
				i18n.T(msgid.SecurityHintUseCosignPub))
	}
	if block.Type != "PUBLIC KEY" {
		// 最常见的是把 cosign.key（私钥）当成公钥配了进来。这必须直说：
		// 私钥进了 Git 是要立刻轮换的事故，不能只报一句"格式不对"。
		return nil, badKey(ref, i18n.T(msgid.SecurityKeyPEMType, block.Type)).
			WithHint(i18n.T(msgid.SecurityHintWantsPublicKey)).
			WithTip(i18n.T(msgid.SecurityTipRotateKey))
	}

	parsed, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return nil, badKey(ref, i18n.T(msgid.SecurityKeyUnparseable)).WithCause(err)
	}
	key, ok := parsed.(*ecdsa.PublicKey)
	if !ok {
		return nil, badKey(ref, i18n.T(msgid.SecurityKeyNotECDSA)).
			WithHint(i18n.T(msgid.SecurityHintUseDefaultKeygen))
	}
	if key.Curve != elliptic.P256() {
		return nil, badKey(ref, i18n.T(msgid.SecurityKeyNotP256)).
			WithHint(i18n.T(msgid.SecurityHintUseDefaultKeygen))
	}
	return key, nil
}

// badKey 报告一把配错了的可信公钥。
//
// 这条**必须给出下一步**：公钥是使用者自己在 installer.publicKeys 里配的，
// 配错了他完全能改。只说"不可用"而不说怎么办，人会以为是组件或市场的问题，
// 而实际上要改的是自己那三行配置（开发计划 33.17）。
func badKey(ref, reason string) *clierr.Error {
	return clierr.New(clierr.CodeConfigInvalid, i18n.T(msgid.SecurityKeyUnusable)).
		WithDetail(i18n.T(msgid.SecurityLabelKeyRef), ref).
		WithDetail(i18n.T(msgid.LabelReason), reason).
		WithHint(
			i18n.T(msgid.SecurityHintCheckPublicKeyEntry),
			i18n.T(msgid.SecurityHintMustBePKIX),
			i18n.T(msgid.SecurityHintNotPrivateKey),
		)
}
