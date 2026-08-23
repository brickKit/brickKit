package source

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/config"
	"github.com/brickkit/brickkit/internal/security"
)

// 本文件覆盖开发计划 20.2–20.5 在下载链路上的行为：
//
//	20.2  有效签名 → 通过
//	20.3  内容被篡改 → 报错
//	20.4  requireSignature: true 且组件未签名 → 阻断
//	20.5  requireSignature: false 且组件未签名 → 可安装
//
// 校验放在 internal/source 而不是 add 命令里：add / up / sync 都会取 Manifest，
// 放在命令层就得在三个地方各写一遍，漏一个就是一条不验签的通路。

// ============================================================
// 签名辅助
// ============================================================

type signingKey struct {
	priv   *ecdsa.PrivateKey
	pubPEM []byte
}

func newSigningKey(t *testing.T) *signingKey {
	t.Helper()

	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	der, err := x509.MarshalPKIXPublicKey(&priv.PublicKey)
	require.NoError(t, err)

	return &signingKey{priv: priv, pubPEM: pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der})}
}

const testKeyRef = "keys/people-basic-release.pub"

// signSpec 对某个组件规格的规范化 Manifest 载荷签名。
func (k *signingKey) signSpec(t *testing.T, spec componentSpec) *security.Signature {
	t.Helper()

	payload, err := security.CanonicalPayload([]byte(spec.yamlText()))
	require.NoError(t, err)
	digest := sha256.Sum256(payload)
	der, err := ecdsa.SignASN1(rand.Reader, k.priv, digest[:])
	require.NoError(t, err)

	return &security.Signature{
		Algorithm:    security.AlgorithmCosign,
		PublicKeyRef: testKeyRef,
		Value:        base64.StdEncoding.EncodeToString(der),
		SignedBy:     "release-bot@brickkit.io",
	}
}

func (k *signingKey) ring(t *testing.T) *security.KeyRing {
	t.Helper()
	return k.ringAs(t, testKeyRef)
}

// ringAs 以指定 ref 声明这把公钥。
//
// 用不同的 ref 才能造出"发布者不在信任列表里"的场景：ref 相同而密钥不同是
// "验不过"，ref 根本不认识才是"没法验"——两者的处置完全不同（见 verify.go）。
func (k *signingKey) ringAs(t *testing.T, ref string) *security.KeyRing {
	t.Helper()

	ring := security.NewKeyRing()
	require.NoError(t, ring.Add(ref, k.pubPEM))
	return ring
}

// signedMarket 建一个提供该组件与签名的市场 Mock。
func signedMarket(t *testing.T, spec componentSpec, sig *security.Signature) *marketMock {
	t.Helper()

	m := newMarketMock(t, spec)
	m.signature = sig
	return m
}

// clientFor 构造一个只有该市场作安装源、并带上签名策略的客户端。
func clientFor(t *testing.T, m *marketMock, policy SignaturePolicy) (*Client, config.Layout) {
	t.Helper()

	layout := newProject(t)
	cfg := cfgWithSources(config.Source{
		ID: "brickkit-market", Type: config.SourceTypeMarket, URL: m.URL(),
	})
	return newClient(t, layout, cfg, Options{Signature: policy}), layout
}

func signedComponent() componentSpec {
	return componentSpec{ID: "people/basic", Version: "1.2.0"}
}

// ============================================================
// 20.2 签名校验通过
// ============================================================

func TestManifestAcceptsValidSignature(t *testing.T) {
	key := newSigningKey(t)
	spec := signedComponent()
	m := signedMarket(t, spec, key.signSpec(t, spec))

	c, _ := clientFor(t, m, SignaturePolicy{Require: true, Ring: key.ring(t)})
	got, err := c.Manifest(context.Background(), "people/basic", "1.2.0")
	require.NoError(t, err)

	assert.Equal(t, "people/basic", got.Manifest.Metadata.ID)
	assert.True(t, got.Verified, "验过的要标出来，brickkit status 才能显示「签名：✅ 已校验」")
	require.NotNil(t, got.Signature)
	assert.Equal(t, testKeyRef, got.Signature.PublicKeyRef)
}

// ============================================================
// 20.3 篡改后校验失败
// ============================================================

