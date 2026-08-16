package cli

// 本文件是 P29「镜像签名」的业务行为测试。
//
// # 缺口比"没做镜像签名"要小
//
// Manifest 签名覆盖的是**规范化后的整个 Manifest**，而 `deployment.image`
// 就在里面。所以"部署哪个镜像"已经被签名保护了。剩下的缺口只有一个：
//
//	签名保证了镜像**字符串**没被改，但保证不了那个字符串**还指向同样的字节**。
//
// tag 是可变的：发布者签名时是 `repo:1.0.0`，攻击者拿到 registry 权限后
// 用同一个 tag 重新 push，签名依然有效、跑起来的却是另一个镜像。
//
// 把 tag 换成 digest，这个缺口就自己关上了——digest 按定义不可变，
// 而它已经在签名覆盖范围内。**不需要任何新的密码学机制。**
//
// 顺序因此是关键：**先钉 digest，再签名**。反过来的话签的是旧内容，
// 上传的 Manifest 与签名对不上，消费方一律验签失败。

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"os"
	"path/filepath"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/security"
)

// 一个合法的多架构索引 digest。
const testDigest = "sha256:d9e853e87e55526f6b2917df91a2115c36dd7c696a35be12163d44e6e2a4b6bc"

// pinningProject 造一个装好登录态的市场项目。
func pinningProject(t *testing.T, m *fakeMarket) *projectFixture {
	t.Helper()

	f := newMarketProject(t, m, "")
	loginTo(t, f, m)
	return f
}

// publishWith 用可控的 digest 解析器跑一次 publish。
func publishWith(
	t *testing.T, f *projectFixture,
	resolve func(context.Context, string) (string, error), args ...string,
) result {
	t.Helper()
	return runWith(t, func(o *Options) { o.ResolveDigest = resolve }, f.Dir, args...)
}

// okResolver 永远返回同一个 digest，并记下被问过哪些镜像。
func okResolver(seen *[]string) func(context.Context, string) (string, error) {
	return func(_ context.Context, image string) (string, error) {
		*seen = append(*seen, image)
		return testDigest, nil
	}
}

// uploadedImage 取出上传到市场的那份 Manifest 里的 deployment.image。
func uploadedImage(t *testing.T, m *fakeMarket) string {
	t.Helper()

	_, raw := publishedSignature(t, m)
	var doc struct {
		Deployment struct {
			Image string `json:"image"`
		} `json:"deployment"`
	}
	require.NoError(t, json.Unmarshal(raw, &doc))
	return doc.Deployment.Image
}

// ============================================================
// 核心：发布时把 tag 钉成 digest
// ============================================================

func TestPublishPinsImageDigest(t *testing.T) {
	m := newFakeMarket(t)
	m.artifacts = []map[string]any{artifactEntry("art-0", "api-docs", "openapi", "openapi.json")}
	var asked []string
	f := pinningProject(t, m)
	root := writeComponentDir(t, f.Dir, publishable())

	r := publishWith(t, f, okResolver(&asked), "publish", "--path", root)

	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)
	assert.Equal(t, []string{"registry.example.com/people-basic:1.2.0"}, asked,
		"P29：应该拿着 Manifest 里的 tag 去问 registry")
	assert.Contains(t, uploadedImage(t, m), "@"+testDigest,
		"P29：上传的 Manifest 里应该是 digest，而不是 tag")
	assert.NotContains(t, uploadedImage(t, m), ":1.2.0",
		"P29：tag 该被换掉，不是附加上去")
}

