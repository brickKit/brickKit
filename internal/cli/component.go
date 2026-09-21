package cli

import (
	"bufio"
	"strings"

	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/config"
	"github.com/brickkit/brickkit/internal/i18n"
	"github.com/brickkit/brickkit/internal/manifest"
	"github.com/brickkit/brickkit/internal/msgid"
	"github.com/brickkit/brickkit/internal/resolver"
)

// parseComponentRef 解析命令行上的组件引用 `<组件ID>[@<精确版本>]`。
//
// 省略版本合法：add 由此触发"取安装源最新版本"，remove/fetch 由调用方按
// brickkit.yaml 中的条目推断，多版本时再要求指定。
func parseComponentRef(arg string) (id, version string, err error) {
	id, version, hasVersion := strings.Cut(strings.TrimSpace(arg), "@")

	if problem := manifest.ComponentIDProblem(id); problem != "" {
		return "", "", clierr.New(clierr.CodeInvalidArgument, i18n.T(msgid.InvalidComponentID, id)).
			WithDetail(i18n.T(msgid.LabelReason), problem).
			WithHint(i18n.T(msgid.HintComponentIDFormat)).
			WithExit(clierr.ExitUsage)
	}

	if !hasVersion {
		return id, "", nil
	}

	if !manifest.IsExactVersion(version) {
		return "", "", clierr.New(clierr.CodeInvalidArgument, i18n.T(msgid.SourceInvalidVersion, version)).
			WithDetail(i18n.T(msgid.LabelComponent), id).
			WithHint(i18n.T(msgid.SourceHintExactVersionOnly)).
			WithExit(clierr.ExitUsage)
	}
	return id, version, nil
}

// confirm 打印提示并读取一行输入，只有 y / yes 才算确认。
//
// 没有输入（CI 环境、管道结束）等价于拒绝：宁可什么都不做，也不要替使用者做决定。
func confirm(opts *Options, prompt string) bool {
	opts.Printf("%s", prompt)
	if opts.Stdin == nil {
		opts.Printf("\n")
		return false
	}
	line, _ := bufio.NewReader(opts.Stdin).ReadString('\n')
	opts.Printf("\n")
	switch strings.ToLower(strings.TrimSpace(line)) {
	case "y", "yes":
		return true
	default:
		return false
	}
}

// hasComponent 判断 brickkit.yaml 中是否已有该组件版本。
func hasComponent(cfg *config.Config, id, version string) bool {
	for _, c := range cfg.Components {
		if c.ID == id && c.Version == version {
			return true
		}
	}
	return false
}

// dependencyKinds 把依赖图中的节点分成"强依赖可达"与"仅弱依赖可达"两类，
// 用于输出时区分 `依赖` 与 `弱依赖`（004 §3.3 输出样例）。
func dependencyKinds(g *resolver.Graph) map[resolver.Ref]bool {
	optionalOnly := map[resolver.Ref]bool{}
	for _, n := range g.Nodes {
		for _, ref := range n.Optional {
			if _, seen := optionalOnly[ref]; !seen {
				optionalOnly[ref] = true
			}
		}
		for _, ref := range n.Requires {
			optionalOnly[ref] = false
		}
	}
	return optionalOnly
}
