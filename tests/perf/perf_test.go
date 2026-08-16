// Package perf 是性能基准（开发计划 Step 36）。
//
// # 为什么这里几乎没有时间断言
//
// 计划里写的是'''"50 个组件 up < 30 秒"、"10 层依赖解析 < 5 秒"'''这类阈值。
// 那是**人跑一次时的验收标准**，不适合直接写成自动化断言：
//
//	机器快    阈值永远满足，测了等于没测
//	CI 负载高 阈值随机失败，几次之后所有人开始无视它
//
// 所以这里分成两类：
//
//	Benchmark*  报告数字（ns/op、B/op、allocs/op），让**回归可见**。
//	            它不判定对错——判定交给人看趋势，或将来接进 benchstat。
//	Test*       只守**灾难性回归**：阈值放得极宽（秒级），
//	            正常机器上快好几个数量级，只有算法退化成指数级才会red。
//
// 换句话说：数字用来看，断言用来兜底。把两者混在一起，
// 得到的是一个既不敢收紧、又挡不住真问题的阈值。
//
// # 覆盖范围
//
// 只测**纯计算**那几层：解析、依赖解析、拓扑排序、生成。
// 涉及 Docker 与网络的（36.1 up、36.4 artifacts 下载、36.8 status、36.9 sync）
// 不在这里——它们的耗时由镜像大小、网络、磁盘决定，测出来的数字
// 说明不了 BrickKit 的性能，只说明当时那台机器的状态。
package perf

import (
	"context"
	"fmt"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/brickkit/brickkit/internal/cascade"
	"github.com/brickkit/brickkit/internal/compose"
	"github.com/brickkit/brickkit/internal/config"
	"github.com/brickkit/brickkit/internal/inject"
	"github.com/brickkit/brickkit/internal/manifest"
	"github.com/brickkit/brickkit/internal/resolver"
)

// ============================================================
// 夹具
// ============================================================

// provider 是一份内存里的 Manifest 表。
type provider map[string]*manifest.Manifest

func (p provider) Manifest(_ context.Context, id, version string) (*manifest.Manifest, error) {
	m, ok := p[id+"@"+version]
	if !ok {
		return nil, fmt.Errorf("没有 %s@%s", id, version)
	}
	return m, nil
}

// componentManifest 造一个最小组件，requires 是它的强依赖。
func componentManifest(id string, requires ...string) *manifest.Manifest {
	m := &manifest.Manifest{
		APIVersion: manifest.APIVersion,
		Kind:       manifest.Kind,
		Metadata:   manifest.Metadata{ID: id, Name: id, Version: "1.0.0"},
		Deployment: manifest.Deployment{
			Type:  manifest.DeploymentTypeContainer,
			Image: "registry.example.com/" + strings.ReplaceAll(id, "/", "-") + ":1.0.0",
			Port:  8080,
		},
		HealthCheck: manifest.HealthCheck{Type: manifest.HealthCheckHTTP, Path: "/healthz"},
	}
	if len(requires) > 0 {
		m.Dependencies = &manifest.Dependencies{}
		for _, r := range requires {
			m.Dependencies.Components = append(m.Dependencies.Components,
				manifest.ComponentDep{ID: r, Version: "1.0.0"})
		}
	}
	return m
}

// deepChain 造一条 depth 层的依赖链：n00 → n01 → … → n(depth-1)。
func deepChain(depth int) (provider, resolver.Ref) {
	p := provider{}
	for i := 0; i < depth; i++ {
		id := fmt.Sprintf("x/n%03d", i)
		var requires []string
		if i < depth-1 {
			requires = append(requires, fmt.Sprintf("x/n%03d", i+1))
		}
		p[id+"@1.0.0"] = componentManifest(id, requires...)
	}
	return p, resolver.Ref{ID: "x/n000", Version: "1.0.0"}
}

// wideGraph 造一个根组件 + width 个平级依赖。
func wideGraph(width int) (provider, resolver.Ref) {
	p := provider{}
	var requires []string
	for i := 0; i < width; i++ {
		id := fmt.Sprintf("dep/d%03d", i)
		p[id+"@1.0.0"] = componentManifest(id)
		requires = append(requires, id)
	}
	p["app/root@1.0.0"] = componentManifest("app/root", requires...)
	return p, resolver.Ref{ID: "app/root", Version: "1.0.0"}
}

