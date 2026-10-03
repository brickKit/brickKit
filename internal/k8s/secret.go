package k8s

// 本文件渲染 Secret。
//
// 规则只有一条：**密码永远不出现在 Deployment 里**。
// Deployment 那份 YAML 是给人看、给 git 记的，密码进去就等于泄露；
// Secret 单独一份文件，可以单独设权限、单独排除出版本库。

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
	"unicode/utf8"

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
// existingSecret 直接指向外部系统建好的 Secret；其余进平台为它所属组件（Owner）生成的那一份，
// key 是原始配置键。外壳的 BRICKKIT_SERVED_MEMBERS_CONFIG 归外壳自己。
func secretRef(v inject.Var) (name, key string) {
	if v.IsSecretRef() {
		return v.Value.SecretName, v.Value.SecretKey
	}
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
	for _, c := range append(append([]componentPlan{}, p.components...), p.memberMigrations...) {
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

	// 以文件交付的那几项也在平台生成的 Secret 里，只是不经 secretKeyRef，而是挂成文件（secretfiles.go）
	for name, data := range p.fileData {
		if byName[name] == nil {
			byName[name] = map[string]string{}
		}
		for key, value := range data {
			byName[name][key] = value
		}
	}

	for name, data := range byName {
		p.secrets = append(p.secrets, secretPlan{Name: name, Data: data})
	}
	sort.Slice(p.secrets, func(i, j int) bool { return p.secrets[i].Name < p.secrets[j].Name })
	return p.expand.check()
}

// secretDigest 是 c 的 Pod 经 secretKeyRef 读的、由平台生成的那些密钥的摘要；一项都没有时是空串。
//
// # 为什么需要
//
// 密钥进环境变量是在容器启动的那一刻：之后 Secret 再怎么变，已经在跑的 Pod 手里还是旧值。而改一个密钥的值，
// Deployment 的清单一个字都不变（它只写着 secretKeyRef），`kubectl apply` 就不会滚动更新——`up` 报成功，
// Secret 是新的，组件用的是旧的，要等到 Pod 哪天因为别的原因重启才换过来。Docker 下同一次改动会重建容器
// （env 文件变了），两个目标说法不一。在 minikube 上真跑撞到的。
// 把摘要写在 Pod 模板上：值一变，模板就变，滚动更新照常发生，waitRollout 也照常等它。
//
// # 算进去的与不算的
//
//	算     经 secretKeyRef 进环境变量、值由平台生成的那些（含外壳的成员 JSON：成员的配置变了，外壳要重启）
//	不算   以文件交付的（mount: file）——那些有意不重启，组件自己重新读文件
//	不算   existingSecret——平台读不到它的值。别的工具轮换了它，环境变量形式的要自己重启 Pod
//
// # 摘要会不会泄露密钥
//
// 它是密钥内容的确定性函数：能读 Deployment 却读不了 Secret 的人，可以离线验证对**弱**密钥的猜测。
// 这与 Helm 的 checksum 注解是同一个取舍。几项一起算、只留 64 位，猜的人得同时猜中这个组件的全部密钥；
// 真正的防线仍是密钥本身够强。
func (p *plan) secretDigest(c componentPlan) string {
	h := sha256.New()
	found := false
	// c.Env.Env 按变量名排序（inject 的约定），摘要因此稳定
	for _, v := range c.Env.Env {
		if envPlacement(v) != placeGeneratedSecret {
			continue
		}
		// 求值错误已经在 collectSecrets 里报过，这里不会再出现
		value, err := p.valueOf(v)
		if err != nil {
			continue
		}
		found = true
		// 带长度前缀：否则 A="b", C="d" 与 A="bC=d" 这样的两组值会算出同一个摘要
		// hash.Hash 的 Write 从不返回错误
		_, _ = fmt.Fprintf(h, "%d:%s=%d:%s", len(v.Name), v.Name, len(value), value)
	}
	if !found {
		return ""
	}
	return hex.EncodeToString(h.Sum(nil))[:16]
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

		// 文本进 stringData，不是合法 UTF-8 的（以文件交付的二进制内容：密钥库、DER 证书）进 data。
		// stringData 只能装文本——YAML 写不下任意字节；环境变量也装不了，所以走到 data 的只会是挂成文件的项
		text, binary := map[string]any{}, map[string]any{}
		for _, key := range keys {
			if utf8.ValidString(s.Data[key]) {
				text[key] = s.Data[key]
			} else {
				binary[key] = base64.StdEncoding.EncodeToString([]byte(s.Data[key]))
			}
		}

		doc := map[string]any{
			"apiVersion": "v1",
			"kind":       "Secret",
			"metadata": map[string]any{
				"name":      s.Name,
				"namespace": p.namespace,
				"labels":    map[string]any{labelProject: p.proj.Decl.Project},
			},
			"type": "Opaque",
		}
		// stringData 而不是 data：写进去的是明文，由 API Server 自己 base64。
		// 手工 base64 只是把密码变得不可读，并不会更安全，却让排障时
		// 看不出这份文件里到底是什么
		if len(text) > 0 || len(binary) == 0 {
			doc["stringData"] = text
		}
		if len(binary) > 0 {
			doc["data"] = binary
		}
		out = append(out, doc)
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