// TestManifestRejectsTamperedContent 模拟"市场被攻破，Manifest 被改了"。
//
// 签名是照原始规格签的，市场发出去的却是改过镜像的版本——这正是 008 §14.1
// 里"组件篡改"与"市场被攻破"两条威胁的实际形态。
func TestManifestRejectsTamperedContent(t *testing.T) {
	key := newSigningKey(t)
	original := signedComponent()
	sig := key.signSpec(t, original)

	tampered := original
	tampered.Image = "evil.example.com/people-basic:1.2.0"
	m := signedMarket(t, tampered, sig)

	c, _ := clientFor(t, m, SignaturePolicy{Require: true, Ring: key.ring(t)})
	_, err := c.Manifest(context.Background(), "people/basic", "1.2.0")

	require.Error(t, err)
	assert.Equal(t, clierr.CodeSignatureInvalid, clierr.As(err).Code)
}

// TestManifestRejectsUntrustedSigner：签名有效，但签的人不在信任列表里。
func TestManifestRejectsUntrustedSigner(t *testing.T) {
	trusted := newSigningKey(t)
	attacker := newSigningKey(t)
	spec := signedComponent()
	m := signedMarket(t, spec, attacker.signSpec(t, spec))

	c, _ := clientFor(t, m, SignaturePolicy{Require: true, Ring: trusted.ring(t)})
	_, err := c.Manifest(context.Background(), "people/basic", "1.2.0")

	require.Error(t, err)
	assert.Equal(t, clierr.CodeSignatureInvalid, clierr.As(err).Code)
}

// TestManifestRejectsVersionSubstitution 挡降级攻击。
//
// 市场返回的是同一个发布者**真的签过**的另一个版本（含已知漏洞的 0.9.0）。
// 签名本身完全有效，只有核对 metadata 才拦得住。
func TestManifestRejectsVersionSubstitution(t *testing.T) {
	key := newSigningKey(t)
	old := componentSpec{ID: "people/basic", Version: "0.9.0"}
	oldSig := key.signSpec(t, old)

	// 市场把 1.2.0 的请求应答成 0.9.0 的内容与签名
	m := newMarketMock(t)
	m.components["people/basic@1.2.0"] = old
	m.signature = oldSig

	c, _ := clientFor(t, m, SignaturePolicy{Require: true, Ring: key.ring(t)})
	_, err := c.Manifest(context.Background(), "people/basic", "1.2.0")

	require.Error(t, err, "签名有效不等于内容就是我要的那个版本")
}

// ============================================================
// 20.4 / 20.5 requireSignature 的开与关
// ============================================================

// TestRequireSignatureBlocksUnsigned 是 20.4。
func TestRequireSignatureBlocksUnsigned(t *testing.T) {
	key := newSigningKey(t)
	m := newMarketMock(t, signedComponent()) // 市场不返回签名

	c, _ := clientFor(t, m, SignaturePolicy{Require: true, Ring: key.ring(t)})
	_, err := c.Manifest(context.Background(), "people/basic", "1.2.0")

	require.Error(t, err)
	cerr := clierr.As(err)
	assert.Equal(t, clierr.CodeSignatureInvalid, cerr.Code)

	rendered := cerr.Format()
	assert.Contains(t, rendered, "people/basic@1.2.0", "要指出是哪个组件")
	assert.Contains(t, rendered, "requireSignature",
		"要告诉使用者本地开发可以怎么关掉，否则他只能干瞪眼")
}

// TestRequireSignatureFalseAllowsUnsigned 是 20.5。
func TestRequireSignatureFalseAllowsUnsigned(t *testing.T) {
	m := newMarketMock(t, signedComponent())

	c, _ := clientFor(t, m, SignaturePolicy{Require: false})
	got, err := c.Manifest(context.Background(), "people/basic", "1.2.0")

	require.NoError(t, err)
	assert.Equal(t, "people/basic", got.Manifest.Metadata.ID)
	assert.False(t, got.Verified, "没验就是没验，不能因为放行了就说验过")
}

