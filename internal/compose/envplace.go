package compose

import (
	"sort"
	"strings"

	"github.com/brickkit/brickkit/internal/configdir"
	"github.com/brickkit/brickkit/internal/envref"
	"github.com/brickkit/brickkit/internal/machine"
)

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
func (r *renderer) placeEnvironment() error {
	undefined := map[string]bool{}
	for _, c := range append(append([]machine.Workload{}, r.Components()...), r.MemberMigrations()...) {
		var inline []string
		var file strings.Builder
		for _, v := range c.Env.Env {
			placement := machine.PlacementOf(v)
			if placement != machine.PlaceSkip && v.Value.Kind == configdir.KindEnvTemplate {
				r.CollectUndefined(v.Value.Text, undefined)
			}
			switch placement {
			case machine.PlaceSkip:
				continue
			case machine.PlaceInline:
				text := composeEscape(v.Value.Text)
				if v.Value.Kind == configdir.KindEnvTemplate {
					// ${NAME} 留给 compose 启动时展开，其余的 $ 都是字面量
					text = envref.EscapeLiterals(v.Value.Text)
				}
				inline = append(inline, v.Name+"="+text)
			case machine.PlaceSecret:
				if v.Value.Kind == configdir.KindEnvTemplate {
					file.WriteString(envFileLine(v.Name, envref.EscapeLiterals(v.Value.Text), false))
					continue
				}
				value, err := configdir.Evaluate(v.Value, r.Root(), r.Lookup())
				if err != nil {
					return machine.WithVariable(err, c.Ref, v.Name)
				}
				file.WriteString(envFileLine(v.Name, value, true))
			}
		}
		r.inline[c.Service] = inline
		if file.Len() > 0 {
			r.envFile[c.Service] = []byte(file.String())
		}
	}
	return machine.UndefinedError(undefined)
}

// applyEnvironment 把算好的环境变量挂到一个 service（主容器或迁移容器）上。
func (r *renderer) applyEnvironment(svc map[string]any, service string) {
	if inline := r.inline[service]; len(inline) > 0 {
		svc["environment"] = inline
	}
	if _, ok := r.envFile[service]; ok {
		svc["env_file"] = []any{map[string]any{"path": envFilePath(service)}}
	}
}

func envFilePath(service string) string { return EnvFileDir + "/" + service + ".env" }

// envFileList 按服务名排序返回全部 env 文件。
func (r *renderer) envFileList() []EnvFile {
	services := make([]string, 0, len(r.envFile))
	for service := range r.envFile {
		services = append(services, service)
	}
	sort.Strings(services)
	out := make([]EnvFile, 0, len(services))
	for _, service := range services {
		out = append(out, EnvFile{Service: service, Path: envFilePath(service), Content: r.envFile[service]})
	}
	return out
}
