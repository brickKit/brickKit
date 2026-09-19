package config

import "testing"

// ExistingSecretRef 只认恰好两个键、都是非空字符串的对象；其它任何形状都当"不是"处理，
// 落回原来的标量/引用路径（inject.addConfig 据此判断要不要拦截 formatValue）。
func TestExistingSecretRefRecognizesTheExactShape(t *testing.T) {
	cases := []struct {
		name string
		v    any
		ok   bool
	}{
		{"两键对象", map[string]any{"existingSecret": "vault-synced", "key": "api-key"}, true},
		{"标量字符串", "sk-live-plain", false},
		{"${VAR} 引用", "${API_TOKEN}", false},
		{"只有 existingSecret 没有 key", map[string]any{"existingSecret": "vault-synced"}, false},
		{"多了一个键", map[string]any{"existingSecret": "vault-synced", "key": "api-key", "extra": "x"}, false},
		{"existingSecret 是空字符串", map[string]any{"existingSecret": "", "key": "api-key"}, false},
		{"key 不是字符串", map[string]any{"existingSecret": "vault-synced", "key": 1}, false},
		{"nil", nil, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, _, ok := ExistingSecretRef(c.v)
			if ok != c.ok {
				t.Fatalf("%s: got ok=%v, want %v", c.name, ok, c.ok)
			}
		})
	}
}

func TestExistingSecretRefReturnsTheTwoValues(t *testing.T) {
	name, key, ok := ExistingSecretRef(map[string]any{"existingSecret": "vault-synced", "key": "api-key"})
	if !ok || name != "vault-synced" || key != "api-key" {
		t.Fatalf("got name=%q key=%q ok=%v", name, key, ok)
	}
}
