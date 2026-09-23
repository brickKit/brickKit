package override

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/brickkit/brickkit/internal/config"
)

func dockerConfig(components ...config.Component) *config.Config {
	return &config.Config{
		Project:    "p",
		Deploy:     config.Deploy{Target: config.TargetDocker},
		Components: components,
	}
}

func k8sConfig(components ...config.Component) *config.Config {
	return &config.Config{
		Project:    "p",
		Deploy:     config.Deploy{Target: config.TargetK8s},
		Components: components,
	}
}

func TestCheckAgainstNilOverrideIsAlwaysValid(t *testing.T) {
	assert.NoError(t, CheckAgainst(dockerConfig(), nil))
}

func TestCheckAgainstRejectsDanglingComponentEntry(t *testing.T) {
	cfg := dockerConfig(config.Component{ID: "demo/hello", Version: "1.0.0"})
	ov := &Override{Components: []ComponentOverride{{ID: "demo/gone"}}}

	err := CheckAgainst(cfg, ov)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "demo/gone")
}

func TestCheckAgainstRejectsDanglingNestedMember(t *testing.T) {
	cfg := dockerConfig(config.Component{ID: "erp/backend", Version: "1.0.0"})
	ov := &Override{Components: []ComponentOverride{
		{ID: "erp/backend", Members: []MemberOverride{{ID: "demo/gone"}}},
	}}

	err := CheckAgainst(cfg, ov)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "demo/gone")
}

func TestCheckAgainstAllowsDowngradeFromK8sToDocker(t *testing.T) {
	cfg := k8sConfig(config.Component{ID: "demo/hello", Version: "1.0.0"})
	ov := &Override{Target: config.TargetDocker, TargetBaseline: config.TargetK8s}

	assert.NoError(t, CheckAgainst(cfg, ov))
}

func TestCheckAgainstAllowsDowngradeFromK8sToPodman(t *testing.T) {
	cfg := k8sConfig(config.Component{ID: "demo/hello", Version: "1.0.0"})
	ov := &Override{Target: config.TargetPodman, TargetBaseline: config.TargetK8s}

	assert.NoError(t, CheckAgainst(cfg, ov))
}

func TestCheckAgainstAllowsDockerToPodmanEitherDirection(t *testing.T) {
	assert.NoError(t, CheckAgainst(dockerConfig(), &Override{Target: config.TargetPodman}))

	podmanCfg := &config.Config{Project: "p", Deploy: config.Deploy{Target: config.TargetPodman}}
	assert.NoError(t, CheckAgainst(podmanCfg, &Override{Target: config.TargetDocker}))
}

func TestCheckAgainstRejectsUpgradeFromDockerToK8s(t *testing.T) {
	cfg := dockerConfig(config.Component{ID: "demo/hello", Version: "1.0.0"})
	ov := &Override{Target: config.TargetK8s}

	err := CheckAgainst(cfg, ov)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "docker")
	assert.Contains(t, err.Error(), "k8s")
}

func TestCheckAgainstRejectsUpgradeFromPodmanToK8s(t *testing.T) {
	cfg := &config.Config{Project: "p", Deploy: config.Deploy{Target: config.TargetPodman}}
	ov := &Override{Target: config.TargetK8s}

	err := CheckAgainst(cfg, ov)

	require.Error(t, err)
}

func TestCheckAgainstRejectsDebugModeWhenEffectiveTargetIsK8s(t *testing.T) {
	cfg := k8sConfig(config.Component{ID: "demo/hello", Version: "1.0.0"})
	ov := &Override{Components: []ComponentOverride{{ID: "demo/hello", Mode: config.ModeDebug}}}

	err := CheckAgainst(cfg, ov)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "demo/hello")
}

func TestCheckAgainstRejectsLocalModeWhenEffectiveTargetIsK8s(t *testing.T) {
	cfg := k8sConfig(config.Component{ID: "demo/hello", Version: "1.0.0"})
	ov := &Override{Components: []ComponentOverride{{ID: "demo/hello", Mode: config.ModeLocal}}}

	err := CheckAgainst(cfg, ov)

	require.Error(t, err)
}

func TestCheckAgainstAllowsDebugModeWhenTargetDowngradedAwayFromK8s(t *testing.T) {
	// brickkit.yaml 自身是 k8s，但这份覆盖同时把 target 降级到 docker——
	// 那么"k8s 禁止 debug/local"这条规则不该再拦它：生效的目标已经不是 k8s 了。
	cfg := k8sConfig(config.Component{ID: "demo/hello", Version: "1.0.0"})
	ov := &Override{
		Target: config.TargetDocker, TargetBaseline: config.TargetK8s,
		Components: []ComponentOverride{{ID: "demo/hello", Mode: config.ModeDebug}},
	}

	assert.NoError(t, CheckAgainst(cfg, ov))
}

