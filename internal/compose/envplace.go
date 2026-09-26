package compose

import (
	"sort"
	"strings"

	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/configdir"
	"github.com/brickkit/brickkit/internal/envref"
	"github.com/brickkit/brickkit/internal/i18n"
	"github.com/brickkit/brickkit/internal/inject"
	"github.com/brickkit/brickkit/internal/msgid"
	"github.com/brickkit/brickkit/internal/resolver"
)

// placement 是一条变量在 Docker 目标下的去处（附录 A6/A7）。
type placement int

const (
	placeInline  placement = iota // 写进 compose.yaml 的 environment
	placeEnvFile                  // 写进 0600 的 env 文件
	placeSkip                     // Docker 没有对应概念（existingSecret）
)

// envPlacement 是 Docker 目标下"这条变量放哪"的唯一判定处：
// 密钥与 file:// 内容一律不进 compose.yaml——那份文件会被人打开看、进 git diff。
func envPlacement(v inject.Var) placement {
	switch {
	case v.Value.Kind == configdir.KindSecretRef:
		return placeSkip
	case v.Value.Kind == configdir.KindFileRef, v.Secret:
		return placeEnvFile
	default:
		return placeInline
	}
}

// composeEscape 让字面量不被 compose 插值：$ → $$。
// 不转义的话，直接写在配置里的密码 pa$$word 到容器里就变了样。
func composeEscape(s string) string { return strings.ReplaceAll(s, "$", "$$") }

// envFileLine 写一行 NAME="value"。literal 为 true 时 $ 也转义（字面量、文件内容），
// 为 false 时保留 ${VAR}，让 compose 在启动时插值（写成模板的密钥）。
//
// 转义规则实测于 Compose v5.3.1（2026-09-26）：多行 PEM、$、引号、反斜杠逐字节到达容器。
func envFileLine(name, value string, literal bool) string {
	var b strings.Builder
	b.WriteString(name)
	b.WriteString(`="`)
	for _, r := range value {
		switch r {
		case '\\':
			b.WriteString(`\\`)
		case '"':
			b.WriteString(`\"`)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '$':
			if literal {
				b.WriteString("$$")
			} else {
				b.WriteRune(r)
			}
		default:
			b.WriteRune(r)
		}
	}
	b.WriteString("\"\n")
	return b.String()
}

// placeEnvironment 为每个要生成容器的服务算好 inline 与 env 文件两份内容。
// 必须在外壳合并（applyShellGroups）之后跑：外壳的环境变量那时才齐。
func (p *plan) placeEnvironment() error {
	for _, c := range append(append([]componentPlan{}, p.components...), p.memberMigrations...) {
		var inline []string
		var file strings.Builder
		for _, v := range c.Env.Env {
			switch envPlacement(v) {
			case placeSkip:
				continue
			case placeInline:
				text := composeEscape(v.Value.Text)
				if v.Value.Kind == configdir.KindEnvTemplate {
					// ${NAME} 留给 compose 启动时展开，其余的 $ 都是字面量
					text = envref.EscapeLiterals(v.Value.Text)
				}
				inline = append(inline, v.Name+"="+text)
			case placeEnvFile:
				if v.Value.Kind == configdir.KindEnvTemplate {
					file.WriteString(envFileLine(v.Name, envref.EscapeLiterals(v.Value.Text), false))
					continue
				}
				value, err := configdir.Evaluate(v.Value, p.root, p.lookup)
				if err != nil {
					return withVariable(err, c.Ref, v.Name)
				}
				file.WriteString(envFileLine(v.Name, value, true))
			}
		}
		p.inline[c.Service] = inline
		if file.Len() > 0 {
			p.envFile[c.Service] = []byte(file.String())
		}
	}
	return nil
}

// withVariable 给求值错误补上"是哪个组件的哪一项"。
func withVariable(err error, ref resolver.Ref, name string) error {
	e := clierr.As(err)
	if e == nil {
		return err
	}
	return e.WithDetail(i18n.T(msgid.LabelComponent), ref.String()).
		WithDetail(i18n.T(msgid.LabelConfigKey), name)
}

// applyEnvironment 把算好的环境变量挂到一个 service（主容器或迁移容器）上。
func (p *plan) applyEnvironment(svc map[string]any, service string) {
	if inline := p.inline[service]; len(inline) > 0 {
		svc["environment"] = inline
	}
	if _, ok := p.envFile[service]; ok {
		svc["env_file"] = []any{map[string]any{"path": envFilePath(service)}}
	}
}

func envFilePath(service string) string { return EnvFileDir + "/" + service + ".env" }

// envFileList 按服务名排序返回全部 env 文件。
func (p *plan) envFileList() []EnvFile {
	services := make([]string, 0, len(p.envFile))
	for service := range p.envFile {
		services = append(services, service)
	}
	sort.Strings(services)
	out := make([]EnvFile, 0, len(services))
	for _, service := range services {
		out = append(out, EnvFile{Service: service, Path: envFilePath(service), Content: p.envFile[service]})
	}
	return out
}
