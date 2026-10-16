package project

// 本文件回答"这个组件的本地仓库在哪、是哪个版本"——代码从本地仓库运行时（mode: local /
// debug、以裸进程运行的外壳的成员）up 要核对它，mode: local 也从这里启动。
//
// 找的顺序：
//
//	1 组件自己写了 source.type: local       那个目录就是它
//	2 组件自己写了 source.type: git + path  --repo 克隆的是整个 monorepo：components/<id>/<path>
//	3 启用的本地安装源                       <源目录>/<scope>/<name>，按 sources 的顺序
//	4 components/<scope>/<name>              --repo 克隆的位置
//
// 3、4 两处的目录也可能是 --repo 克隆下来的整个 monorepo（组件来自仓库源 sources[].repo）：
// 那时组件在克隆里的某个子目录（ComponentDir）。只认有 component.yaml 的目录。

import (
	"os"
	"path"
	"path/filepath"

	"gopkg.in/yaml.v3"

	"github.com/brickkit/brickkit/internal/gitrepo"
	"github.com/brickkit/brickkit/internal/manifest"
	"github.com/brickkit/brickkit/internal/projfile"
)

// LocalRepo 返回组件的本地仓库目录；找不到时 ok 为 false。
func (p *Project) LocalRepo(id string) (dir string, ok bool) {
	var candidates []string
	if src := p.componentSource(id); src != nil {
		switch src.Type {
		case projfile.SourceTypeLocal:
			candidates = append(candidates, p.Layout.Resolve(src.Path))
		case projfile.SourceTypeGit:
			if src.Path != "" {
				candidates = append(candidates, filepath.Join(p.Layout.ComponentsDir(), filepath.FromSlash(id), filepath.FromSlash(src.Path)))
			}
		}
	}
	for _, s := range p.Decl.EnabledSources() {
		if s.Type == projfile.SourceTypeLocal {
			candidates = append(candidates, filepath.Join(p.Layout.Resolve(s.Path), filepath.FromSlash(id)))
		}
	}
	candidates = append(candidates, filepath.Join(p.Layout.ComponentsDir(), filepath.FromSlash(id)))
	for _, c := range candidates {
		if dir, ok := ComponentDir(c, id); ok {
			return dir, true
		}
	}
	return "", false
}

// ComponentDir 是组件 id 在 dir 里的目录：dir 自己有 component.yaml 时就是 dir；dir 是
// --repo 克隆下来的整个 monorepo 时，是克隆里 git 跟踪着的、metadata.id 是它的那份 component.yaml
// 所在的子目录——与仓库源在 tag 里找组件同一条规则（manifest.InRepository）。本地安装源按 ID
// 找组件、LocalRepo 找本地仓库都用它。
func ComponentDir(dir, id string) (string, bool) {
	if info, err := os.Stat(filepath.Join(dir, manifest.FileName)); err == nil && !info.IsDir() {
		return dir, true
	}
	return componentInClone(dir, id)
}

// componentInClone 在 --repo 克隆下来的整个 monorepo 里找组件。克隆本身要是仓库根
// （components/ 被项目自己的仓库跟踪着时，不能把整个项目当成这份克隆）；
// 找到两份同 id 的 component.yaml 时不猜是哪一份。
func componentInClone(clone, id string) (string, bool) {
	repo, err := gitrepo.Open(clone)
	if err != nil {
		return "", false
	}
	if rel, ok := repo.Rel(clone); !ok || rel != "." {
		return "", false
	}
	entries, err := repo.IndexEntries(".")
	if err != nil {
		return "", false
	}
	var found []string
	for _, e := range entries {
		if !manifest.InRepository(e.Path) {
			continue
		}
		dir := filepath.Join(clone, filepath.FromSlash(path.Dir(e.Path)))
		if head, err := readHead(dir); err == nil && head.Metadata.ID == id {
			found = append(found, dir)
		}
	}
	if len(found) != 1 {
		return "", false
	}
	return found[0], true
}

// head 是 component.yaml 里回答"这是哪个组件的哪个版本"的两个字段：只读这两个，
// 正在开发的 component.yaml 别处写了一半也不妨碍回答。
type head struct {
	Metadata struct {
		ID      string `yaml:"id"`
		Version string `yaml:"version"`
	} `yaml:"metadata"`
}

func readHead(dir string) (head, error) {
	var h head
	data, err := os.ReadFile(filepath.Join(dir, manifest.FileName))
	if err != nil {
		return h, err
	}
	err = yaml.Unmarshal(data, &h)
	return h, err
}

// LocalRepoVersion 读本地仓库 component.yaml 里的 metadata.version。
func LocalRepoVersion(dir string) (string, error) {
	h, err := readHead(dir)
	return h.Metadata.Version, err
}

// LocalRepoSubpath 是组件在它的 git 仓库里的子目录（monorepo）；在仓库根目录时为空。
// 版本 tag 的命名跟着它，规则与 brickkit release 一样：本地仓库在时看它相对 git 仓库根的位置，
// 不在时看组件级 source.path。
func (p *Project) LocalRepoSubpath(id string) string {
	if dir, ok := p.LocalRepo(id); ok {
		if repo, err := gitrepo.Open(dir); err == nil {
			if rel, ok := repo.Rel(dir); ok && rel != "." {
				return rel
			}
			return ""
		}
	}
	if src := p.componentSource(id); src != nil && src.Type == projfile.SourceTypeGit {
		return src.Path
	}
	return ""
}

// componentSource 是组件在 brickkit.yaml 里自己写的来源（各行一致，由校验保证）。
func (p *Project) componentSource(id string) *projfile.ComponentSource {
	for _, c := range p.Decl.ComponentsByID(id) {
		if c.Source != nil {
			return c.Source
		}
	}
	return nil
}
