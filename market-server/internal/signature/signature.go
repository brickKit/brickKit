// Package signature 是市场侧的规范化与验签（P30）。
//
// # 为什么这里有一份"重复"的实现
//
// market-server 是独立 module，引用不了主 module 的 internal/security。
// 两份实现必然带来漂移风险，而漂移的表现极其难查：**同一个签名在 CLI 侧
// 验得过、市场侧验不过**（或反过来），两边代码单独看都是对的。
//
// 唯一的防线是 testdata/signature-golden.json：两侧各有一条测试，
// 断言自己的规范化输出与那份样本**逐字节相同**。改这里的任何逻辑之前，
// 先读 golden_test.go。
//
// # 市场为什么现在可以验签了
//
// 008 §8.2 原本有意偏离过：市场手里没有可信公钥，让发布者连公钥一起上传
// 就是自己给自己发证（D285）。TOFU 改变了这个前提——市场**不为密钥的身份
// 背书**，只强制"还是首次那把、且你真的持有它"。验签在这里证明的是
// 持有私钥，不是密钥可信。
package signature

import (
	"crypto/ecdsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

// Canonical 把一份 Manifest 规范化成待签名字节。
//
// 与主 module 的 security.CanonicalPayload **必须逐字节一致**：
// 解析成数据结构，再按键名字典序编码回 JSON。
// yaml.v3 能解析 JSON（JSON 是 YAML 的子集），并且默认拒绝重复键——
// 那正是需要的：不同解析器对重复键的取舍不同，是规范化最现实的攻击面。
func Canonical(raw []byte) ([]byte, error) {
	var doc map[string]any
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		return nil, fmt.Errorf("Manifest 无法解析：%w", err)
	}
	if len(doc) == 0 {
		return nil, errors.New("Manifest 为空")
	}

	normalized, err := normalize(doc)
	if err != nil {
		return nil, err
	}
	// json.Marshal 对 map 按键名字典序输出，这就是"固定规则"的来源
	payload, err := json.Marshal(normalized)
	if err != nil {
		return nil, fmt.Errorf("Manifest 无法规范化：%w", err)
	}
	return payload, nil
}

// normalize 把 yaml 解析出的结构收敛成 json 能编码的形状。
//
// map[any]any 那一支是 yaml 特有的：它允许非字符串键，而 JSON 不允许。
// 直接拒绝而不是强转——强转会让 `1: a` 与 `"1": a` 规范化成同一个东西。
func normalize(value any) (any, error) {
	switch v := value.(type) {
	case map[string]any:
		out := make(map[string]any, len(v))
		for key, item := range v {
			normalized, err := normalize(item)
			if err != nil {
				return nil, err
			}
			out[key] = normalized
		}
		return out, nil

	case map[any]any:
		out := make(map[string]any, len(v))
		for key, item := range v {
			name, ok := key.(string)
			if !ok {
				return nil, fmt.Errorf("Manifest 的键必须是字符串：%v（%T）", key, key)
			}
			normalized, err := normalize(item)
			if err != nil {
				return nil, err
			}
			out[name] = normalized
		}
		return out, nil

	case []any:
		out := make([]any, len(v))
		for i, item := range v {
			normalized, err := normalize(item)
			if err != nil {
				return nil, err
			}
			out[i] = normalized
		}
		return out, nil

	default:
		return v, nil
	}
}

// Verify 用给定的公钥校验一份 Manifest 的签名。
//
// 算法固定为 ECDSA P-256 over SHA-256、ASN.1 DER 编码后 base64——
// 与 cosign 的默认输出一致（008 §8.3.1）。
func Verify(manifest []byte, signatureBase64 string, publicKeyPEM []byte) error {
	key, err := ParsePublicKey(publicKeyPEM)
	if err != nil {
		return err
	}

	der, err := base64.StdEncoding.DecodeString(strings.TrimSpace(signatureBase64))
	if err != nil {
		return errors.New("签名值不是合法的 base64")
	}

	payload, err := Canonical(manifest)
	if err != nil {
		return err
	}

	digest := sha256.Sum256(payload)
	if !ecdsa.VerifyASN1(key, digest[:], der) {
		return errors.New("签名校验不通过：内容与签名对不上，或用的不是这把密钥")
	}
	return nil
}

// ParsePublicKey 解析 PKIX PEM 公钥。
func ParsePublicKey(pemBytes []byte) (*ecdsa.PublicKey, error) {
	block, _ := pem.Decode(pemBytes)
	if block == nil {
		return nil, errors.New("公钥不是合法的 PEM")
	}
	parsed, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("公钥解析失败：%w", err)
	}
	key, ok := parsed.(*ecdsa.PublicKey)
	if !ok {
		return nil, fmt.Errorf("只支持 ECDSA 公钥，当前是 %T", parsed)
	}
	return key, nil
}

// Fingerprint 是公钥的指纹：PKIX DER 的 SHA-256，格式 sha256:<hex>。
//
// 用它做 TOFU 比对与审计展示——比整段 PEM 短得多，且与编码格式无关
// （同一把密钥换个 PEM 换行风格，指纹不变）。
func Fingerprint(publicKeyPEM []byte) (string, error) {
	key, err := ParsePublicKey(publicKeyPEM)
	if err != nil {
		return "", err
	}
	der, err := x509.MarshalPKIXPublicKey(key)
	if err != nil {
		return "", fmt.Errorf("公钥无法编码：%w", err)
	}
	sum := sha256.Sum256(der)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}
