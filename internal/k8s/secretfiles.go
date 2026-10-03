package k8s

import (
	"fmt"
	"sort"

	"github.com/brickkit/brickkit/internal/inject"
	"github.com/brickkit/brickkit/internal/manifest"
)

// 本文件是"配置项以文件交付"（configSchema 的 mount: file）在 K8s 下的落地。
//
// 值照旧进 Secret（平台生成的那份，或 existingSecret 指向的那份），但不再经 secretKeyRef 进环境变量：
// Secret 的那个键被挂成 /run/brickkit/secrets/<版本化服务名>/<键> 这个文件，环境变量里放的是路径。
// 路径与 Docker / Podman 下一样；被外壳承载的成员的文件挂进外壳的 Pod，路径不变。
//
// 用 projected 卷：一个组件的文件可能来自两份 Secret（平台生成的 + existingSecret），而它们要落在同一个目录里。
// Secret 的内容变了，kubelet 会把卷里的文件换成新的（不是 subPath 挂载），组件不重启也读得到——
// existingSecret 指向 cert-manager 这类工具轮换的 Secret 时，这正是要的效果。

// secretFile 是挂进 Pod 的一个文件：来自哪份 Secret 的哪个键，落成目录里的哪个文件名。
type secretFile struct {
	SecretName string
	SecretKey  string
	File       string
}

// mountSecretFiles 把这次要跑的组件里以文件交付的配置项记进 p.secretFiles（并把平台生成的值记进 p.fileData），
// 变量换成文件路径。返回换过之后的注入结果，原来那份不动——之后的逻辑（放进 env、外壳的成员 JSON）
// 看到的都只是一条普通的明文变量。
func (p *plan) mountSecretFiles(env *inject.Result) (*inject.Result, error) {
	out := &inject.Result{Warnings: env.Warnings}
	for _, c := range env.Components {
		if !p.states.IsRunning(c.Ref) {
			out.Components = append(out.Components, c)
			continue
		}
		vars := make([]inject.Var, len(c.Env))
		for i, v := range c.Env {
			vars[i] = v
			if !v.MountsFile() {
				continue
			}
			file := secretFile{File: v.Key}
			if v.IsSecretRef() {
				file.SecretName, file.SecretKey = v.Value.SecretName, v.Value.SecretKey
			} else {
				value, err := p.valueOf(v)
				if err != nil {
					return nil, withVariable(err, componentPlan{Ref: c.Ref}, v.Name)
				}
				file.SecretName, file.SecretKey = configSecretName(v.Owner), v.Key
				if p.fileData[file.SecretName] == nil {
					p.fileData[file.SecretName] = map[string]string{}
				}
				p.fileData[file.SecretName][v.Key] = value
			}
			p.secretFiles[v.Owner] = append(p.secretFiles[v.Owner], file)
			vars[i] = v.AsFilePath(manifest.SecretFilePath(v.Owner, v.Key))
		}
		c.Env = vars
		out.Components = append(out.Components, c)
	}
	return out, nil
}

// secretOwnersOf 是 c 的 Pod 要挂的目录属于谁：它自己，加上它（作为外壳）这次承载的成员。按服务名排序。
func (p *plan) secretOwnersOf(c componentPlan) []string {
	owners := []string{c.Service}
	for _, s := range p.served {
		if s.Shell == c.Ref {
			owners = append(owners, s.Service)
		}
	}
	sort.Strings(owners)
	return owners
}

// secretVolumes 返回 c 的 Pod 要加的 volumes 与容器要加的 volumeMounts；一项都没有时两者都是 nil。
// 主容器与迁移容器走同一个函数：迁移读到的要和主服务一模一样。
func (p *plan) secretVolumes(c componentPlan) (volumes, mounts []any) {
	for _, owner := range p.secretOwnersOf(c) {
		files := p.secretFiles[owner]
		if len(files) == 0 {
			continue
		}
		// 卷名在 Pod 里唯一即可；用序号而不是服务名——服务名加前缀可能超过 63 个字符
		name := fmt.Sprintf("brickkit-secrets-%d", len(volumes))
		volumes = append(volumes, map[string]any{
			"name":      name,
			"projected": map[string]any{"sources": projectedSources(files)},
		})
		mounts = append(mounts, map[string]any{
			"name":      name,
			"mountPath": manifest.SecretFilesRoot + "/" + owner,
			"readOnly":  true,
		})
	}
	return volumes, mounts
}

// projectedSources 把文件按来源的 Secret 归并：每份 Secret 一个 source，按 Secret 名、文件名排序。
func projectedSources(files []secretFile) []any {
	bySecret := map[string][]secretFile{}
	var names []string
	for _, f := range files {
		if _, seen := bySecret[f.SecretName]; !seen {
			names = append(names, f.SecretName)
		}
		bySecret[f.SecretName] = append(bySecret[f.SecretName], f)
	}
	sort.Strings(names)

	sources := make([]any, 0, len(names))
	for _, name := range names {
		group := bySecret[name]
		sort.Slice(group, func(i, j int) bool { return group[i].File < group[j].File })
		items := make([]any, 0, len(group))
		for _, f := range group {
			items = append(items, map[string]any{"key": f.SecretKey, "path": f.File})
		}
		sources = append(sources, map[string]any{
			"secret": map[string]any{"name": name, "items": items},
		})
	}
	return sources
}
