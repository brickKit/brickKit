package msgid

// internal/resolver/resolver.go
const (
	ResolverNotDeclared               = "resolver.not_declared"
	ResolverHintAddDependent          = "resolver.hint_add_dependent"
	ResolverHintAddMissing            = "resolver.hint_add_missing"
	ResolverStrongDependencyMissing   = "resolver.strong_dependency_missing"
	ResolverLabelMissingDependency    = "resolver.label.missing_dependency"
	ResolverHintCheckSources          = "resolver.hint.check_sources"
	ResolverHintCheckPublished        = "resolver.hint.check_published"
	ResolverHintCheckVersion          = "resolver.hint.check_version"
	ResolverOptionalDependencyMissing = "resolver.optional_dependency_missing"
	ResolverLabelAffectedComponent    = "resolver.label.affected_component"
	ResolverOptionalImpactDetail      = "resolver.optional_impact_detail"
	ResolverOptionalTip               = "resolver.optional_tip"
	ResolverDependencyCycleDetected   = "resolver.dependency_cycle_detected"
	ResolverLabelCyclePath            = "resolver.label.cycle_path"
	ResolverCycleReasonDetail         = "resolver.cycle_reason_detail"
	ResolverHintCheckManifestDeps     = "resolver.hint.check_manifest_deps"
	ResolverHintMakeOptional          = "resolver.hint.make_optional"
	ResolverHintAddDeclared           = "resolver.hint_add_declared"
)

// resolver 补漏：不经过 clierr、直接当数据显示给用户的文案
const ()