func TestCheckAgainstAllowsDebugModeUnderDockerTarget(t *testing.T) {
	cfg := dockerConfig(config.Component{ID: "demo/hello", Version: "1.0.0"})
	ov := &Override{Components: []ComponentOverride{{ID: "demo/hello", Mode: config.ModeDebug}}}

	assert.NoError(t, CheckAgainst(cfg, ov))
}

func TestCheckAgainstRejectsDebugModeOnNestedMemberWhenEffectiveTargetIsK8s(t *testing.T) {
	cfg := k8sConfig(config.Component{ID: "erp/backend", Version: "1.0.0"})
	ov := &Override{Components: []ComponentOverride{
		{ID: "erp/backend", Members: []MemberOverride{{ID: "people/basic", Mode: config.ModeDebug}}},
	}}

	err := CheckAgainst(cfg, ov)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "people/basic")
}

func TestDriftNilOverrideIsEmpty(t *testing.T) {
	assert.Empty(t, Drift(dockerConfig(), nil))
}

func TestDriftDetectsTargetBaselineMismatch(t *testing.T) {
	cfg := k8sConfig(config.Component{ID: "demo/hello", Version: "1.0.0"})
	ov := &Override{Target: config.TargetDocker, TargetBaseline: config.TargetDocker} // brickkit.yaml 已经从 docker 变成了 k8s

	notes := Drift(cfg, ov)

	require.Len(t, notes, 1)
	assert.Equal(t, "target", notes[0].Field)
}

func TestDriftNoNoteWhenTargetBaselineMatchesCurrent(t *testing.T) {
	cfg := k8sConfig(config.Component{ID: "demo/hello", Version: "1.0.0"})
	ov := &Override{Target: config.TargetDocker, TargetBaseline: config.TargetK8s}

	assert.Empty(t, Drift(cfg, ov))
}

func TestDriftNoNoteWhenTargetOverrideNotSet(t *testing.T) {
	// 没写 target 就没有 targetBaseline，也就没有可比的东西。
	cfg := k8sConfig(config.Component{ID: "demo/hello", Version: "1.0.0"})
	ov := &Override{Components: []ComponentOverride{{ID: "demo/hello", Mode: config.ModeDebug}}}

	assert.Empty(t, Drift(cfg, ov))
}

func TestDriftDetectsComponentBaselineMismatch(t *testing.T) {
	cfg := dockerConfig(config.Component{ID: "payment/gateway", Version: "1.0.0", Mode: config.ModeLocal})
	ov := &Override{Components: []ComponentOverride{
		{ID: "payment/gateway", Mode: config.ModeDebug, Baseline: config.ModeEnabled},
	}}

	notes := Drift(cfg, ov)

	require.Len(t, notes, 1)
	assert.Contains(t, notes[0].Field, "payment/gateway")
}

func TestDriftNoNoteWhenComponentBaselineMatchesCurrent(t *testing.T) {
	cfg := dockerConfig(config.Component{ID: "payment/gateway", Version: "1.0.0", Mode: config.ModeLocal})
	ov := &Override{Components: []ComponentOverride{
		{ID: "payment/gateway", Mode: config.ModeDebug, Baseline: config.ModeLocal},
	}}

	assert.Empty(t, Drift(cfg, ov))
}

func TestDriftNoNoteWhenComponentBaselineNotRecorded(t *testing.T) {
	// baseline 只在 brickkit.yaml 当时已经显式写了取值时才落笔（设计书 §7）——
	// 没有 baseline 就没有"漂移"这回事，不是"缺失的旧值等于空字符串"。
	cfg := dockerConfig(config.Component{ID: "payment/gateway", Version: "1.0.0"})
	ov := &Override{Components: []ComponentOverride{{ID: "payment/gateway", Mode: config.ModeDebug}}}

	assert.Empty(t, Drift(cfg, ov))
}

func TestDriftDetectsNestedMemberBaselineMismatch(t *testing.T) {
	cfg := dockerConfig(config.Component{ID: "erp/backend", Version: "1.0.0"},
		config.Component{ID: "people/basic", Version: "1.0.0", Mode: config.ModeLocal})
	ov := &Override{Components: []ComponentOverride{
		{ID: "erp/backend", Members: []MemberOverride{
			{ID: "people/basic", Mode: config.ModeDebug, Baseline: config.ModeEnabled},
		}},
	}}

	notes := Drift(cfg, ov)

	require.Len(t, notes, 1)
	assert.Contains(t, notes[0].Field, "people/basic")
}
