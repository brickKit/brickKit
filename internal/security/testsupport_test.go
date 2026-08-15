package security_test

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// testKey 是一对测试用的 ECDSA P-256 密钥。
//
// 用 Go 现场生成而不是提交固定的密钥文件：签名算法本身是被测对象，
// 固定密钥只能证明"这一份签名能过"，现场生成每次都是新的密钥与新的签名。
// 与真 cosign 的互操作由 cosign_interop_test.go 单独证明。
type testKey struct {
	priv   *ecdsa.PrivateKey
	pubPEM []byte
}

func newTestKey(t *testing.T) *testKey {
	t.Helper()

	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)

	der, err := x509.MarshalPKIXPublicKey(&priv.PublicKey)
	require.NoError(t, err)

	return &testKey{
		priv:   priv,
		pubPEM: pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der}),
	}
}

// sign 按 cosign sign-blob 的方式签名：SHA-256 摘要 + ASN.1 DER + base64。
func (k *testKey) sign(t *testing.T, payload []byte) string {
	t.Helper()

	digest := sha256.Sum256(payload)
	der, err := ecdsa.SignASN1(rand.Reader, k.priv, digest[:])
	require.NoError(t, err)
	return base64.StdEncoding.EncodeToString(der)
}

// writePub 把公钥写进临时目录，返回文件路径。
func (k *testKey) writePub(t *testing.T, name string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), name)
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, k.pubPEM, 0o644))
	return path
}
