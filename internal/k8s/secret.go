package k8s

// 本文件渲染 Secret（005 §5.6）。
//
// 规则只有一条：**密码永远不出现在 Deployment 里**。
// Deployment 那份 YAML 是给人看、给 git 记的，密码进去就等于泄露；
// Secret 单独一份文件，可以单独设权限、单独排除出版本库。

import (
	"sort"
	"strings"

	"github.com/brickkit/brickkit/internal/inject"
)

// secretPlan 是一个资源对应的 Secret。
type secretPlan struct {
	// Name 是 Secret 名：<资源ID>-secret。
	Name string
	// Data 是 key → 已求值的明文（K8s 的 stringData 由 API Server 自己做 base64）。
	Data map[string]string
	// Config 为 true 表示这是组件配置项的 Secret（configSchema 的 secret: true），
	// 否则是资源连接的 Secret。两类分文件落盘，文件名一眼看出归属。
	Config bool
}

// secretName 是某个资源的 Secret 名。
func secretName(resourceID string) string { return sanitizeName(resourceID) + "-secret" }

// configSecretName 是某个组件的配置类 Secret 名：<版本化服务名>-config-secret。
//
// 与资源 Secret 分开命名：资源 ID 是使用者起的，可能恰好等于某个服务名；
// 多一个 -config- 才保证两类 Secret 永远不会撞名。
func configSecretName(service string) string { return sanitizeName(service) + "-config-secret" }

// secretRef 返回一条敏感变量在 K8s Secret 里的位置：Secret 名 + key。
func secretRef(v inject.Var) (name, key string) {
	if v.ExistingSecretRef != "" {
		// 引用外部系统已经建好的 Secret，不生成任何平台自己的 Secret
		return v.ExistingSecretRef, v.SecretKey
	}
	if v.Source == inject.SourceResource {
		return secretName(v.ResourceID), v.SecretKey
	}
	return configSecretName(v.Owner), v.SecretKey
}

// collectSecrets 从注入结果里挑出敏感变量，按资源归集成 Secret。
//
// 按**资源**归集而不是按组件：同一个数据库被三个组件用到时，
// 密码只该有一份；按组件归集会生成三份内容相同的 Secret，
// 改密码时改漏一份就是一次线上故障。
func (p *plan) collectSecrets() error {
	type slot struct {
		config bool
		data   map[string]string
	}
	byName := map[string]*slot{}

	for _, c := range p.components {
		for _, v := range c.Env.Env {
			if !v.IsSecret() || v.ExistingSecretRef != "" {
				continue
			}
			name, key := secretRef(v)
			s := byName[name]
			if s == nil {
				s = &slot{config: v.Source != inject.SourceResource, data: map[string]string{}}
				byName[name] = s
			}
			s.data[key] = p.expand.value(v.Value)
		}
	}

	for name, s := range byName {
		p.secrets = append(p.secrets, secretPlan{Name: name, Data: s.data, Config: s.config})
	}
	sort.Slice(p.secrets, func(i, j int) bool { return p.secrets[i].Name < p.secrets[j].Name })

	// 明文变量里的 ${VAR} 同样要求值——host、用户名一样可能写成引用。
	// 这里只是先跑一遍把缺失的收集齐，渲染时再算一次（纯函数，结果一致）
	for _, c := range p.components {
		for _, v := range c.Env.Env {
			if !v.IsSecret() {
				p.expand.value(v.Value)
			}
		}
	}
	return p.expand.check()
}

// secretDocs 渲染一类 Secret：config 为 true 是组件配置项的，否则是资源连接的。
func (p *plan) secretDocs(config bool) []map[string]any {
	out := make([]map[string]any, 0, len(p.secrets))
	for _, s := range p.secrets {
		if s.Config != config {
			continue
		}
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
				"labels":    map[string]any{labelProject: p.cfg.Project},
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
