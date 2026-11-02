package machine

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

// Placement 是一条变量在容器里的去处。
type Placement int

const (
	// PlaceInline：写进生成文件本身（compose.yaml 的 environment、Quadlet 单元的 Environment=）。
	PlaceInline Placement = iota
	// PlaceSecret：不进生成文件——它会被人打开看、进 git diff（compose 的 0600 env 文件、Podman secret）。
	PlaceSecret
	// PlaceSkip：一台机器上没有对应概念（existingSecret 引用的是 K8s 的 Secret）。
	PlaceSkip
)

// PlacementOf 是一台机器上"这条变量放哪"的唯一判定处：
// 密钥与 file:// 内容一律不进生成文件。
func PlacementOf(v inject.Var) Placement {
	switch {
	case v.Value.Kind == configdir.KindSecretRef:
		return PlaceSkip
	case v.Value.Kind == configdir.KindFileRef, v.Secret:
		return PlaceSecret
	default:
		return PlaceInline
	}
}

// CollectUndefined 记下模板里用渲染器求值时会看到的同一个查找（进程环境，其次 .env）找不到的 ${NAME}。
// compose 把 ${NAME} 留给 docker compose 在启动时展开，生成时就能知道它展不展得开：compose 把展不开的换成空字符串、
// 只在它自己的输出里警告，而 up 成功时那段输出没人看得到——组件带着残缺的值跑起来。
// K8s 与 Quadlet 在生成时求值、展不开就失败；三者说法一致。${NAME:-默认值} 不算。
func (p *Plan) CollectUndefined(template string, undefined map[string]bool) {
	for _, name := range envref.Required(template) {
		if p.lookup == nil {
			undefined[name] = true
			continue
		}
		if _, ok := p.lookup(name); !ok {
			undefined[name] = true
		}
	}
}

// UndefinedError 是 CollectUndefined 收集到的变量的报错；一个都没有时返回 nil。
func UndefinedError(undefined map[string]bool) error {
	if len(undefined) == 0 {
		return nil
	}
	names := make([]string, 0, len(undefined))
	for name := range undefined {
		names = append(names, name)
	}
	sort.Strings(names)
	return clierr.New(clierr.CodeConfigInvalid, i18n.T(msgid.ComposeEnvVarsUndefined)).
		WithDetail(i18n.T(msgid.K8sLabelMissingVars), strings.Join(names, i18n.T(msgid.ListSeparator))).
		WithDetail(i18n.T(msgid.LabelReason), i18n.T(msgid.ComposeEnvVarsReasonDetail)).
		WithHint(i18n.T(msgid.K8sHintDefineEnvVars), i18n.T(msgid.K8sHintEnvVarDefault))
}

// WithVariable 给求值错误补上"是哪个组件的哪一项"。
func WithVariable(err error, ref resolver.Ref, name string) error {
	e := clierr.As(err)
	if e == nil {
		return err
	}
	return e.WithDetail(i18n.T(msgid.LabelComponent), ref.String()).
		WithDetail(i18n.T(msgid.LabelConfigKey), name)
}

// Root 是项目根：file:// 相对它求值。Lookup 是 ${VAR} 的查找（进程环境，其次 .env）。
func (p *Plan) Root() string                             { return p.root }
func (p *Plan) Lookup() func(name string) (string, bool) { return p.lookup }
