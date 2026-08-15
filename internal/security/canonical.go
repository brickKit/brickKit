package security

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"

	"gopkg.in/yaml.v3"
)

// CanonicalPayload 把一份 Manifest（YAML 或 JSON 写法）规范化成待签名字节。
//
// # 为什么不能直接签原始字节
//
// 一份 Manifest 在到达校验方之前会被反复改写形态：
//
//	component.yaml (YAML)  →  publish 转成 JSON 上传  →  市场存 JSON
//	                       →  CLI 取回  →  转回 YAML 交给 Manifest 解析器
//
// 每一步的字节序都不一样。对"某一种写法的字节"签名，在这条链路上必然失效，
// 而且失效方式还是随机的——换个市场版本、换个反向代理就可能变。
//
// 所以签名的对象是规范化载荷：解析成数据结构，再用固定规则（键名字典序的 JSON）
// 编码回来。只要语义相同，不论原始写法如何，结果都逐字节相同。
// 顺带的好处是：注释、缩进、引号风格的改动不算篡改——它们确实不改变任何行为。
//
// # 规范化本身的攻击面
//
// 让双方各自计算规范形式，就必须保证"同一份文档不会被两边解析成不同的东西"。
// 重复键是这里最现实的漏洞（不同解析器有取第一个的、有取最后一个的），
// 因此直接拒绝，不做取舍。
func CanonicalPayload(raw []byte) ([]byte, error) {
	// yaml.v3 能解析 JSON（JSON 是 YAML 的子集），两种写法走同一条路径。
	// 它默认对重复键报错，正好是我们要的。
	var doc map[string]any
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		return nil, invalid("错误：待校验的 Manifest 无法解析").
			WithDetail("原因", err.Error()).
			WithHint("确认市场返回的内容是完整的 component.yaml").
			WithCause(err)
	}
	if len(doc) == 0 {
		return nil, invalid("错误：待校验的 Manifest 为空").
			WithHint("确认市场返回的内容是完整的 component.yaml")
	}

	normalized, err := normalize(doc)
	if err != nil {
		return nil, err
	}

	// json.Marshal 对 map 按键名字典序输出，这就是"固定规则"的来源。
	payload, err := json.Marshal(normalized)
	if err != nil {
		return nil, invalid("错误：Manifest 无法规范化").
			WithDetail("原因", err.Error()).
			WithCause(err)
	}
	return payload, nil
}

// normalize 把 yaml 解出来的结构收敛成 json.Marshal 能稳定编码的形态。
//
// yaml.v3 在键不全是字符串时会给出 map[any]any，json.Marshal 处理不了。
// 与其让它报一个含糊的编码错误，不如在这里点名是哪种键——Manifest 的键
// 本来就都该是字符串。
func normalize(value any) (any, error) {
	switch v := value.(type) {
	case map[string]any:
		out := make(map[string]any, len(v))
		for key, item := range v {
			normalizedItem, err := normalize(item)
			if err != nil {
				return nil, err
			}
			out[key] = normalizedItem
		}
		return out, nil

	case map[any]any:
		out := make(map[string]any, len(v))
		for key, item := range v {
			name, ok := key.(string)
			if !ok {
				return nil, invalid("错误：Manifest 的键必须是字符串").
					WithDetailf("出问题的键", "%v（%T）", key, key).
					WithHint("修改 component.yaml，把该键写成字符串")
			}
			normalizedItem, err := normalize(item)
			if err != nil {
				return nil, err
			}
			out[name] = normalizedItem
		}
		return out, nil

	case []any:
		out := make([]any, len(v))
		for i, item := range v {
			normalizedItem, err := normalize(item)
			if err != nil {
				return nil, err
			}
			out[i] = normalizedItem
		}
		return out, nil

	default:
		return v, nil
	}
}

// CanonicalDigest 返回规范化载荷的 SHA-256，格式为 sha256:<hex>。
// 用于日志与 brickkit status 展示，让人能一眼对上是不是同一份内容。
func CanonicalDigest(raw []byte) (string, error) {
	payload, err := CanonicalPayload(raw)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(payload)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}
