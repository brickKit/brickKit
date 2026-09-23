package cli

// 本文件是"这个项目的拓扑是什么样"的共用部分：解析依赖图、算级联。
//
// up、down/status、sync、graph 四处问的都是同一个问题的不同侧面。前三处从前各自
// 写了一遍这两步——以后其中任何一步的调用方式变了，要记得同步三处，而
// "记得"正是会出事的地方。共用的只该是它们都需要的这一部分。

import (
	"context"
	"os"
	"path/filepath"

	"github.com/brickkit/brickkit/internal/cascade"
	"github.com/brickkit/brickkit/internal/config"
	"github.com/brickkit/brickkit/internal/i18n"
	"github.com/brickkit/brickkit/internal/msgid"
	"github.com/brickkit/brickkit/internal/override"
	"github.com/brickkit/brickkit/internal/resolver"
	"github.com/brickkit/brickkit/internal/source"
)

// resolveTopology 解析依赖图并算出级联状态（哪些组件这次会启动）。
//
// client 由调用方创建、也由调用方关闭：up 在这之后还要用同一个客户端补升级摘要，
// 不能在这里替它关。出错时两个结果都是 nil。
func resolveTopology(
	ctx context.Context, client *source.Client, cfg *config.Config,
) (*resolver.Graph, *cascade.Result, error) {
	graph, err := resolver.New(resolver.FromSource(client)).ResolveConfig(ctx, cfg)
	if err != nil {
		return nil, nil, err
	}
	states, err := cascade.Compute(cfg, graph)
	if err != nil {
		return nil, nil, err
	}
	return graph, states, nil
}

// clearServedBy 在内存里清空全部 servedBy 声明（--ignore-served-by 的实现）。
//
// 格式校验在 ParseConfigFile 里已经跑完，不会被这一步绕过。下游 resolver /
// shell.Resolve / compose / k8s 全部只读 ServedBy 这一个字段，没有任何一处维护
// 自己的派生状态，清空一次就够，不需要逐处打补丁。只改内存里的 cfg，从不写回
// 磁盘上的 brickkit.yaml。
//
// 只清空、不打印：up 用一行 ⚠️ 告诉使用者，graph 的 stdout 必须是纯 Mermaid，
// 只能写成注释——各自决定怎么说。
func clearServedBy(cfg *config.Config) {
	for i := range cfg.Components {
		cfg.Components[i].ServedBy = ""
	}
}

// isDefaultConfigFile 判断 --config 传入的路径，规整后是不是就是默认的
// brickkit.yaml。`--config ./brickkit.yaml`、`--config brickkit.yaml` 是同一份
// 文件的两种写法，不能被 filepath.Clean 之外的差异（多余的 `./` 前缀等）误判成
// "指到了别处"，从而错误地拒绝/忽略 override.yaml——设计书 §10 的多环境护栏只该
// 拦真正的 --config brickkit.prod.yaml，不该拦同一个文件的另一种写法。
func isDefaultConfigFile(path string) bool {
	return path == "" || filepath.Clean(path) == DefaultConfigFile
}

// isNonDefaultConfigRun 是 override.yaml 设计书 §10 多环境护栏的**唯一**判定入口：
// 只有针对**默认** brickkit.yaml 的这次运行才应用 override.yaml——--config 指到
// 别处时，存在的 override.yaml 必须被完全忽略（既不读也不写），并且要明说一声
// （既不能悄悄生效，也不能悄悄不提，两种沉默都会让使用者对"这次到底生效了什么"
// 产生错误的预期）。
//
// **每一个读或写 override.yaml 的入口都必须先过这一关**——不只是 up/sync/status/
// down 共用的 loadOverride，brickkit add/remove/lint 各自新增的 override.yaml
// 读写点同样要过（真机复现过一次：add/remove 漏了这道检查，对着 --config
// brickkit.prod.yaml 跑，会把默认 override.yaml 的内容整个改掉或删掉，评审 Important #1）。
func isNonDefaultConfigRun(opts *Options, layout config.Layout) bool {
	if isDefaultConfigFile(opts.ConfigPath) {
		return false
	}
	if _, err := os.Stat(layout.OverridePath()); err == nil {
		opts.Printf("%s\n", i18n.T(msgid.OverrideIgnoredNonDefaultConfig, opts.ConfigPath))
	}
	return true
}

