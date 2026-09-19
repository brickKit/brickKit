// 本文件用随机图给 cascade.Compute 对拍一份独立的暴力参照物。
//
// # 为什么要有它
//
// 启停判定是全平台规则最密的一处：三态 enabled × 强/弱依赖 × 多个上层共享 × 环。
// cascade_test.go 的示例式测试每条只验证一个人想到的组合，而这个组合空间远超手写
// 用例的数量；这条规则还改过不止一次（措辞改成"跟着上层走"、删掉过"只被弱依赖
// 引用就不自动拉起"），改一次就得把所有组合在脑子里重新推一遍。
//
// 这里换一种验证：随机生成一批小图，每张都用**暴力枚举**算出参照答案，再要求
// Compute 与它一致。参照物不复刻 computeStopped 的传播算法，而是直接枚举全部 2^n
// 个候选集合，找出满足文档逐条规则的**最大**集合——两边只共享"规则"，不共享算法，
// 所以任何一边写错都会被对拍出来。
//
// # 参照物
//
// 一个候选"在跑"集合 S 合规，当且仅当对每个组件 x：
//
//	enabled: false  → x 不在 S 里
//	enabled: true   → x 在 S 里（钉住，不看上层）
//	没写 enabled    → x 在 S 里，当且仅当它的强依赖全在 S 里，
//	                  并且（它是顶层，或至少有一个上层在 S 里）
//
// 满足这些的集合有时不止一个：两个组件互相弱依赖时，"都跑"和"都不跑"各自自洽。
// 文档给的答案是前者（环上没有更上层，互为顶层），所以取**最大**的合规集合——
// 它正是 computeStopped 里"算谁不跑的最小不动点"的补集。
// 钉住的组件若有强依赖不在 S 里，两个意图直接冲突，Compute 必须报
// COMPONENT_DISABLED。
//
// 随机图只生成合法输入：强依赖只指向编号更大的组件（强依赖环是解析期错误，
// 归 resolver 管），弱依赖可以指向任何方向，弱环正是要覆盖的情形。规模压小
// （至多 6 个组件），一次失败的反例天然可读；每个用例用固定种子，失败信息里带着
// 种子，原样可复现。
package cascade_test

import (
	"fmt"
	"math/rand"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/brickkit/brickkit/internal/cascade"
	"github.com/brickkit/brickkit/internal/clierr"
)

const (
	propertyCases = 3000
	maxComponents = 6
)

// randomCase 是一个随机出来的场景。
type randomCase struct {
	specs   []spec
	enabled []string // 每个组件的 enabled："" / "true" / "false"
	listed  []bool   // 没写 enabled 的组件是否出现在 brickkit.yaml 里——不出现也要按"没写"处理
}

func componentID(i int) string { return fmt.Sprintf("c%d/x", i) }

func generate(rng *rand.Rand) randomCase {
	n := 1 + rng.Intn(maxComponents)
	c := randomCase{specs: make([]spec, n), enabled: make([]string, n), listed: make([]bool, n)}
	for i := 0; i < n; i++ {
		c.specs[i].id = componentID(i)
		for j := 0; j < n; j++ {
			if i == j || rng.Float64() >= 0.3 {
				continue
			}
			if j > i && rng.Intn(2) == 0 {
				c.specs[i].requires = append(c.specs[i].requires, componentID(j))
			} else {
				c.specs[i].optional = append(c.specs[i].optional, componentID(j))
			}
		}
		c.enabled[i] = [...]string{"", "", "", "true", "false"}[rng.Intn(5)]
		c.listed[i] = c.enabled[i] != "" || rng.Intn(4) != 0
	}
	return c
}

// permuted 返回同一个场景、组件与配置条目顺序被打乱的版本。
func (c randomCase) permuted(rng *rand.Rand) randomCase {
	order := rng.Perm(len(c.specs))
	out := randomCase{
		specs:   make([]spec, len(c.specs)),
		enabled: make([]string, len(c.specs)),
		listed:  make([]bool, len(c.specs)),
	}
	for to, from := range order {
		out.specs[to], out.enabled[to], out.listed[to] = c.specs[from], c.enabled[from], c.listed[from]
	}
	return out
}

func (c randomCase) config() [][2]string {
	var entries [][2]string
	for i, s := range c.specs {
		if c.listed[i] {
			entries = append(entries, entry(s.id, c.enabled[i]))
		}
	}
	return entries
}

