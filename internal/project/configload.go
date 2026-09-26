package project

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/configdir"
	"github.com/brickkit/brickkit/internal/i18n"
	"github.com/brickkit/brickkit/internal/msgid"
	"github.com/brickkit/brickkit/internal/yamlfile"
)

// loadConfig 读 config/：公共变量、部署文件的 vars:、每个组件版本对应的配置文件，
// 并检查文件名冲突、多版本歧义、孤儿文件与悬空的 $var: 引用（提案 §7、附录 A5）。
func (p *Project) loadConfig() error {
	if err := p.loadVars(); err != nil {
		return err
	}
	deployVars, err := configdir.ParseVarsMap(p.Deploy.Vars, p.DeployPath)
	if err != nil {
		return err
	}
	p.DeployVars = deployVars

	if err := p.checkConfigNames(); err != nil {
		return err
	}
	present, err := p.scanConfigDir()
	if err != nil {
		return err
	}

	p.configs = map[string]*configdir.File{}
	used := map[string]bool{}
	conflicts := &configdir.ConflictError{}
	for _, c := range p.Decl.Components {
		name := configdir.FileName(c.ID, c.Version)
		if !present[name] {
			name = configdir.FileName(c.ID, "")
			if !present[name] {
				continue
			}
			if versions := p.Decl.Versions(c.ID); len(versions) > 1 {
				return ambiguityError(name, c.ID, versions)
			}
		}
		used[name] = true

		path := filepath.Join(p.Layout.ConfigDir(), name)
		data, err := yamlfile.Read(path)
		if err != nil {
			return err
		}
		f, err := configdir.ParseComponentFile(data, path)
		var conflict *configdir.ConflictError
		if errors.As(err, &conflict) {
			conflicts.Merge(conflict)
			continue
		}
		if err != nil {
			return err
		}
		p.configs[c.Ref()] = f
	}
	if len(conflicts.Files) > 0 {
		return conflicts.Render()
	}

	names := make([]string, 0, len(present))
	for name := range present {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if !used[name] {
			p.Warnings = append(p.Warnings, clierr.Warn(clierr.CodeConfigInvalid,
				i18n.T(msgid.ProjectConfigOrphan, filepath.Join(DirConfig, name))))
		}
	}
	return p.checkVarRefs()
}

func (p *Project) loadVars() error {
	path := p.Layout.VarsPath()
	data, err := yamlfile.Read(path)
	if errors.Is(err, fs.ErrNotExist) {
		p.Vars = map[string]configdir.Value{}
		return nil
	}
	if err != nil {
		return err
	}
	f, err := configdir.ParseVarsFile(data, path)
	var conflict *configdir.ConflictError
	if errors.As(err, &conflict) {
		return conflict.Render()
	}
	if err != nil {
		return err
	}
	p.Vars = f.Map()
	return nil
}

// checkConfigNames 拦下"两个组件 ID 对应到同一个配置文件名"（a-b/c 与 a/b-c）。
func (p *Project) checkConfigNames() error {
	owner := map[string]string{}
	for _, id := range p.Decl.IDs() {
		base := configdir.FileBase(id)
		if prev, ok := owner[base]; ok {
			return clierr.New(clierr.CodeConfigInvalid,
				i18n.T(msgid.ProjectConfigNameCollision, prev, id, configdir.FileName(id, ""))).
				WithHint(i18n.T(msgid.ProjectHintConfigCollision))
		}
		owner[base] = id
	}
	return nil
}

// scanConfigDir 列出 config/ 下的组件配置文件名（不含 vars.yaml、子目录与其它文件）。
func (p *Project) scanConfigDir() (map[string]bool, error) {
	entries, err := os.ReadDir(p.Layout.ConfigDir())
	if errors.Is(err, fs.ErrNotExist) {
		return map[string]bool{}, nil
	}
	if err != nil {
		return nil, err
	}
	present := map[string]bool{}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if _, _, ok := configdir.ParseFileName(e.Name()); ok {
			present[e.Name()] = true
		}
	}
	return present, nil
}

func ambiguityError(name, id string, versions []string) *clierr.Error {
	perVersion := make([]string, 0, len(versions))
	for _, v := range versions {
		perVersion = append(perVersion, filepath.Join(DirConfig, configdir.FileName(id, v)))
	}
	return clierr.New(clierr.CodeConfigInvalid,
		i18n.T(msgid.ProjectConfigAmbiguous, name, id, strings.Join(versions, ", "))).
		WithHint(i18n.T(msgid.ProjectHintConfigAmbiguous, strings.Join(perVersion, ", ")))
}

// checkVarRefs 在装载阶段就拦下悬空的 $var:——lint 与 up 走同一处，不必等到解析某个组件。
func (p *Project) checkVarRefs() error {
	var dangling []string
	for _, c := range p.Decl.Components {
		f := p.configs[c.Ref()]
		if f == nil {
			continue
		}
		for _, e := range f.Entries {
			if e.Value.Kind != configdir.KindVarRef {
				continue
			}
			if _, ok := configdir.LookupVar(e.Value.Name, p.DeployVars, p.Vars); ok {
				continue
			}
			rel, err := filepath.Rel(p.Layout.Root, f.Path)
			if err != nil {
				rel = f.Path
			}
			dangling = append(dangling, rel+": "+e.Key+" → "+e.Value.String())
		}
	}
	if len(dangling) == 0 {
		return nil
	}
	err := clierr.New(clierr.CodeConfigInvalid, i18n.T(msgid.ProjectVarUndefined))
	for _, ref := range dangling {
		err = err.WithDetail(i18n.T(msgid.ConfigdirLabelUndefinedRef), ref)
	}
	return err.WithHint(i18n.T(msgid.ConfigdirHintDefineVar))
}
