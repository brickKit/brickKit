package security_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/security"
)

// 本文件是整个 security 包最要紧的一组测试。
//
// 包里其余测试都用 Go 自己生成的密钥和自己造的签名——它们能证明逻辑自洽，
// 却证明不了"我们理解的 cosign 签名就是 cosign 真正产出的那个"。这里用真的
// cosign 二进制签，再用我们的标准库代码验；反过来也验一次。这条断了，
// "发布者用 cosign，使用者零依赖"的整个前提就不成立。
//
// 没装 cosign 时跳过：使用者本来就不需要装（见 security.go 的包注释），
// 不能因为验签方没装签名工具就让测试红掉。

// cosignBin 找到 cosign，找不到就跳过整组测试。
func cosignBin(t *testing.T) string {
	t.Helper()

	if path, err := exec.LookPath("cosign"); err == nil {
		return path
	}
	// go install 默认装到 GOPATH/bin，它常常不在测试进程的 PATH 里
	if home, err := os.UserHomeDir(); err == nil {
		candidate := filepath.Join(home, "go", "bin", "cosign")
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
	}
	t.Skip("未安装 cosign，跳过跨工具验证（安装：go install github.com/sigstore/cosign/v2/cmd/cosign@latest）")
	return ""
}

// cosignKeyPair 用真 cosign 生成密钥对，返回目录、私钥、公钥路径。
//
// COSIGN_PASSWORD 置空是为了非交互：留空时 cosign 不会弹密码提示。
// 生产环境的私钥当然要有口令，那属于 CI 的密钥管理，不是本测试要证明的事。
func cosignKeyPair(t *testing.T, bin string) (dir, keyPath, pubPath string) {
	t.Helper()

	dir = t.TempDir()
	cmd := exec.Command(bin, "generate-key-pair")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "COSIGN_PASSWORD=")
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "cosign generate-key-pair 失败：%s", out)

	return dir, filepath.Join(dir, "cosign.key"), filepath.Join(dir, "cosign.pub")
}

// cosignSignBlob 用真 cosign 对 payload 签名，返回 base64 签名值。
//
// --tlog-upload=false 是**必须**的，不是可选优化：cosign 默认会把签名条目
// 传到 Sigstore 的公共 Rekor 透明日志（全世界可见）。对开源组件那是优点，
// 对私有组件等于把"某公司某时刻发布了某个哈希"公开了。测试里更是绝不能
// 往公网写任何东西。
func cosignSignBlob(t *testing.T, bin, keyPath string, payload []byte) string {
	t.Helper()

	dir := t.TempDir()
	blob := filepath.Join(dir, "payload")
	sigFile := filepath.Join(dir, "signature")
	require.NoError(t, os.WriteFile(blob, payload, 0o600))

	cmd := exec.Command(bin, "sign-blob",
		"--key", keyPath,
		"--tlog-upload=false",
		"--yes",
		"--output-signature", sigFile,
		blob)
	cmd.Env = append(os.Environ(), "COSIGN_PASSWORD=")
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "cosign sign-blob 失败：%s", out)

	value, err := os.ReadFile(sigFile)
	require.NoError(t, err)
	return strings.TrimSpace(string(value))
}

// TestCosignSignedManifestVerifiesWithStdlib 是本包的立身之本：
// 真 cosign 签的名，我们用 Go 标准库验得过。
func TestCosignSignedManifestVerifiesWithStdlib(t *testing.T) {
	bin := cosignBin(t)
	_, keyPath, pubPath := cosignKeyPair(t, bin)

	payload, err := security.CanonicalPayload([]byte(sampleManifest))
	require.NoError(t, err)

	pubPEM, err := os.ReadFile(pubPath)
	require.NoError(t, err)
	ring := security.NewKeyRing()
	require.NoError(t, ring.Add(keyRef, pubPEM),
		"cosign generate-key-pair 产出的公钥必须能被我们直接加载")

	sig := security.Signature{
		Algorithm:    security.AlgorithmCosign,
		PublicKeyRef: keyRef,
		Value:        cosignSignBlob(t, bin, keyPath, payload),
	}

	assert.NoError(t, security.Verify(payload, sig, ring))
	assert.NoError(t, security.VerifyManifest([]byte(sampleManifest), sig, ring,
		"people/basic", "1.2.0"))
}