// flatProject 造一份含 n 个互不依赖组件的项目（配置 + provider）。
func flatProject(n int) (*config.Config, provider, []resolver.Ref) {
	cfg := &config.Config{Project: "perf", Deploy: config.Deploy{Target: config.TargetDocker}}
	p := provider{}
	refs := make([]resolver.Ref, 0, n)
	for i := 0; i < n; i++ {
		id := fmt.Sprintf("svc/s%03d", i)
		p[id+"@1.0.0"] = componentManifest(id)
		cfg.Components = append(cfg.Components, config.Component{ID: id, Version: "1.0.0"})
		refs = append(refs, resolver.Ref{ID: id, Version: "1.0.0"})
	}
	return cfg, p, refs
}

// configYAML 造一份含 n 个组件条目的 brickkit.yaml。
func configYAML(n int) []byte {
	var b strings.Builder
	b.WriteString("project: perf\ndeploy:\n  target: docker\ncomponents:\n")
	for i := 0; i < n; i++ {
		fmt.Fprintf(&b, "  - id: svc/s%03d\n    version: 1.0.0\n", i)
	}
	return []byte(b.String())
}

// ============================================================
// Benchmark：报告数字，不判定对错
// ============================================================

// 36.2 深层依赖树解析。
func BenchmarkResolveDeepChain10(b *testing.B) {
	p, root := deepChain(10)
	r := resolver.New(p)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := r.Resolve(context.Background(), root); err != nil {
			b.Fatal(err)
		}
	}
}

// 更深一档：用来看增长是不是线性的。
func BenchmarkResolveDeepChain100(b *testing.B) {
	p, root := deepChain(100)
	r := resolver.New(p)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := r.Resolve(context.Background(), root); err != nil {
			b.Fatal(err)
		}
	}
}

// 36.3 一个组件 100 个依赖。
func BenchmarkResolveWide100(b *testing.B) {
	p, root := wideGraph(100)
	r := resolver.New(p)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := r.Resolve(context.Background(), root); err != nil {
			b.Fatal(err)
		}
	}
}

// 36.5 brickkit.yaml 解析（100 个组件条目）。
func BenchmarkParseConfig100(b *testing.B) {
	raw := configYAML(100)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := config.ParseConfig(raw, "brickkit.yaml"); err != nil {
			b.Fatal(err)
		}
	}
}

// 36.6 docker-compose.yaml 生成（50 个组件）。
//
// 这条走的是完整链路：解析 → 级联 → 注入 → 生成，
// 因为使用者感知到的'''"生成有多慢"'''就是这一整条。
func BenchmarkGenerateCompose50(b *testing.B) {
	cfg, p, refs := flatProject(50)
	graph, err := resolver.New(p).Resolve(context.Background(), refs...)
	require.NoError(b, err)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		states, err := cascade.Compute(cfg, graph)
		if err != nil {
			b.Fatal(err)
		}
		env, err := inject.Build(cfg, graph, states)
		if err != nil {
			b.Fatal(err)
		}
		if _, err := compose.Generate(cfg, graph, states, env, compose.Options{}); err != nil {
			b.Fatal(err)
		}
	}
}

// 36.7 拓扑排序（50 个组件）。
func BenchmarkTopologicalOrder50(b *testing.B) {
	_, p, refs := flatProject(50)
	graph, err := resolver.New(p).Resolve(context.Background(), refs...)
	require.NoError(b, err)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := resolver.Order(graph); err != nil {
			b.Fatal(err)
		}
	}
}

// ============================================================
// 灾难性回归守卫：阈值极宽，只有算法退化才会红
// ============================================================

// catastropheBudget 是守卫用的时间上限。
//
// 取得**极宽**是有意的：正常机器上这些操作在毫秒级，放到秒级意味着
// 只有量级上的退化（比如把线性算法写成了指数级、在循环里做了 I/O）
// 才会触发。它不是性能指标，是保险丝。
const catastropheBudget = 5 * time.Second

func within(t *testing.T, budget time.Duration, name string, fn func()) {
	t.Helper()

	start := time.Now()
	fn()
	if elapsed := time.Since(start); elapsed > budget {
		t.Fatalf("%s 用了 %v，超过灾难阈值 %v——"+
			"这个阈值放得极宽，触发它通常意味着算法退化了量级，而不是机器慢",
			name, elapsed, budget)
	}
}

// 36.2 十层依赖链不该出现量级退化。
func TestDeepChainDoesNotBlowUp(t *testing.T) {
	p, root := deepChain(10)

	within(t, catastropheBudget, "解析 10 层依赖链", func() {
		_, err := resolver.New(p).Resolve(context.Background(), root)
		require.NoError(t, err)
	})
}

// 36.3 100 个依赖不该出现量级退化。
func TestWideGraphDoesNotBlowUp(t *testing.T) {
	p, root := wideGraph(100)

	within(t, catastropheBudget, "解析 100 个平级依赖", func() {
		g, err := resolver.New(p).Resolve(context.Background(), root)
		require.NoError(t, err)
		require.Len(t, g.Nodes, 101, "根组件 + 100 个依赖")
	})
}

