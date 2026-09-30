package cascade_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/brickkit/brickkit/internal/cascade"
	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/i18n"
	"github.com/brickkit/brickkit/internal/msgid"
	"github.com/brickkit/brickkit/internal/project"
)

func focusOn(p *project.Project, id string) *project.Project {
	p.Deploy.Focus = id
	return p
}

// 焦点：只有焦点组件和它需要的组件跑（强弱依赖都算），别的顶层不跑。
func TestFocusStartsOnlyTheFocusAndWhatItNeeds(t *testing.T) {
	graph := newGraph(t,
		spec{id: "shop/web", requires: []string{"erp/api"}},
		spec{id: "erp/api", requires: []string{"erp/db"}, optional: []string{"infra/cache"}},
		spec{id: "erp/db"},
		spec{id: "infra/cache"},
		spec{id: "shop/report"},
	)
	p := focusOn(cfgOf(entry("shop/web", ""), entry("erp/api", ""), entry("erp/db", ""),
		entry("infra/cache", ""), entry("shop/report", "")), "erp/api")

	result, err := cascade.Compute(p, graph)
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"erp/api", "erp/db", "infra/cache"}, runningIDs(result))

	_, why := reasonOf(t, result, "erp/api")
	assert.Equal(t, i18n.T(msgid.CascadeReasonFocus), why)
	_, why = reasonOf(t, result, "erp/db")
	assert.Equal(t, i18n.T(msgid.CascadeReasonNeededBy, "erp/api"), why)
	for _, id := range []string{"shop/web", "shop/report"} {
		state, why := reasonOf(t, result, id)
		assert.Equal(t, cascade.StateSkipped, state, id)
		assert.Equal(t, i18n.T(msgid.CascadeReasonOutsideFocus), why, id)
	}
}

// 显式钉住的组件照样跑：mode: enabled 是写下来的意图。
func TestFocusKeepsExplicitPins(t *testing.T) {
	graph := newGraph(t, spec{id: "erp/api"}, spec{id: "shop/report"})
	p := focusOn(cfgOf(entry("erp/api", ""), entry("shop/report", "enabled")), "erp/api")

	result, err := cascade.Compute(p, graph)
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"erp/api", "shop/report"}, runningIDs(result))
}

// 与焦点无关的弱依赖环不跑：环上谁都不是顶层，"顶层才跑"那条规则停不下它们（Review Focus 4）。
func TestFocusDoesNotStartAnUnrelatedCycle(t *testing.T) {
	graph := newGraph(t,
		spec{id: "x/a", optional: []string{"x/b"}},
		spec{id: "x/b", optional: []string{"x/a"}},
		spec{id: "erp/api"},
	)
	p := focusOn(cfgOf(entry("x/a", ""), entry("x/b", ""), entry("erp/api", "")), "erp/api")

	result, err := cascade.Compute(p, graph)
	require.NoError(t, err)
	assert.Equal(t, []string{"erp/api"}, runningIDs(result))
}

// 焦点需要的强依赖被关掉：两个意图矛盾，报 COMPONENT_DISABLED（与钉住的组件同一条错误）。
func TestFocusWithADisabledRequirementIsAnError(t *testing.T) {
	graph := newGraph(t, spec{id: "erp/api", requires: []string{"erp/db"}}, spec{id: "erp/db"})
	p := focusOn(cfgOf(entry("erp/api", ""), entry("erp/db", "disable")), "erp/api")

	_, err := cascade.Compute(p, graph)
	require.Error(t, err)
	assert.Equal(t, clierr.CodeComponentDisabled, clierr.As(err).Code)
}

// 没写焦点时一切照旧：同一张图，顶层照跑。
func TestNoFocusKeepsTheTopLevelRule(t *testing.T) {
	graph := newGraph(t, spec{id: "erp/api"}, spec{id: "shop/report"})
	result, err := cascade.Compute(cfgOf(entry("erp/api", ""), entry("shop/report", "")), graph)
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"erp/api", "shop/report"}, runningIDs(result))
}

// 焦点走得到、却因为上层都不跑而停下的组件：原因是"上层都不启动"，不是"焦点之外"。
func TestAReachableComponentStoppedByItsParentsIsNotOutsideTheFocus(t *testing.T) {
	graph := newGraph(t,
		spec{id: "shop/web", optional: []string{"erp/api"}},
		spec{id: "erp/api", requires: []string{"erp/db"}, optional: []string{"infra/cache"}},
		spec{id: "erp/db"},
		spec{id: "infra/cache"},
	)
	p := focusOn(cfgOf(entry("shop/web", ""), entry("erp/api", ""), entry("erp/db", "disable"),
		entry("infra/cache", "")), "shop/web")

	result, err := cascade.Compute(p, graph)
	require.NoError(t, err)
	assert.Equal(t, []string{"shop/web"}, runningIDs(result))
	_, why := reasonOf(t, result, "infra/cache")
	assert.Equal(t, i18n.T(msgid.CascadeReasonNothingAbove), why)
}