// TestRequireSignatureFalseStillRejectsTampered 是一条容易被想当然弄反的规则。
//
// requireSignature 管的是"**没有**签名允不允许"，不是"**坏**签名允不允许"。
// 一个验不过的签名说明内容被改过、或来源根本不对——那在任何模式下都不该放行。
// 若关掉 requireSignature 就连坏签名一起放过，本地开发环境就成了投毒的入口，
// 而开发机上的组件照样连着真实的数据库。
func TestRequireSignatureFalseStillRejectsTampered(t *testing.T) {
	key := newSigningKey(t)
	original := signedComponent()
	sig := key.signSpec(t, original)

	tampered := original
	tampered.Image = "evil.example.com/people-basic:1.2.0"
	m := signedMarket(t, tampered, sig)

	c, _ := clientFor(t, m, SignaturePolicy{Require: false, Ring: key.ring(t)})
	_, err := c.Manifest(context.Background(), "people/basic", "1.2.0")

	require.Error(t, err, "关掉强制校验，也不等于接受一个对不上的签名")
	assert.Equal(t, clierr.CodeSignatureInvalid, clierr.As(err).Code)
}

// TestUnknownSignerPassesWhenNotRequired：不强制时，不认识的发布者放行。
//
// 与上一条的区别在于"验不过"和"没法验"是两回事：ref 不在信任列表里，我们
// 并不知道内容有没有问题，只是无从判断。不强制的语气下，这该是放行 + 提示，
// 而不是把一个使用者从来没配过公钥的项目直接卡死。
func TestUnknownSignerPassesWhenNotRequired(t *testing.T) {
	signer := newSigningKey(t)
	other := newSigningKey(t)
	spec := signedComponent()
	m := signedMarket(t, spec, signer.signSpec(t, spec))

	// 项目信任的是另一个 ref：签名声明的那个 ref 我们根本不认识
	c, _ := clientFor(t, m, SignaturePolicy{
		Require: false, Ring: other.ringAs(t, "keys/somebody-else.pub"),
	})
	got, err := c.Manifest(context.Background(), "people/basic", "1.2.0")

	require.NoError(t, err)
	assert.False(t, got.Verified)
	require.NotEmpty(t, got.Warnings, "放行了也要说一声：这个发布者你没声明过")
	assert.Contains(t, got.Warnings[0].Format(), "installer.publicKeys")
}

// TestNoKeysConfiguredSkipsVerification：没配任何公钥时完全不校验。
//
// 没有信任锚点就无从校验——就算组件真带了签名，我们也没有公钥去验它。
// 但使用者以为自己开着强制校验，这个落差必须说出来。
func TestNoKeysConfiguredSkipsVerification(t *testing.T) {
	key := newSigningKey(t)
	spec := signedComponent()
	m := signedMarket(t, spec, key.signSpec(t, spec))

	c, _ := clientFor(t, m, SignaturePolicy{Require: true}) // Ring 为空
	got, err := c.Manifest(context.Background(), "people/basic", "1.2.0")

	require.NoError(t, err)
	assert.False(t, got.Verified)
	require.NotEmpty(t, got.Warnings, "不能一声不吭地跳过校验")
	assert.Contains(t, got.Warnings[0].Format(), "installer.publicKeys")
}

// TestNoKeysConfiguredDoesNotBlockUnsignedComponents 是这一整套策略里
// 最要紧的一条，也是整个 Step 20 最容易做错的地方。
//
// requireSignature **默认为 true**，而现存的每一个项目都还没配过 publicKeys。
// 若在这种处境下因为"组件没有签名"就阻断，所有人的下一次 brickkit add 立刻
// 全部失败——换来的安全收益却是零：就算组件带了签名，没有公钥也验不了。
//
// 这条测试是补写的：实现的第一版把"没有签名"判在"没有公钥"之前，
// 一次打挂了 11 个既有用例，而那 11 个用例描述的正是所有真实项目当天的状态。
func TestNoKeysConfiguredDoesNotBlockUnsignedComponents(t *testing.T) {
	m := newMarketMock(t, signedComponent()) // 未签名的组件

	c, _ := clientFor(t, m, SignaturePolicy{Require: true}) // Ring 为空
	got, err := c.Manifest(context.Background(), "people/basic", "1.2.0")

	require.NoError(t, err, "没有可信公钥时，requireSignature 不该把人挡在门外")
	assert.Equal(t, "people/basic", got.Manifest.Metadata.ID)
	assert.False(t, got.Verified)
	require.NotEmpty(t, got.Warnings, "但要提醒：你以为在强制校验，其实没有")
	assert.Contains(t, got.Warnings[0].Format(), "requireSignature")
}

