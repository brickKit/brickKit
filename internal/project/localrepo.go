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
// 只认有 component.yaml 的目录。

import (
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"

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
		if _, err := os.Stat(filepath.Join(c, manifest.FileName)); err == nil {
			return c, true
		}
	}
	return "", false
}

// LocalRepoVersion 读本地仓库 component.yaml 里的 metadata.version（只读这一个字段：
// 正在开发的 component.yaml 别处写了一半也不妨碍回答"它是哪个版本"）。
func LocalRepoVersion(dir string) (string, error) {
	data, err := os.ReadFile(filepath.Join(dir, manifest.FileName))
	if err != nil {
		return "", err
	}
	var head struct {
		Metadata struct {
			Version string `yaml:"version"`
		} `yaml:"metadata"`
	}
	if err := yaml.Unmarshal(data, &head); err != nil {
		return "", err
	}
	return head.Metadata.Version, nil
}

// LocalRepoSubpath 是组件在它的 git 仓库里的子目录（monorepo，组件级 source.path）；
// 在仓库根目录时为空。版本 tag 的命名跟着它。
func (p *Project) LocalRepoSubpath(id string) string {
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