// **签名必须覆盖钉过 digest 的那份内容。**
//
// 这是整件事里唯一会静默出错的地方：顺序反了的话签的是旧 Manifest，
// 上传的却是新的——消费方 `add` 时一律验签失败，而发布者这边一切正常。
func TestPublishSignsAfterPinning(t *testing.T) {
	requireCosign(t)
	t.Setenv("COSIGN_PASSWORD", "")
	m := newFakeMarket(t)
	m.artifacts = []map[string]any{artifactEntry("art-0", "api-docs", "openapi", "openapi.json")}
	var asked []string
	f := pinningProject(t, m)
	root := writeComponentDir(t, f.Dir, publishable())
	keyPath, pubPath := generateKeyPair(t, filepath.Join(f.Dir, "keys"), "people-basic-release")

	r := publishWith(t, f, okResolver(&asked), "publish", "--path", root, "--sign", "--key", keyPath)
	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)

	sig, raw := publishedSignature(t, m)
	pubPEM, err := os.ReadFile(pubPath)
	require.NoError(t, err)
	ring := security.NewKeyRing()
	require.NoError(t, ring.Add(sig.PublicKeyRef, pubPEM))

	assert.NoError(t, security.VerifyManifest(raw, sig, ring, "people/basic", "1.2.0"),
		"P29：签名必须对上传的那份（已钉 digest 的）Manifest 有效")
	assert.Contains(t, string(raw), testDigest, "上传的确实是钉过的版本")
}

// 已经是 digest 就不该再去问 registry。
func TestPublishKeepsExistingDigest(t *testing.T) {
	m := newFakeMarket(t)
	m.artifacts = []map[string]any{artifactEntry("art-0", "api-docs", "openapi", "openapi.json")}
	var asked []string
	f := pinningProject(t, m)

	c := publishable()
	c.Image = "registry.example.com/people-basic@" + testDigest
	root := writeComponentDir(t, f.Dir, c)

	r := publishWith(t, f, okResolver(&asked), "publish", "--path", root)

	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)
	assert.Empty(t, asked, "P29：已经是 digest 了，没必要再问 registry")
	assert.Contains(t, uploadedImage(t, m), testDigest)
}

// ============================================================
// 解析失败：阻断，并说清两种原因
// ============================================================

// 解析不出 digest 时**阻断发布**。
//
// 放过去的话，市场里会留下一个消费方根本拉不到镜像的版本——
// 而版本号一旦建出来就不可回收（市场侧 18.14）。
// 两种原因要都说出来：镜像没推上去，和 registry 连不上——
// 它们该做的下一步完全不同。
func TestPublishBlocksWhenDigestUnresolvable(t *testing.T) {
	m := newFakeMarket(t)
	f := pinningProject(t, m)
	failing := func(context.Context, string) (string, error) {
		return "", errors.New("pull access denied, repository does not exist")
	}
	root := writeComponentDir(t, f.Dir, publishable())

	r := publishWith(t, f, failing, "publish", "--path", root)

	require.Equal(t, clierr.ExitError, r.code)
	assert.Contains(t, r.stderr, "推送", "要提到镜像可能没推上去：%s", r.stderr)
	assert.Contains(t, r.stderr, "--no-pin-digest", "要给出跳过的办法：%s", r.stderr)
	for _, req := range m.requests() {
		assert.NotContains(t, req, "/components/",
			"P29：本地检查没过，市场里不该建出任何版本——版本号不可回收")
	}
}

// `--no-pin-digest` 可以跳过，但必须警告。
//
// 留这条出路是因为确实有解析不了的场景（气隙环境、私有 registry 权限受限）。
// 但它把可复现性交还给了 tag，使用者得知道自己放弃了什么。
func TestPublishNoPinDigestWarnsLoudly(t *testing.T) {
	m := newFakeMarket(t)
	m.artifacts = []map[string]any{artifactEntry("art-0", "api-docs", "openapi", "openapi.json")}
	var asked []string
	f := pinningProject(t, m)
	root := writeComponentDir(t, f.Dir, publishable())

	r := publishWith(t, f, okResolver(&asked), "publish", "--path", root, "--no-pin-digest")

	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)
	assert.Empty(t, asked, "给了 --no-pin-digest 就不该去问 registry")
	assert.Contains(t, r.stdout, "⚠️", "P29：要警告：%s", r.stdout)
	assert.Contains(t, uploadedImage(t, m), ":1.2.0", "跳过时保持原样的 tag")
}