// TestNoKeysAndNotRequiredIsSilent：明确关掉校验的项目不该被反复唠叨。
func TestNoKeysAndNotRequiredIsSilent(t *testing.T) {
	m := newMarketMock(t, signedComponent())

	c, _ := clientFor(t, m, SignaturePolicy{Require: false})
	got, err := c.Manifest(context.Background(), "people/basic", "1.2.0")

	require.NoError(t, err)
	assert.Empty(t, got.Warnings, "设成 false 就是明确表示不校验，不必每装一个都提醒一次")
}

// ============================================================
// 缓存不能成为绕过校验的通路
// ============================================================

// TestCachedManifestIsVerifiedToo 堵住一条很隐蔽的绕过。
//
// Manifest 命中 .brickkit/manifests/ 缓存时会直接返回。如果缓存不参与校验，
// 那么"先在不校验的情况下 add 一次、再打开 requireSignature"就能让一份从未
// 验过的 Manifest 一直被用下去——而且看起来一切正常。
func TestCachedManifestIsVerifiedToo(t *testing.T) {
	key := newSigningKey(t)
	spec := signedComponent()
	m := signedMarket(t, spec, key.signSpec(t, spec))

	// 第一次：不校验，写下缓存
	c1, layout := clientFor(t, m, SignaturePolicy{})
	_, err := c1.Manifest(context.Background(), "people/basic", "1.2.0")
	require.NoError(t, err)
	require.FileExists(t, c1.ManifestCachePath("people/basic", "1.2.0"))

	// 第二次：同一个项目目录，打开强制校验
	cfg := cfgWithSources(config.Source{
		ID: "brickkit-market", Type: config.SourceTypeMarket, URL: m.URL(),
	})
	c2 := newClient(t, layout, cfg, Options{
		Signature: SignaturePolicy{Require: true, Ring: key.ring(t)},
	})
	got, err := c2.Manifest(context.Background(), "people/basic", "1.2.0")
	require.NoError(t, err, "缓存里的这份是签过名的，应当验过并放行")
	assert.True(t, got.Verified, "命中缓存也必须真的验过，而不是直接放行")
}

// TestCachedManifestWithoutSignatureIsRefetched：缓存里没有签名时不能就此放行。
func TestCachedManifestWithoutSignatureIsRefetched(t *testing.T) {
	key := newSigningKey(t)
	spec := signedComponent()

	// 先让市场不给签名，写下一份"无签名"的缓存
	m := newMarketMock(t, spec)
	c1, layout := clientFor(t, m, SignaturePolicy{})
	_, err := c1.Manifest(context.Background(), "people/basic", "1.2.0")
	require.NoError(t, err)

	// 市场后来补上了签名；打开强制校验后应当重新去取，而不是用旧缓存
	m.signature = key.signSpec(t, spec)
	cfg := cfgWithSources(config.Source{
		ID: "brickkit-market", Type: config.SourceTypeMarket, URL: m.URL(),
	})
	c2 := newClient(t, layout, cfg, Options{
		Signature: SignaturePolicy{Require: true, Ring: key.ring(t)},
	})
	got, err := c2.Manifest(context.Background(), "people/basic", "1.2.0")
	require.NoError(t, err)
	assert.True(t, got.Verified)
	assert.False(t, got.FromCache, "缓存里没有签名就得重新拉，不能拿旧的顶数")
}

