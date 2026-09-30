package cli

// 本文件是 lint 的配置检查：必填项有没有值、configSchema 里没有的键、
// --strict 下 ${VAR} 与 file:// 取不取得到、外壳成员的值能不能装进外壳的 JSON；
// 以及外壳声明（kind: shell 与 shell 块一致、放进外壳的成员确实编进了它）。
//
// 仍然离线：每个组件版本的 configSchema 只从盘上读——permanent 缓存 .brickkit/manifests/
// （git、market 组件 add 过就在那里），或者正好是这个版本的本地源目录。两处都没有的组件
// 查不了，列一条说明，不去安装源取（那可能联网，lint 的承诺就破了）。
//
// 规则本身不是新的：必填与未知键走 up 同一处（configdir.Resolve、inject.MissingRequiredError），
// 成员值走 shell.CheckMemberValue。

import (
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/configdir"
	"github.com/brickkit/brickkit/internal/envref"
	"github.com/brickkit/brickkit/internal/i18n"
	"github.com/brickkit/brickkit/internal/inject"
	"github.com/brickkit/brickkit/internal/manifest"
	"github.com/brickkit/brickkit/internal/msgid"
	"github.com/brickkit/brickkit/internal/project"
	"github.com/brickkit/brickkit/internal/resolver"
	"github.com/brickkit/brickkit/internal/shell"
)

// lintConfigResult 是配置检查的结论。
type lintConfigResult struct {
	errors, warnings []*clierr.Error
	// unchecked 是盘上找不到 Manifest、没能检查的组件版本。
	unchecked []string
	// unreadable 是盘上有 Manifest 但解析不了的组件版本（→ 原因）。
	unreadable [][2]string
}

// lintConfig 检查项目里每个组件版本的配置。strict 时才查 ${VAR} 与 file:// 取不取得到。
func lintConfig(proj *project.Project, strict bool) lintConfigResult {
	var res lintConfigResult
	members := memberEntries(proj)
	lookup := envref.Lookup(proj.Layout.Root)
	missing := map[resolver.Ref][]string{}

	for _, c := range proj.Decl.Components {
		ref := resolver.Ref{ID: c.ID, Version: c.Version}
		m, problem, found := diskManifest(proj, c.ID, c.Version)
		switch {
		case !found:
			res.unchecked = append(res.unchecked, ref.String())
			continue
		case problem != "":
			res.unreadable = append(res.unreadable, [2]string{ref.String(), problem})
			continue
		}
		if err := shell.CheckDeclaration(proj, ref, c.IsShell(), m); err != nil {
			res.errors = append(res.errors, err)
		}
		resolved, err := configdir.Resolve(proj.ConfigInput(c.ID, c.Version, m.ConfigSchema))
		if err != nil {
			res.errors = append(res.errors, clierr.As(err))
			continue
		}
		res.warnings = append(res.warnings, resolved.Warnings...)
		if len(resolved.Missing) > 0 {
			missing[ref] = resolved.Missing
		}
		if strict {
			res.warnings = append(res.warnings, referenceWarnings(proj, ref, resolved, lookup)...)
		}
		if members[ref] {
			res.errors = append(res.errors, memberValueErrors(proj, ref, resolved, strict, lookup)...)
		}
	}
	if err := inject.MissingRequiredError(proj, missing); err != nil {
		res.errors = append(res.errors, err)
	}
	return res
}

// diskManifest 读一个组件版本在盘上的 Manifest，顺序与 up 取 Manifest 一致：本地源目录里正好是
// 这个版本时只认它（本地源从不走缓存——改了 component.yaml，下一次 up 就按新的来），否则看永久缓存。
// found 为假表示两处都没有；problem 非空表示找到了但解析不了。
func diskManifest(proj *project.Project, id, version string) (m *manifest.Manifest, problem string, found bool) {
	if dir, ok := proj.LocalRepo(id); ok {
		if v, err := project.LocalRepoVersion(dir); err == nil && v == version {
			m, err := manifest.ParseFile(filepath.Join(dir, manifest.FileName))
			if err != nil {
				// 本地 component.yaml 的问题 lint 在后面逐文件报，这里只说没查成
				return nil, clierr.As(err).Message, true
			}
			return m, "", true
		}
	}
	path := proj.Layout.CachedManifestPath(id, version)
	if _, err := os.Stat(path); err != nil {
		return nil, "", false
	}
	m, err := manifest.ParseFile(path)
	if err != nil {
		return nil, clierr.As(err).Message, true
	}
	return m, "", true
}

// memberEntries 是部署文件里嵌在外壳条目下面的成员（它们的配置要 JSON 编码进外壳）。
func memberEntries(proj *project.Project) map[resolver.Ref]bool {
	out := map[resolver.Ref]bool{}
	for _, entry := range proj.Deploy.Components {
		for _, member := range entry.Members {
			id, version := member.Key()
			if version == "" {
				version, _ = proj.Decl.DefaultVersion(id)
			}
			out[resolver.Ref{ID: id, Version: version}] = true
		}
	}
	return out
}

// referenceWarnings：${VAR} 在进程环境与 .env 里都找不到、file:// 指向的文件不存在。
// 只在 --strict 下查——平常这些多半是 CI 或部署机上才有的东西。
func referenceWarnings(proj *project.Project, ref resolver.Ref, resolved *configdir.Result, lookup func(string) (string, bool)) []*clierr.Error {
	var out []*clierr.Error
	for _, r := range resolved.Values {
		switch r.Value.Kind {
		case configdir.KindEnvTemplate:
			var unset []string
			for _, name := range envref.Required(r.Value.Text) {
				if _, ok := lookup(name); !ok {
					unset = append(unset, name)
				}
			}
			if len(unset) > 0 {
				sort.Strings(unset)
				out = append(out, clierr.Warn(clierr.CodeConfigInvalid, i18n.T(msgid.CliLintEnvRefUnset, ref.String(), r.Key)).
					WithDetail(i18n.T(msgid.CliLintLabelVariable), strings.Join(unset, ", ")).
					WithHint(i18n.T(msgid.CliLintHintEnvRef)))
			}
		case configdir.KindFileRef:
			path := r.Value.Path
			if !filepath.IsAbs(path) {
				path = filepath.Join(proj.Layout.Root, path)
			}
			if _, err := os.Stat(path); err != nil {
				out = append(out, clierr.Warn(clierr.CodeConfigInvalid, i18n.T(msgid.CliLintFileRefMissing, ref.String(), r.Key)).
					WithDetail(i18n.T(msgid.LabelPath), r.Value.Path).
					WithHint(i18n.T(msgid.ConfigdirHintFileRef)))
			}
		}
	}
	return out
}

// memberValueErrors 把成员的值求出来，确认每一个都装得进外壳的 JSON。字面量与 file:// 总是求；
// ${VAR} 只在 --strict 下求（平常变量多半不在本机）。file:// 读不到归 --strict 的引用检查管。
func memberValueErrors(proj *project.Project, ref resolver.Ref, resolved *configdir.Result, strict bool, lookup func(string) (string, bool)) []*clierr.Error {
	var out []*clierr.Error
	for _, r := range resolved.Values {
		switch r.Value.Kind {
		case configdir.KindLiteral, configdir.KindFileRef:
		case configdir.KindEnvTemplate:
			if !strict {
				continue
			}
		default:
			continue
		}
		value, err := configdir.Evaluate(r.Value, proj.Layout.Root, lookup)
		if err != nil {
			continue
		}
		if err := shell.CheckMemberValue(ref, r.Key, value); err != nil {
			out = append(out, clierr.As(err))
		}
	}
	return out
}
