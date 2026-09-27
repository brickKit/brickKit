package projfile

import (
	"strings"

	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/i18n"
	"github.com/brickkit/brickkit/internal/manifest"
	"github.com/brickkit/brickkit/internal/msgid"
	"github.com/brickkit/brickkit/internal/yamlfile"
)

// Validate 校验全部字段，一次报出所有问题。
func (f *File) Validate() error {
	p := newProblems(f.Source)
	f.validateProject(p)
	f.validateSources(p)
	f.validateComponents(p)
	return p.Err()
}

func (f *File) validateProject(p *clierr.ProblemSet) {
	if f.Project == "" {
		p.Missing("project")
		return
	}
	if reason := ProjectNameProblem(f.Project); reason != "" {
		p.Add("project", i18n.T(msgid.ConfigProjectNameProblemWithRule, reason, ProjectNameRule()))
	}
}

func (f *File) validateSources(p *clierr.ProblemSet) {
	seen := map[string]int{}
	for i, s := range f.Sources {
		field := yamlfile.Indexed("sources", i)
		if s.Name == "" {
			p.Missing(field + ".name")
		} else if prev, ok := seen[s.Name]; ok {
			p.Add(field+".name", i18n.T(msgid.ProjfileSourceNameDuplicate, yamlfile.Indexed("sources", prev)))
		} else {
			seen[s.Name] = i
		}

		switch s.Type {
		case "":
			p.Missing(field + ".type")
		case SourceTypeMarket:
			requireFor(p, field, "url", s.URL, s.Type)
		case SourceTypeGit:
			requireFor(p, field, "baseUrl", s.BaseURL, s.Type)
			rejectOptionLike(p, field+".baseUrl", s.BaseURL)
		case SourceTypeLocal:
			requireFor(p, field, "path", s.Path, s.Type)
		default:
			p.Add(field+".type", i18n.T(msgid.ProblemMustBeOneOfThree,
				SourceTypeMarket, SourceTypeGit, SourceTypeLocal, s.Type))
		}
	}
}

func requireFor(p *clierr.ProblemSet, field, key, value, typ string) {
	if value == "" {
		p.Add(field+"."+key, i18n.T(msgid.ProjfileFieldRequiredFor, key, typ))
	}
}

func (f *File) validateComponents(p *clierr.ProblemSet) {
	seen := map[string]int{}
	firstOfID := map[string]int{}
	for i, c := range f.Components {
		field := yamlfile.Indexed("components", i)

		if c.ID == "" {
			p.Missing(field + ".id")
		} else if reason := manifest.ComponentIDProblem(c.ID); reason != "" {
			p.Add(field+".id", reason)
		}
		switch {
		case c.Version == "":
			p.Missing(field + ".version")
		case !manifest.IsExactVersion(c.Version):
			p.Add(field+".version", i18n.T(msgid.ConfigVersionNotExactRange, c.Version))
		}
		if c.ID != "" && c.Version != "" {
			if prev, ok := seen[c.Ref()]; ok {
				p.Add(field, i18n.T(msgid.ConfigComponentDuplicate, yamlfile.Indexed("components", prev), c.Ref()))
			} else {
				seen[c.Ref()] = i
			}
		}

		switch c.Kind {
		case "", KindShell:
		default:
			p.Add(field+".kind", i18n.T(msgid.ProjfileKindInvalid, c.Kind))
		}

		if c.ID != "" {
			if prev, ok := firstOfID[c.ID]; ok {
				first := f.Components[prev]
				prevField := yamlfile.Indexed("components", prev)
				switch {
				case first.Kind != c.Kind:
					p.Add(field+".kind", i18n.T(msgid.ProjfileKindInconsistent, c.ID, prevField))
				case !sameSource(first.Source, c.Source):
					// 来源按组件 ID 认（一个组件一个仓库）：几行各写各的，取哪个版本就成了看运气
					p.Add(field+".source", i18n.T(msgid.ProjfileSourceInconsistent, c.ID, prevField))
				case c.IsShell() && first.Version != c.Version:
					p.Add(field, i18n.T(msgid.ProjfileShellSingleVersion, c.ID, prevField))
				}
			} else {
				firstOfID[c.ID] = i
			}
		}

		validateComponentSource(p, field, c.Source)
	}
	f.validateDefaults(p)
}

