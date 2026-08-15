package security_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/security"
)

// signedAt 是一个固定时间，避免测试依赖真实时钟。
var signedAt = time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)

// TestSignThenVerify 是签名侧的主干：签出来的东西，我们自己验得过。
func TestSignThenVerify(t *testing.T) {
	bin := cosignBin(t)
	_, keyPath, pubPath := cosignKeyPair(t, bin)
	// 测试进程没有终端。不设这个变量的话 cosign 会去提示输入口令并失败——
	// 这正是 TestSignReportsMissingPassword 覆盖的那种情况。
	t.Setenv("COSIGN_PASSWORD", "")

	payload, err := security.CanonicalPayload([]byte(sampleManifest))
	require.NoError(t, err)

	sig, err := security.Sign(context.Background(), payload, security.SignOptions{
		CosignPath:   bin,
		KeyPath:      keyPath,
		PublicKeyRef: keyRef,
		SignedBy:     "release-bot@brickkit.io",
		Now:          func() time.Time { return signedAt },
	})
	require.NoError(t, err)

	assert.Equal(t, security.AlgorithmCosign, sig.Algorithm)
	assert.Equal(t, keyRef, sig.PublicKeyRef)
	assert.Equal(t, "release-bot@brickkit.io", sig.SignedBy)
	assert.Equal(t, signedAt, sig.SignedAt)
	assert.NotEmpty(t, sig.Value)

	ring, err := security.LoadKeyRing(map[string]string{keyRef: pubPath}, "")
	require.NoError(t, err)
	assert.NoError(t, security.Verify(payload, *sig, ring))
}

// TestSignReportsMissingPassword 覆盖 CI 里必然会撞上的一种失败。
//
// 私钥要口令、又没有终端可以提示时，cosign 报的是
// "reading key: inappropriate ioctl for device"——跟口令一个字都不沾边。
// 原样透传出去，使用者只会往终端或权限的方向白查一通。
//
// 这条是真跑出来的：第一版实现直接把 cosign 的输出贴给使用者，就是这句话。
func TestSignReportsMissingPassword(t *testing.T) {
	bin := cosignBin(t)
	_, keyPath, _ := cosignKeyPair(t, bin)
	t.Setenv("COSIGN_PASSWORD", "") // 先置空，保证密钥对生成成功

	payload, err := security.CanonicalPayload([]byte(sampleManifest))
	require.NoError(t, err)

	// 再抹掉，模拟"忘了设 COSIGN_PASSWORD 就在 CI 里跑签名"
	require.NoError(t, os.Unsetenv("COSIGN_PASSWORD"))

	_, err = security.Sign(context.Background(), payload, security.SignOptions{
		CosignPath:   bin,
		KeyPath:      keyPath,
		PublicKeyRef: keyRef,
	})
	require.Error(t, err)

	var cerr *clierr.Error
	require.ErrorAs(t, err, &cerr)
	rendered := cerr.Format()
	assert.Contains(t, rendered, "COSIGN_PASSWORD", "必须点名要设哪个环境变量")
	assert.NotContains(t, rendered, "ioctl", "不能把 cosign 那句看不懂的话原样丢给使用者")
}

// TestSignRequiresCosign 覆盖"发布者没装 cosign"。
//
// 这条错误的价值全在提示上：使用者看到"cosign 未安装"只会去搜一圈，
// 直接把安装命令给出来才是有用的。
func TestSignRequiresCosign(t *testing.T) {
	_, err := security.Sign(context.Background(), []byte("payload"), security.SignOptions{
		CosignPath:   filepath.Join(t.TempDir(), "cosign-does-not-exist"),
		KeyPath:      "cosign.key",
		PublicKeyRef: keyRef,
	})
	require.Error(t, err)

	var cerr *clierr.Error
	require.ErrorAs(t, err, &cerr)
	rendered := cerr.Format()
	assert.Contains(t, rendered, "cosign")
	assert.Contains(t, rendered, "go install", "错误里要直接给出安装命令")
}

// TestSignRequiresPrivateKey 覆盖私钥路径写错。
func TestSignRequiresPrivateKey(t *testing.T) {
	bin := cosignBin(t)
	missing := filepath.Join(t.TempDir(), "cosign.key")

	_, err := security.Sign(context.Background(), []byte("payload"), security.SignOptions{
		CosignPath:   bin,
		KeyPath:      missing,
		PublicKeyRef: keyRef,
	})
	require.Error(t, err)

	var cerr *clierr.Error
	require.ErrorAs(t, err, &cerr)
	assert.Contains(t, cerr.Format(), missing, "要指出是哪个路径找不到")
}

// TestSignRequiresPublicKeyRef 说明 ref 为什么不能由我们代为编造。
//
// ref 是发布者与使用者之间的契约：使用者要照着它在 installer.publicKeys 里
// 配同名条目。我们随手起一个默认值，就等于替双方定了一个谁都没约定过的名字。
func TestSignRequiresPublicKeyRef(t *testing.T) {
	_, err := security.Sign(context.Background(), []byte("payload"), security.SignOptions{
		CosignPath: "cosign",
		KeyPath:    "cosign.key",
	})
	require.Error(t, err)

	var cerr *clierr.Error
	require.ErrorAs(t, err, &cerr)
	assert.Contains(t, cerr.Format(), "publicKeyRef")
}
