package cli

import (
	"bufio"
	"strings"

	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/config"
	"github.com/brickkit/brickkit/internal/manifest"
	"github.com/brickkit/brickkit/internal/resolver"
)

// parseComponentRef 解析命令行上的组件引用 `<组件ID>[@<精确版本>]`。
//
// requireVersion 为 true 时（add）必须带版本；为 false 时（remove）允许省略，
// 由调用方按 brickkit.yaml 中的条目推断，多版本时再要求指定。
func parseComponentRef(arg string, requireVersion bool) (id, version string, err error) {
	id, version, hasVersion := strings.Cut(strings.TrimSpace(arg), "@")

	if problem := manifest.ComponentIDProblem(id); problem != "" {
		return "", "", clierr.Newf(clierr.CodeInvalidArgument, "错误：组件 ID 不合法：%s", id).
			WithDetail("原因", problem).
			WithHint("组件 ID 格式为 <scope>/<name>，如 people/basic（002 §2.3）").
			WithExit(clierr.ExitUsage)
	}

	if !hasVersion {
		if requireVersion {
			return "", "", clierr.New(clierr.CodeInvalidArgument, "错误：请指定精确版本").
				WithDetail("用法", "brickkit add <组件ID>@<精确版本>").
				WithDetail("示例", "brickkit add "+id+"@1.0.0").
				WithHint("BrickKit 只接受精确版本 major.minor.patch，不接受 ^ 或 ~ 范围约束（002 §3.3）").
				WithExit(clierr.ExitUsage)
		}
		return id, "", nil
	}

	if !manifest.IsExactVersion(version) {
		return "", "", clierr.Newf(clierr.CodeInvalidArgument, "错误：版本号不合法：%s", version).
			WithDetail("组件", id).
			WithHint("必须是精确版本 major.minor.patch，如 1.0.0；不接受 ^ 或 ~ 等范围约束（002 §3.3）").
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
