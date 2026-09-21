package msgid

// internal/cascade/cascade.go
const (
	CascadeStrongDependencyDisabled = "cascade.strong_dependency_disabled"
	CascadePinnedComponentDetail    = "cascade.pinned_component_detail"
	CascadeLabelDependencyChain     = "cascade.label.dependency_chain"
	CascadeLabelDisabledComponent   = "cascade.label.disabled_component"
	CascadeHintRemoveDisabledFlag   = "cascade.hint.remove_disabled_flag"
	CascadeHintRemovePinnedFlag     = "cascade.hint.remove_pinned_flag"
)

// cascade 补漏：不经过 clierr、直接当数据显示给用户的文案
const (
	CascadeReasonDisabled          = "cascade.reason.disabled"
	CascadeReasonBlockedByRequired = "cascade.reason.blocked_by_required"
	CascadeReasonNothingAbove      = "cascade.reason.nothing_above"
	CascadeReasonTopLevel          = "cascade.reason.top_level"
	CascadeReasonPinned            = "cascade.reason.pinned"
	CascadeReasonStarting          = "cascade.reason.starting"
	CascadeReasonNeededBy          = "cascade.reason.needed_by"
)