// describe 把场景写成一行，失败信息里用。
func (c randomCase) describe() string {
	var parts []string
	for i, s := range c.specs {
		state := c.enabled[i]
		switch state {
		case "":
			state = "未写"
			if !c.listed[i] {
				state = "未列出"
			}
		case "true":
			state = "enabled: true"
		default:
			state = "enabled: false"
		}
		parts = append(parts, fmt.Sprintf("%s[%s 强:%v 弱:%v]", s.id, state, s.requires, s.optional))
	}
	return strings.Join(parts, "  ")
}

// expectation 是参照物给出的答案。
type expectation struct {
	running  map[string]bool
	conflict bool // 有钉住的组件，它的强依赖不跑
	hasUpper map[string]bool
}

// expected 用暴力枚举给出参照答案。
func (c randomCase) expected(t *testing.T) expectation {
	t.Helper()
	n := len(c.specs)

	position := map[string]int{}
	for i, s := range c.specs {
		position[s.id] = i
	}
	requires := make([][]int, n)
	dependents := make([][]int, n)
	for i, s := range c.specs {
		for _, id := range s.requires {
			requires[i] = append(requires[i], position[id])
			dependents[position[id]] = append(dependents[position[id]], i)
		}
		for _, id := range s.optional {
			dependents[position[id]] = append(dependents[position[id]], i)
		}
	}

	valid := func(set uint) bool {
		in := func(i int) bool { return set&(1<<uint(i)) != 0 }
		for i := 0; i < n; i++ {
			switch c.enabled[i] {
			case "false":
				if in(i) {
					return false
				}
			case "true":
				if !in(i) {
					return false
				}
			default:
				want := true
				for _, d := range requires[i] {
					if !in(d) {
						want = false
					}
				}
				if want && len(dependents[i]) > 0 {
					anyUpper := false
					for _, p := range dependents[i] {
						if in(p) {
							anyUpper = true
						}
					}
					want = anyUpper
				}
				if in(i) != want {
					return false
				}
			}
		}
		return true
	}

	var greatest uint
	found := false
	for set := uint(0); set < 1<<uint(n); set++ {
		if valid(set) {
			greatest |= set
			found = true
		}
	}
	// 合规集合对并封闭（规则单调，最大不动点本身合规）——不成立说明参照物自己坏了。
	require.True(t, found && valid(greatest), "参照物自己坏了：没有合规集合，或最大集合不合规\n%s", c.describe())

	out := expectation{running: map[string]bool{}, hasUpper: map[string]bool{}}
	for i, s := range c.specs {
		out.running[s.id] = greatest&(1<<uint(i)) != 0
		out.hasUpper[s.id] = len(dependents[i]) > 0
	}
	for i := 0; i < n; i++ {
		if c.enabled[i] != "true" {
			continue
		}
		for _, d := range requires[i] {
			if greatest&(1<<uint(d)) == 0 {
				out.conflict = true
			}
		}
	}
	return out
}

// 参照物自己要先对得上文档里写明的规则——否则拿它去对拍实现，"全绿"没有意义。
func TestOracleAgreesWithDocumentedRules(t *testing.T) {
	newCase := func(enabled []string, specs ...spec) randomCase {
		c := randomCase{specs: specs, enabled: enabled, listed: make([]bool, len(specs))}
		for i := range c.listed {
			c.listed[i] = true
		}
		return c
	}

	tests := []struct {
		name     string
		c        randomCase
		running  map[string]bool
		conflict bool
	}{
		{
			name: "两个组件互相弱依赖：环上没有更上层，互为顶层，都跑",
			c: newCase([]string{"", ""},
				spec{id: "a", optional: []string{"b"}}, spec{id: "b", optional: []string{"a"}}),
			running: map[string]bool{"a": true, "b": true},
		},
		{
			name: "环上一个被关掉：另一个的上层全都不跑，跟着不跑",
			c: newCase([]string{"false", ""},
				spec{id: "a", optional: []string{"b"}}, spec{id: "b", optional: []string{"a"}}),
			running: map[string]bool{"a": false, "b": false},
		},
		{
			name: "一条链没写 enabled：顶层跑，下面跟着跑",
			c: newCase([]string{"", "", ""},
				spec{id: "top", requires: []string{"mid"}}, spec{id: "mid", requires: []string{"low"}}, spec{id: "low"}),
			running: map[string]bool{"top": true, "mid": true, "low": true},
		},
		{
			name: "顶层被关掉：整条链跟着不跑",
			c: newCase([]string{"false", "", ""},
				spec{id: "top", requires: []string{"mid"}}, spec{id: "mid", requires: []string{"low"}}, spec{id: "low"}),
			running: map[string]bool{"top": false, "mid": false, "low": false},
		},
		{
			name: "共用的下层：一个上层被关掉，另一个还在跑，它就不能倒",
			c: newCase([]string{"false", "", ""},
				spec{id: "a", requires: []string{"shared"}}, spec{id: "b", requires: []string{"shared"}}, spec{id: "shared"}),
			running: map[string]bool{"a": false, "b": true, "shared": true},
		},
		{
			name: "强依赖不跑，没钉住的上层跟着不跑",
			c: newCase([]string{"", "false"},
				spec{id: "app", requires: []string{"db"}}, spec{id: "db"}),
			running: map[string]bool{"app": false, "db": false},
		},
		{
			name: "钉住的组件强依赖被关掉：两个意图冲突",
			c: newCase([]string{"true", "false"},
				spec{id: "app", requires: []string{"db"}}, spec{id: "db"}),
			running:  map[string]bool{"app": true, "db": false},
			conflict: true,
		},
		{
			name: "弱依赖被关掉是正常状态，不冲突",
			c: newCase([]string{"true", "false"},
				spec{id: "app", optional: []string{"cache"}}, spec{id: "cache"}),
			running: map[string]bool{"app": true, "cache": false},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.c.expected(t)
			require.Equal(t, tt.running, got.running)
			require.Equal(t, tt.conflict, got.conflict)
		})
	}
}