// 钉住之后要如实汇报——使用者得看见镜像被改写了。
func TestPublishReportsPinnedDigest(t *testing.T) {
	m := newFakeMarket(t)
	m.artifacts = []map[string]any{artifactEntry("art-0", "api-docs", "openapi", "openapi.json")}
	var asked []string
	f := pinningProject(t, m)
	root := writeComponentDir(t, f.Dir, publishable())

	r := publishWith(t, f, okResolver(&asked), "publish", "--path", root)

	require.Equal(t, clierr.ExitOK, r.code, r.stdout+r.stderr)
	assert.Contains(t, r.stdout, "digest", "%s", r.stdout)
	assert.Contains(t, r.stdout, testDigest[:20], "要把钉住的 digest 打出来：%s", r.stdout)
}

// ============================================================
// digest 格式校验
// ============================================================

// 写坏的 digest 要在发布前就被拦下。
//
// `@` 后面原本什么都能写——`repo@latest` 这种会一路传到市场，
// 消费方拉取时才失败，而那时已经查不清是谁传坏的了。
func TestPublishRejectsMalformedDigest(t *testing.T) {
	cases := map[string]string{
		"不是 sha256": "registry.example.com/app@md5:abc",
		"长度不对":      "registry.example.com/app@sha256:abc",
		"含非十六进制字符":  "registry.example.com/app@sha256:zzzz1111222233334444555566667777888899990000aaaabbbbccccddddeeee",
		"@ 后面什么都没有": "registry.example.com/app@",
	}

	for name, image := range cases {
		t.Run(name, func(t *testing.T) {
			err := checkImageReference(image)
			require.Error(t, err, "P29：%s 应该被拦下", image)
			assert.Contains(t, clierr.As(err).Format(), "digest",
				"错误要说清是 digest 的问题")
		})
	}
}

// 合法的 digest 照常通过。
func TestPublishAcceptsValidDigest(t *testing.T) {
	assert.NoError(t, checkImageReference("registry.example.com/app@"+testDigest))
	assert.NoError(t, checkImageReference("registry:5000/team/app@"+testDigest),
		"registry 带端口号时也要认")
}

// ============================================================
// 解析器本身
// ============================================================

// 解析器只认 buildx，**不做 fallback**。
//
// `docker manifest inspect -v` 也能给出一个 digest，但那是**单个平台**的
// manifest digest，不是多架构索引的。实测 alpine:3.20：
//
//	buildx              → sha256:d9e853e8…  application/vnd.oci.image.index.v1+json
//	manifest inspect -v → sha256:c64c687c…  ...image.manifest.v1+json  linux/amd64
//
// 钉住后者会把组件**锁死在 amd64**，ARM 节点上拉不到。
// 一个悄悄锁架构的 fallback 比直接报错糟得多。
func TestDigestResolverUsesBuildxOnly(t *testing.T) {
	var calls []string
	resolve := digestResolverWith(func(_ context.Context, name string, args ...string) ([]byte, error) {
		calls = append(calls, name+" "+strings.Join(args, " "))
		return []byte(testDigest + "\n"), nil
	})

	got, err := resolve(context.Background(), "registry.example.com/app:1.0.0")

	require.NoError(t, err)
	assert.Equal(t, testDigest, got)
	require.Len(t, calls, 1, "只该问一次，不做 fallback")
	assert.Contains(t, calls[0], "buildx imagetools inspect")
	assert.NotContains(t, calls[0], "manifest inspect",
		"manifest inspect 给的是单平台 digest，会把组件锁死在一种架构上")
}

// 返回的东西不像 digest 时要报错，而不是当成结果传下去。
func TestDigestResolverRejectsGarbageOutput(t *testing.T) {
	resolve := digestResolverWith(func(context.Context, string, ...string) ([]byte, error) {
		return []byte("ERROR: pull access denied\n"), nil
	})

	_, err := resolve(context.Background(), "registry.example.com/app:1.0.0")

	require.Error(t, err, "P29：拿到一串不是 digest 的东西时不能当成成功")
}
