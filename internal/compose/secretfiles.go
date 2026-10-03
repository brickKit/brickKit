package compose

import (
	"path/filepath"
	"sort"

	"github.com/brickkit/brickkit/internal/configdir"
	"github.com/brickkit/brickkit/internal/inject"
	"github.com/brickkit/brickkit/internal/manifest"
	"github.com/brickkit/brickkit/internal/project"
)

// 本文件是"配置项以文件交付"（configSchema 的 mount: file）在 Docker / Podman 下的落地。
//
// # 做法
//
// 每个组件一个目录 .brickkit/generated/secrets/<版本化服务名>/，每个键一个文件，文件名就是键名。
// 这个目录只读挂进容器的 /run/brickkit/secrets/<版本化服务名>/，那个键的环境变量里放的是文件路径。
// 被外壳承载的成员的目录挂进外壳的容器，路径不变；在宿主机上跑的进程（mode: local / debug、焦点）
// 拿到的是这个文件在宿主机上的路径。
//
// # 为什么挂目录，不用 compose 的 secrets:
//
// 没有 swarm 时 compose 的 file 型 secret 就是一次单文件 bind mount：挂的是那个 inode，之后把文件换掉
// （写临时文件再改名）容器里看到的还是旧的。挂目录则换了就能看到——值变了不用重建容器，组件自己重新读，
// 这正是用文件而不是环境变量的理由。K8s 的 Secret 卷也是一个目录，两边行为一致。
//
// # 权限
//
// secrets/ 是 0700：宿主机上别的用户进不来。它下面每个组件的目录是 0755、文件是 0644：容器里的进程常常不是
// root，也不是宿主机上的这个用户，要读得到只能靠"其他人可读"；而 bind mount 按 inode 挂，不经过 0700 的那一层。
// 实测于 Docker 29 / Compose v5.3.1：uid 10001 的容器读得到，改名替换后容器里立刻是新内容，
// `docker inspect` 的 Env 里只有路径。

// SecretFileDir 是以文件交付的配置项在宿主机上的目录，相对项目根。
const SecretFileDir = ".brickkit/generated/secrets"

// SecretFile 是要写盘的一个文件：<SecretFileDir>/<Service>/<Key>。
type SecretFile struct {
	// Service 是这一项所属组件的版本化服务名（目录名），Key 是配置键（文件名）。
	Service string
	Key     string
	Content []byte
}

// mountSecretFiles 把这次要跑的组件里以文件交付的配置项求出值、记进 p.secretFiles，
// 并把变量换成文件路径。返回的是换过之后的注入结果，原来那份不动。
//
// 路径取决于读它的进程在哪：容器里是 /run/brickkit/secrets/…，宿主机上的进程（mode: local / debug）是文件在
// 宿主机上的绝对路径。被裸进程外壳承载的成员在这里先拿容器里的路径——它的迁移仍是容器；交给外壳的那一份
// 之后再改成宿主机路径（hostPathsForBareShellMembers）。
// 之后的逻辑（环境变量放哪、外壳的成员 JSON、调试用的 env 文件）看到的都只是一条普通的明文变量。
//
// existingSecret 引用留着不动：Docker 没有 Secret，那条变量照旧不注入（envPlacement）。
func (p *plan) mountSecretFiles(env *inject.Result) (*inject.Result, error) {
	undefined := map[string]bool{}
	out := &inject.Result{Warnings: env.Warnings}
	for _, c := range env.Components {
		if !p.states.IsRunning(c.Ref) || !hasFileVars(c.Env) {
			out.Components = append(out.Components, c)
			continue
		}
		onHost := p.proj.DeployEntry(c.Ref.ID, c.Ref.Version).IsBareProcess()
		vars := make([]inject.Var, len(c.Env))
		for i, v := range c.Env {
			vars[i] = v
			if !v.MountsFile() || v.IsSecretRef() {
				continue
			}
			if v.Value.Kind == configdir.KindEnvTemplate {
				p.collectUndefined(v.Value.Text, undefined)
			}
			value, err := configdir.Evaluate(v.Value, p.root, p.lookup)
			if err != nil {
				return nil, withVariable(err, c.Ref, v.Name)
			}
			if p.secretFiles[v.Owner] == nil {
				p.secretFiles[v.Owner] = map[string][]byte{}
			}
			p.secretFiles[v.Owner][v.Key] = []byte(value)

			path := manifest.SecretFilePath(v.Owner, v.Key)
			if onHost {
				path = p.hostSecretFilePath(v.Owner, v.Key)
			}
			vars[i] = v.AsFilePath(path)
		}
		c.Env = vars
		out.Components = append(out.Components, c)
	}
	return out, undefinedError(undefined)
}