// 36.5 / 36.6 / 36.7 完整链路：100 个组件条目从解析到生成。
//
// 这条最接近使用者的真实感受——他改一行配置按下 `up`，
// 在容器起来之前 CLI 要走完的就是这一整条。
func TestFullPipelineDoesNotBlowUp(t *testing.T) {
	cfg, p, refs := flatProject(100)

	within(t, catastropheBudget, "100 个组件走完解析→级联→注入→生成", func() {
		graph, err := resolver.New(p).Resolve(context.Background(), refs...)
		require.NoError(t, err)

		states, err := cascade.Compute(cfg, graph)
		require.NoError(t, err)

		env, err := inject.Build(cfg, graph, states)
		require.NoError(t, err)

		result, err := compose.Generate(cfg, graph, states, env, compose.Options{})
		require.NoError(t, err)
		require.NotEmpty(t, result.YAML)
	})
}

// 36.10 内存使用（50 个组件）< 100MB。
//
// # 量的是累计分配，不是常驻堆——这是被实测逼出来的选择
//
// 最直觉的写法是"GC 一次、跑完、再 GC 一次，看 HeapAlloc 涨了多少"。
// 试过，测出来是 **-0.03 MB**：负数。
//
// 原因不是哪里写错了，而是 50 个组件的常驻状态（几百 KB）**比两次 GC 之间的
// 噪声还小**，差值里全是噪声。那样的测试会永远绿着——它没有量到任何东西，
// 只是恰好小于 100MB。一个量不到信号的测试比没有测试更糟：它让人以为测过了。
//
// 所以改量 TotalAlloc（整条流水线累计分配了多少）。它的好处是：
//
//	稳定    只增不减，不受 GC 时机影响，跑几次都是同一个数
//	保守    **累计分配是峰值驻留的上界**——就算一个字节都不回收，
//	        占用也不会超过它。它宁可高估，不会漏报
//
// 于是"累计 2.67 MB"这句话直接蕴含了"峰值远低于 100MB"，
// 而且这个结论不依赖 GC 什么时候跑。
func TestMemoryForFiftyComponents(t *testing.T) {
	cfg, p, refs := flatProject(50)

	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)

	graph, err := resolver.New(p).Resolve(context.Background(), refs...)
	require.NoError(t, err)
	states, err := cascade.Compute(cfg, graph)
	require.NoError(t, err)
	env, err := inject.Build(cfg, graph, states)
	require.NoError(t, err)
	result, err := compose.Generate(cfg, graph, states, env, compose.Options{})
	require.NoError(t, err)

	runtime.ReadMemStats(&after)
	runtime.KeepAlive(result) // 别让它在测量前就被回收掉

	const budget = 100 << 20 // 100MB，计划 36.10 定的
	churned := after.TotalAlloc - before.TotalAlloc
	t.Logf("50 个组件走完整条流水线累计分配 %.2f MB（上限 %d MB）——"+
		"这是峰值驻留的上界，实际驻留还要小得多",
		float64(churned)/(1<<20), budget>>20)

	require.Less(t, churned, uint64(budget),
		"36.10：50 个组件的累计分配超了 100MB，峰值驻留有可能也超")
}

// 依赖链变深 10 倍，耗时不该涨到失控。
//
// 这条比绝对阈值更能说明问题：它测的是**增长的形状**，
// 而形状与机器快慢无关。指数级退化在这里会非常显眼。
func TestResolutionGrowthStaysReasonable(t *testing.T) {
	measure := func(depth int) time.Duration {
		p, root := deepChain(depth)
		r := resolver.New(p)
		start := time.Now()
		for i := 0; i < 20; i++ {
			if _, err := r.Resolve(context.Background(), root); err != nil {
				t.Fatal(err)
			}
		}
		return time.Since(start)
	}

	small, large := measure(10), measure(100)

	// 深度涨 10 倍，线性的话耗时约涨 10 倍。放到 100 倍才判失败：
	// 计时本身在毫秒级有很大噪声，卡太紧只会得到一个随机失败的测试。
	const maxRatio = 100
	if ratio := float64(large) / float64(small); ratio > maxRatio {
		t.Fatalf("深度从 10 涨到 100（10 倍），耗时涨了 %.1f 倍（上限 %d 倍）——"+
			"这不像线性算法，检查是不是引入了重复遍历或指数级回溯",
			ratio, maxRatio)
	} else {
		t.Logf("深度 10→100（10 倍），耗时 %v→%v（%.1f 倍）", small, large, ratio)
	}
}
