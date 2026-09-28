package cli

import (
	"sort"
	"strings"

	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/configdir"
	"github.com/brickkit/brickkit/internal/deployfile"
	"github.com/brickkit/brickkit/internal/i18n"
	"github.com/brickkit/brickkit/internal/msgid"
	"github.com/brickkit/brickkit/internal/project"
	"github.com/brickkit/brickkit/internal/resolver"
)

// secretishKey 判断一个配置项名字看不看得出是密钥。
//
// **判据刻意收窄，宁可漏报也不误报。** 一个见谁都喊的告警，两天之内就会被
// 所有人无视——所以只认那些几乎不可能有别的含义的词，而不是"包含 key 就算"
// （API_KEY 是密钥，但 SORT_KEY、CACHE_KEY 不是）。
func secretishKey(name string) bool {
	lower := strings.ToLower(name)
	for _, word := range []string{
		"password", "passwd", "secret", "token", "credential",
		"privatekey", "private_key", "apikey", "api_key", "accesskey", "access_key",
	} {
		if strings.Contains(lower, word) {
			return true
		}
	}
	return false
}

// declaredSecretKeys 返回组件在 configSchema 里声明了 secret: true 的配置项。
// graph 里没有这个组件时返回 nil，退回按名字判断。
func declaredSecretKeys(graph *resolver.Graph, ref resolver.Ref) map[string]bool {
	if graph == nil {
		return nil
	}
	node := graph.Node(ref)
	if node == nil || node.Manifest == nil || node.Manifest.ConfigSchema == nil {
		return nil
	}
	out := map[string]bool{}
	for key, property := range node.Manifest.ConfigSchema.Properties {
		if property.Secret {
			out[key] = true
		}
	}
	return out
}

// warnConfigSecrets 提醒 config/ 里写了明文密钥。
//
// 泄漏路径是 config/*.yaml 与 config/vars.yaml 本身：它们是要提交进 Git 的。
// 判据是：组件声明了 secret: true，或名字长得像密钥（只看名字，不看值）；
// 写成 ${ENV_VAR}、file://、existingSecret 的都是做对了。经 $var: 引到 vars.yaml
// 里的明文，点名的是那个公共变量；来自个人 deploy.local.yaml 的不算（它不进 Git）。
// **绝不打印值本身。**
func warnConfigSecrets(opts *Options, p *project.Project, graph *resolver.Graph) {
	var offenders []string
	for _, c := range p.Decl.Components {
		f := p.Config(c.ID, c.Version)
		if f == nil {
			continue
		}
		declared := declaredSecretKeys(graph, resolver.Ref{ID: c.ID, Version: c.Version})
		for _, e := range f.Entries {
			if !declared[e.Key] && !secretishKey(e.Key) {
				continue
			}
			value, where := e.Value, c.Ref()+" → "+e.Key
			if value.Kind == configdir.KindVarRef {
				target, ok := configdir.LookupVar(value.Name, p.DeployVars, p.Vars)
				if !ok {
					continue
				}
				if _, fromDeploy := p.DeployVars[value.Name]; fromDeploy && p.DeploySource == project.DeployLocal {
					// 个人的 deploy.local.yaml 不进 Git：本地口令写在那里正是它的用处
					continue
				}
				value, where = target, where+" ($var:"+value.Name+")"
			}
			if value.Kind == configdir.KindLiteral && !value.IsUnset() && !value.IsEmpty() {
				offenders = append(offenders, where)
			}
		}
	}
	if len(offenders) == 0 {
		return
	}
	sort.Strings(offenders)

	renderWarnings(opts, []*clierr.Error{
		clierr.Warn(clierr.CodeConfigInvalid, i18n.T(msgid.CliUpSecretsTheConfigInBrickkitYaml)).
			WithDetail(i18n.T(msgid.CliUpSecretsConfigItems), strings.Join(offenders, i18n.T(msgid.ListSeparator))).
			WithDetail(i18n.T(msgid.CliUpSecretsWhyItMatters), i18n.T(msgid.CliUpSecretsBrickkitYamlIsMeantTo)).
			WithHint(
				i18n.T(msgid.CliUpSecretsChangeItToAReference),
				i18n.T(msgid.CliUpSecretsEnvMustBeListedIn),
				i18n.T(msgid.CliUpSecretsItemsThatDeclareSecretTrue),
			),
	})
}

// warnExistingSecretOnDocker 提醒 existingSecret 写法在 Docker 下不生效：
// Docker 没有"引用外部已建好的 Secret"这回事，这一项在容器里表现成"没配"。
// （没声明 secret: true 的那一种由 configdir.Resolve 报，这里不重复。）
func warnExistingSecretOnDocker(opts *Options, p *project.Project) {
	if p.Deploy.Target == deployfile.TargetK8s {
		return
	}
	var refs []string
	for _, c := range p.Decl.Components {
		f := p.Config(c.ID, c.Version)
		for _, e := range entriesOf(f) {
			if e.Value.Kind == configdir.KindSecretRef {
				refs = append(refs, c.Ref()+" → "+e.Key)
			}
		}
	}
	if len(refs) == 0 {
		return
	}
	sort.Strings(refs)
	renderWarnings(opts, []*clierr.Error{
		clierr.Warn(clierr.CodeConfigInvalid, i18n.T(msgid.CliUpSecretsExistingsecretOnlyWorksOnK8s)).
			WithDetail(i18n.T(msgid.CliUpSecretsConfigItems), strings.Join(refs, i18n.T(msgid.ListSeparator))).
			WithHint(i18n.T(msgid.CliUpSecretsDockerHasNoConceptOf)),
	})
}

func entriesOf(f *configdir.File) []configdir.Entry {
	if f == nil {
		return nil
	}
	return f.Entries
}