// TestRequireSignatureRejectsUnknownKeyRef 补上一条安全分支：
// 签名声明的公钥 ref **根本不在**信任列表里，且 requireSignature: true。
//
// 它与 TestManifestRejectsUntrustedSigner 不是一回事，两者差在哪见 ringAs 的注释：
// 那条是"ref 认识、但密钥对不上"（验不过），这条是"ref 压根不认识"（没法验）。
// 走的是 verify.go 里完全不同的分支，而这一条此前没有任何用例——
// 强制模式下"不认识的发布者不能装"是这套机制的核心承诺，不能只靠读代码相信它。
func TestRequireSignatureRejectsUnknownKeyRef(t *testing.T) {
	key := newSigningKey(t)
	spec := signedComponent()
	sig := key.signSpec(t, spec)
	sig.PublicKeyRef = "keys/somebody-else.pub" // 信任列表里没有这个 ref
	m := signedMarket(t, spec, sig)

	// 信任列表里有一把钥匙，但登记在另一个 ref 下
	c, _ := clientFor(t, m, SignaturePolicy{Require: true, Ring: key.ringAs(t, testKeyRef)})
	_, err := c.Manifest(context.Background(), "people/basic", "1.2.0")

	require.Error(t, err)
	text := clierr.As(err).Format()
	assert.Contains(t, text, "people/basic@1.2.0", "错误里要点名是哪个组件")
	assert.Contains(t, text, "keys/somebody-else.pub", "要说清楚是哪个 ref 不认识")
}

// 老缓存里没有签名信封（早于签名功能写下的那些）。
//
// 这时"这份缓存是哪种安装源给的"无从得知，而那正是要不要校验的判据。
// 策略生效时只能当缓存未命中重新拉；策略不生效时照常用缓存——
// 否则每个还没用上签名的项目都会平白多一次网络往返。
func TestCacheWithoutSignatureEnvelope(t *testing.T) {
	key := newSigningKey(t)
	spec := signedComponent()
	m := signedMarket(t, spec, key.signSpec(t, spec))

	// 先正常取一次，写下 Manifest 缓存与签名信封
	c1, layout := clientFor(t, m, SignaturePolicy{})
	_, err := c1.Manifest(context.Background(), "people/basic", "1.2.0")
	require.NoError(t, err)

	// 抹掉信封，只留 Manifest 缓存——模拟老版本 CLI 留下的缓存
	require.NoError(t, os.Remove(c1.SignatureCachePath("people/basic", "1.2.0")))
	require.FileExists(t, c1.ManifestCachePath("people/basic", "1.2.0"))

	cfg := cfgWithSources(config.Source{
		ID: "brickkit-market", Type: config.SourceTypeMarket, URL: m.URL(),
	})

	// 策略生效 → 不敢用这份缓存，重新拉
	strict := newClient(t, layout, cfg, Options{
		Signature: SignaturePolicy{Require: true, Ring: key.ring(t)},
	})
	got, err := strict.Manifest(context.Background(), "people/basic", "1.2.0")
	require.NoError(t, err)
	assert.False(t, got.FromCache, "信封不在又要校验，只能重新拉")
	assert.True(t, got.Verified)

	// 再抹一次信封，换成不校验的策略 → 缓存照用
	require.NoError(t, os.Remove(strict.SignatureCachePath("people/basic", "1.2.0")))
	relaxed := newClient(t, layout, cfg, Options{})
	got, err = relaxed.Manifest(context.Background(), "people/basic", "1.2.0")
	require.NoError(t, err)
	assert.True(t, got.FromCache, "不校验的项目不该为此平白多一次网络往返")
}

// ============================================================
// 本地安装源不受 requireSignature 约束
// ============================================================

// TestLocalSourceIsNotSubjectToSignature 记录一条边界。
//
// 008 §8.4 说的是"从**市场**获取 Manifest 和签名"。本地安装源指向的是使用者
// 自己硬盘上的目录、由他自己在编辑——那里根本没有"发布者"这个角色，也就无所谓
// 签名。若一并强制，打开 requireSignature 会让所有用本地源开发的项目当场瘫痪，
// 结果只会是大家把它关掉——那才是真正的安全损失。
func TestLocalSourceIsNotSubjectToSignature(t *testing.T) {
	spec := signedComponent()
	dir := t.TempDir()
	writeComponent(t, dir, spec)

	layout := newProject(t)
	cfg := cfgWithSources(config.Source{
		ID: "local", Type: config.SourceTypeLocal, Path: dir,
	})
	c := newClient(t, layout, cfg, Options{
		Signature: SignaturePolicy{Require: true, Ring: newSigningKey(t).ring(t)},
	})

	got, err := c.Manifest(context.Background(), "people/basic", "1.2.0")
	require.NoError(t, err, "本地源的组件不该被签名策略挡住")
	assert.Equal(t, "people/basic", got.Manifest.Metadata.ID)
}