// TestCosignSignedManifestRejectsTamper 对应 20.3：真签名 + 被改过的内容 = 拒绝。
func TestCosignSignedManifestRejectsTamper(t *testing.T) {
	bin := cosignBin(t)
	_, keyPath, pubPath := cosignKeyPair(t, bin)

	payload, err := security.CanonicalPayload([]byte(sampleManifest))
	require.NoError(t, err)

	pubPEM, err := os.ReadFile(pubPath)
	require.NoError(t, err)
	ring := security.NewKeyRing()
	require.NoError(t, ring.Add(keyRef, pubPEM))

	sig := security.Signature{
		Algorithm:    security.AlgorithmCosign,
		PublicKeyRef: keyRef,
		Value:        cosignSignBlob(t, bin, keyPath, payload),
	}

	// 只改一个字符：版本 1.2.0 → 1.2.1
	tampered := strings.Replace(sampleManifest, "version: 1.2.0", "version: 1.2.1", 1)
	err = security.VerifyManifest([]byte(tampered), sig, ring, "people/basic", "1.2.1")
	assert.Equal(t, clierr.CodeSignatureInvalid, codeOf(t, err))
}

// TestOurCanonicalPayloadVerifiesWithCosign 走反方向：
// 我们规范化出来的载荷 + cosign 自己的 verify-blob。
//
// 有了这一条，签名就不是只有 BrickKit 认得的私有格式——运维、审计、第三方
// 都能拿标准 cosign 复核一遍。这是"用标准工具而不是自己发明签名格式"的意义所在。
func TestOurCanonicalPayloadVerifiesWithCosign(t *testing.T) {
	bin := cosignBin(t)
	_, keyPath, pubPath := cosignKeyPair(t, bin)

	payload, err := security.CanonicalPayload([]byte(sampleManifest))
	require.NoError(t, err)
	value := cosignSignBlob(t, bin, keyPath, payload)

	dir := t.TempDir()
	blob := filepath.Join(dir, "payload")
	sigFile := filepath.Join(dir, "signature")
	require.NoError(t, os.WriteFile(blob, payload, 0o600))
	require.NoError(t, os.WriteFile(sigFile, []byte(value), 0o600))

	// --insecure-ignore-tlog 与签名时的 --tlog-upload=false 配套：
	// 没上传透明日志，验签时自然也不能去查它。
	cmd := exec.Command(bin, "verify-blob",
		"--key", pubPath,
		"--signature", sigFile,
		"--insecure-ignore-tlog=true",
		blob)
	out, err := cmd.CombinedOutput()
	assert.NoError(t, err, "cosign verify-blob 应当通过：%s", out)
	assert.Contains(t, string(out), "Verified OK")
}

// TestKeyRingRejectsPrivateKey 覆盖最常见的配置事故：
// 把 cosign.key 当成公钥配进了 installer.publicKeys。
func TestKeyRingRejectsPrivateKey(t *testing.T) {
	bin := cosignBin(t)
	_, keyPath, _ := cosignKeyPair(t, bin)

	keyPEM, err := os.ReadFile(keyPath)
	require.NoError(t, err)

	err = security.NewKeyRing().Add(keyRef, keyPEM)
	require.Error(t, err)

	var cerr *clierr.Error
	require.ErrorAs(t, err, &cerr)
	assert.Contains(t, cerr.Format(), "cosign.key",
		"必须点名这是私钥——私钥进了 Git 是要立刻轮换的事故，不能只说一句格式不对")
}
