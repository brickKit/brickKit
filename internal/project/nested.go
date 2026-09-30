package project

import (
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/brickkit/brickkit/internal/manifest"
	"github.com/brickkit/brickkit/internal/projfile"
)

// NestedCopy 是嵌在另一个组件目录里的一份组件源码（设计 §5）：组件源码只该放在一处——
// 项目本地安装源（components/、shell/）的 <scope>/<name>/ 下。组件目录里再出现一份，
// 改那一份的代码永远不会被运行，而本地源只扫两层，一个字的提示都没有。
type NestedCopy struct {
	// ID 是那份副本的组件 ID。
	ID string
	// Dir 是副本所在的目录（绝对路径）。
	Dir string
	// Inside 是装着它的那个组件的 ID。
	Inside string
	// TopHasIt 表示项目的本地安装源里也有同一个 ID。
	TopHasIt bool
}

// NestedCopies 找出每个本地组件目录里的 components/<scope>/<name>/component.yaml。
// 只认带 component.yaml 的目录：git submodule 留下的空目录不是副本（设计 §6）。
// 按 Dir 排序，输出稳定。
func (p *Project) NestedCopies() ([]NestedCopy, error) {
	type local struct{ id, dir string }
	var components []local
	top := map[string]bool{}
	for _, s := range p.Decl.Sources {
		if s.Type != projfile.SourceTypeLocal || s.Path == "" {
			continue
		}
		found, err := componentDirs(p.Layout.Resolve(s.Path))
		if err != nil {
			return nil, err
		}
		for id, dir := range found {
			components = append(components, local{id, dir})
			top[id] = true
		}
	}
	var out []NestedCopy
	for _, c := range components {
		nested, err := componentDirs(filepath.Join(c.dir, DirComponents))
		if err != nil {
			return nil, err
		}
		for id, dir := range nested {
			out = append(out, NestedCopy{ID: id, Dir: dir, Inside: c.id, TopHasIt: top[id]})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Dir < out[j].Dir })
	return out, nil
}

// componentDirs 列出 root 下 <scope>/<name>/ 里带 component.yaml 的目录（与本地安装源的扫描同一个形状：
// 跳过以 . 开头的目录，比如 .archived）。root 不存在时返回空。
func componentDirs(root string) (map[string]string, error) {
	out := map[string]string{}
	scopes, err := os.ReadDir(root)
	if os.IsNotExist(err) {
		return out, nil
	}
	if err != nil {
		return nil, err
	}
	for _, scope := range scopes {
		if !scope.IsDir() || strings.HasPrefix(scope.Name(), ".") {
			continue
		}
		names, err := os.ReadDir(filepath.Join(root, scope.Name()))
		if err != nil {
			return nil, err
		}
		for _, name := range names {
			if !name.IsDir() || strings.HasPrefix(name.Name(), ".") {
				continue
			}
			dir := filepath.Join(root, scope.Name(), name.Name())
			if _, err := os.Stat(filepath.Join(dir, manifest.FileName)); err == nil {
				out[scope.Name()+"/"+name.Name()] = dir
			}
		}
	}
	return out, nil
}