// validateDefaults 核对默认版本：同一个 ID 恰好一行不带 requiredBy；requiredBy 点名的是
// 项目里另一个组件；外壳只有一个版本，不存在"因依赖而存在"的外壳版本。
func (f *File) validateDefaults(p *clierr.ProblemSet) {
	declared := map[string]bool{}
	for _, c := range f.Components {
		declared[c.ID] = true
	}
	defaultAt := map[string]int{}
	lastAt := map[string]int{}
	for i, c := range f.Components {
		if c.ID == "" {
			continue
		}
		field := yamlfile.Indexed("components", i)
		lastAt[c.ID] = i
		if c.IsShell() && len(c.RequiredBy) > 0 {
			p.Add(field+".requiredBy", i18n.T(msgid.ProjfileRequiredByShell, c.ID))
			continue
		}
		for j, dependent := range c.RequiredBy {
			switch {
			case dependent == c.ID:
				p.Add(yamlfile.Indexed(field+".requiredBy", j), i18n.T(msgid.ProjfileRequiredBySelf))
			case !declared[dependent]:
				p.Add(yamlfile.Indexed(field+".requiredBy", j), i18n.T(msgid.ProjfileRequiredByUnknown, dependent))
			}
		}
		if len(c.RequiredBy) > 0 {
			continue
		}
		if prev, ok := defaultAt[c.ID]; ok {
			p.Add(field, i18n.T(msgid.ProjfileDefaultTwice, c.ID, yamlfile.Indexed("components", prev)))
			continue
		}
		defaultAt[c.ID] = i
	}
	for _, id := range f.IDs() {
		if _, ok := defaultAt[id]; !ok && id != "" {
			p.Add(yamlfile.Indexed("components", lastAt[id]), i18n.T(msgid.ProjfileDefaultMissing, id))
		}
	}
}

func validateComponentSource(p *clierr.ProblemSet, field string, s *ComponentSource) {
	if s == nil {
		return
	}
	field += ".source"
	switch s.Type {
	case "":
		p.Missing(field + ".type")
	case SourceTypeGit:
		requireFor(p, field, "repo", s.Repo, s.Type)
		rejectOptionLike(p, field+".repo", s.Repo)
		// git 源的 path 是组件在仓库里的子目录（monorepo，附录 A9）：只能往仓库里面指
		if s.Path != "" && !isInsideRelative(s.Path) {
			p.Add(field+".path", i18n.T(msgid.ProjfileSourcePathOutsideRepo, s.Path))
		}
	case SourceTypeLocal:
		requireFor(p, field, "path", s.Path, s.Type)
	default:
		p.Add(field+".type", i18n.T(msgid.ProjfileComponentSourceTypeInvalid, s.Type))
	}
}

// rejectOptionLike：仓库地址以 - 开头会被 git 当成参数（--upload-pack=… 能执行任意命令），
// 而项目文件可能来自别人。
func rejectOptionLike(p *clierr.ProblemSet, field, value string) {
	if strings.HasPrefix(strings.TrimSpace(value), "-") {
		p.Add(field, i18n.T(msgid.ProjfileRepoLooksLikeOption, value))
	}
}

// isInsideRelative 判断 path 是相对路径，且不会经 .. 走到起点外面。
func isInsideRelative(path string) bool {
	path = strings.ReplaceAll(path, "\\", "/")
	if strings.HasPrefix(path, "/") || (len(path) > 1 && path[1] == ':') {
		return false
	}
	for _, seg := range strings.Split(path, "/") {
		if seg == ".." {
			return false
		}
	}
	return true
}

// sameSource 报告两行的组件级来源是否相同（都没写也算相同）。
func sameSource(a, b *ComponentSource) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}