// loadOverride 读取并校验 override.yaml（先过 isNonDefaultConfigRun 那道多环境护栏）。
//
// 文件不存在时返回 (nil, nil)：没有覆盖是完全合法、最常见的状态。
func loadOverride(opts *Options, layout config.Layout, cfg *config.Config) (*override.Override, error) {
	if isNonDefaultConfigRun(opts, layout) {
		return nil, nil
	}

	ov, err := override.ParseOverrideFile(layout.OverridePath())
	if err != nil || ov == nil {
		return ov, err
	}
	if err := override.CheckAgainst(cfg, ov); err != nil {
		return nil, err
	}
	return ov, nil
}

// overrideValues 是 applyOverride 从 override.yaml 的一个条目里只取出、apply 真正
// 关心的两个字段——ComponentOverride 与 MemberOverride 是两个不同的 Go 类型
// （internal/override 的类型定义：schemagen 的反射生成器不支持自引用递归类型），
// 展平成一份索引表时只需要两者共有的这两个值，不需要保留整个结构体。
type overrideValues struct {
	Mode      string
	LocalPort int
}

// applyOverride 把 override.yaml 的取值写进内存里的 cfg，从不写回磁盘——
// 跟 clearServedBy 同一个手法：一次性改完，下游 resolver / cascade / compose /
// k8s 全部只读 cfg 本身的字段，不需要单独知道"这个值是不是被覆盖过"。
//
// 改完之后重新跑一遍 config.ValidateAfterOverride：override.yaml 能写的
// Mode/LocalPort 两个字段，必须仍然满足 brickkit.yaml 自己对它们的规则
// （端口范围、端口冲突、mode 跟 replicas/servedBy 的组合）——不然 override.yaml
// 就成了绕开这些规则的后门（brickKit 反馈：一个 servedBy 成员能被 override.yaml
// 标成 mode: debug 而不报错）。返回的错误归因到 ov.Source（override.yaml
// 自己的路径），不是 brickkit.yaml。
//
// ov 为 nil 时什么都不做（调用方在没有 override.yaml 时无条件调用它也是安全的）。
func applyOverride(cfg *config.Config, ov *override.Override) error {
	if ov == nil {
		return nil
	}
	if ov.Target != "" {
		cfg.Deploy.Target = ov.Target
	}

	overrides := flattenOverrideValues(ov.Components)
	for i := range cfg.Components {
		o, ok := overrides[cfg.Components[i].ID]
		if !ok {
			continue
		}
		if o.Mode != "" {
			cfg.Components[i].Mode = o.Mode
		}
		if o.LocalPort != 0 {
			cfg.Components[i].LocalPort = o.LocalPort
		}
	}

	return cfg.ValidateAfterOverride(ov.Source)
}

// flattenOverrideValues 把顶层组件与它们嵌套的 members 展平成一份按组件 ID 索引的表——
// apply 只关心"这个 ID 有没有被覆盖"，不关心它在 override.yaml 里嵌在哪个外壳下面
// （那层嵌套纯粹是给人看的分组，设计书 §8）。
func flattenOverrideValues(entries []override.ComponentOverride) map[string]overrideValues {
	out := map[string]overrideValues{}
	for _, c := range entries {
		out[c.ID] = overrideValues{Mode: c.Mode, LocalPort: c.LocalPort}
		for _, m := range c.Members {
			out[m.ID] = overrideValues{Mode: m.Mode, LocalPort: m.LocalPort}
		}
	}
	return out
}
