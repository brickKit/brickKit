// 本文件用随机输入验证 mergeResources（资源配额合并）的性质。
//
// # 为什么要有它
//
// 配额优先级是 brickkit.yaml > component.yaml > CLI 默认值，而且**逐字段**合并而不是
// 整块覆盖：使用者常常只想调大内存，不该因此把组件推荐的 CPU 一起丢掉。这条规则的
// 输入是三层 × 每层 requests/limits 两段 × 每段 cpu/memory 两个字段，每个字段又有
// "没写 / 写了"两种状态——组合数远超手写用例，而错一个字段的优先级，效果是某个组件
// 悄悄拿到了别人的配额，且不会有任何报错。
//
// 参照物直接照着规则写成"取第一个非空值"，不复用 mergeSpec 的叠加实现：
//
//	requests  brickkit.yaml 写了就用它，否则 component.yaml 的，否则 CLI 默认值
//	limits    同上，但**没有**默认值那一层——两边都没写就是不设上限，整段是 nil
//	          （渲染器据此决定生成物里要不要出现 limits 这一段）
//
// 另外两条性质：把合并结果当作 override 再合并一次，结果不变（幂等）；以及
// 一层都没写时，requests 恰好是默认值、limits 是 nil。
//
// 本文件是包内测试（package inject）：mergeResources 是私有函数，现有测试都在
// 外部包里够不着它。
package inject

import (
	"fmt"
	"math/rand"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/brickkit/brickkit/internal/manifest"
)

const mergePropertyCases = 5000

var (
	cpuValues    = []string{"", "", "100m", "250m", "1"}
	memoryValues = []string{"", "", "64Mi", "512Mi", "2Gi"}
)

func randomSpec(rng *rand.Rand) *manifest.ResourceSpec {
	if rng.Intn(3) == 0 {
		return nil
	}
	return &manifest.ResourceSpec{
		CPU:    cpuValues[rng.Intn(len(cpuValues))],
		Memory: memoryValues[rng.Intn(len(memoryValues))],
	}
}

func randomResources(rng *rand.Rand) *manifest.Resources {
	if rng.Intn(4) == 0 {
		return nil
	}
	return &manifest.Resources{Requests: randomSpec(rng), Limits: randomSpec(rng)}
}

// fields 取出一段配额里的 cpu / memory，nil 当作两个都没写。
func fields(spec *manifest.ResourceSpec) (cpu, memory string) {
	if spec == nil {
		return "", ""
	}
	return spec.CPU, spec.Memory
}

// firstNonEmpty 是参照物：优先级从高到低，取第一个写了的值。
func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

func describeResources(r *manifest.Resources) string {
	if r == nil {
		return "<nil>"
	}
	show := func(s *manifest.ResourceSpec) string {
		if s == nil {
			return "<nil>"
		}
		return fmt.Sprintf("{cpu:%q memory:%q}", s.CPU, s.Memory)
	}
	return fmt.Sprintf("requests=%s limits=%s", show(r.Requests), show(r.Limits))
}

func TestMergeResourcesPropertiesHoldOnRandomInputs(t *testing.T) {
	var overriddenOneField, limitsAbsent int

	for seed := int64(1); seed <= mergePropertyCases; seed++ {
		rng := rand.New(rand.NewSource(seed))
		recommended, override := randomResources(rng), randomResources(rng)
		context := fmt.Sprintf("种子 %d\n推荐（component.yaml）: %s\n覆盖（brickkit.yaml）: %s",
			seed, describeResources(recommended), describeResources(override))

		got := mergeResources(recommended, override)

		recReqCPU, recReqMem := fields(specOf(recommended, true))
		ovrReqCPU, ovrReqMem := fields(specOf(override, true))
		require.NotNil(t, got.Requests, "requests 有 CLI 默认值兜底，永远不为 nil\n%s", context)
		require.Equal(t, firstNonEmpty(ovrReqCPU, recReqCPU, DefaultRequestCPU), got.Requests.CPU,
			"requests.cpu 的优先级不对\n%s", context)
		require.Equal(t, firstNonEmpty(ovrReqMem, recReqMem, DefaultRequestMemory), got.Requests.Memory,
			"requests.memory 的优先级不对\n%s", context)

		recLimCPU, recLimMem := fields(specOf(recommended, false))
		ovrLimCPU, ovrLimMem := fields(specOf(override, false))
		wantLimCPU := firstNonEmpty(ovrLimCPU, recLimCPU)
		wantLimMem := firstNonEmpty(ovrLimMem, recLimMem)
		if wantLimCPU == "" && wantLimMem == "" {
			limitsAbsent++
			require.Nil(t, got.Limits, "两边都没写 limits，就是不设上限，整段必须是 nil\n%s", context)
		} else {
			require.NotNil(t, got.Limits, "有一层写了 limits，结果里必须有\n%s", context)
			require.Equal(t, wantLimCPU, got.Limits.CPU, "limits.cpu 的优先级不对\n%s", context)
			require.Equal(t, wantLimMem, got.Limits.Memory, "limits.memory 的优先级不对\n%s", context)
		}

		// 只写了一个字段的覆盖，不能把另一个字段的推荐值一起丢掉——逐字段合并的全部意义。
		if (ovrReqCPU == "") != (ovrReqMem == "") {
			overriddenOneField++
		}

		// 幂等：把结果当作 override 再合并一次，结果不变。
		require.Equal(t, got, mergeResources(recommended, &got), "合并结果再合并一次，结果变了\n%s", context)
	}

	// 覆盖率自检：随机输入里要真的有"只覆盖一个字段"和"两边都没写 limits"这两类。
	require.Greater(t, overriddenOneField, mergePropertyCases/10, "只覆盖一个字段的输入太少（%d）", overriddenOneField)
	require.Greater(t, limitsAbsent, mergePropertyCases/10, "没有 limits 的输入太少（%d）", limitsAbsent)
}

// 一层都没写：requests 恰好是 CLI 默认值，limits 是 nil。
func TestMergeResourcesWithNothingWrittenUsesDefaults(t *testing.T) {
	got := mergeResources(nil, nil)
	require.Equal(t, &manifest.ResourceSpec{CPU: DefaultRequestCPU, Memory: DefaultRequestMemory}, got.Requests)
	require.Nil(t, got.Limits)
}
