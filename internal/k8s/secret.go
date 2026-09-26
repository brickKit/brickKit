package k8s

// 本文件渲染 Secret（005 §5.6）。
//
// 规则只有一条：**密码永远不出现在 Deployment 里**。
// Deployment 那份 YAML 是给人看、给 git 记的，密码进去就等于泄露；
// Secret 单独一份文件，可以单独设权限、单独排除出版本库。

import (
	"sort"
	"strings"

	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/i18n"
	"github.com/brickkit/brickkit/internal/inject"
	"github.com/brickkit/brickkit/internal/msgid"
)

// secretPlan 是一个组件的配置类 Secret（configSchema 的 secret: true 与 file:// 内容）。
type secretPlan struct {
	// Name 是 Secret 名：<版本化服务名>-config-secret。
	Name string
	// Data 是 key → 已求值的明文（K8s 的 stringData 由 API Server 自己做 base64）。
	Data map[string]string
}

// configSecretName 是某个组件的配置类 Secret 名：<版本化服务名>-config-secret。
func configSecretName(service string) string { return sanitizeName(service) + "-config-secret" }

// secretRef 返回一条走 Secret 的变量在 K8s 里的位置：Secret 名 + key。
//
// existingSecret 直接指向外部系统建好的 Secret；其余进平台为它所属组件生成的那一份，
// key 是原始配置键。外壳成员的变量名被加了组件 ID 前缀，但 Owner 仍是成员：
// Secret 归成员，外壳的 Deployment 只是引用它。
func secretRef(v inject.Var) (name, key string) {
	if v.IsSecretRef() {
		return v.Value.SecretName, v.Value.SecretKey
	}
	// key 用原始配置键：外壳成员的变量名被加了组件 ID 前缀，但它在成员自己那份 Secret 里
	// 仍叫原来的名字——同一个 Secret 被成员独立部署与外壳收编两种形态共用
	key = v.Key
	if key == "" {
		key = v.Name
	}
	return configSecretName(v.Owner), key
}

// collectSecrets 从注入结果里挑出要进 Secret 的变量，按所属组件归集；
// 同时把明文变量里的 ${VAR} 先求一遍，缺失的一次性报全（expander.check）。
func (p *plan) collectSecrets() error {
	byName := map[string]map[string]string{}
	for _, c := range p.components {
		for _, v := range c.Env.Env {
			switch envPlacement(v) {
			case placeGeneratedSecret:
				value, err := p.valueOf(v)
				if err != nil {
					return withVariable(err, c, v.Name)
				}
				name, key := secretRef(v)
				if byName[name] == nil {
					byName[name] = map[string]string{}
				}
				byName[name][key] = value
			case placePlain:
				if _, err := p.valueOf(v); err != nil {
					return withVariable(err, c, v.Name)
				}
			}
		}
	}

	for name, data := range byName {
		p.secrets = append(p.secrets, secretPlan{Name: name, Data: data})
	}
	sort.Slice(p.secrets, func(i, j int) bool { return p.secrets[i].Name < p.secrets[j].Name })
	return p.expand.check()
}

// withVariable 给求值错误补上"是哪个组件的哪一项"。
func withVariable(err error, c componentPlan, name string) error {
	e := clierr.As(err)
	if e == nil {
		return err
	}
	return e.WithDetail(i18n.T(msgid.LabelComponent), c.Ref.String()).
		WithDetail(i18n.T(msgid.LabelConfigKey), name)
}

// secretDocs 渲染全部生成的 Secret。
func (p *plan) secretDocs() []map[string]any {
	out := make([]map[string]any, 0, len(p.secrets))
	for _, s := range p.secrets {
		keys := make([]string, 0, len(s.Data))
		for key := range s.Data {
			keys = append(keys, key)
		}
		sort.Strings(keys)

		data := map[string]any{}
		for _, key := range keys {
			data[key] = s.Data[key]
		}

		out = append(out, map[string]any{
			"apiVersion": "v1",
			"kind":       "Secret",
			"metadata": map[string]any{
				"name":      s.Name,
				"namespace": p.namespace,
				"labels":    map[string]any{labelProject: p.proj.Decl.Project},
			},
			// stringData 而不是 data：写进去的是明文，由 API Server 自己 base64。
			// 手工 base64 只是把密码变得不可读，并不会更安全，却让排障时
			// 看不出这份文件里到底是什么
			"type":       "Opaque",
			"stringData": data,
		})
	}
	return out
}

// sanitizeName 把一个标识符转成合法的 K8s 资源名（DNS-1123 label）。
func sanitizeName(raw string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(raw) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		default:
			b.WriteRune('-')
		}
	}
	return strings.Trim(b.String(), "-")
}