// Compute 的结果必须与暴力参照物一致，并且不受组件与配置条目顺序影响。
func TestComputeMatchesBruteForceOracle(t *testing.T) {
	var conflicts, withSkipped, followsTop int

	for seed := int64(1); seed <= propertyCases; seed++ {
		rng := rand.New(rand.NewSource(seed))
		c := generate(rng)
		context := fmt.Sprintf("种子 %d\n%s", seed, c.describe())
		want := c.expected(t)

		result, err := cascade.Compute(cfgOf(c.config()...), newGraph(t, c.specs...))

		if want.conflict {
			conflicts++
			require.Error(t, err, "钉住的组件强依赖不跑，必须报错\n%s", context)
			structured, ok := clierr.Structured(err)
			require.True(t, ok, "报错必须是结构化错误\n%s", context)
			require.Equal(t, clierr.CodeComponentDisabled, structured.Code, context)
			continue
		}
		require.NoError(t, err, context)

		skipped := false
		for i, s := range c.specs {
			expectedState := cascade.StateSkipped
			switch {
			case c.enabled[i] == "false":
				expectedState = cascade.StateDisabled
			case want.running[s.id]:
				expectedState = cascade.StateRunning
			}
			state, _ := reasonOf(t, result, s.id)
			require.Equal(t, expectedState, state, "%s 的状态不对\n%s", s.id, context)
			require.Equal(t, want.running[s.id], result.IsRunning(ref(s.id)), "%s：IsRunning 与状态不一致\n%s", s.id, context)

			for _, comp := range result.Components {
				if comp.Ref.ID == s.id {
					require.Equal(t, !want.hasUpper[s.id], comp.TopLevel,
						"%s 的 TopLevel 标记不对（顶层就是没有任何组件依赖它）\n%s", s.id, context)
				}
			}
			if expectedState == cascade.StateSkipped {
				skipped = true
			}
			if want.running[s.id] && want.hasUpper[s.id] {
				followsTop++
			}
		}
		if skipped {
			withSkipped++
		}

		// 顺序无关：打乱组件与配置条目的顺序，每个组件的启停不变。
		shuffled := c.permuted(rng)
		again, err := cascade.Compute(cfgOf(shuffled.config()...), newGraph(t, shuffled.specs...))
		require.NoError(t, err, "打乱顺序后不该出错\n%s", context)
		for _, s := range c.specs {
			require.Equal(t, result.IsRunning(ref(s.id)), again.IsRunning(ref(s.id)),
				"%s：打乱组件顺序后启停变了\n%s", s.id, context)
		}
	}

	// 覆盖率自检：随机场景要真的覆盖到各类情形，而不是全落在"什么都在跑"这一种上。
	// 否则"对拍全绿"只说明简单情形没写错。
	require.Greater(t, conflicts, propertyCases/50, "冲突场景太少（%d），生成器没覆盖到", conflicts)
	require.Greater(t, withSkipped, propertyCases/20, "有组件被跳过的场景太少（%d），生成器没覆盖到", withSkipped)
	require.Greater(t, followsTop, propertyCases/20, "下层跟着上层跑的场景太少（%d），生成器没覆盖到", followsTop)
}
