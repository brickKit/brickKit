package source

import (
	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/security"
)

// SignaturePolicy 是签名校验策略（008 §8.5、003 §3.6）。
//
// # 两条独立的规则，别把它们混成一条
//
//	Require   管的是"**没有**签名允不允许"
//	验签本身  管的是"**坏**签名允不允许"——任何模式下都不允许
//
// 把后者也挂到 Require 上是很自然的误解，但那意味着"本地开发关掉强制校验"
// 顺带把"接受被篡改的组件"也打开了；而开发机上的组件照样连着真实的数据库。
//
// # 钥匙串才是总开关
//
// 没有声明任何可信公钥时，签名校验**整体失效**，Require 也一并不起作用：
//
//	                          Require=true       Require=false
//	Ring 为空（没配公钥）       放行 + 警告         放行（静默）
//	Ring 非空，组件没有签名     阻断（20.4）        放行（20.5）
//	Ring 非空，ref 不在列表里   阻断               放行 + 警告
//	Ring 非空，签名验不过       阻断               阻断
//
// 第一行不是妥协，是唯一说得通的做法：**没有信任锚点就没有"强制"可言**。
// requireSignature 默认为 true，而现存项目都还没配过 publicKeys；此时若因
// "组件没签名"而阻断，所有人的下一次 add 立刻全部失败——却换不来任何安全收益，
// 因为就算组件真带了签名，我们也没有公钥可以拿去验它。
//
// 这一条是真跑出来的：第一版把"没有签名"判在"没有公钥"之前，一次就打挂了
// 11 个既有用例——那正是所有真实项目当天的处境。
//
// Require=true 而没配公钥时必须**出声**：使用者以为自己开着强制校验，
// 实际上一次也没校验过，这个落差不能默不作声。
type SignaturePolicy struct {
	// Require 对应 installer.requireSignature。
	Require bool
	// Ring 是项目信任的发布者公钥（installer.publicKeys）。
	Ring *security.KeyRing
}

// enabled 表示这条策略会不会真的做点什么。
func (p SignaturePolicy) enabled() bool { return p.Require || !p.Ring.Empty() }

// verifyResult 是一次校验的结果。
type verifyResult struct {
	verified bool
	warnings []*clierr.Error
}

// verify 按策略校验一份 Manifest。
//
// raw 必须是**安装源给出的原始字节**，不能是重新序列化过的——规范化由
// security.CanonicalPayload 负责，但它规范化的对象得是原件。
func (p SignaturePolicy) verify(
	raw []byte, sig *security.Signature, componentID, version string,
) (verifyResult, error) {
	ref := componentID + "@" + version

	// 没有信任锚点就无从校验，Require 也无从谈起。这不是"验过了"，只是"没得验"。
	if p.Ring.Empty() {
		if p.Require {
			return verifyResult{warnings: []*clierr.Error{noKeysWarning(ref)}}, nil
		}
		return verifyResult{}, nil
	}

	if sig == nil || sig.Empty() {
		if p.Require {
			return verifyResult{}, unsignedError(ref)
		}
		return verifyResult{}, nil
	}

	if _, known := p.Ring.Get(sig.PublicKeyRef); !known {
		if p.Require {
			// 强制模式下，不认识的发布者就是不能装
			return verifyResult{}, withComponentRef(
				security.Verify(nil, *sig, p.Ring), ref)
		}
		return verifyResult{warnings: []*clierr.Error{unknownSignerWarning(ref, sig.PublicKeyRef, p.Ring)}}, nil
	}

	// ref 认识 → 必须验得过。这一步与 Require 无关。
	if err := security.VerifyManifest(raw, *sig, p.Ring, componentID, version); err != nil {
		return verifyResult{}, err
	}
	return verifyResult{verified: true}, nil
}

// unsignedError 是 20.4 的阻断。
//
// 提示里必须给出关掉的办法：使用者装不上一个没签名的组件时，他既不能替
// 发布者签名，也未必知道 requireSignature 的存在——只说"未签名"等于让他干瞪眼。
func unsignedError(ref string) *clierr.Error {
	return clierr.New(clierr.CodeSignatureInvalid, "错误：组件未签名，安装被阻断").
		WithDetail("组件", ref).
		WithDetail("原因", "项目要求强制签名校验（installer.requireSignature 默认为 true）").
		WithHint(
			"联系组件发布者用 brickkit publish --sign 重新发布",
			"本地开发可在 brickkit.yaml 设置 installer.requireSignature: false 关闭校验",
		)
}

// noKeysWarning 提醒"你以为在强制校验，其实一次也没校验过"。
//
// 只在 requireSignature 为 true 时出现：设成 false 就是明确表示不校验，
// 那种情况下每装一个组件都唠叨一遍纯属噪音。
func noKeysWarning(ref string) *clierr.Error {
	return clierr.Warn(clierr.CodeSignatureInvalid,
		"警告：requireSignature 为 true，但项目没有声明任何可信公钥，签名校验实际未生效").
		WithDetail("组件", ref).
		WithHint(
			"在 brickkit.yaml 的 installer.publicKeys 下声明发布者公钥，校验才会真正开始生效",
			"确实不需要校验时，把 installer.requireSignature 显式设为 false，这条提醒就不再出现",
		).
		WithTip("没有可信公钥就没有可校验的对象——此时的 requireSignature: true " +
			"不是更严格的策略，只是还没配完。")
}

func unknownSignerWarning(ref, keyRef string, ring *security.KeyRing) *clierr.Error {
	w := clierr.Warn(clierr.CodeSignatureInvalid, "警告：签名来自未声明的发布者，未做校验").
		WithDetail("组件", ref).
		WithDetail("签名声明的公钥", keyRef)
	if refs := ring.Refs(); len(refs) > 0 {
		w = w.WithDetail("项目信任的公钥", joinRefs(refs))
	}
	return w.WithHint("确认发布者身份后，把该公钥加进 brickkit.yaml 的 installer.publicKeys")
}

// withComponentRef 给错误补上"是哪个组件"。
func withComponentRef(err error, ref string) error {
	cerr := clierr.As(err)
	if cerr == nil {
		return err
	}
	cerr.Details = append([]clierr.Detail{{Key: "组件", Value: ref}}, cerr.Details...)
	return cerr
}

func joinRefs(refs []string) string {
	out := ""
	for i, r := range refs {
		if i > 0 {
			out += "、"
		}
		out += r
	}
	return out
}