func hasFileVars(vars []inject.Var) bool {
	for _, v := range vars {
		if v.MountsFile() {
			return true
		}
	}
	return false
}

// hostPathsForBareShellMembers 把裸进程外壳承载的成员交给外壳的那份环境里的文件路径改成宿主机上的：
// 成员的代码在外壳进程里、也就是在宿主机上读这个文件。成员的迁移容器用的是另一份环境（memberMigrations），
// 路径仍是容器里的——与地址的处理同一个道理（rewriteEndpointsForLocalDependencies）。
func (p *plan) hostPathsForBareShellMembers() {
	for i := range p.served {
		if !p.bareShell(p.served[i].Shell) {
			continue
		}
		for j, v := range p.served[i].Env.Env {
			if v.FileOf != "" {
				p.served[i].Env.Env[j].Value = inject.Literal(p.hostSecretFilePath(v.Owner, v.FileOf))
			}
		}
	}
}

// hostSecretFilePath 是那个文件在宿主机上的绝对路径：宿主机上的进程从哪个目录启动都读得到。
func (p *plan) hostSecretFilePath(service, key string) string {
	root := p.root
	if abs, err := filepath.Abs(root); err == nil {
		root = abs
	}
	return filepath.Join(root, filepath.FromSlash(SecretFileDir), service, key)
}

// secretVolume 是把 service 的那个目录只读挂进容器的 volumes 项。
func secretVolume(service string) map[string]any {
	return map[string]any{
		"type":      "bind",
		"source":    "./" + SecretFileDir + "/" + service,
		"target":    manifest.SecretFilesRoot + "/" + service,
		"read_only": true,
	}
}

// applySecretVolumes 给一个 service 挂上它要读的目录：owners 里有文件的那些，按服务名排序。
func (p *plan) applySecretVolumes(svc map[string]any, owners ...string) {
	sort.Strings(owners)
	var volumes []any
	for _, owner := range owners {
		if len(p.secretFiles[owner]) > 0 {
			volumes = append(volumes, secretVolume(owner))
		}
	}
	if len(volumes) > 0 {
		svc["volumes"] = volumes
	}
}

// secretOwnersOf 是 c 的容器要挂的目录属于谁：它自己，加上它（作为外壳）这次承载的成员。
// 只对容器有意义：裸进程外壳直接读宿主机上的文件，不挂任何东西。
func (p *plan) secretOwnersOf(c componentPlan) []string {
	owners := []string{c.Service}
	for _, s := range p.served {
		if s.Shell == c.Ref {
			owners = append(owners, s.Service)
		}
	}
	return owners
}

// secretFileList 按服务名、键名排序返回全部要写盘的文件。
func (p *plan) secretFileList() []SecretFile {
	var out []SecretFile
	for service, files := range p.secretFiles {
		for key, content := range files {
			out = append(out, SecretFile{Service: service, Key: key, Content: content})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Service != out[j].Service {
			return out[i].Service < out[j].Service
		}
		return out[i].Key < out[j].Key
	})
	return out
}

// SecretFilesPath 是 layout 对应项目里 SecretFileDir 的绝对路径（命令层写盘、清理用）。
func SecretFilesPath(layout project.Layout) string {
	return filepath.Join(layout.Root, filepath.FromSlash(SecretFileDir))
}
