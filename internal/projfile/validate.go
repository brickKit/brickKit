package projfile

import (
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
				case c.IsShell() && first.Version != c.Version:
					p.Add(field, i18n.T(msgid.ProjfileShellSingleVersion, c.ID, prevField))
				}
			} else {
				firstOfID[c.ID] = i
			}
		}

		validateComponentSource(p, field, c.Source)
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
	case SourceTypeLocal:
		requireFor(p, field, "path", s.Path, s.Type)
	default:
		p.Add(field+".type", i18n.T(msgid.ProjfileComponentSourceTypeInvalid, s.Type))
	}
}
